package texture

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

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
		got, err := f.s.Unfavorite(ctx, bob, skin.ID, true, nil)
		if err != nil || got != 5 || f.score(t, bob) != 1000 || f.score(t, alice) != aliceAfterUploads {
			t.Fatalf("Unfavorite: %d, %v; scores bob %d alice %d", got, err, f.score(t, bob), f.score(t, alice))
		}
		if p := f.db.Player.GetX(ctx, pl); p.SkinID != nil {
			t.Fatal("players only wear closet textures: the skin comes off")
		}
		if got := f.db.Texture.GetX(ctx, skin.ID).Likes; got != 1 {
			t.Fatalf("likes after unfavorite %d", got)
		}
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true, nil); !errors.Is(err, ErrNotInCloset) {
			t.Fatalf("twice: %v", err)
		}

		// An award the uploader has spent is not taken back below zero.
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid)
		f.db.User.UpdateOneID(alice).SetScore(0).ExecX(ctx)
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true, nil); err != nil || f.score(t, alice) != 0 {
			t.Fatalf("spent award: %v, alice %d", err, f.score(t, alice))
		}
		// Without refunds the fee stays spent.
		_, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid)
		before := f.score(t, bob)
		if got, _ := f.s.Unfavorite(ctx, bob, skin.ID, false, nil); got != 0 || f.score(t, bob) != before {
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
		if _, err := f.s.Unfavorite(ctx, bob, skin.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		bobBefore, aliceBefore := f.score(t, bob), f.score(t, alice)
		free := paid
		free.PerClosetItem, free.AwardPerLike = 0, 0
		for range 3 {
			if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", free); err != nil {
				t.Fatal(err)
			}
			if got, err := f.s.Unfavorite(ctx, bob, skin.ID, true, nil); err != nil || got != 0 {
				t.Fatalf("a free item refunds nothing: %d, %v", got, err)
			}
		}
		if f.score(t, bob) != bobBefore || f.score(t, alice) != aliceBefore {
			t.Fatalf("scores moved: bob %d → %d, alice %d → %d", bobBefore, f.score(t, bob), aliceBefore, f.score(t, alice))
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

		if _, err := f.s.GiveToCloset(ctx, bob, secret.ID, nil); len(validate.FieldErrors(err)) == 0 {
			t.Fatalf("someone else's private texture: %v", err)
		}
		if in, _ := f.s.InCloset(ctx, bob, secret.ID); in {
			t.Fatal("the private texture is in bob's closet")
		}
		if _, err := f.s.GiveToCloset(ctx, bob, skin.ID, nil); err != nil {
			t.Fatalf("a public texture: %v", err)
		}
		if f.score(t, bob) != 1000 || f.score(t, alice) != aliceBefore {
			t.Fatal("a gift costs nothing and earns nothing")
		}
		if _, err := f.s.GiveToCloset(ctx, bob, skin.ID, nil); !errors.Is(err, ErrInCloset) {
			t.Fatalf("twice: %v", err)
		}
		if _, err := f.s.GiveToCloset(ctx, bob, 999999, nil); !errors.Is(err, ErrNotFound) {
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
					if _, err := f.s.Unfavorite(ctx, side.who, side.tex, true, nil); err != nil {
						t.Error("unfavorite:", err)
						return
					}
				}
			})
		}
		wg.Wait()
	})
}
