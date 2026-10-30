package texture

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

func isDeadlock(err error) bool {
	s := fmt.Sprint(err)
	return strings.Contains(s, "deadlock") || strings.Contains(s, "40P01") || strings.Contains(s, "1213")
}

// "Make it private" sent several times at once (a double click) charges once. Update
// decided from a read made before its transaction, so every request charged the
// difference (2026-10-30 review: 6 requests, charged 6 times, on every database).
func TestGoPrivateConcurrentlyChargesOnce(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100000)
		for round := range 5 {
			skin := f.upload(t, alice, "steve", 64, 64, true, byte(50+round))
			before := f.score(t, alice)
			off := false
			var wg sync.WaitGroup
			for range 6 {
				wg.Go(func() {
					if _, err := f.s.Update(ctx, Viewer{UserID: alice}, skin.ID, Edit{Public: &off}, policy, paid, nil); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			diff := paid.UploadCost(skin.Size, false) - paid.UploadCost(skin.Size, true)
			entries := f.db.ScoreEntry.Query().Where(scoreentry.Reason(score.ReasonVisibility), scoreentry.RefID(fmt.Sprint(skin.ID))).CountX(ctx)
			if got := before - f.score(t, alice); got != diff || entries != 1 {
				t.Fatalf("round %d: charged %d in %d entries, want %d once", round, got, entries, diff)
			}
		}
	})
}

// Adding a texture races its uploader making it private: the texture must not end up
// in someone else's closet (the visibility was read before the transaction).
func TestFavoriteRacingGoPrivate(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100000)
		bob := f.user(t, "bob@example.com", 100000)
		for round := range 40 {
			skin := f.upload(t, alice, "steve", 64, 64, true, byte(100+round))
			off := false
			var wg sync.WaitGroup
			wg.Go(func() { _, _, _ = f.s.Favorite(ctx, Viewer{UserID: bob}, skin.ID, "", paid) })
			wg.Go(func() {
				if _, err := f.s.Update(ctx, Viewer{UserID: alice}, skin.ID, Edit{Public: &off}, policy, paid, nil); err != nil {
					t.Error(err)
				}
			})
			wg.Wait()
			if f.db.ClosetItem.Query().Where(closetitem.UserID(bob), closetitem.TextureID(skin.ID)).ExistX(ctx) {
				t.Fatalf("round %d: someone else's private texture is in bob's closet", round)
			}
		}
	})
}

// Deleting an uploader's account races someone adding one of their textures. The
// deletion locked the user and then (SET NULL) their textures, the reverse of a closet
// add; and the add read the uploader before its transaction, then failed with "user
// not found" awarding a deleted account (2026-10-30 review: 58 of 60 rounds).
func TestUserDeletionRacingFavorite(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		carol := f.user(t, "carol@example.com", 1000000)
		for i := range 40 {
			up := f.user(t, fmt.Sprintf("up%d@example.com", i), 100000)
			skin := f.upload(t, up, "steve", 64, 64, true, byte(i))
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Go(func() {
				errs <- withTx(ctx, f.db, func(tx *ent.Client) error {
					if err := f.s.BeforeUserDelete(ctx, tx, up); err != nil {
						return err
					}
					return tx.User.DeleteOneID(up).Exec(ctx)
				})
			})
			wg.Go(func() { _, _, err := f.s.Favorite(ctx, Viewer{UserID: carol}, skin.ID, "", paid); errs <- err })
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("round %d (deadlock: %v): %v", i, isDeadlock(err), err)
				}
			}
			if got := f.db.Texture.GetX(ctx, skin.ID).Likes; got != 1 {
				t.Fatalf("round %d: likes %d, want 1 (carol's; the uploader's closet went)", i, got)
			}
		}
	})
}

// Deleting a texture its uploader's player wears changes the player through ent, so
// players.updated_at (CSL's Last-Modified) moves; the foreign key's SET NULL left it,
// and /{name}.json kept answering 304 with the deleted texture.
func TestDeletionMovesWearersLastModified(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 9)
		pid := f.player(t, alice, "alice", &skin.ID, nil)
		old := time.Now().Add(-time.Hour)
		f.db.Player.UpdateOneID(pid).SetUpdatedAt(old).ExecX(ctx)
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, skin.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		p := f.db.Player.GetX(ctx, pid)
		if p.SkinID != nil || !p.UpdatedAt.After(old.Add(time.Minute)) {
			t.Fatalf("skin %v, updated_at %v (was %v)", p.SkinID, p.UpdatedAt, old)
		}
	})
}

// Changes publish their events in their transaction (the outbox), only while
// something listens, and the job delivers them.
func TestUploadPublishesEvents(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		f.upload(t, alice, "steve", 64, 64, true, 1)
		if n := f.db.Job.Query().Where(job.Kind(hook.OutboxJobKind)).CountX(ctx); n != 0 {
			t.Fatalf("no bus, no events: %d", n)
		}
		bus := hook.NewBus(quiet, 1)
		t.Cleanup(func() { bus.Close(ctx) })
		f.s.SetHooks(bus)
		got := make(chan hook.TextureEvent, 1)
		if err := bus.Replace("test", hook.OnTextureUploaded.Handle(0, func(_ context.Context, e hook.TextureEvent) error {
			got <- e
			return nil
		})); err != nil {
			t.Fatal(err)
		}
		skin := f.upload(t, alice, "alex", 64, 64, false, 2)
		jobs := f.db.Job.Query().Where(job.Kind(hook.OutboxJobKind)).AllX(ctx)
		if len(jobs) != 1 { // closet.added has no listener
			t.Fatalf("%d events stored, want 1", len(jobs))
		}
		if err := bus.Deliver(ctx, json.RawMessage(jobs[0].Payload)); err != nil {
			t.Fatal(err)
		}
		if e := <-got; e.TextureID != skin.ID || e.Type != "alex" || e.Public || e.UploaderID == nil || *e.UploaderID != alice {
			t.Fatalf("event %+v", e)
		}
	})
}

// Likes drift if any path ever deletes a closet item without counting it out (a race
// in an account deletion, data imported from Blessing Skin). The daily calibration
// sets them back to the closet count, one texture per transaction.
func TestCalibrateLikes(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		a := f.upload(t, alice, "steve", 64, 64, true, 1)
		b := f.upload(t, alice, "steve", 64, 64, true, 2)
		if _, _, err := f.s.Favorite(ctx, Viewer{UserID: bob}, b.ID, "", paid); err != nil {
			t.Fatal(err)
		}
		f.db.Texture.UpdateOneID(a.ID).SetLikes(7).ExecX(ctx) // drifted up
		f.db.Texture.UpdateOneID(b.ID).SetLikes(0).ExecX(ctx) // drifted down
		n, err := f.s.CalibrateLikes(ctx)
		if err != nil || n != 2 {
			t.Fatalf("calibrated %d, %v", n, err)
		}
		if got := f.db.Texture.GetX(ctx, a.ID).Likes; got != 1 {
			t.Fatalf("a: %d", got)
		}
		if got := f.db.Texture.GetX(ctx, b.ID).Likes; got != 2 {
			t.Fatalf("b: %d", got)
		}
		if n, err := f.s.CalibrateLikes(ctx); err != nil || n != 0 {
			t.Fatalf("second run: %d, %v", n, err)
		}
	})
}

// A texture worn by more players than a database takes parameters in one statement
// (SQLite 32766, PostgreSQL 65535) can still be deleted: the slots were cleared with
// an IN list of every player id.
func TestDeleteTextureWornByManyPlayers(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		if testing.Short() {
			t.Skip("creates 70000 players")
		}
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		skin := f.upload(t, alice, "steve", 64, 64, true, 3)
		const n = 70000
		for start := 0; start < n; start += 2000 {
			batch := make([]*ent.PlayerCreate, 0, 2000)
			for i := start; i < min(start+2000, n); i++ {
				name := fmt.Sprintf("p%d", i)
				batch = append(batch, f.db.Player.Create().SetUserID(alice).SetName(name).SetNameLower(name).
					SetUUID(fmt.Sprintf("%032x", i)).SetSkinID(skin.ID))
			}
			f.db.Player.CreateBulk(batch...).ExecX(ctx)
		}
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, skin.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		if left := f.db.Player.Query().Where(entplayer.SkinIDNotNil()).CountX(ctx); left != 0 {
			t.Fatalf("%d players still wear it", left)
		}
	})
}

// With a listener, every player a deletion undresses is published as player.updated
// (a CDN purge needs each one); without one, nothing is listed or stored.
func TestDeletionPublishesPlayerUpdates(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bus := hook.NewBus(quiet, 1)
		t.Cleanup(func() { bus.Close(ctx) })
		f.s.SetHooks(bus)
		got := map[int64]bool{}
		if err := bus.Replace("test", hook.OnPlayerUpdated.Handle(0, func(_ context.Context, e hook.PlayerEvent) error {
			got[e.PlayerID] = true
			return nil
		})); err != nil {
			t.Fatal(err)
		}
		skin := f.upload(t, alice, "steve", 64, 64, true, 4)
		one := f.player(t, alice, "one", &skin.ID, nil)
		two := f.player(t, alice, "two", &skin.ID, nil)
		if err := f.s.Delete(ctx, Viewer{UserID: alice}, skin.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		for _, j := range f.db.Job.Query().Where(job.Kind(hook.OutboxJobKind)).AllX(ctx) {
			if err := bus.Deliver(ctx, json.RawMessage(j.Payload)); err != nil {
				t.Fatal(err)
			}
		}
		if !got[one] || !got[two] || len(got) != 2 {
			t.Fatalf("player.updated for %v, want %d and %d", got, one, two)
		}
	})
}
