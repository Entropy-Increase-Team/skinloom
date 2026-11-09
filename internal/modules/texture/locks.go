package texture

import (
	"context"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
)

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
