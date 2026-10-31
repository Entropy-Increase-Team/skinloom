package texture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/blob"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

var (
	// ErrForbidden: only the uploader and administrators change a texture.
	ErrForbidden = errors.New("only the uploader or an administrator can do this")
	// ErrInCloset: the texture is in the closet already.
	ErrInCloset = errors.New("already in the closet")
	// ErrNotInCloset: the closet has no such texture.
	ErrNotInCloset = errors.New("not in the closet")
)

// PublicDuplicateError refuses to make a texture public when the same image is
// public already (as Blessing Skin did): the library shows each image once.
type PublicDuplicateError struct{ ID int64 }

func (e *PublicDuplicateError) Error() string {
	return fmt.Sprintf("the same image is public already as texture %d", e.ID)
}

// SweepJobKind deletes a file that no texture uses any more, once it is safe (see
// SweepFile).
const SweepJobKind = "texture.sweep"

// SweepGrace is how long an unreferenced file is kept: an upload writes its file
// before its transaction, and a file just written again by an upload of the same
// image must not be deleted under it.
const SweepGrace = time.Hour

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
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
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
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
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

func textureEvent(t *ent.Texture) hook.TextureEvent {
	return hook.TextureEvent{TextureID: t.ID, UploaderID: t.UploaderID, Hash: t.Hash, Type: string(t.Type),
		Public: t.Visibility == tex.VisibilityPublic}
}

// GiveToCloset adds a texture to a user's closet as an administrator does (Blessing
// Skin's closet management API): free, and nothing for the uploader. Taking it out
// again is Unfavorite, which refunds only what this item cost: nothing. A private
// texture goes only into its uploader's closet, as everywhere else (dropOthers). rec,
// if not nil, is written to the audit log in the same transaction.
func (s *Service) GiveToCloset(ctx context.Context, userID, id int64, rec *audit.Entry) (*ent.ClosetItem, error) {
	var item *ent.ClosetItem
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
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

// Edit changes the fields that are set.
type Edit struct {
	Name   *string
	Type   *string
	Public *bool
}

// mayEdit loads a texture its uploader or an administrator may change.
func (s *Service) mayEdit(ctx context.Context, v Viewer, id int64) (*ent.Texture, error) {
	t, err := s.Get(ctx, id, v)
	if err != nil {
		return nil, err
	}
	if !v.Admin && (t.UploaderID == nil || *t.UploaderID != v.UserID) {
		return nil, ErrForbidden
	}
	return t, nil
}

// Update applies an uploader's or administrator's edit.
//
//   - A new type must fit the image (a skin cannot become a cape of the wrong
//     shape), and players wearing it in the slot it no longer fits take it off
//     (Blessing Skin left capes on as skins, C08).
//   - Going private costs the uploader the difference between the private and the
//     public price of the upload, including the award public uploads earn; going
//     public gives the same difference back (Blessing Skin took the award back on
//     the way to private and never returned it, C12). Administrators changing
//     someone else's texture charge nobody. Other users' closets lose a texture that
//     goes private, with their fees returned and their players reset.
//
// rec is written to the audit log in the same transaction when the viewer is not the
// uploader (an administrator moderating).
func (s *Service) Update(ctx context.Context, v Viewer, id int64, e Edit, p Policy, r score.Rates, rec *audit.Entry) (*ent.Texture, error) {
	t, err := s.mayEdit(ctx, v, id) // early, friendly refusals; decided again under the lock
	if err != nil {
		return nil, err
	}
	var errs []error
	var newName string
	if e.Name != nil {
		newName = strings.TrimSpace(*e.Name)
		errs = append(errs, ValidateName(newName, p.NameRule))
	}
	var newType tex.Type
	if e.Type != nil {
		newType = tex.Type(*e.Type)
		if tex.TypeValidator(newType) != nil {
			errs = append(errs, &validate.FieldError{Field: "type", Message: "want steve, alex or cape"})
		} else {
			b, err := s.db.Blob.Get(ctx, t.Hash) // the image never changes: no need to lock it
			if err != nil {
				return nil, err
			}
			errs = append(errs, checkShape(b.Width, b.Height, newType, "type"))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		// The texture is locked first (see score.Lock) and everything the edit decides
		// on is read after that: two "make private" requests at once each charged the
		// difference when they decided from a read made before the transaction.
		t, err := lockAndGet(ctx, tx, id)
		if err != nil {
			return err
		}
		if !v.Admin && (t.UploaderID == nil || *t.UploaderID != v.UserID) {
			return ErrForbidden
		}
		goingPublic := e.Public != nil && *e.Public && t.Visibility == tex.VisibilityPrivate
		goingPrivate := e.Public != nil && !*e.Public && t.Visibility == tex.VisibilityPublic
		charged := (goingPublic || goingPrivate) && t.UploaderID != nil && *t.UploaderID == v.UserID // the uploader's own change
		var lock []int64
		if charged {
			lock = append(lock, v.UserID)
		}
		if goingPrivate {
			err = lockUsersOf(ctx, tx, t, false, lock...)
		} else {
			err = score.Lock(ctx, tx, lock...)
		}
		if err != nil {
			return err
		}
		if goingPublic {
			dup, err := tx.Texture.Query().
				Where(tex.Hash(t.Hash), tex.VisibilityEQ(tex.VisibilityPublic), tex.IDNEQ(t.ID)).First(ctx)
			if err == nil {
				return &PublicDuplicateError{ID: dup.ID}
			}
			if !ent.IsNotFound(err) {
				return err
			}
		}
		u := tx.Texture.UpdateOneID(t.ID)
		if newName != "" {
			u.SetName(newName)
		}
		if newType != "" && newType != t.Type {
			u.SetType(newType)
			wasCape, isCape := t.Type == tex.TypeCape, newType == tex.TypeCape
			if wasCape != isCape {
				if err := s.takeOff(ctx, tx, t); err != nil { // t still has the old type
					return err
				}
			}
		}
		if goingPublic || goingPrivate {
			if goingPublic {
				u.SetVisibility(tex.VisibilityPublic)
			} else {
				u.SetVisibility(tex.VisibilityPrivate)
			}
			if charged {
				diff := r.UploadCost(t.Size, false) - r.UploadCost(t.Size, true) // what private costs more
				delta := -diff
				if goingPublic {
					delta = diff
				}
				if _, err := score.Apply(ctx, tx, score.Change{UserID: v.UserID, Delta: delta,
					Reason: score.ReasonVisibility, RefType: "texture", RefID: strconv.FormatInt(t.ID, 10)}); err != nil {
					return err
				}
			}
			if goingPrivate {
				if err := s.dropOthers(ctx, tx, t, r.ReturnOnDelete); err != nil {
					return err
				}
			}
		}
		saved, err := u.Save(ctx)
		if err != nil {
			return err
		}
		if newName != "" || (newType != "" && newType != t.Type) || goingPublic || goingPrivate {
			if err := hook.OnTextureUpdated.Publish(ctx, s.hooks, tx, textureEvent(saved)); err != nil {
				return err
			}
		}
		if t.UploaderID == nil || *t.UploaderID != v.UserID {
			return audit.Write(ctx, tx, rec)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.db.Texture.Query().Where(tex.ID(id)).WithUploader(selectUploader).Only(ctx)
}

// Lock takes the row lock of texture id for the rest of tx, as the first lock of a
// transaction that changes it or the scores tied to it (see score.Lock). It reports
// ErrNotFound when the texture is gone (deleted by the transaction it waited for).
func Lock(ctx context.Context, tx *ent.Client, id int64) error {
	n, err := tx.Texture.Update().Where(tex.ID(id)).AddLikes(0).Save(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// lockAndGet locks texture id (Lock) and reads it under the lock. A transaction must
// decide from this read, not from one made before it started: the texture may have
// changed (gone private, lost its uploader, changed type) while the lock was awaited.
func lockAndGet(ctx context.Context, tx *ent.Client, id int64) (*ent.Texture, error) {
	if err := Lock(ctx, tx, id); err != nil {
		return nil, err
	}
	return tx.Texture.Get(ctx, id)
}

// UsersOf lists the users a deletion of texture id changes, for a caller that locks
// them first (score.Lock): everyone whose closet holds it, its uploader, and everyone
// showing it as their avatar.
func (s *Service) UsersOf(ctx context.Context, tx *ent.Client, id int64) ([]int64, error) {
	t, err := tx.Texture.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return usersOf(ctx, tx, t, true)
}

// usersOf lists everyone whose closet holds t and its uploader, plus with avatars
// everyone whose avatar it is (a deletion clears those). Only a transaction holding
// t's lock gets a list that stays true: closets and avatars change under that lock.
func usersOf(ctx context.Context, tx *ent.Client, t *ent.Texture, avatars bool) ([]int64, error) {
	items, err := tx.ClosetItem.Query().Where(closetitem.TextureID(t.ID)).Select(closetitem.FieldUserID).All(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]int64, 0, len(items)+1)
	for _, it := range items {
		users = append(users, it.UserID)
	}
	if t.UploaderID != nil {
		users = append(users, *t.UploaderID)
	}
	if avatars {
		holders, err := tx.User.Query().Where(user.AvatarTextureID(t.ID)).IDs(ctx)
		if err != nil {
			return nil, err
		}
		users = append(users, holders...)
	}
	return users, nil
}

// lockUsersOf locks, once t is locked, the users usersOf lists: all the users
// dropOthers and a deletion may refund, charge or (avatars) change.
func lockUsersOf(ctx context.Context, tx *ent.Client, t *ent.Texture, avatars bool, more ...int64) error {
	users, err := usersOf(ctx, tx, t, avatars)
	if err != nil {
		return err
	}
	return score.Lock(ctx, tx, append(users, more...)...)
}

// dropOthers removes t from every closet but its uploader's, as when it goes
// private or is deleted.
func (s *Service) dropOthers(ctx context.Context, tx *ent.Client, t *ent.Texture, refund bool) error {
	q := tx.ClosetItem.Query().Where(closetitem.TextureID(t.ID))
	if t.UploaderID != nil {
		q = q.Where(closetitem.UserIDNEQ(*t.UploaderID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := tx.ClosetItem.DeleteOne(it).Exec(ctx); err != nil {
			return err
		}
		if _, err := s.release(ctx, tx, t, it, refund); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a texture (its uploader or an administrator). With refund, everyone
// who added it to a closet gets the fee back, and the uploader gets back what the
// upload cost in all (upload and visibility changes, net of the upload award; a net
// gain is taken back if the uploader can cover it). Players wearing it wear nothing.
// The file goes once no texture uses it (SweepFile). rec is written to the audit log
// in the same transaction when the viewer is not the uploader (an administrator
// moderating), with the texture's name and hash.
func (s *Service) Delete(ctx context.Context, v Viewer, id int64, refund bool, rec *audit.Entry) error {
	t, err := s.mayEdit(ctx, v, id)
	if err != nil {
		return err
	}
	if t.UploaderID != nil && *t.UploaderID == v.UserID {
		rec = nil // the uploader's own deletion (the uploader never changes, only goes)
	}
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		if err := s.deleteIn(ctx, tx, t.ID, refund); err != nil {
			return err
		}
		return audit.Write(ctx, tx, rec.With("name", t.Name).With("hash", t.Hash))
	})
	if err == nil {
		s.kick()
	}
	return err
}

// DeleteIn deletes a texture as Delete does, inside the caller's transaction (a report
// review deletes the texture and settles the report at once). The caller checks who
// may delete it, and calls Kick once the transaction has committed.
func (s *Service) DeleteIn(ctx context.Context, tx *ent.Client, id int64, refund bool) error {
	return s.deleteIn(ctx, tx, id, refund)
}

func (s *Service) deleteIn(ctx context.Context, tx *ent.Client, id int64, refund bool) error {
	t, err := lockAndGet(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := lockUsersOf(ctx, tx, t, true); err != nil {
		return err
	}
	if err := s.dropOthers(ctx, tx, t, refund); err != nil {
		return err
	}
	if up := t.UploaderID; up != nil && refund {
		ref := strconv.FormatInt(t.ID, 10)
		net, err := score.Net(ctx, tx, *up, "texture", ref, score.ReasonTextureUpload, score.ReasonVisibility)
		if err != nil {
			return err
		}
		if net != 0 {
			_, err := score.Apply(ctx, tx, score.Change{UserID: *up, Delta: -net,
				Reason: score.ReasonTextureDelete, RefType: "texture", RefID: ref})
			if err != nil && !errors.Is(err, score.ErrInsufficient) {
				return err
			}
		}
	}
	return s.remove(ctx, tx, t)
}

// remove deletes a locked texture whose scores are settled: the players still
// wearing it (its uploader's) take it off, avatars showing it are cleared, the row
// goes (closet items cascade) and the file loses a reference.
func (s *Service) remove(ctx context.Context, tx *ent.Client, t *ent.Texture) error {
	// Not left to the foreign key's SET NULL: /{name}.json kept answering 304 with the
	// deleted texture (see clearSlot). Both slots: data imported from Blessing Skin can
	// have a texture in the wrong one (C07).
	if err := s.clearSlot(ctx, tx, false, entplayer.SkinID(t.ID)); err != nil {
		return err
	}
	if err := s.clearSlot(ctx, tx, true, entplayer.CapeID(t.ID)); err != nil {
		return err
	}
	if err := tx.Texture.DeleteOneID(t.ID).Exec(ctx); err != nil {
		return err
	}
	if err := hook.OnTextureDeleted.Publish(ctx, s.hooks, tx, textureEvent(t)); err != nil {
		return err
	}
	if err := clearAvatars(ctx, tx, t.ID); err != nil {
		return err
	}
	return s.unref(ctx, tx, t.Hash)
}

// clearAvatars unsets a deleted texture as anyone's avatar: users.avatar_texture_id
// has no foreign key (see ent/schema/user.go), so every deletion does it.
func clearAvatars(ctx context.Context, tx *ent.Client, textureID int64) error {
	return tx.User.Update().Where(user.AvatarTextureID(textureID)).ClearAvatarTextureID().Exec(ctx)
}

// BeforeUserDelete runs in the transaction that deletes an account for good, before
// the user's row goes:
//   - it locks, in the one order (see score.Lock), every texture the deletion changes
//     (the user's uploads, which lose their uploader, and the textures in their
//     closet), then the user and everyone whose avatar is one of the private uploads.
//     Deleting the row first locked the user and then, through the foreign key, their
//     textures: the reverse of a closet add, and the two deadlocked;
//   - it deletes the private uploads: nobody else could ever see them. Public ones
//     stay, without an uploader (Blessing Skin left both behind, C09);
//   - it counts the closet items, which the foreign key deletes, out of the textures'
//     likes (they drifted upwards, Blessing Skin C04).
//
// The caller kicks the job worker once the transaction has committed (file sweeps).
func (s *Service) BeforeUserDelete(ctx context.Context, tx *ent.Client, userID int64) error {
	uploads, err := tx.Texture.Query().Where(tex.UploaderID(userID)).IDs(ctx)
	if err != nil {
		return err
	}
	items, err := tx.ClosetItem.Query().Where(closetitem.UserID(userID)).Select(closetitem.FieldTextureID).All(ctx)
	if err != nil {
		return err
	}
	ids := uploads
	for _, it := range items {
		ids = append(ids, it.TextureID)
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if err := Lock(ctx, tx, id); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	private, err := tx.Texture.Query().Where(tex.UploaderID(userID), tex.VisibilityEQ(tex.VisibilityPrivate)).All(ctx)
	if err != nil {
		return err
	}
	users := []int64{userID}
	for chunk := range slices.Chunk(private, 500) { // one query per 500, within any statement's parameter limit
		ids := make([]int64, len(chunk))
		for i, t := range chunk {
			ids[i] = t.ID
		}
		holders, err := tx.User.Query().Where(user.AvatarTextureIDIn(ids...)).IDs(ctx)
		if err != nil {
			return err
		}
		users = append(users, holders...)
	}
	if err := score.Lock(ctx, tx, users...); err != nil {
		return err
	}
	if err := tx.Texture.Update().Where(tex.HasClosetItemsWith(closetitem.UserID(userID)), tex.LikesGT(0)).
		AddLikes(-1).Exec(ctx); err != nil {
		return err
	}
	for _, t := range private {
		if err := s.remove(ctx, tx, t); err != nil {
			return err
		}
	}
	return nil
}

// unref drops one reference to a blob; the last one deletes the row and schedules the
// file for deletion.
func (s *Service) unref(ctx context.Context, tx *ent.Client, hash string) error {
	if err := tx.Blob.UpdateOneID(hash).AddRefCount(-1).Exec(ctx); ent.IsNotFound(err) {
		return nil // no row to count down (imported data); the orphan sweep handles the file
	} else if err != nil {
		return err
	}
	n, err := tx.Blob.Delete().Where(blob.ID(hash), blob.RefCountLTE(0)).Exec(ctx)
	if err != nil || n == 0 {
		return err
	}
	return jobs.EnqueueAt(ctx, tx, SweepJobKind, sweepPayload{Hash: hash}, time.Now().Add(SweepGrace))
}

type sweepPayload struct {
	Hash string `json:"hash"`
}

// SweepFile deletes a stored file that no blob row names and that nothing has written
// for SweepGrace. Both conditions matter: the row is created only after an upload's
// file is written, and writing the same image again refreshes the file's time.
func (s *Service) SweepFile(ctx context.Context, hash string, now time.Time) (bool, error) {
	used, err := s.db.Blob.Query().Where(blob.ID(hash)).Exist(ctx)
	if err != nil || used {
		return false, err
	}
	written, err := s.store.Stat(ctx, hash)
	if err != nil {
		return false, nil // gone already
	}
	if now.Sub(written) < SweepGrace {
		return false, nil
	}
	return true, s.store.Delete(ctx, hash)
}

// SweepOrphans deletes every stored file without a blob row, older than SweepGrace:
// files of uploads whose transaction failed, or whose sweep job was lost.
func (s *Service) SweepOrphans(ctx context.Context, now time.Time) (int, error) {
	var old []string
	err := s.store.Walk(ctx, func(key string, mod time.Time) error {
		if now.Sub(mod) >= SweepGrace {
			old = append(old, key)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for len(old) > 0 {
		chunk := old[:min(len(old), 500)]
		old = old[len(chunk):]
		known, err := s.db.Blob.Query().Where(blob.IDIn(chunk...)).IDs(ctx)
		if err != nil {
			return deleted, err
		}
		keep := make(map[string]bool, len(known))
		for _, k := range known {
			keep[k] = true
		}
		for _, k := range chunk {
			if keep[k] {
				continue
			}
			if ok, err := s.SweepFile(ctx, k, now); err != nil {
				return deleted, err
			} else if ok {
				deleted++
			}
		}
	}
	return deleted, nil
}

// HandleSweepJob is the job handler for SweepJobKind.
func (s *Service) HandleSweepJob(ctx context.Context, payload json.RawMessage) error {
	var p sweepPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Hash == "" {
		return jobs.Permanent(fmt.Errorf("bad sweep payload: %v", err))
	}
	_, err := s.SweepFile(ctx, p.Hash, time.Now())
	return err
}

// SetJobNotifier sets a function called after a transaction that queued a job
// commits, so the worker picks it up without waiting for its next poll.
func (s *Service) SetJobNotifier(f func()) { s.notify = f }

// Kick wakes the job worker after a transaction that called DeleteIn has committed.
func (s *Service) Kick() { s.kick() }

func (s *Service) kick() {
	if s.notify != nil {
		s.notify()
	}
}

// ClosetSize is how many textures a user's closet holds.
func (s *Service) ClosetSize(ctx context.Context, userID int64) (int, error) {
	return s.db.ClosetItem.Query().Where(closetitem.UserID(userID)).Count(ctx)
}

// Usage is how much a user has uploaded.
func (s *Service) Usage(ctx context.Context, userID int64) (count int, bytes int64, err error) {
	list, err := s.db.Texture.Query().Where(tex.UploaderID(userID)).Select(tex.FieldSize).All(ctx)
	for _, t := range list {
		bytes += t.Size
	}
	return len(list), bytes, err
}

// CalibrateLikes sets every texture's likes back to the number of closets holding it
// and reports how many it corrected. Likes change with every closet add and removal,
// in the same transaction; this catches what slips past: an account deleted at the
// very moment its owner added a texture, data imported from Blessing Skin (design 04
// §5.2). It reads in pages and corrects one texture per transaction, under that
// texture's lock, so it never holds two locks and cannot deadlock with anything.
func (s *Service) CalibrateLikes(ctx context.Context) (int, error) {
	const pageSize = 500
	fixed := 0
	var after int64
	for {
		page, err := s.db.Texture.Query().Where(tex.IDGT(after)).Order(ent.Asc(tex.FieldID)).Limit(pageSize).
			Select(tex.FieldID, tex.FieldLikes).All(ctx)
		if err != nil || len(page) == 0 {
			return fixed, err
		}
		after = page[len(page)-1].ID
		ids := make([]int64, len(page))
		for i, t := range page {
			ids[i] = t.ID
		}
		var counts []struct {
			TextureID int64 `json:"texture_id"`
			Count     int   `json:"count"`
		}
		if err := s.db.ClosetItem.Query().Where(closetitem.TextureIDIn(ids...)).
			GroupBy(closetitem.FieldTextureID).Aggregate(ent.Count()).Scan(ctx, &counts); err != nil {
			return fixed, err
		}
		have := make(map[int64]int, len(counts))
		for _, c := range counts {
			have[c.TextureID] = c.Count
		}
		for _, t := range page {
			if t.Likes == have[t.ID] {
				continue
			}
			changed, err := s.calibrate(ctx, t.ID)
			if err != nil {
				return fixed, err
			}
			if changed {
				fixed++
			}
		}
		if len(page) < pageSize {
			return fixed, nil
		}
	}
}

// calibrate recounts one texture's likes under its lock (the count read outside may
// be out of date by then).
func (s *Service) calibrate(ctx context.Context, id int64) (bool, error) {
	changed := false
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		t, err := lockAndGet(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			return nil // deleted meanwhile
		}
		if err != nil {
			return err
		}
		n, err := tx.ClosetItem.Query().Where(closetitem.TextureID(id)).Count(ctx)
		if err != nil || t.Likes == n {
			return err
		}
		changed = true
		return tx.Texture.UpdateOneID(id).SetLikes(n).Exec(ctx)
	})
	return changed, err
}
