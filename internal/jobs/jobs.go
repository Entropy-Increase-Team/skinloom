// Package jobs runs background work stored in the jobs table. Producers enqueue a job
// in the same transaction as the change that needs it; every node runs a worker, and
// a short lease makes sure each job runs on one node at a time.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
)

// Handler runs one job. Returning an error retries it later; Permanent(err) gives up.
type Handler func(ctx context.Context, payload json.RawMessage) error

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error that retrying cannot fix (e.g. a malformed payload).
func Permanent(err error) error { return permanentError{err} }

// Retry delays after the 1st, 2nd, ... failure; after the last one the job fails.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

const (
	lease     = 5 * time.Minute // a worker that dies mid-job frees it after this
	batchSize = 10
	keepDone  = 7 * 24 * time.Hour
)

// Enqueue adds a job. Pass a transaction's client to make it part of that transaction.
func Enqueue(ctx context.Context, c *ent.Client, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.Job.Create().SetKind(kind).SetPayload(string(b)).Exec(ctx)
}

// Queue dispatches due jobs to registered handlers.
type Queue struct {
	db      *ent.Client
	log     *slog.Logger
	now     func() time.Time
	timeout time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler
	wake     chan struct{}
}

// New creates a queue; Register handlers, then Run it.
func New(db *ent.Client, log *slog.Logger) *Queue {
	return &Queue{db: db, log: log, now: time.Now, timeout: time.Minute,
		handlers: map[string]Handler{}, wake: make(chan struct{}, 1)}
}

// Register sets the handler for a kind of job.
func (q *Queue) Register(kind string, h Handler) {
	q.mu.Lock()
	q.handlers[kind] = h
	q.mu.Unlock()
}

// Kick asks the worker to look for work now instead of at the next poll (call it
// after committing a transaction that enqueued jobs, so mail goes out at once).
func (q *Queue) Kick() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Run works until ctx ends, polling every interval (and whenever kicked).
func (q *Queue) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	lastCleanup := time.Time{}
	for {
		for {
			n, err := q.RunDue(ctx)
			if err != nil && ctx.Err() == nil {
				q.log.Warn("job queue", "err", err)
			}
			if n < batchSize || ctx.Err() != nil {
				break // drained; a full batch means more may be waiting
			}
		}
		if now := q.now(); now.Sub(lastCleanup) > time.Hour {
			lastCleanup = now
			q.cleanup(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-q.wake:
		}
	}
}

// RunDue runs the jobs that are due now and returns how many it claimed.
func (q *Queue) RunDue(ctx context.Context) (int, error) {
	now := q.now()
	due, err := q.db.Job.Query().
		Where(job.StatusEQ(job.StatusPending), job.RunAtLTE(now),
			job.Or(job.LockedUntilIsNil(), job.LockedUntilLT(now))).
		Order(ent.Asc(job.FieldRunAt)).Limit(batchSize).All(ctx)
	if err != nil {
		return 0, err
	}
	claimed := 0
	for _, j := range due {
		// Claim with a conditional update: only one node wins the row. The lease runs
		// from the claim, not from the start of the batch: the jobs before this one
		// may have taken minutes (slow SMTP), and a lease that is already over would
		// let another node claim and send the same mail.
		now := q.now()
		n, err := q.db.Job.Update().
			Where(job.ID(j.ID), job.StatusEQ(job.StatusPending),
				job.Or(job.LockedUntilIsNil(), job.LockedUntilLT(now))).
			SetLockedUntil(now.Add(lease)).AddAttempts(1).Save(ctx)
		if err != nil {
			return claimed, err
		}
		if n == 0 {
			continue
		}
		claimed++
		q.run(ctx, j)
	}
	return claimed, nil
}

func (q *Queue) run(ctx context.Context, j *ent.Job) {
	q.mu.RLock()
	h := q.handlers[j.Kind]
	q.mu.RUnlock()
	attempt := j.Attempts + 1
	var err error
	if h == nil {
		err = Permanent(fmt.Errorf("no handler for job kind %q", j.Kind))
	} else {
		jctx, cancel := context.WithTimeout(ctx, q.timeout)
		err = h(jctx, json.RawMessage(j.Payload))
		cancel()
	}
	now := q.now()
	upd := q.db.Job.UpdateOneID(j.ID).ClearLockedUntil()
	switch {
	case err == nil:
		upd.SetStatus(job.StatusDone).SetFinishedAt(now).SetPayload("{}").SetLastError("")
	case errors.As(err, new(permanentError)) || attempt > len(backoff):
		q.log.Warn("job failed for good", "kind", j.Kind, "id", j.ID, "attempts", attempt, "err", err)
		upd.SetStatus(job.StatusFailed).SetFinishedAt(now).SetPayload("{}").SetLastError(truncate(err.Error(), 1024))
	default:
		q.log.Info("job failed, will retry", "kind", j.Kind, "id", j.ID, "attempt", attempt, "err", err)
		upd.SetRunAt(now.Add(backoff[attempt-1])).SetLastError(truncate(err.Error(), 1024))
	}
	// The job's own context may be done (shutdown); record the outcome regardless.
	if err := upd.Exec(context.WithoutCancel(ctx)); err != nil {
		q.log.Warn("record job result", "id", j.ID, "err", err)
	}
}

// cleanup deletes finished jobs after a week.
func (q *Queue) cleanup(ctx context.Context) {
	n, err := q.db.Job.Delete().
		Where(job.StatusNEQ(job.StatusPending), job.FinishedAtLT(q.now().Add(-keepDone))).Exec(ctx)
	if err != nil {
		q.log.Warn("clean up old jobs", "err", err)
	} else if n > 0 {
		q.log.Debug("cleaned up old jobs", "count", n)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
