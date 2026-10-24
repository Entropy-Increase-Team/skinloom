package score

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func newUser(t *testing.T, c *ent.Client, score int64) *ent.User {
	t.Helper()
	return c.User.Create().SetEmail("u@example.com").SetEmailNormalized("u@example.com").
		SetNickname("U").SetPasswordHash("x").SetScore(score).SaveX(context.Background())
}

func inTx(ctx context.Context, c *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := c.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestApply(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		c := d.Ent
		u := newUser(t, c, 100)
		admin := int64(7)
		var balance int64
		err := inTx(ctx, c, func(tx *ent.Client) (err error) {
			balance, err = Apply(ctx, tx, Change{UserID: u.ID, Delta: -30, Reason: ReasonTextureUpload, RefType: "texture", RefID: "1"})
			return err
		})
		if err != nil || balance != 70 {
			t.Fatalf("Apply = %d, %v", balance, err)
		}
		err = inTx(ctx, c, func(tx *ent.Client) error {
			_, err := Apply(ctx, tx, Change{UserID: u.ID, Delta: -71, Reason: ReasonTextureUpload})
			return err
		})
		if !errors.Is(err, ErrInsufficient) {
			t.Fatalf("overdraft: %v", err)
		}
		_ = inTx(ctx, c, func(tx *ent.Client) error {
			_, err := Apply(ctx, tx, Change{UserID: u.ID, Delta: 5, Reason: ReasonAdmin, Actor: &admin})
			return err
		})
		if got := c.User.GetX(ctx, u.ID).Score; got != 75 {
			t.Fatalf("score = %d", got)
		}
		entries := c.ScoreEntry.Query().Order(ent.Asc(scoreentry.FieldID)).AllX(ctx)
		if len(entries) != 2 || entries[0].Delta != -30 || entries[0].Balance != 70 || entries[0].RefID != "1" ||
			entries[1].Balance != 75 || entries[1].ActorID == nil || *entries[1].ActorID != admin {
			t.Fatalf("ledger %+v", entries)
		}
		err = inTx(ctx, c, func(tx *ent.Client) error {
			_, err := Apply(ctx, tx, Change{UserID: 999999, Delta: 1, Reason: ReasonAdmin})
			return err
		})
		if !ent.IsNotFound(err) {
			t.Fatalf("unknown user: %v", err)
		}
		// A free action reports the balance and leaves no ledger noise.
		err = inTx(ctx, c, func(tx *ent.Client) (err error) {
			balance, err = Apply(ctx, tx, Change{UserID: u.ID, Delta: 0, Reason: ReasonTextureUpload})
			return err
		})
		if err != nil || balance != 75 || c.ScoreEntry.Query().CountX(ctx) != 2 {
			t.Fatalf("zero delta: %d, %v, %d entries", balance, err, c.ScoreEntry.Query().CountX(ctx))
		}
	})
}

// Regression for Blessing Skin C03: concurrent spending must not overdraw.
func TestApplyConcurrent(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		c := d.Ent
		u := newUser(t, c, 1000)
		var wg sync.WaitGroup
		var mu sync.Mutex
		spent, refused := 0, 0
		for range 15 {
			wg.Go(func() {
				err := inTx(ctx, c, func(tx *ent.Client) error {
					_, err := Apply(ctx, tx, Change{UserID: u.ID, Delta: -100, Reason: ReasonTextureUpload})
					return err
				})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					spent++
				case errors.Is(err, ErrInsufficient):
					refused++
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if got := c.User.GetX(ctx, u.ID).Score; spent != 10 || refused != 5 || got != 0 {
			t.Fatalf("%d spent, %d refused, score %d", spent, refused, got)
		}
		if n := c.ScoreEntry.Query().CountX(ctx); n != 10 {
			t.Fatalf("ledger entries %d", n)
		}
	})
}
