package player

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthcode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthdevicecode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthtoken"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggsession"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggtoken"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
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
		cur, err := s.db.Player.Get(ctx, id)
		if ent.IsNotFound(err) || (err == nil && c.Owner != 0 && cur.UserID != c.Owner) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		name = strings.TrimSpace(hook.FilterPlayerName.Apply(ctx, s.hooks, strings.TrimSpace(*c.Name), hook.PlayerNameFilterContext{
			UserID:   cur.UserID,
			PlayerID: &id,
		}))
		if err := ValidateName(name, p); err != nil {
			return nil, err
		}
		if name != cur.Name {
			if err := hook.GuardPlayerRename.Check(ctx, s.hooks, hook.PlayerRenameGuard{
				UserID:   cur.UserID,
				PlayerID: id,
				OldName:  cur.Name,
				NewName:  name,
			}); err != nil {
				return nil, err
			}
		}
	}
	for range 3 {
		var out *ent.Player
		err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
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
				if !p.UUIDv4 && !p.KeepUUIDOnRename {
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
			if db.IsConstraint(err) {
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
				if err := revokePlayerSessions(ctx, tx, cur.ID, false); err != nil {
					return err
				}
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

func revokePlayerSessions(ctx context.Context, tx *ent.Client, playerID int64, deleteYggTokens bool) error {
	if _, err := tx.YggSession.Delete().Where(yggsession.PlayerID(playerID)).Exec(ctx); err != nil {
		return err
	}
	if deleteYggTokens {
		if _, err := tx.YggToken.Delete().Where(yggtoken.PlayerID(playerID)).Exec(ctx); err != nil {
			return err
		}
	}
	if _, err := tx.OAuthToken.Delete().Where(oauthtoken.PlayerID(playerID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.OAuthCode.Delete().Where(oauthcode.PlayerID(playerID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.OAuthDeviceCode.Delete().Where(oauthdevicecode.PlayerID(playerID)).Exec(ctx); err != nil {
		return err
	}
	return nil
}

// NormalizeUUID parses a UUID in canonical 32-hex or 8-4-4-4-12 dashed format and
// returns its canonical 32-char lowercase hex representation.
func NormalizeUUID(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	switch len(s) {
	case 32:
		// Undashed 32-hex UUID.
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return "", false
		}
		s = s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	default:
		return "", false
	}
	u := strings.ToLower(s)
	if _, err := hex.DecodeString(u); err != nil {
		return "", false
	}
	return u, true
}

// FindByUUID looks up a player by its 32-hex UUID (dashes allowed).
func (s *Service) FindByUUID(ctx context.Context, rawUUID string) (*ent.Player, error) {
	u, ok := NormalizeUUID(rawUUID)
	if !ok {
		return nil, ErrNotFound
	}
	p, err := s.db.Player.Query().Where(entplayer.UUID(u)).WithOwner().WithSkin().WithCape().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return p, err
}

// ListAll returns players ordered by ascending ID after afterID (excluding players
// whose owner account is soft-deleted), up to limit (default 200, max 500).
func (s *Service) ListAll(ctx context.Context, afterID int64, limit int) ([]*ent.Player, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	q := s.db.Player.Query().Where(entplayer.HasOwnerWith(user.StatusNEQ(user.StatusDeleted)))
	if afterID > 0 {
		q = q.Where(entplayer.IDGT(afterID))
	}
	return q.Order(ent.Asc(entplayer.FieldID)).Limit(limit).All(ctx)
}

var errUUIDTaken = &validate.FieldError{Field: "uuid", Message: "UUID already in use"}

// RemapUUID changes the UUID of the player currently holding oldUUID to newUUID inside
// one transaction, clears stale Yggdrasil join sessions for that player, and writes rec
// to the audit log when provided.
func (s *Service) RemapUUID(ctx context.Context, oldUUID, newUUID string, rec *audit.Entry) (*ent.Player, error) {
	oldNorm, ok := NormalizeUUID(oldUUID)
	if !ok {
		return nil, &validate.FieldError{Field: "oldUuid", Message: "invalid UUID"}
	}
	newNorm, ok := NormalizeUUID(newUUID)
	if !ok || newNorm == "00000000000000000000000000000000" {
		return nil, &validate.FieldError{Field: "newUuid", Message: "invalid UUID"}
	}
	var out *ent.Player
	err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		cur, err := tx.Player.Query().Where(entplayer.UUID(oldNorm)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if oldNorm == newNorm {
			out = cur
			return nil
		}
		taken, err := tx.Player.Query().Where(entplayer.UUID(newNorm), entplayer.IDNEQ(cur.ID)).Exist(ctx)
		if err != nil {
			return err
		}
		if taken {
			return errUUIDTaken
		}
		pl, err := tx.Player.UpdateOne(cur).SetUUID(newNorm).Save(ctx)
		if db.IsConstraint(err) {
			return errUUIDTaken
		}
		if err != nil {
			return err
		}
		if err := revokePlayerSessions(ctx, tx, cur.ID, true); err != nil {
			return err
		}
		if err := hook.OnPlayerUpdated.Publish(ctx, s.hooks, tx, event(pl)); err != nil {
			return err
		}
		out = pl
		return audit.Write(ctx, tx, rec.With("uuid", []string{oldNorm, newNorm}))
	})
	return out, err
}
