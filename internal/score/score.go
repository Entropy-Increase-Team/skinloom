// Package score changes users' scores. Every change is one conditional update (the
// balance can never go negative, whatever runs concurrently) plus a ledger entry in
// the same transaction (design 04 §5.2; Blessing Skin read, modified and wrote the
// score, so concurrent requests could overdraw it or earn twice, C03).
package score

import (
	"context"
	"errors"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// ErrInsufficient means the user's score does not cover a negative delta.
var ErrInsufficient = errors.New("not enough score")

// Reasons recorded in the ledger.
const (
	ReasonRegister      = "register"
	ReasonTextureUpload = "texture.upload"
	ReasonAdmin         = "admin" // an administrator set the score
)

// Change is one score change.
type Change struct {
	UserID int64
	Delta  int64
	Reason string
	// RefType and RefID name what the change is about, e.g. "texture" and "42".
	RefType string
	RefID   string
	Actor   *int64 // the administrator making it; nil for the user or the system
}

// Apply changes a score inside tx and returns the new balance. A negative delta the
// balance does not cover fails with ErrInsufficient and changes nothing. A zero delta
// (a free upload, say) writes no ledger entry.
func Apply(ctx context.Context, tx *ent.Client, c Change) (int64, error) {
	if c.Delta == 0 {
		u, err := tx.User.Get(ctx, c.UserID)
		if err != nil {
			return 0, err
		}
		return u.Score, nil
	}
	n, err := tx.User.Update().
		Where(user.ID(c.UserID), user.ScoreGTE(-c.Delta)).
		AddScore(c.Delta).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		if _, err := tx.User.Get(ctx, c.UserID); err != nil {
			return 0, err
		}
		return 0, ErrInsufficient
	}
	u, err := tx.User.Get(ctx, c.UserID)
	if err != nil {
		return 0, err
	}
	err = tx.ScoreEntry.Create().
		SetUserID(c.UserID).
		SetDelta(c.Delta).
		SetBalance(u.Score).
		SetReason(c.Reason).
		SetRefType(c.RefType).
		SetRefID(c.RefID).
		SetNillableActorID(c.Actor).
		Exec(ctx)
	return u.Score, err
}
