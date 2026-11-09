package texture

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

// ErrForbidden: only the uploader and administrators change a texture.
var ErrForbidden = errors.New("only the uploader or an administrator can do this")

// PublicDuplicateError refuses to make a texture public when the same image is
// public already (as Blessing Skin did): the library shows each image once.
type PublicDuplicateError struct{ ID int64 }

func (e *PublicDuplicateError) Error() string {
	return fmt.Sprintf("the same image is public already as texture %d", e.ID)
}

func textureEvent(t *ent.Texture) hook.TextureEvent {
	return hook.TextureEvent{TextureID: t.ID, UploaderID: t.UploaderID, Hash: t.Hash, Type: string(t.Type),
		Public: t.Visibility == tex.VisibilityPublic}
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
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
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
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
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
