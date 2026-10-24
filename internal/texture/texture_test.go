package texture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"regexp"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// pngOf draws a w×h image; seed changes the pixels (and so the hash).
func pngOf(t *testing.T, w, h int, seed byte) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x) + seed, G: byte(y), B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withChunk inserts an ancillary chunk right after IHDR.
func withChunk(p []byte, typ string, data []byte) []byte {
	const afterIHDR = 8 + 4 + 4 + 13 + 4
	var c bytes.Buffer
	_ = binary.Write(&c, binary.BigEndian, uint32(len(data)))
	c.WriteString(typ)
	c.Write(data)
	_ = binary.Write(&c, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	return append(append(append([]byte{}, p[:afterIHDR]...), c.Bytes()...), p[afterIHDR:]...)
}

// header is a PNG signature and IHDR claiming w×h, with no image data at all.
func header(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	b.WriteString("IHDR")
	b.Write(ihdr)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	return b.Bytes()
}

func TestCheckImage(t *testing.T) {
	s := NewService(nil, nil, quiet)
	ctx := context.Background()
	cases := []struct {
		w, h int
		typ  tex.Type
		ok   bool
	}{
		{64, 32, tex.TypeSteve, true}, {64, 64, tex.TypeSteve, true}, {128, 128, tex.TypeSteve, true},
		{64, 64, tex.TypeAlex, true}, {64, 32, tex.TypeAlex, false},
		{64, 32, tex.TypeCape, true}, {64, 64, tex.TypeCape, false},
		{100, 50, tex.TypeSteve, false}, {64, 96, tex.TypeSteve, false}, {2176, 2176, tex.TypeSteve, false},
	}
	for _, c := range cases {
		_, err := s.checkImage(ctx, pngOf(t, c.w, c.h, 0), c.typ, MaxWidth)
		if (err == nil) != c.ok {
			t.Errorf("%d×%d %s: %v", c.w, c.h, c.typ, err)
		}
		if err != nil && len(validate.FieldErrors(err)) != 1 {
			t.Errorf("%d×%d %s: want a field error, got %v", c.w, c.h, c.typ, err)
		}
	}
	if _, err := s.checkImage(ctx, []byte("GIF89a not a png"), tex.TypeSteve, MaxWidth); len(validate.FieldErrors(err)) != 1 {
		t.Fatalf("not a PNG: %v", err)
	}
	// A header claiming a huge image is refused from the header alone, before any
	// decoding (otherwise this would be a decompression bomb).
	if _, err := s.checkImage(ctx, header(65536, 32768), tex.TypeSteve, MaxWidth); err == nil || !bytes.Contains([]byte(err.Error()), []byte("wide")) {
		t.Fatalf("huge header: %v", err)
	}
	// Metadata is dropped; the hash names the re-encoded bytes.
	src := withChunk(pngOf(t, 64, 64, 1), "tEXt", []byte("Comment\x00taken at my home address"))
	got, err := s.checkImage(ctx, src, tex.TypeSteve, MaxWidth)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got.png, []byte("home address")) || got.width != 64 || got.height != 64 {
		t.Fatal("the stored image must not keep text chunks")
	}
	if _, err := png.Decode(bytes.NewReader(got.png)); err != nil {
		t.Fatalf("re-encoded image: %v", err)
	}
}

type fixture struct {
	s     *Service
	db    *ent.Client
	store *storage.Local
	dir   string
}

func newFixture(t *testing.T, d *db.DB) *fixture {
	dir := t.TempDir()
	store, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{s: NewService(d.Ent, store, quiet), db: d.Ent, store: store, dir: dir}
}

func (f *fixture) user(t *testing.T, email string, points int64) int64 {
	t.Helper()
	return f.db.User.Create().SetEmail(email).SetEmailNormalized(email).SetNickname(email[:1]).
		SetPasswordHash("x").SetScore(points).SaveX(context.Background()).ID
}

var policy = Policy{MaxBytes: 1 << 20, MaxWidth: MaxWidth, AllowDownload: true, PrivateStatus: 403}
var rates = score.Rates{PerStorage: 1, PrivatePerStorage: 10, PerClosetItem: 0, AwardPerTexture: 0}

func TestUpload(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		img := pngOf(t, 64, 64, 7)

		bad, err := f.s.Upload(ctx, alice, UploadInput{Name: " ", Type: "creeper", Data: img}, policy, rates)
		if len(validate.FieldErrors(err)) != 2 || bad != nil {
			t.Fatalf("bad name and type: %v", err)
		}
		res, err := f.s.Upload(ctx, alice, UploadInput{Name: "  Steve 皮肤  ", Type: "steve", Public: true, Data: img}, policy, rates)
		if err != nil {
			t.Fatal(err)
		}
		tx := res.Texture
		kb := (tx.Size + 1023) / 1024
		if res.Duplicate || tx.Name != "Steve 皮肤" || tx.Likes != 1 || res.Cost != kb || res.Balance != 1000-kb ||
			tx.Edges.Uploader == nil || tx.Edges.Uploader.ID != alice {
			t.Fatalf("upload %+v (cost %d, balance %d)", tx, res.Cost, res.Balance)
		}
		if e := f.db.ScoreEntry.Query().Where(scoreentry.UserID(alice)).OnlyX(ctx); e.Delta != -kb || e.RefType != "texture" || e.RefID != fmt.Sprint(tx.ID) {
			t.Fatalf("ledger %+v", e)
		}
		if ok, _ := f.s.InCloset(ctx, alice, tx.ID); !ok {
			t.Fatal("an upload goes into the uploader's closet")
		}
		file, err := f.s.OpenHash(ctx, tx.Hash)
		if err != nil {
			t.Fatal(err)
		}
		file.Close()

		// The same image again: the existing texture, nothing charged.
		again, err := f.s.Upload(ctx, alice, UploadInput{Name: "again", Type: "steve", Data: img}, policy, rates)
		if err != nil || !again.Duplicate || again.Texture.ID != tx.ID {
			t.Fatalf("duplicate: %+v, %v", again, err)
		}
		if n := f.db.Texture.Query().CountX(ctx); n != 1 {
			t.Fatalf("textures %d", n)
		}

		// A private image of Bob's that Alice uploads too: two textures, one file.
		secret := pngOf(t, 64, 32, 9)
		b1, err := f.s.Upload(ctx, bob, UploadInput{Name: "mine", Type: "steve", Data: secret}, policy, rates)
		if err != nil || b1.Cost != 10*((b1.Texture.Size+1023)/1024) {
			t.Fatalf("private upload costs the private rate: %+v, %v", b1, err)
		}
		a2, err := f.s.Upload(ctx, alice, UploadInput{Name: "copy", Type: "steve", Data: secret}, policy, rates)
		if err != nil || a2.Duplicate || a2.Texture.Hash != b1.Texture.Hash {
			t.Fatalf("someone else's private image is not a duplicate: %+v, %v", a2, err)
		}
		if b := f.db.Blob.GetX(ctx, b1.Texture.Hash); b.RefCount != 2 || b.Width != 64 || b.Height != 32 {
			t.Fatalf("shared blob %+v", b)
		}

		// Not enough score: nothing stored, nothing charged.
		poor := f.user(t, "poor@example.com", 0)
		if _, err := f.s.Upload(ctx, poor, UploadInput{Name: "x", Type: "cape", Data: pngOf(t, 64, 32, 42)}, policy, rates); !errors.Is(err, score.ErrInsufficient) {
			t.Fatalf("no score: %v", err)
		}
		entries, _ := os.ReadDir(f.dir)
		if len(entries) != 2 { // the public image and the shared private one
			t.Fatalf("files stored: %d", len(entries))
		}

		// Size and name rules from the settings.
		small := policy
		small.MaxBytes = 100
		if _, err := f.s.Upload(ctx, alice, UploadInput{Name: "x", Type: "steve", Data: img}, small, rates); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("too large: %v", err)
		}
		ruled := policy
		ruled.NameRule = regexp.MustCompile(`^[a-z]+$`)
		if _, err := f.s.Upload(ctx, alice, UploadInput{Name: "Bad Name", Type: "steve", Data: pngOf(t, 64, 64, 3)}, ruled, rates); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("name rule: %v", err)
		}
	})
}

// Regression: two concurrent uploads of the same new image by one user both passed
// the duplicate check, stored two textures and charged twice.
func TestUploadSameImageConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 1000)
		img := pngOf(t, 64, 64, 77)
		const n = 4
		results := make(chan *UploadResult, n)
		start := make(chan struct{})
		for range n {
			go func() {
				<-start
				r, err := f.s.Upload(ctx, alice, UploadInput{Name: "twice", Type: "steve", Public: true, Data: img}, policy, rates)
				if err != nil {
					t.Error(err)
				}
				results <- r
			}()
		}
		close(start)
		var id int64
		created := 0
		for range n {
			r := <-results
			if r == nil {
				continue
			}
			if id != 0 && r.Texture.ID != id {
				t.Fatalf("uploads of one image made textures %d and %d", id, r.Texture.ID)
			}
			id = r.Texture.ID
			if !r.Duplicate {
				created++
			}
		}
		u := f.db.User.GetX(ctx, alice)
		if created != 1 || f.db.Texture.Query().CountX(ctx) != 1 || f.db.ScoreEntry.Query().CountX(ctx) != 1 || u.Score != 1000-1 {
			t.Fatalf("%d created, %d textures, %d ledger entries, score %d", created,
				f.db.Texture.Query().CountX(ctx), f.db.ScoreEntry.Query().CountX(ctx), u.Score)
		}
		if b := f.db.Blob.GetX(ctx, f.db.Texture.Query().OnlyX(ctx).Hash); b.RefCount != 1 {
			t.Fatalf("the losing uploads must not count a reference: %d", b.RefCount)
		}
	})
}

func TestLibrary(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100000)
		bob := f.user(t, "bob@example.com", 100000)
		up := func(owner int64, name, typ string, public bool, seed byte) *ent.Texture {
			w, h := 64, 64
			if typ == "cape" {
				h = 32
			}
			r, err := f.s.Upload(ctx, owner, UploadInput{Name: name, Type: typ, Public: public, Data: pngOf(t, w, h, seed)}, policy, rates)
			if err != nil {
				t.Fatal(err)
			}
			return r.Texture
		}
		s1 := up(alice, "Red Steve", "steve", true, 1)
		a1 := up(alice, "Slim one", "alex", true, 2)
		c1 := up(bob, "Red cape", "cape", true, 3)
		p1 := up(bob, "Bob's secret", "steve", false, 4)
		f.db.Texture.UpdateOneID(c1.ID).SetLikes(5).ExecX(ctx)
		f.db.Texture.UpdateOneID(a1.ID).SetLikes(5).ExecX(ctx)

		ids := func(list []*ent.Texture) (out []int64) {
			for _, x := range list {
				out = append(out, x.ID)
			}
			return out
		}
		guest, bobView := Viewer{}, Viewer{UserID: bob}
		if list, _, _ := f.s.List(ctx, Filter{}, guest); fmt.Sprint(ids(list)) != fmt.Sprint([]int64{c1.ID, a1.ID, s1.ID}) {
			t.Fatalf("guests see public textures, newest first: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{}, bobView); len(list) != 3 {
			t.Fatalf("the library is public textures, also for their uploaders: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{UploaderID: bob}, bobView); fmt.Sprint(ids(list)) != fmt.Sprint([]int64{p1.ID, c1.ID}) {
			t.Fatalf("your own uploads include the private ones: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{UploaderID: bob}, Viewer{UserID: alice}); fmt.Sprint(ids(list)) != fmt.Sprint([]int64{c1.ID}) {
			t.Fatalf("someone else's uploads are only the public ones: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{UploaderID: bob}, Viewer{UserID: alice, Admin: true}); len(list) != 2 {
			t.Fatalf("administrators see a user's private uploads: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{Category: "skin"}, guest); fmt.Sprint(ids(list)) != fmt.Sprint([]int64{a1.ID, s1.ID}) {
			t.Fatalf("skins: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{Query: "red"}, guest); len(list) != 2 {
			t.Fatalf("name search: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{UploaderID: alice}, guest); len(list) != 2 {
			t.Fatalf("by uploader: %v", ids(list))
		}
		if list, _, _ := f.s.List(ctx, Filter{}, guest); list[0].Edges.Uploader == nil || list[0].Edges.Uploader.Nickname != "b" {
			t.Fatal("list items carry their uploader")
		}

		// Cursor paging, both orders, one item per page.
		walk := func(sort string) (out []int64) {
			cursor := ""
			for range 10 {
				list, next, err := f.s.List(ctx, Filter{Sort: sort, Cursor: cursor, Limit: 1}, guest)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, ids(list)...)
				if next == "" {
					return out
				}
				cursor = next
			}
			t.Fatal("paging does not end")
			return nil
		}
		if got := walk(SortTime); fmt.Sprint(got) != fmt.Sprint([]int64{c1.ID, a1.ID, s1.ID}) {
			t.Fatalf("paging by time: %v", got)
		}
		if got := walk(SortLikes); fmt.Sprint(got) != fmt.Sprint([]int64{c1.ID, a1.ID, s1.ID}) {
			t.Fatalf("paging by likes (ties by newest): %v", got)
		}
		if _, _, err := f.s.List(ctx, Filter{Sort: "password"}, guest); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("unknown sort (S07): %v", err)
		}
		if _, _, err := f.s.List(ctx, Filter{Sort: SortLikes, Cursor: "x"}, guest); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("bad cursor: %v", err)
		}

		// Private textures: the uploader and administrators only.
		if _, err := f.s.Get(ctx, p1.ID, guest); !errors.Is(err, ErrPrivate) {
			t.Fatalf("guest: %v", err)
		}
		if _, err := f.s.Get(ctx, p1.ID, Viewer{UserID: alice}); !errors.Is(err, ErrPrivate) {
			t.Fatalf("another user: %v", err)
		}
		for _, v := range []Viewer{bobView, {UserID: alice, Admin: true}} {
			if _, err := f.s.Get(ctx, p1.ID, v); err != nil {
				t.Fatalf("%+v: %v", v, err)
			}
		}
		if _, err := f.s.Get(ctx, 999999, guest); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		if _, file, err := f.s.Open(ctx, s1.ID, guest); err != nil {
			t.Fatal(err)
		} else {
			file.Close()
		}

		// Closets: newest first, by category and item name.
		items, next, err := f.s.Closet(ctx, alice, ClosetFilter{})
		if err != nil || len(items) != 2 || next != 0 || items[0].Edges.Texture.ID != a1.ID {
			t.Fatalf("closet %v, %v", items, err)
		}
		if items, _, _ := f.s.Closet(ctx, bob, ClosetFilter{Category: "cape"}); len(items) != 1 || items[0].TextureID != c1.ID {
			t.Fatalf("capes %v", items)
		}
		if items, _, _ := f.s.Closet(ctx, bob, ClosetFilter{Query: "SECRET"}); len(items) != 1 {
			t.Fatalf("closet search %v", items)
		}
		page, next, _ := f.s.Closet(ctx, alice, ClosetFilter{Limit: 1})
		rest, end, _ := f.s.Closet(ctx, alice, ClosetFilter{Limit: 1, Before: next})
		if len(page) != 1 || len(rest) != 1 || end != 0 || page[0].ID == rest[0].ID {
			t.Fatalf("closet paging: %v %v", page, rest)
		}
	})
}
