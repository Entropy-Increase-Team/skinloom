package texture

import (
	"context"
	"testing"
	"time"

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
