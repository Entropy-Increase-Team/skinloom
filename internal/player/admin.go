package player

import (
	"context"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

// Filter selects players for administrators.
type Filter struct {
	// Query matches a player id, a UUID (with or without dashes) or the start of a
	// name, ignoring case.
	Query  string
	UserID int64 // one owner's players
	Before int64 // players with smaller ids (the next page)
	Limit  int   // 1–100, default 20
}

// Search returns one page of players, newest first, with their owner and what they
// wear, and the id to pass as Before for the next page (0 on the last page).
func (s *Service) Search(ctx context.Context, f Filter) ([]*ent.Player, int64, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := s.db.Player.Query()
	if term := strings.TrimSpace(f.Query); term != "" {
		match := []predicate.Player{entplayer.NameLowerHasPrefix(Key(term))}
		if id, err := strconv.ParseInt(term, 10, 64); err == nil {
			match = append(match, entplayer.ID(id))
		}
		if u := strings.ToLower(strings.ReplaceAll(term, "-", "")); len(u) == 32 {
			match = append(match, entplayer.UUID(u))
		}
		q = q.Where(entplayer.Or(match...))
	}
	if f.UserID > 0 {
		q = q.Where(entplayer.UserID(f.UserID))
	}
	if f.Before > 0 {
		q = q.Where(entplayer.IDLT(f.Before))
	}
	list, err := q.WithOwner().WithSkin().WithCape().Order(ent.Desc(entplayer.FieldID)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) > limit {
		list = list[:limit]
		next = list[limit-1].ID
	}
	return list, next, nil
}

// Get returns any player with its owner and what it wears (administrators).
func (s *Service) Get(ctx context.Context, id int64) (*ent.Player, error) {
	p, err := s.db.Player.Query().Where(entplayer.ID(id)).WithOwner().WithSkin().WithCape().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return p, err
}

// Transfer gives a player to another user (an administrator's action, Blessing Skin's
// "change owner"). Players only wear their owner's closet textures, so what the new
// owner's closet lacks comes off. The ledger stays with the old owner: deleting the
// player later refunds the new owner nothing, as they paid nothing.
func (s *Service) Transfer(ctx context.Context, id, newOwner int64) (*ent.Player, error) {
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		p, err := tx.Player.Get(ctx, id)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		ok, err := tx.User.Query().Where(user.ID(newOwner), user.StatusNEQ(user.StatusDeleted)).Exist(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return &validate.FieldError{Field: "userId", Message: "no such user"}
		}
		upd := tx.Player.UpdateOne(p).SetUserID(newOwner)
		for _, slot := range []struct {
			id    *int64
			clear func() *ent.PlayerUpdateOne
		}{{p.SkinID, upd.ClearSkinID}, {p.CapeID, upd.ClearCapeID}} {
			if slot.id == nil {
				continue
			}
			has, err := tx.ClosetItem.Query().Where(closetitem.UserID(newOwner), closetitem.TextureID(*slot.id)).Exist(ctx)
			if err != nil {
				return err
			}
			if !has {
				slot.clear()
			}
		}
		return upd.Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}
