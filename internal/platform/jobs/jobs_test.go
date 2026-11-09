package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

func forEachDB(t *testing.T, fn func(t *testing.T, c *ent.Client)) {
	targets := []struct{ driver, dsn string }{{"sqlite", "t.db"}}
	if dsn := os.Getenv("SKINLOOM_TEST_MYSQL"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"mysql", dsn})
	}
	if dsn := os.Getenv("SKINLOOM_TEST_POSTGRES"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"postgres", dsn})
	}
	for _, tg := range targets {
		t.Run(tg.driver, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			database, err := db.Open(ctx, db.Options{Driver: tg.driver, DSN: tg.dsn, DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			if err := migrate.Up(ctx, database, dir, quiet); err != nil {
				t.Fatal(err)
			}
			if tg.driver != "sqlite" {
				if _, err := database.SQL.Exec("DELETE FROM jobs"); err != nil {
					t.Fatal(err)
				}
			}
			fn(t, database.Ent)
		})
	}
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// clock is a settable time source shared by a queue under test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newQueue(c *ent.Client, clk *clock) *Queue {
	q := New(c, quiet)
	q.now = clk.now
	return q
}

func TestRunAndRetry(t *testing.T) { forEachDB(t, testRunAndRetry) }

func testRunAndRetry(t *testing.T, c *ent.Client) {
	ctx := context.Background()
	clk := &clock{t: time.Now().Add(time.Second)} // jobs enqueued "now" are due
	q := newQueue(c, clk)
	var got []string
	q.Register("ok", func(_ context.Context, p json.RawMessage) error {
		var v struct{ To string }
		_ = json.Unmarshal(p, &v)
		got = append(got, v.To)
		return nil
	})
	if err := Enqueue(ctx, c, "ok", map[string]string{"To": "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	if n, err := q.RunDue(ctx); n != 1 || err != nil || len(got) != 1 || got[0] != "a@example.com" {
		t.Fatalf("RunDue = %d, %v; handled %v", n, err, got)
	}
	j := c.Job.Query().OnlyX(ctx)
	if j.Status != job.StatusDone || j.Payload != "{}" || j.FinishedAt == nil || j.Attempts != 1 {
		t.Fatalf("finished job %+v: want done with the payload wiped", j)
	}

	calls := 0
	q.Register("flaky", func(context.Context, json.RawMessage) error { calls++; return errors.New("smtp down") })
	_ = Enqueue(ctx, c, "flaky", map[string]string{"link": "secret"})
	for i := range len(backoff) + 1 {
		if n, _ := q.RunDue(ctx); n != 1 {
			t.Fatalf("attempt %d did not run", i+1)
		}
		if n, _ := q.RunDue(ctx); n != 0 {
			t.Fatal("a failed job must wait for its backoff")
		}
		if i < len(backoff) {
			clk.add(backoff[i] + time.Second)
		}
	}
	f := c.Job.Query().Where(job.Kind("flaky")).OnlyX(ctx)
	if calls != len(backoff)+1 || f.Status != job.StatusFailed || f.Payload != "{}" || f.LastError != "smtp down" {
		t.Fatalf("after %d calls: %+v, want failed with the payload wiped", calls, f)
	}

	q.Register("bad", func(context.Context, json.RawMessage) error { return Permanent(errors.New("malformed")) })
	_ = Enqueue(ctx, c, "bad", nil)
	_ = Enqueue(ctx, c, "unknown", nil)
	_, _ = q.RunDue(ctx)
	if n := c.Job.Query().Where(job.StatusEQ(job.StatusFailed)).CountX(ctx); n != 3 {
		t.Fatalf("permanent errors and unknown kinds fail at once: %d failed", n)
	}

	clk.add(keepDone + time.Hour)
	q.cleanup(ctx)
	if n := c.Job.Query().CountX(ctx); n != 0 {
		t.Fatalf("%d finished jobs left after a week", n)
	}
}

// The outbox guarantee: a job enqueued in a rolled-back transaction never runs.
func TestEnqueueInTransaction(t *testing.T) { forEachDB(t, testEnqueueInTransaction) }

func testEnqueueInTransaction(t *testing.T, c *ent.Client) {
	ctx := context.Background()
	tx, err := c.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(ctx, tx.Client(), "mail", nil); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if n := c.Job.Query().CountX(ctx); n != 0 {
		t.Fatalf("%d jobs survived a rollback", n)
	}
}

// Two nodes polling at once: every job runs exactly once.
func TestOneNodePerJob(t *testing.T) { forEachDB(t, testOneNodePerJob) }

func testOneNodePerJob(t *testing.T, c *ent.Client) {
	ctx := context.Background()
	clk := &clock{t: time.Now().Add(time.Second)} // jobs enqueued "now" are due
	var runs atomic.Int32
	nodes := []*Queue{newQueue(c, clk), newQueue(c, clk)}
	for _, q := range nodes {
		q.Register("count", func(context.Context, json.RawMessage) error { runs.Add(1); return nil })
	}
	for range 20 {
		_ = Enqueue(ctx, c, "count", nil)
	}
	var wg sync.WaitGroup
	for _, q := range nodes {
		wg.Go(func() {
			for {
				if n, _ := q.RunDue(ctx); n == 0 {
					return
				}
			}
		})
	}
	wg.Wait()
	if runs.Load() != 20 {
		t.Fatalf("20 jobs ran %d times", runs.Load())
	}
}

// Slow jobs early in a batch must not leave later ones claimed with a lease that is
// already over (another node could then run them too).
func TestLeaseFromClaim(t *testing.T) { forEachDB(t, testLeaseFromClaim) }

func testLeaseFromClaim(t *testing.T, c *ent.Client) {
	ctx := context.Background()
	clk := &clock{t: time.Now().Add(time.Second)} // jobs enqueued "now" are due
	q := newQueue(c, clk)
	var expired []time.Time
	q.Register("slow", func(context.Context, json.RawMessage) error {
		running := c.Job.Query().Where(job.LockedUntilNotNil()).OnlyX(ctx)
		if !running.LockedUntil.After(clk.now()) {
			expired = append(expired, *running.LockedUntil)
		}
		clk.add(2 * time.Minute) // e.g. an SMTP server that answers slowly
		return nil
	})
	for range 4 {
		_ = Enqueue(ctx, c, "slow", nil)
	}
	if n, err := q.RunDue(ctx); n != 4 || err != nil {
		t.Fatalf("RunDue = %d, %v", n, err)
	}
	if len(expired) > 0 {
		t.Fatalf("%d jobs ran with an expired lease", len(expired))
	}
}

// A worker that died mid-job holds the lease; the job waits for it to expire.
func TestLeaseExpires(t *testing.T) { forEachDB(t, testLeaseExpires) }

func testLeaseExpires(t *testing.T, c *ent.Client) {
	ctx := context.Background()
	clk := &clock{t: time.Now().Add(time.Second)} // jobs enqueued "now" are due
	q := newQueue(c, clk)
	q.Register("x", func(context.Context, json.RawMessage) error { return nil })
	_ = Enqueue(ctx, c, "x", nil)
	c.Job.Update().SetLockedUntil(clk.now().Add(lease)).ExecX(ctx) // claimed by a dead worker
	if n, _ := q.RunDue(ctx); n != 0 {
		t.Fatal("a leased job must not run again before the lease ends")
	}
	clk.add(lease + time.Second)
	if n, _ := q.RunDue(ctx); n != 1 {
		t.Fatal("an expired lease frees the job")
	}
}
