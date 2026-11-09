package texture

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

var (
	// ErrInCloset: the texture is in the closet already.
	ErrInCloset = errors.New("already in the closet")
	// ErrNotInCloset: the closet has no such texture.
	ErrNotInCloset = errors.New("not in the closet")
)

// closetRef names one closet item in the ledger. Each add makes a new item, so the
// fee and award of this item are never confused with those of an earlier add of the
// same texture (which may have been refunded already, or cost something else).
func closetRef(itemID int64) string { return strconv.FormatInt(itemID, 10) }

// Favorite adds a texture the viewer may see to their closet (Blessing Skin calls it
// a "like"): it costs r.PerClosetItem, and the uploader, if it is someone else, earns
// r.AwardPerLike.
func (s *Service) Favorite(ctx context.Context, v Viewer, id int64, name string, r score.Rates) (*ent.ClosetItem, int64, error) {
	t, err := s.Get(ctx, id, v)
	if err != nil {
		return nil, 0, err
	}
	if name = strings.TrimSpace(name); name == "" {
		name = t.Name
	}
	if err := ValidateName(name, nil); err != nil {
		return nil, 0, err
	}
	var (
		item    *ent.ClosetItem
		balance int64
	)
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		// Locks first, in the one order (see score.Lock): the texture, then the user
		// and the uploader whose scores change. What the outcome depends on is read
		// again under the lock: since Get the texture may have gone private (it must
		// not land in another closet) or lost its uploader (deleted: no award).
		t, err := lockAndGet(ctx, tx, id)
		if err != nil {
			return err
		}
		if !v.MayView(t) {
			return ErrPrivate
		}
		if err := tx.Texture.UpdateOneID(id).AddLikes(1).Exec(ctx); err != nil {
			return err
		}
		lock := []int64{v.UserID}
		if t.UploaderID != nil {
			lock = append(lock, *t.UploaderID)
		}
		if err := score.Lock(ctx, tx, lock...); err != nil {
			return err
		}
		item, err = tx.ClosetItem.Create().SetUserID(v.UserID).SetTextureID(id).SetName(name).Save(ctx)
		if db.IsConstraint(err) {
			return ErrInCloset
		}
		if err != nil {
			return err
		}
		ref := closetRef(item.ID)
		if balance, err = score.Apply(ctx, tx, score.Change{UserID: v.UserID, Delta: -r.PerClosetItem,
			Reason: score.ReasonClosetAdd, RefType: "closet", RefID: ref}); err != nil {
			return err
		}
		if up := t.UploaderID; up != nil && *up != v.UserID && r.AwardPerLike > 0 {
			if _, err = score.Apply(ctx, tx, score.Change{UserID: *up, Delta: r.AwardPerLike,
				Reason: score.ReasonLikeAward, RefType: "closet", RefID: ref}); err != nil {
				return err
			}
		}
		return hook.OnClosetAdded.Publish(ctx, s.hooks, tx, hook.ClosetEvent{UserID: v.UserID, TextureID: id})
	})
	return item, balance, err
}

// Unfavorite removes a texture from the user's closet and from the user's players
// (players only wear closet textures). With refund the closet fee comes back, and
// the uploader's award for it is taken back if the uploader can still cover it. rec,
// if not nil, is written to the audit log in the same transaction (an administrator
// taking it out).
func (s *Service) Unfavorite(ctx context.Context, userID, id int64, refund bool, rec *audit.Entry) (int64, error) {
	var returned int64
	err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		t, err := lockAndGet(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			return ErrNotInCloset
		} else if err != nil {
			return err
		}
		lock := []int64{userID}
		if t.UploaderID != nil {
			lock = append(lock, *t.UploaderID)
		}
		if err := score.Lock(ctx, tx, lock...); err != nil {
			return err
		}
		item, err := tx.ClosetItem.Query().Where(closetitem.UserID(userID), closetitem.TextureID(id)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotInCloset
		}
		if err != nil {
			return err
		}
		if n, err := tx.ClosetItem.Delete().Where(closetitem.ID(item.ID)).Exec(ctx); err != nil {
			return err
		} else if n == 0 {
			return ErrNotInCloset // removed by a concurrent request
		}
		if returned, err = s.release(ctx, tx, t, item, refund); err != nil {
			return err
		}
		return audit.Write(ctx, tx, rec.With("refunded", returned))
	})
	return returned, err
}

// release undoes one closet item of t (the row itself is deleted already): the like
// count, the owner's players wearing it, the fee and the uploader's award.
func (s *Service) release(ctx context.Context, tx *ent.Client, t *ent.Texture, item *ent.ClosetItem, refund bool) (int64, error) {
	userID := item.UserID
	if err := tx.Texture.Update().Where(tex.ID(t.ID), tex.LikesGT(0)).AddLikes(-1).Exec(ctx); err != nil {
		return 0, err
	}
	if err := s.takeOff(ctx, tx, t, entplayer.UserID(userID)); err != nil {
		return 0, err
	}
	if err := hook.OnClosetRemoved.Publish(ctx, s.hooks, tx, hook.ClosetEvent{UserID: userID, TextureID: t.ID}); err != nil {
		return 0, err
	}
	ref := closetRef(item.ID)
	var returned int64
	if refund {
		paid, err := score.Last(ctx, tx, userID, score.ReasonClosetAdd, "closet", ref)
		if err != nil {
			return 0, err
		}
		if paid != nil && paid.Delta < 0 {
			returned = -paid.Delta
			if _, err := score.Apply(ctx, tx, score.Change{UserID: userID, Delta: returned,
				Reason: score.ReasonClosetRemove, RefType: "closet", RefID: ref}); err != nil {
				return 0, err
			}
		}
	}
	if up := t.UploaderID; up != nil && *up != userID {
		award, err := score.Last(ctx, tx, *up, score.ReasonLikeAward, "closet", ref)
		if err != nil {
			return 0, err
		}
		if award != nil && award.Delta > 0 {
			_, err := score.Apply(ctx, tx, score.Change{UserID: *up, Delta: -award.Delta,
				Reason: score.ReasonLikeTakeBack, RefType: "closet", RefID: ref})
			if err != nil && !errors.Is(err, score.ErrInsufficient) { // spent already: let it be
				return 0, err
			}
		}
	}
	return returned, nil
}

// takeOff clears t from the slot of its type on the players matching who.
func (s *Service) takeOff(ctx context.Context, tx *ent.Client, t *ent.Texture, who ...predicate.Player) error {
	if t.Type == tex.TypeCape {
		return s.clearSlot(ctx, tx, true, append(who, entplayer.CapeID(t.ID))...)
	}
	return s.clearSlot(ctx, tx, false, append(who, entplayer.SkinID(t.ID))...)
}

// clearSlot empties the skin (or cape) slot of the players matching where and
// publishes each as player.updated. It goes through ent, which bumps
// players.updated_at, the Last-Modified game clients revalidate profiles with: a
// slot emptied by the foreign key's SET NULL alone left it as it was.
//
// The update uses where itself, not a list of the players' ids: a popular texture's
// wearers made that list longer than a statement may be (SQLite 32766 parameters,
// PostgreSQL 65535). The caller holds the texture's lock, under which nobody puts it
// on, so the players listed for the events are the ones updated. They are listed
// only while something listens.
func (s *Service) clearSlot(ctx context.Context, tx *ent.Client, cape bool, where ...predicate.Player) error {
	var list []*ent.Player
	if hook.OnPlayerUpdated.Listening(s.hooks) {
		var err error
		list, err = tx.Player.Query().Where(where...).
			Select(entplayer.FieldID, entplayer.FieldUserID, entplayer.FieldName, entplayer.FieldUUID).All(ctx)
		if err != nil {
			return err
		}
	}
	upd := tx.Player.Update().Where(where...)
	if cape {
		upd.ClearCapeID()
	} else {
		upd.ClearSkinID()
	}
	if err := upd.Exec(ctx); err != nil {
		return err
	}
	for _, p := range list {
		e := hook.PlayerEvent{PlayerID: p.ID, UserID: p.UserID, Name: p.Name, UUID: p.UUID}
		if err := hook.OnPlayerUpdated.Publish(ctx, s.hooks, tx, e); err != nil {
			return err
		}
	}
	return nil
}

// GiveToCloset adds a texture to a user's closet as an administrator does (Blessing
// Skin's closet management API): free, and nothing for the uploader. Taking it out
// again is Unfavorite, which refunds only what this item cost: nothing. A private
// texture goes only into its uploader's closet, as everywhere else (dropOthers). rec,
// if not nil, is written to the audit log in the same transaction.
func (s *Service) GiveToCloset(ctx context.Context, userID, id int64, rec *audit.Entry) (*ent.ClosetItem, error) {
	var item *ent.ClosetItem
	err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		// The texture first (see score.Lock), then the insert that references it; its
		// visibility is read under the lock, so it cannot go private in between.
		t, err := lockAndGet(ctx, tx, id)
		if err != nil {
			return err
		}
		if !(Viewer{UserID: userID}).MayView(t) {
			return &validate.FieldError{Field: "textureId", Message: "private to its uploader"}
		}
		if err := tx.Texture.UpdateOneID(id).AddLikes(1).Exec(ctx); err != nil {
			return err
		}
		item, err = tx.ClosetItem.Create().SetUserID(userID).SetTextureID(id).SetName(t.Name).Save(ctx)
		if db.IsConstraint(err) {
			return ErrInCloset
		}
		if err != nil {
			return err
		}
		if err := hook.OnClosetAdded.Publish(ctx, s.hooks, tx, hook.ClosetEvent{UserID: userID, TextureID: id}); err != nil {
			return err
		}
		return audit.Write(ctx, tx, rec)
	})
	return item, err
}

// RenameClosetItem renames a texture in the user's closet.
func (s *Service) RenameClosetItem(ctx context.Context, userID, id int64, name string) (*ent.ClosetItem, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name, nil); err != nil {
		return nil, err
	}
	n, err := s.db.ClosetItem.Update().Where(closetitem.UserID(userID), closetitem.TextureID(id)).SetName(name).Save(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotInCloset
	}
	return s.db.ClosetItem.Query().Where(closetitem.UserID(userID), closetitem.TextureID(id)).
		WithTexture(func(q *ent.TextureQuery) { q.WithUploader(selectUploader) }).Only(ctx)
}

// ClosetSize is how many textures a user's closet holds.
func (s *Service) ClosetSize(ctx context.Context, userID int64) (int, error) {
	return s.db.ClosetItem.Query().Where(closetitem.UserID(userID)).Count(ctx)
}
