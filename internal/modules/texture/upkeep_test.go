package texture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

// An upload of the same image between the last delete and the sweep keeps its file.
func TestSweepSparesReuploads(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)
		img := pngOf(t, 64, 64, 6)
		first, _ := f.s.Upload(ctx, alice, UploadInput{Name: "a", Type: "steve", Public: true, Data: img}, policy, paid)
		_ = f.s.Delete(ctx, Viewer{UserID: alice}, first.Texture.ID, true, nil)
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

func TestRescanMissingBlobs(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 10000)

		img := pngOf(t, 64, 64, 9)
		sum := sha256.Sum256(img)
		h1 := hex.EncodeToString(sum[:])
		h2 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

		// Simulate two blobs imported without files (width = 0).
		if err := d.Ent.Blob.Create().SetID(h1).SetSize(1024).SetWidth(0).SetHeight(0).SetRefCount(1).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if err := d.Ent.Texture.Create().SetID(101).SetName("Synced").SetType(tex.TypeSteve).SetHash(h1).SetSize(1024).SetUploaderID(alice).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if err := d.Ent.Blob.Create().SetID(h2).SetSize(2048).SetWidth(0).SetHeight(0).SetRefCount(1).Exec(ctx); err != nil {
			t.Fatal(err)
		}

		// Sync only h1 into storage.
		if err := f.store.Put(ctx, h1, img); err != nil {
			t.Fatal(err)
		}

		res, err := f.s.RescanMissingBlobs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Checked != 2 || res.Updated != 1 || res.Missing != 1 || res.Invalid != 0 {
			t.Fatalf("unexpected rescan result: %+v", res)
		}
		b1, err := d.Ent.Blob.Get(ctx, h1)
		if err != nil || b1.Width != 64 || b1.Height != 64 || b1.Size != int64(len(img)) {
			t.Fatalf("updated blob = %+v, err=%v", b1, err)
		}
		t1, err := d.Ent.Texture.Get(ctx, 101)
		if err != nil || t1.Size != int64(len(img)) {
			t.Fatalf("updated texture = %+v, err=%v", t1, err)
		}
	})
}
