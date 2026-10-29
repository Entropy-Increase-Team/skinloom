package texture

import (
	"context"
	"errors"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func TestAvatar(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 1)
		secret := f.upload(t, alice, "steve", 64, 64, false, 2)
		cape := f.upload(t, alice, "cape", 64, 32, true, 3)
		asBob := Viewer{UserID: bob}

		if err := f.s.SetAvatar(ctx, asBob, cape.ID); !errors.Is(err, ErrNotSkin) {
			t.Fatalf("a cape: %v", err)
		}
		if err := f.s.SetAvatar(ctx, asBob, secret.ID); !errors.Is(err, ErrPrivate) {
			t.Fatalf("someone else's private skin: %v", err)
		}
		if err := f.s.SetAvatar(ctx, asBob, skin.ID); err != nil {
			t.Fatalf("a public skin (no need to own it, as in Blessing Skin): %v", err)
		}
		if a, err := f.s.Avatar(ctx, bob); err != nil || a == nil || a.ID != skin.ID {
			t.Fatalf("Avatar: %v, %v", a, err)
		}
		if err := f.s.SetAvatar(ctx, Viewer{UserID: alice}, skin.ID); err != nil {
			t.Fatal(err)
		}

		// The skin goes private: bob's avatar no longer shows it, alice's (hers) does.
		private := false
		if _, err := f.s.Update(ctx, Viewer{UserID: alice}, skin.ID, Edit{Public: &private}, policy, paid); err != nil {
			t.Fatal(err)
		}
		if a, _ := f.s.Avatar(ctx, bob); a != nil {
			t.Fatal("a private texture of someone else is not shown as an avatar")
		}
		if a, _ := f.s.Avatar(ctx, alice); a == nil {
			t.Fatal("the uploader still sees it")
		}

		// Deleting the texture clears every avatar that pointed at it.
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, skin.ID, true); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{alice, bob} {
			if u := f.db.User.GetX(ctx, id); u.AvatarTextureID != nil {
				t.Fatalf("user %d still points at the deleted texture", id)
			}
		}
		if err := f.s.SetAvatar(ctx, asBob, 0); err != nil {
			t.Fatalf("clearing: %v", err)
		}
		if _, err := f.s.ByHash(ctx, cape.Hash); err != nil {
			t.Fatalf("ByHash: %v", err)
		}
		if _, err := f.s.ByHash(ctx, skin.Hash); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByHash of a deleted texture: %v", err)
		}
	})
}

// A banned user's avatar is the placeholder, as a banned owner's player avatars are
// (ADR 0021): a ban often follows a report about the very skin they show.
func TestAvatarOfBannedUser(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 1)
		if err := f.s.SetAvatar(ctx, Viewer{UserID: alice}, skin.ID); err != nil {
			t.Fatal(err)
		}
		f.db.User.UpdateOneID(alice).SetStatus(user.StatusBanned).ExecX(ctx)
		if a, err := f.s.Avatar(ctx, alice); err != nil || a != nil {
			t.Fatalf("banned: %v, %v", a, err)
		}
		f.db.User.UpdateOneID(alice).SetStatus(user.StatusActive).ExecX(ctx)
		if a, err := f.s.Avatar(ctx, alice); err != nil || a == nil {
			t.Fatalf("unbanned: %v, %v", a, err)
		}
	})
}

// Every way a texture is deleted clears the avatars pointing at it (there is no foreign
// key to do it), including the private textures removed with an account.
func TestDeletePrivateOfClearsAvatars(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		admin := f.user(t, "admin@example.com", 1000)
		secret := f.upload(t, alice, "steve", 64, 64, false, 2)
		for _, v := range []Viewer{{UserID: alice}, {UserID: admin, Admin: true}} { // an administrator sees it
			if err := f.s.SetAvatar(ctx, v, secret.ID); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.s.DeletePrivateOf(ctx, alice); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{alice, admin} {
			if u := f.db.User.GetX(ctx, id); u.AvatarTextureID != nil {
				t.Fatalf("user %d still points at the deleted texture %d", id, *u.AvatarTextureID)
			}
		}
	})
}
