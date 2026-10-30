package player

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

// Filter selects players for administrators.
type Filter struct {
	// Query matches a player id, a UUID (with or without dashes) or the start of a
	// name, ignoring case.
	Query  string
	UserID int64 // one owner's players
	Before int64 // players with smaller ids (the next page)
	Offset int   // or skip this many (numbered pages)
	Limit  int   // 1–100, default 20
}

func (f Filter) where() []predicate.Player {
	var where []predicate.Player
	if term := strings.TrimSpace(f.Query); term != "" {
		match := []predicate.Player{entplayer.NameLowerHasPrefix(Key(term))}
		if id, err := strconv.ParseInt(term, 10, 64); err == nil {
			match = append(match, entplayer.ID(id))
		}
		if u := strings.ToLower(strings.ReplaceAll(term, "-", "")); len(u) == 32 {
			match = append(match, entplayer.UUID(u))
		}
		where = append(where, entplayer.Or(match...))
	}
	if f.UserID > 0 {
		where = append(where, entplayer.UserID(f.UserID))
	}
	return where
}

// Search returns one page of players, newest first, with their owner and what they
// wear, and the id to pass as Before for the next page (0 on the last page).
func (s *Service) Search(ctx context.Context, f Filter) ([]*ent.Player, int64, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := f.where()
	if f.Before > 0 {
		where = append(where, entplayer.IDLT(f.Before))
	}
	list, err := s.db.Player.Query().Where(where...).WithOwner().WithSkin().WithCape().
		Order(ent.Desc(entplayer.FieldID)).Offset(max(f.Offset, 0)).Limit(limit + 1).All(ctx)
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

// Count is how many players the filter matches (Before, Offset and Limit aside).
func (s *Service) Count(ctx context.Context, f Filter) (int, error) {
	return s.db.Player.Query().Where(f.where()...).Count(ctx)
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
// "change owner"); see Edit.
func (s *Service) Transfer(ctx context.Context, id, newOwner int64) (*ent.Player, error) {
	if _, err := s.Edit(ctx, id, Change{NewOwner: &newOwner}, Policy{}, nil); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Change is an edit of a player.
type Change struct {
	// Owner is the owner the caller checked its permission against; the edit fails
	// with ErrNotFound if the player has moved to someone else meanwhile (0: any).
	Owner    int64
	Name     *string // a new name
	NewOwner *int64  // a new owner
}

// errMoved: what the player wears changed while its textures were being locked.
var errMoved = errors.New("the player changed meanwhile")

// Edit renames a player and/or gives it to another user, in one transaction (an
// administrator's edit used to be two, and could half succeed).
//   - With v3 UUIDs the UUID follows the name, as on an offline-mode server; v4 UUIDs
//     stay.
//   - Players only wear their owner's closet textures, so what the new owner's closet
//     lacks comes off. The ledger stays with the old owner: deleting the player later
//     refunds the new owner nothing, as they paid nothing.
//
// rec, if not nil, is written to the audit log in the same transaction, with what
// changed in its meta ("name", "owner": [before, after]); nothing is written when
// nothing changed.
func (s *Service) Edit(ctx context.Context, id int64, c Change, p Policy, rec *audit.Entry) (*ent.Player, error) {
	var name string
	if c.Name != nil {
		name = strings.TrimSpace(*c.Name)
		if err := ValidateName(name, p); err != nil {
			return nil, err
		}
	}
	for range 3 {
		var out *ent.Player
		err := withTx(ctx, s.db, func(tx *ent.Client) error {
			cur, err := tx.Player.Get(ctx, id)
			if ent.IsNotFound(err) || (err == nil && c.Owner != 0 && cur.UserID != c.Owner) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			moving := c.NewOwner != nil && *c.NewOwner != cur.UserID
			if moving {
				// What it wears is checked against the new owner's closet: the textures
				// first, then the player again (see lockTextures).
				if err := lockTextures(ctx, tx, cur.SkinID, cur.CapeID); err != nil {
					return err
				}
				again, err := tx.Player.Get(ctx, id)
				if ent.IsNotFound(err) {
					return ErrNotFound
				}
				if err != nil {
					return err
				}
				if !sameID(again.SkinID, cur.SkinID) || !sameID(again.CapeID, cur.CapeID) || again.UserID != cur.UserID {
					return errMoved // the locks taken are not the ones needed: start over
				}
			}
			renamed := c.Name != nil && name != cur.Name
			if !renamed && !moving {
				out = cur
				return nil
			}
			upd := tx.Player.UpdateOne(cur)
			if renamed {
				upd.SetName(name).SetNameLower(Key(name))
				if !p.UUIDv4 {
					upd.SetUUID(OfflineUUID(name))
				}
				rec = rec.With("name", []string{cur.Name, name})
			}
			if moving {
				ok, err := tx.User.Query().Where(user.ID(*c.NewOwner), user.StatusNEQ(user.StatusDeleted)).Exist(ctx)
				if err != nil {
					return err
				}
				if !ok {
					return &validate.FieldError{Field: "userId", Message: "no such user"}
				}
				upd.SetUserID(*c.NewOwner)
				for _, slot := range []struct {
					id    *int64
					clear func() *ent.PlayerUpdateOne
				}{{cur.SkinID, upd.ClearSkinID}, {cur.CapeID, upd.ClearCapeID}} {
					if slot.id == nil {
						continue
					}
					has, err := tx.ClosetItem.Query().Where(closetitem.UserID(*c.NewOwner), closetitem.TextureID(*slot.id)).Exist(ctx)
					if err != nil {
						return err
					}
					if !has {
						slot.clear()
					}
				}
				rec = rec.With("owner", []int64{cur.UserID, *c.NewOwner})
			}
			pl, err := upd.Save(ctx)
			if ent.IsConstraintError(err) {
				return errNameTaken
			}
			if err != nil {
				return err
			}
			if renamed {
				e := event(pl)
				e.OldName = cur.Name
				if err := hook.OnPlayerRenamed.Publish(ctx, s.hooks, tx, e); err != nil {
					return err
				}
			}
			if moving {
				if err := hook.OnPlayerUpdated.Publish(ctx, s.hooks, tx, event(pl)); err != nil {
					return err
				}
			}
			out = pl
			return audit.Write(ctx, tx, rec)
		})
		if errors.Is(err, errMoved) {
			continue
		}
		return out, err
	}
	return nil, errMoved
}

func sameID(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
