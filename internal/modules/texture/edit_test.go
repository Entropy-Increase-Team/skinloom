package texture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

func TestUpdateTexture(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		bob := f.user(t, "bob@example.com", 10000)
		owner, admin := Viewer{UserID: alice}, Viewer{UserID: bob, Admin: true}
		wide := f.upload(t, alice, "cape", 64, 32, true, 1) // 2:1: a cape or a legacy skin
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, wide.ID, "", paid)
		capeWearer := f.player(t, alice, "wearer", nil, &wide.ID)

		str := func(s string) *string { return &s }
		if _, err := f.s.Update(ctx, Viewer{UserID: bob}, wide.ID, Edit{Name: str("x")}, policy, paid, nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("not the uploader: %v", err)
		}
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Type: str("alex")}, policy, paid, nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("a 2:1 image cannot be slim (C08): %v", err)
		}
		up, err := f.s.Update(ctx, owner, wide.ID, Edit{Name: str("Old skin"), Type: str("steve")}, policy, paid, nil)
		if err != nil || up.Name != "Old skin" || up.Type != "steve" {
			t.Fatalf("rename and retype: %+v, %v", up, err)
		}
		if p := f.db.Player.GetX(ctx, capeWearer); p.CapeID != nil {
			t.Fatal("a texture that stops being a cape comes off as a cape")
		}

		// Private costs the price difference plus the upload award back; Bob's closet
		// loses it, so Bob's like award goes back too. Public returns the difference.
		kb := (wide.Size + 1023) / 1024
		diff := kb*(10-1) + 3
		before := f.score(t, alice)
		bobBefore := f.score(t, bob)
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Public: ptr(false)}, policy, paid, nil); err != nil {
			t.Fatal(err)
		}
		if got := f.score(t, alice); got != before-diff-2 {
			t.Fatalf("going private: %d, want %d", got, before-diff-2)
		}
		if ok, _ := f.s.InCloset(ctx, bob, wide.ID); ok || f.score(t, bob) != bobBefore+5 {
			t.Fatalf("other closets lose a texture that goes private, fee returned: bob %d", f.score(t, bob))
		}
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Public: ptr(true)}, policy, paid, nil); err != nil {
			t.Fatal(err)
		}
		if got := f.score(t, alice); got != before-2 {
			t.Fatalf("there and back again costs only the lost like award (C12): %d, want %d", got, before-2)
		}
		before -= 2
		// Administrators moderate without charging anyone.
		if _, err := f.s.Update(ctx, admin, wide.ID, Edit{Public: ptr(false)}, policy, paid, nil); err != nil || f.score(t, alice) != before {
			t.Fatalf("admin: %v, alice %d", err, f.score(t, alice))
		}

		// Two textures of one image cannot both be public.
		img := pngOf(t, 64, 64, 9)
		priv, _ := f.s.Upload(ctx, alice, UploadInput{Name: "priv", Type: "steve", Public: false, Data: img}, policy, paid)
		pub, _ := f.s.Upload(ctx, bob, UploadInput{Name: "pub", Type: "steve", Public: true, Data: img}, policy, paid)
		var dup *PublicDuplicateError
		if _, err := f.s.Update(ctx, owner, priv.Texture.ID, Edit{Public: ptr(true)}, policy, paid, nil); !errors.As(err, &dup) || dup.ID != pub.Texture.ID {
			t.Fatalf("public duplicate: %v", err)
		}
	})
}

func TestDeleteTexture(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		bob := f.user(t, "bob@example.com", 10000)
		img := pngOf(t, 64, 64, 4)
		// Bob's private copy first: a public texture of the image would make it a duplicate.
		theirs, err := f.s.Upload(ctx, bob, UploadInput{Name: "copy", Type: "steve", Public: false, Data: img}, policy, paid)
		if err != nil {
			t.Fatal(err)
		}
		mine, err := f.s.Upload(ctx, alice, UploadInput{Name: "mine", Type: "steve", Public: true, Data: img}, policy, paid)
		if err != nil || mine.Duplicate || mine.Texture.Hash != theirs.Texture.Hash {
			t.Fatalf("second upload of the image: %+v, %v", mine, err)
		}
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, mine.Texture.ID, "", paid)
		wearer := f.player(t, bob, "wearer", &mine.Texture.ID, nil)
		aliceBefore, bobBefore := f.score(t, alice), f.score(t, bob)

		if err := f.s.Delete(ctx, Viewer{UserID: bob}, mine.Texture.ID, true, nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("not the uploader: %v", err)
		}
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, mine.Texture.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		// Alice gets the upload back, net of its award, and loses Bob's like award;
		// Bob gets his closet fee back; Bob's player takes the skin off.
		if got := f.score(t, alice); got != aliceBefore+mine.Cost-2 {
			t.Fatalf("uploader: %d, want %d", got, aliceBefore+mine.Cost-2)
		}
		if got := f.score(t, bob); got != bobBefore+5 {
			t.Fatalf("liker: %d, want %d", got, bobBefore+5)
		}
		if p := f.db.Player.Query().Where(entplayer.ID(wearer)).OnlyX(ctx); p.SkinID != nil {
			t.Fatal("players wearing a deleted texture wear nothing")
		}
		// The file is shared with Bob's private copy: it stays.
		if b := f.db.Blob.GetX(ctx, mine.Texture.Hash); b.RefCount != 1 {
			t.Fatalf("shared blob: %d references", b.RefCount)
		}
		if n := f.db.Job.Query().Where(job.Kind(SweepJobKind)).CountX(ctx); n != 0 {
			t.Fatal("no sweep while another texture uses the file")
		}

		// The last reference: the row goes and the file is swept after the grace time.
		if err := f.s.Delete(ctx, Viewer{UserID: alice, Admin: true}, theirs.Texture.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		if f.db.Blob.Query().CountX(ctx) != 0 || f.db.Job.Query().Where(job.Kind(SweepJobKind)).CountX(ctx) != 1 {
			t.Fatal("the last reference deletes the row and schedules the sweep")
		}
		hash := mine.Texture.Hash
		if ok, _ := f.s.SweepFile(ctx, hash, time.Now()); ok {
			t.Fatal("swept before the grace time")
		}
		if ok, err := f.s.SweepFile(ctx, hash, time.Now().Add(SweepGrace+time.Minute)); !ok || err != nil {
			t.Fatalf("sweep after the grace time: %v, %v", ok, err)
		}
		if _, err := f.s.OpenHash(ctx, hash); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("file still there: %v", err)
		}
	})
}

// Deleting an account deletes its private textures, keeps its public ones without an
// uploader, and counts its closet out of the likes (the foreign key deletes the
// closet rows and used to leave the counts too high, Blessing Skin C04).
func TestBeforeUserDelete(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		bob := f.user(t, "bob@example.com", 10000)
		pub := f.upload(t, alice, "steve", 64, 64, true, 1)
		f.upload(t, alice, "steve", 64, 64, false, 2)
		ofBob := f.upload(t, bob, "steve", 64, 64, true, 3)
		if _, _, err := f.s.Favorite(ctx, Viewer{UserID: alice}, ofBob.ID, "", paid); err != nil {
			t.Fatal(err)
		}
		if got := f.db.Texture.GetX(ctx, ofBob.ID).Likes; got != 2 {
			t.Fatalf("likes %d", got)
		}
		f.deleteUser(t, alice)
		if f.db.Texture.Query().CountX(ctx) != 2 || f.db.Texture.GetX(ctx, pub.ID).UploaderID != nil {
			t.Fatal("the public texture stays, without an uploader; the private one goes")
		}
		if f.db.Blob.Query().CountX(ctx) != 2 || f.db.Job.Query().Where(job.Kind(SweepJobKind)).CountX(ctx) != 1 {
			t.Fatalf("the private texture's file is released: blobs %d", f.db.Blob.Query().CountX(ctx))
		}
		if got := f.db.Texture.GetX(ctx, ofBob.ID).Likes; got != 1 {
			t.Fatalf("likes after the account went: %d, want 1", got)
		}
		if got := f.db.Texture.GetX(ctx, pub.ID).Likes; got != 0 {
			t.Fatalf("the uploader's own closet item is gone too: likes %d", got)
		}
	})
}
