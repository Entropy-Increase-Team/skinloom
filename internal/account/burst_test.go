package account

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

func TestLimiterTakeRelease(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	for i := range 2 {
		if _, blocked := l.Take("k"); blocked {
			t.Fatalf("take %d blocked", i)
		}
	}
	if wait, blocked := l.Take("k"); !blocked || wait <= 0 {
		t.Fatalf("third take: %v %v", wait, blocked)
	}
	l.Release("k")
	if _, blocked := l.Take("k"); blocked {
		t.Fatal("a released attempt frees its slot")
	}
	var ok atomic.Int64
	var wg sync.WaitGroup
	m := NewLimiter(10, time.Minute)
	for range 200 {
		wg.Go(func() {
			if _, blocked := m.Take("burst"); !blocked {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 10 {
		t.Fatalf("%d of 200 concurrent takes passed, want 10", ok.Load())
	}
}

// A burst of wrong passwords sent at once gets the same number of guesses as one sent
// in sequence. Checking the limit and counting the failure after the password hash
// let every request of a burst through (2026-10-30 review: 200 of 200).
func TestLoginBurst(t *testing.T) {
	s := newService(t, "sqlite", "t.db")
	ctx := context.Background()
	for _, check := range []struct {
		name  string
		login func() error
	}{
		{"web", func() error {
			_, _, err := s.Login(ctx, LoginInput{Email: "victim@example.com", Password: "guess-123456"}, LoginPolicy{}, meta)
			return err
		}},
		{"launcher", func() error {
			_, err := s.VerifyCredentials(ctx, "victim2@example.com", "guess-123456", meta)
			return err
		}},
	} {
		var checked, limited atomic.Int64
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				var rl *RateLimitError
				switch err := check.login(); {
				case errors.Is(err, ErrInvalidCredentials):
					checked.Add(1)
				case errors.As(err, &rl):
					limited.Add(1)
				default:
					t.Errorf("%s: %v", check.name, err)
				}
			})
		}
		wg.Wait()
		if checked.Load() != 10 || limited.Load() != 90 {
			t.Fatalf("%s: %d passwords checked, %d refused; want 10 and 90", check.name, checked.Load(), limited.Load())
		}
	}
}

// A successful login does not count against the address, and forgives this client's
// failures on the account (as before counting moved ahead of the check).
func TestLoginSuccessIsNotCounted(t *testing.T) { forEachDB(t, testLoginSuccessIsNotCounted) }

func testLoginSuccessIsNotCounted(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	for range 9 {
		if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1"}, LoginPolicy{}, meta); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	for range 3 { // 9 failures + 3 successes would block the 10th attempt if successes counted
		if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1"}, LoginPolicy{}, meta); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.ipLimit.Count(ipKey(meta.IP)); n != 9 {
		t.Fatalf("address count %d, want the 9 failures", n)
	}
	if n := s.clientAccountLimit.Count(ipKey(meta.IP) + "|admin@example.com"); n != 0 {
		t.Fatalf("this client's failures on the account are forgiven: %d", n)
	}
}

// Purging runs the before-delete hook in the deletion's transaction: when the delete
// matches nothing (the user signed in again, restoring the account, after the list
// was read), the hook's work rolls back with it. Run on its own, the hook had already
// deleted the user's private textures.
func TestPurgeRollsBackRestored(t *testing.T) { forEachDB(t, testPurgeRollsBackRestored) }

func testPurgeRollsBackRestored(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	v, _ := newUser(t, s, "back@example.com")
	s.db.User.UpdateOneID(v.User.ID).SetStatus(user.StatusDeleted).SetDeletedAt(time.Now().Add(-DeleteGrace - time.Hour)).ExecX(ctx)
	s.SetBeforeDelete(func(ctx context.Context, tx *ent.Client, id int64) error {
		if err := audit(ctx, tx, nil, "probe.hook", "user", "", "", nil); err != nil { // the hook's work
			return err
		}
		// The restore, seen by the delete that follows (in this transaction, so that
		// SQLite's single writer does not wait for itself).
		return tx.User.UpdateOneID(id).SetStatus(user.StatusActive).ClearDeletedAt().Exec(ctx)
	})
	n, err := s.PurgeDeleted(ctx)
	if err != nil || n != 0 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if s.db.AuditLog.Query().Where(auditlog.Action("probe.hook")).ExistX(ctx) {
		t.Fatal("the hook's work must roll back with a delete that matched nothing")
	}
}
