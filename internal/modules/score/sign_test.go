package score

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func TestSignIn(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		c := d.Ent
		u := newUser(t, c, 0)
		gap := SignPolicy{Min: 10, Max: 100, Gap: 24 * time.Hour, Location: time.UTC}
		t0 := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

		gained, balance, next, err := SignIn(ctx, c, u.ID, gap, t0)
		if err != nil || gained < 10 || gained > 100 || balance != gained || !next.Equal(t0.Add(24*time.Hour)) {
			t.Fatalf("SignIn = %d, %d, %v, %v", gained, balance, next, err)
		}
		var early *TooEarlyError
		if _, _, _, err := SignIn(ctx, c, u.ID, gap, t0.Add(23*time.Hour)); !errors.As(err, &early) || !early.Next.Equal(t0.Add(24*time.Hour)) {
			t.Fatalf("before the gap: %v", err)
		}
		if _, _, _, err := SignIn(ctx, c, u.ID, gap, t0.Add(24*time.Hour)); err != nil {
			t.Fatalf("after the gap: %v", err)
		}
		if n := c.ScoreEntry.Query().Where(scoreentry.Reason(ReasonSignIn)).CountX(ctx); n != 2 {
			t.Fatalf("ledger entries %d", n)
		}

		// Daily: a new day starts at midnight in the site's time zone.
		shanghai, _ := time.LoadLocation("Asia/Shanghai")
		daily := SignPolicy{Min: 5, Max: 5, Daily: true, Location: shanghai}
		v := newUser(t, c, 0)
		late := time.Date(2026, 10, 24, 23, 50, 0, 0, shanghai)
		if _, _, next, err := SignIn(ctx, c, v.ID, daily, late); err != nil || !next.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, shanghai)) {
			t.Fatalf("daily: %v, %v", next, err)
		}
		if _, _, _, err := SignIn(ctx, c, v.ID, daily, late.Add(5*time.Minute)); !errors.As(err, &early) {
			t.Fatalf("same day: %v", err)
		}
		if gained, _, _, err := SignIn(ctx, c, v.ID, daily, late.Add(15*time.Minute)); err != nil || gained != 5 {
			t.Fatalf("after midnight (still the 24th in UTC): %d, %v", gained, err)
		}
	})
}

// Regression for Blessing Skin C03: concurrent check-ins earned more than once.
func TestSignInConcurrent(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		u := newUser(t, d.Ent, 0)
		p := SignPolicy{Min: 10, Max: 10, Gap: time.Hour, Location: time.UTC}
		now := time.Now()
		var wg sync.WaitGroup
		var mu sync.Mutex
		won := 0
		for range 10 {
			wg.Go(func() {
				_, _, _, err := SignIn(ctx, d.Ent, u.ID, p, now)
				mu.Lock()
				defer mu.Unlock()
				var early *TooEarlyError
				switch {
				case err == nil:
					won++
				case errors.As(err, &early):
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if got := d.Ent.User.GetX(ctx, u.ID).Score; won != 1 || got != 10 {
			t.Fatalf("%d check-ins succeeded, score %d", won, got)
		}
	})
}

// The site's time zone need not be the server's. SQLite keeps times as text with the
// offset they were written in (the server's, time.Now) and compares that text, so a
// cutoff in another zone ordered wrongly: on a UTC server with a site in Shanghai, a
// check-in at 05:00 (21:00 UTC the day before) looked like yesterday's, and a second
// one at 07:00 earned again.
func TestSignInDailyInAnotherZone(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		_, off := time.Now().Zone()
		east := time.FixedZone("server+10h", off+10*3600) // the site, ten hours ahead of the server
		daily := SignPolicy{Min: 5, Max: 5, Daily: true, Location: east}
		u := newUser(t, d.Ent, 0)
		morning := time.Date(2026, 10, 24, 5, 0, 0, 0, east).In(time.Local) // what time.Now gives
		if _, _, _, err := SignIn(ctx, d.Ent, u.ID, daily, morning); err != nil {
			t.Fatal(err)
		}
		var early *TooEarlyError
		if _, _, _, err := SignIn(ctx, d.Ent, u.ID, daily, morning.Add(2*time.Hour)); !errors.As(err, &early) {
			t.Fatalf("the same day in the site's zone earns once: %v", err)
		}
		if _, _, _, err := SignIn(ctx, d.Ent, u.ID, daily, morning.Add(19*time.Hour)); err != nil {
			t.Fatalf("the next day: %v", err)
		}
	})
}
