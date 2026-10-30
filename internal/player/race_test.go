package player

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

func withTextures(t *testing.T, d *db.DB) (fixture, *texture.Service) {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return fixture{s: NewService(d.Ent, quiet), db: d.Ent}, texture.NewService(d.Ent, store, quiet)
}

func isDeadlock(err error) bool {
	s := fmt.Sprint(err)
	return strings.Contains(s, "deadlock") || strings.Contains(s, "40P01") || strings.Contains(s, "1213")
}

// Putting a texture on races taking it out of the closet. Wear checked the closet
// outside any transaction, so the texture could leave in between and stay on the
// player (2026-10-30 review: 149 of 150 rounds on SQLite, 12 on PostgreSQL); if it
// then went private, someone else's private texture showed in /{name}.json.
func TestWearRacingClosetRemoval(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f, textures := withTextures(t, d)
		alice := f.user(t, "alice@example.com", 0)
		bob := f.user(t, "bob@example.com", 1000)
		skin := f.texture(t, alice, "steve", strings.Repeat("a", 64), false)
		f.db.Texture.UpdateOneID(skin).SetVisibility("public").ExecX(ctx)
		p, _, err := f.s.Create(ctx, bob, "bobby", official, rates)
		if err != nil {
			t.Fatal(err)
		}
		for round := range 100 {
			if _, _, err := textures.Favorite(ctx, texture.Viewer{UserID: bob}, skin, "", rates); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			wg.Go(func() { _, _ = f.s.Wear(ctx, bob, p.ID, &skin, nil, nil) })
			wg.Go(func() { _, _ = textures.Unfavorite(ctx, bob, skin, true, nil) })
			wg.Wait()
			worn := f.db.Player.GetX(ctx, p.ID).SkinID != nil
			kept := f.db.ClosetItem.Query().Where(closetitem.UserID(bob), closetitem.TextureID(skin)).ExistX(ctx)
			if worn && !kept {
				t.Fatalf("round %d: the player wears a texture that left the closet", round)
			}
			f.db.Player.UpdateOneID(p.ID).ClearSkinID().ExecX(ctx)
			if kept {
				if _, err := textures.Unfavorite(ctx, bob, skin, true, nil); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}

// Deleting a player races taking the skin it wears out of the closet. Delete removed
// the player row before locking the owner it refunds; the closet removal locks the
// texture, the owner, then the owner's players: they deadlocked (PostgreSQL 59 of 60
// rounds before the fix).
func TestDeleteRacingClosetRemoval(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f, textures := withTextures(t, d)
		alice := f.user(t, "alice@example.com", 0)
		bob := f.user(t, "bob@example.com", 100000)
		skin := f.texture(t, alice, "steve", strings.Repeat("b", 64), false)
		f.db.Texture.UpdateOneID(skin).SetVisibility("public").ExecX(ctx)
		for i := range 40 {
			p, _, err := f.s.Create(ctx, bob, fmt.Sprintf("bob%d", i), official, rates)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := textures.Favorite(ctx, texture.Viewer{UserID: bob}, skin, "", rates); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.Wear(ctx, bob, p.ID, &skin, nil, nil); err != nil {
				t.Fatal(err)
			}
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Go(func() { _, err := f.s.Delete(ctx, bob, p.ID, true, nil); errs <- err })
			wg.Go(func() { _, err := textures.Unfavorite(ctx, bob, skin, true, nil); errs <- err })
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("round %d (deadlock: %v): %v", i, isDeadlock(err), err)
				}
			}
		}
		if got := f.db.User.GetX(ctx, bob).Score; got != 100000 {
			t.Fatalf("every player creation was refunded: score %d", got)
		}
	})
}

// An administrator's rename and transfer are one change: a failed transfer leaves the
// name as it was (they used to be two transactions).
func TestEditAllOrNothing(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 1000)
		p, _, err := f.s.Create(ctx, alice, "Steve", official, rates)
		if err != nil {
			t.Fatal(err)
		}
		name, nobody := "Alex", int64(999999)
		if _, err := f.s.Edit(ctx, p.ID, Change{Name: &name, NewOwner: &nobody}, official, nil); err == nil {
			t.Fatal("a transfer to nobody must fail")
		}
		if got := f.db.Player.GetX(ctx, p.ID); got.Name != "Steve" || got.UserID != alice {
			t.Fatalf("half applied: %+v", got)
		}
		bob := f.user(t, "bob@example.com", 0)
		got, err := f.s.Edit(ctx, p.ID, Change{Owner: alice, Name: &name, NewOwner: &bob}, official, nil)
		if err != nil || got.Name != "Alex" || got.UserID != bob || got.UUID != OfflineUUID("Alex") {
			t.Fatalf("edit: %+v, %v", got, err)
		}
		if _, err := f.s.Edit(ctx, p.ID, Change{Owner: alice, Name: &name}, official, nil); err != ErrNotFound {
			t.Fatalf("the caller checked the old owner: %v", err)
		}
	})
}
