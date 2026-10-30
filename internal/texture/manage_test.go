package texture

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// paid makes every fee and award visible in the scores.
var paid = score.Rates{PerStorage: 1, PrivatePerStorage: 10, PerClosetItem: 5, AwardPerTexture: 3, AwardPerLike: 2, ReturnOnDelete: true}

func (f *fixture) score(t *testing.T, id int64) int64 {
	t.Helper()
	return f.db.User.GetX(context.Background(), id).Score
}

func (f *fixture) upload(t *testing.T, owner int64, typ string, w, h int, public bool, seed byte) *ent.Texture {
	t.Helper()
	r, err := f.s.Upload(context.Background(), owner, UploadInput{Name: typ, Type: typ, Public: public, Data: pngOf(t, w, h, seed)}, policy, paid)
	if err != nil {
		t.Fatal(err)
	}
	return r.Texture
}

func (f *fixture) player(t *testing.T, owner int64, name string, skin, cape *int64) int64 {
	t.Helper()
	c := f.db.Player.Create().SetUserID(owner).SetName(name).SetNameLower(name).SetUUID(name + "00000000000000000000000000000000"[:32-len(name)])
	if skin != nil {
		c.SetSkinID(*skin)
	}
	if cape != nil {
		c.SetCapeID(*cape)
	}
	return c.SaveX(context.Background()).ID
}

func TestFavorites(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 1)
		secret := f.upload(t, alice, "steve", 64, 64, false, 2)
		aliceAfterUploads := f.score(t, alice)

		item, balance, err := f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid)
		if err != nil || item.Name != skin.Name || balance != 1000-5 {
			t.Fatalf("Favorite: %+v, %d, %v", item, balance, err)
		}
		if got := f.score(t, alice); got != aliceAfterUploads+2 {
			t.Fatalf("the uploader earns the like award: %d", got)
		}
		if got := f.db.Texture.GetX(ctx, skin.ID).Likes; got != 2 {
			t.Fatalf("likes %d", got)
		}
		if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "again", paid); !errors.Is(err, ErrInCloset) {
			t.Fatalf("twice: %v", err)
		}
		if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, secret.ID, "", paid); !errors.Is(err, ErrPrivate) {
			t.Fatalf("someone else's private texture: %v", err)
		}
		if _, err := f.s.RenameClosetItem(ctx, bob, skin.ID, "Mine now"); err != nil {
			t.Fatal(err)
		}

		// Unfavorite: the fee back, the award taken back, players reset.
		pl := f.player(t, bob, "bobby", &skin.ID, nil)
		got, err := f.s.Unfavorite(ctx, bob, skin.ID, true)
		if err != nil || got != 5 || f.score(t, bob) != 1000 || f.score(t, alice) != aliceAfterUploads {
			t.Fatalf("Unfavorite: %d, %v; scores bob %d alice %d", got, err, f.score(t, bob), f.score(t, alice))
		}
		if p := f.db.Player.GetX(ctx, pl); p.SkinID != nil {
			t.Fatal("players only wear closet textures: the skin comes off")
		}
		if got := f.db.Texture.GetX(ctx, skin.ID).Likes; got != 1 {
			t.Fatalf("likes after unfavorite %d", got)
		}
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true); !errors.Is(err, ErrNotInCloset) {
			t.Fatalf("twice: %v", err)
		}

		// An award the uploader has spent is not taken back below zero.
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid)
		f.db.User.UpdateOneID(alice).SetScore(0).ExecX(ctx)
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true); err != nil || f.score(t, alice) != 0 {
			t.Fatalf("spent award: %v, alice %d", err, f.score(t, alice))
		}
		// Without refunds the fee stays spent.
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid)
		before := f.score(t, bob)
		if got, _ := f.s.Unfavorite(ctx, bob, skin.ID, false); got != 0 || f.score(t, bob) != before {
			t.Fatalf("no refund: %d, bob %d", got, f.score(t, bob))
		}
	})
}

// Each closet item settles its own fee and award: once the site makes closets free,
// adding and removing a texture again must not refund (or take back) the fee or award
// of an earlier add, which was settled already. Keyed by texture and user, the old
// entries were found again and every free add and remove paid out once more.
func TestFavoriteRefundsOnlyItsOwnFee(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 1)
		if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true); err != nil {
			t.Fatal(err)
		}
		bobBefore, aliceBefore := f.score(t, bob), f.score(t, alice)
		free := paid
		free.PerClosetItem, free.AwardPerLike = 0, 0
		for range 3 {
			if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", free); err != nil {
				t.Fatal(err)
			}
			if got, err := f.s.Unfavorite(ctx, bob, skin.ID, true); err != nil || got != 0 {
				t.Fatalf("a free item refunds nothing: %d, %v", got, err)
			}
		}
		if f.score(t, bob) != bobBefore || f.score(t, alice) != aliceBefore {
			t.Fatalf("scores moved: bob %d → %d, alice %d → %d", bobBefore, f.score(t, bob), aliceBefore, f.score(t, alice))
		}
	})
}

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
		if _, err := f.s.Update(ctx, Viewer{UserID: bob}, wide.ID, Edit{Name: str("x")}, policy, paid); !errors.Is(err, ErrForbidden) {
			t.Fatalf("not the uploader: %v", err)
		}
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Type: str("alex")}, policy, paid); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("a 2:1 image cannot be slim (C08): %v", err)
		}
		up, err := f.s.Update(ctx, owner, wide.ID, Edit{Name: str("Old skin"), Type: str("steve")}, policy, paid)
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
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Public: ptr(false)}, policy, paid); err != nil {
			t.Fatal(err)
		}
		if got := f.score(t, alice); got != before-diff-2 {
			t.Fatalf("going private: %d, want %d", got, before-diff-2)
		}
		if ok, _ := f.s.InCloset(ctx, bob, wide.ID); ok || f.score(t, bob) != bobBefore+5 {
			t.Fatalf("other closets lose a texture that goes private, fee returned: bob %d", f.score(t, bob))
		}
		if _, err := f.s.Update(ctx, owner, wide.ID, Edit{Public: ptr(true)}, policy, paid); err != nil {
			t.Fatal(err)
		}
		if got := f.score(t, alice); got != before-2 {
			t.Fatalf("there and back again costs only the lost like award (C12): %d, want %d", got, before-2)
		}
		before -= 2
		// Administrators moderate without charging anyone.
		if _, err := f.s.Update(ctx, admin, wide.ID, Edit{Public: ptr(false)}, policy, paid); err != nil || f.score(t, alice) != before {
			t.Fatalf("admin: %v, alice %d", err, f.score(t, alice))
		}

		// Two textures of one image cannot both be public.
		img := pngOf(t, 64, 64, 9)
		priv, _ := f.s.Upload(ctx, alice, UploadInput{Name: "priv", Type: "steve", Public: false, Data: img}, policy, paid)
		pub, _ := f.s.Upload(ctx, bob, UploadInput{Name: "pub", Type: "steve", Public: true, Data: img}, policy, paid)
		var dup *PublicDuplicateError
		if _, err := f.s.Update(ctx, owner, priv.Texture.ID, Edit{Public: ptr(true)}, policy, paid); !errors.As(err, &dup) || dup.ID != pub.Texture.ID {
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

		if err := f.s.Delete(ctx, Viewer{UserID: bob}, mine.Texture.ID, true); !errors.Is(err, ErrForbidden) {
			t.Fatalf("not the uploader: %v", err)
		}
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, mine.Texture.ID, true); err != nil {
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
		if err := f.s.Delete(ctx, Viewer{UserID: alice, Admin: true}, theirs.Texture.ID, true); err != nil {
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

// An upload of the same image between the last delete and the sweep keeps its file.
func TestSweepSparesReuploads(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		img := pngOf(t, 64, 64, 6)
		first, _ := f.s.Upload(ctx, alice, UploadInput{Name: "a", Type: "steve", Public: true, Data: img}, policy, paid)
		_ = f.s.Delete(ctx, Viewer{UserID: alice}, first.Texture.ID, true)
		again, err := f.s.Upload(ctx, alice, UploadInput{Name: "b", Type: "steve", Public: true, Data: img}, policy, paid)
		if err != nil {
			t.Fatal(err)
		}
		if ok, _ := f.s.SweepFile(ctx, again.Texture.Hash, time.Now().Add(SweepGrace+time.Minute)); ok {
			t.Fatal("a file with a blob row again must stay")
		}
		// A stray file (an upload whose transaction failed) goes with the orphan sweep.
		stray := pngOf(t, 64, 64, 7)
		if _, err := f.s.checkImage(ctx, stray, "steve", MaxWidth); err != nil {
			t.Fatal(err)
		}
		chk, _ := f.s.checkImage(ctx, stray, "steve", MaxWidth)
		_ = f.store.Put(ctx, chk.hash, chk.png)
		n, err := f.s.SweepOrphans(ctx, time.Now().Add(SweepGrace+time.Minute))
		if err != nil || n != 1 {
			t.Fatalf("orphan sweep: %d, %v", n, err)
		}
		if file, err := f.s.OpenHash(ctx, again.Texture.Hash); err != nil {
			t.Fatalf("the used file must survive: %v", err)
		} else {
			file.Close()
		}
		if count, bytes, _ := f.s.Usage(ctx, alice); count != 1 || bytes != again.Texture.Size {
			t.Fatalf("usage %d, %d", count, bytes)
		}
	})
}

func ptr[T any](v T) *T { return &v }

func TestDeletePrivateOf(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		pub := f.upload(t, alice, "steve", 64, 64, true, 1)
		f.upload(t, alice, "steve", 64, 64, false, 2)
		if err := f.s.DeletePrivateOf(ctx, alice); err != nil {
			t.Fatal(err)
		}
		if f.db.Texture.Query().CountX(ctx) != 1 || f.db.Texture.Query().OnlyX(ctx).ID != pub.ID {
			t.Fatal("only the public texture stays")
		}
		if f.db.Blob.Query().CountX(ctx) != 1 || f.db.Job.Query().Where(job.Kind(SweepJobKind)).CountX(ctx) != 1 {
			t.Fatalf("the private texture's file is released: blobs %d", f.db.Blob.Query().CountX(ctx))
		}
	})
}

// An administrator's gift follows the closet rule that a private texture is only in
// its uploader's closet (going private drops it from every other one): otherwise the
// receiver could see it in their closet and put it on a player, whose profile shows
// it to everyone.
func TestGiveToClosetKeepsPrivateTexturesPrivate(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 1)
		secret := f.upload(t, alice, "steve", 64, 64, false, 2)
		aliceBefore := f.score(t, alice)

		if _, err := f.s.GiveToCloset(ctx, bob, secret.ID); len(validate.FieldErrors(err)) == 0 {
			t.Fatalf("someone else's private texture: %v", err)
		}
		if in, _ := f.s.InCloset(ctx, bob, secret.ID); in {
			t.Fatal("the private texture is in bob's closet")
		}
		if _, err := f.s.GiveToCloset(ctx, bob, skin.ID); err != nil {
			t.Fatalf("a public texture: %v", err)
		}
		if f.score(t, bob) != 1000 || f.score(t, alice) != aliceBefore {
			t.Fatal("a gift costs nothing and earns nothing")
		}
		if _, err := f.s.GiveToCloset(ctx, bob, skin.ID); !errors.Is(err, ErrInCloset) {
			t.Fatalf("twice: %v", err)
		}
		if _, err := f.s.GiveToCloset(ctx, bob, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("no such texture: %v", err)
		}
	})
}

// Uploads and closet additions that race on the same rows queue rather than fail
// (MySQL deadlocked on the user and texture rows; see score.Lock). The ledger and
// counters stay exact.
func TestConcurrentUploadsAndFavorites(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		var wg sync.WaitGroup
		for i := range 4 {
			wg.Go(func() {
				in := UploadInput{Name: "mine", Type: "steve", Public: true, Data: pngOf(t, 64, 64, byte(100+i))}
				if _, err := f.s.Upload(ctx, alice, in, policy, paid); err != nil {
					t.Error("upload:", err)
				}
			})
		}
		wg.Wait()
		if n := f.db.Texture.Query().CountX(ctx); n != 4 {
			t.Fatalf("%d textures", n)
		}
		afterUploads := f.score(t, alice)

		skin := f.db.Texture.Query().FirstX(ctx)
		var fans []int64
		for i := range 4 {
			fans = append(fans, f.user(t, fmt.Sprintf("fan%d@example.com", i), 1000))
		}
		for _, fan := range fans {
			wg.Go(func() {
				if _, _, err := f.s.Favorite(ctx, Viewer{UserID: fan}, skin.ID, "", paid); err != nil {
					t.Error("favorite:", err)
				}
			})
		}
		wg.Wait()
		if got := f.db.Texture.GetX(ctx, skin.ID); got.Likes != 1+4 {
			t.Fatalf("likes %d", got.Likes)
		}
		if got := f.score(t, alice); got != afterUploads+4*paid.AwardPerLike {
			t.Fatalf("uploader's awards: %d, want %d", got, afterUploads+4*paid.AwardPerLike)
		}
		for _, fan := range fans {
			if got := f.score(t, fan); got != 1000-paid.PerClosetItem {
				t.Fatalf("fan paid %d", 1000-got)
			}
		}
	})
}

// Two people adding (and removing) each other's textures at the same time: each
// change touches both scores (the fee and the uploader's award). Locked in opposite
// orders they deadlocked, on PostgreSQL too; score.Lock takes them in id order.
func TestMutualFavoritesConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		bob := f.user(t, "bob@example.com", 10000)
		ofAlice := f.upload(t, alice, "steve", 64, 64, true, 31)
		ofBob := f.upload(t, bob, "steve", 64, 64, true, 32)
		var wg sync.WaitGroup
		for _, side := range []struct{ who, tex int64 }{{alice, ofBob.ID}, {bob, ofAlice.ID}} {
			wg.Go(func() {
				for range 15 {
					if _, _, err := f.s.Favorite(ctx, Viewer{UserID: side.who}, side.tex, "", paid); err != nil {
						t.Error("favorite:", err)
						return
					}
					if _, err := f.s.Unfavorite(ctx, side.who, side.tex, true); err != nil {
						t.Error("unfavorite:", err)
						return
					}
				}
			})
		}
		wg.Wait()
	})
}
