package texture

import (
	"context"
	"errors"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// ErrNotSkin refuses a cape where a skin is needed (an avatar is a skin's head).
var ErrNotSkin = errors.New("only a skin can be an avatar")

// SetAvatar makes a skin the viewer can see their avatar (Blessing Skin's "set as
// avatar"); 0 clears it.
func (s *Service) SetAvatar(ctx context.Context, v Viewer, id int64) error {
	if id == 0 {
		return s.db.User.UpdateOneID(v.UserID).ClearAvatarTextureID().Exec(ctx)
	}
	if _, err := s.Get(ctx, id, v); err != nil { // the usual not found / private answers
		return err
	}
	// The texture first, then the user (see score.Lock): a deletion clears the avatars
	// showing it under the texture's lock, and this must not set one in between.
	return withTx(ctx, s.db, func(tx *ent.Client) error {
		t, err := lockAndGet(ctx, tx, id)
		if err != nil {
			return err
		}
		if !v.MayView(t) {
			return ErrPrivate
		}
		if t.Type == tex.TypeCape {
			return ErrNotSkin
		}
		return tx.User.UpdateOneID(v.UserID).SetAvatarTextureID(id).Exec(ctx)
	})
}

// Avatar returns the skin whose head is a user's avatar, or nil: none set, gone,
// private to someone else (it went private after being chosen), so an avatar never
// shows what its owner could not see, or the user is banned or deleted (as a banned
// owner's players show the placeholder).
func (s *Service) Avatar(ctx context.Context, userID int64) (*ent.Texture, error) {
	u, err := s.db.User.Query().Where(user.ID(userID), user.StatusEQ(user.StatusActive)).
		Select(user.FieldAvatarTextureID).Only(ctx)
	if ent.IsNotFound(err) || (err == nil && u.AvatarTextureID == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := s.db.Texture.Get(ctx, *u.AvatarTextureID)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t.Visibility != tex.VisibilityPublic && (t.UploaderID == nil || *t.UploaderID != userID) {
		return nil, nil
	}
	return t, nil
}

// ByHash returns a texture with this file (the oldest), for the hash-addressed
// avatar and preview routes; ErrNotFound if none.
func (s *Service) ByHash(ctx context.Context, hash string) (*ent.Texture, error) {
	t, err := s.db.Texture.Query().Where(tex.Hash(hash)).Order(ent.Asc(tex.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return t, err
}
