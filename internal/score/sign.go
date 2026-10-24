package score

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// ReasonSignIn is the ledger reason of a daily check-in.
const ReasonSignIn = "sign_in"

// TooEarlyError means the user already checked in for now; Next is when they can again.
type TooEarlyError struct{ Next time.Time }

func (e *TooEarlyError) Error() string {
	return fmt.Sprintf("already checked in; next check-in at %s", e.Next.Format(time.RFC3339))
}

// SignPolicy is how check-ins work on the site.
type SignPolicy struct {
	Min, Max int64
	Gap      time.Duration  // after the last check-in (when not Daily)
	Daily    bool           // once per calendar day instead, in Location
	Location *time.Location // the site's time zone
}

// SignPolicyFrom reads the check-in settings from a snapshot.
func SignPolicyFrom(s *settings.Snapshot) SignPolicy {
	p := SignPolicy{
		Min:   settings.Decode[int64](s, "score.sign_min"),
		Max:   settings.Decode[int64](s, "score.sign_max"),
		Gap:   time.Duration(settings.Decode[int64](s, "score.sign_gap_hours")) * time.Hour,
		Daily: settings.Decode[bool](s, "score.sign_daily"),
	}
	if p.Max < p.Min {
		p.Min, p.Max = p.Max, p.Min
	}
	loc, err := time.LoadLocation(s.String("site.timezone"))
	if err != nil {
		loc = time.UTC
	}
	p.Location = loc
	return p
}

// Next is when a user who last checked in at last can check in again (zero: now).
func (p SignPolicy) Next(last *time.Time) time.Time {
	if last == nil {
		return time.Time{}
	}
	if p.Daily {
		y, m, d := last.In(p.Location).Date()
		return time.Date(y, m, d+1, 0, 0, 0, 0, p.Location)
	}
	return last.Add(p.Gap)
}

// SignIn checks a user in: a random score between Min and Max, at most once per
// period. The period is enforced by one conditional update of users.last_sign_at,
// so concurrent check-ins earn once (Blessing Skin read, compared and wrote it, C03).
// It returns what was earned, the new balance and the next check-in time.
func SignIn(ctx context.Context, db *ent.Client, userID int64, p SignPolicy, now time.Time) (int64, int64, time.Time, error) {
	// A check-in at or before cutoff is from an earlier period.
	cutoff := now.Add(-p.Gap)
	if p.Daily {
		y, m, d := now.In(p.Location).Date()
		cutoff = time.Date(y, m, d, 0, 0, 0, 0, p.Location).Add(-time.Nanosecond)
	}
	gained := p.Min
	if p.Max > p.Min {
		gained += rand.Int64N(p.Max - p.Min + 1)
	}
	var balance int64
	err := withTx(ctx, db, func(tx *ent.Client) error {
		n, err := tx.User.Update().
			Where(user.ID(userID), user.Or(user.LastSignAtIsNil(), user.LastSignAtLTE(cutoff))).
			SetLastSignAt(now).Save(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			u, err := tx.User.Get(ctx, userID)
			if err != nil {
				return err
			}
			return &TooEarlyError{Next: p.Next(u.LastSignAt)}
		}
		balance, err = Apply(ctx, tx, Change{UserID: userID, Delta: gained, Reason: ReasonSignIn})
		return err
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	return gained, balance, p.Next(&now), nil
}

func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}
