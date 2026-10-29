package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/image/webp"
)

var (
	red    = color.NRGBA{255, 0, 0, 255}
	green  = color.NRGBA{0, 255, 0, 255}
	blue   = color.NRGBA{0, 0, 255, 255}
	yellow = color.NRGBA{255, 255, 0, 255}
	clear  = color.NRGBA{}
)

func fill(img *image.NRGBA, u, x, y, w, h int, c color.NRGBA) {
	draw.Draw(img, image.Rect(x*u, y*u, (x+w)*u, (y+h)*u), image.NewUniform(c), image.Point{}, draw.Src)
}

func TestSnap(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 100}, {-5, 100}, {1, 16}, {20, 24}, {99, 100}, {112, 100}, {114, 128}, {9999, 512}} {
		if got := Snap(c.in, AvatarSizes, 100); got != c.want {
			t.Errorf("Snap(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNormalizeLegacy(t *testing.T) {
	skin := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	fill(skin, 1, 0, 0, 64, 32, green) // everything opaque, including the hat area
	skin.SetNRGBA(4, 20, red)          // right leg front, leftmost column
	skin.SetNRGBA(7, 20, blue)         // rightmost column
	skin.SetNRGBA(44, 20, yellow)      // right arm front
	out := Normalize(skin)
	if out.Bounds().Dy() != 64 {
		t.Fatalf("square: %v", out.Bounds())
	}
	if out.NRGBAAt(23, 52) != red || out.NRGBAAt(20, 52) != blue {
		t.Fatalf("the left leg mirrors the right one: %v %v", out.NRGBAAt(20, 52), out.NRGBAAt(23, 52))
	}
	if out.NRGBAAt(39, 52) != yellow {
		t.Fatalf("the left arm mirrors the right one: %v", out.NRGBAAt(39, 52))
	}
	if out.NRGBAAt(40, 8).A != 0 {
		t.Fatal("an entirely opaque hat area of an old skin counts as empty")
	}

	// A hat with any transparency is a real hat.
	skin.SetNRGBA(60, 2, clear)
	if Normalize(skin).NRGBAAt(40, 8) != green {
		t.Fatal("a hat with transparency is kept")
	}
}

func TestNormalizeMakesBaseOpaque(t *testing.T) {
	skin := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	if Normalize(skin).NRGBAAt(8, 8).A != 255 || Normalize(skin).NRGBAAt(40, 8).A != 0 {
		t.Fatal("the base layer is drawn opaque, the overlay keeps its transparency")
	}
}

// skin is a 64×64 skin (at u pixels per skin pixel) with a red face, a blue hat
// pixel, a green head top and a yellow left side.
func skin(u int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 64*u, 64*u))
	fill(img, u, 8, 8, 8, 8, red)
	fill(img, u, 8, 0, 8, 8, green)
	fill(img, u, 16, 8, 8, 8, yellow)
	fill(img, u, 40, 8, 1, 1, blue) // the hat's top-left pixel
	return Normalize(img)
}

func TestAvatar2D(t *testing.T) {
	for _, u := range []int{1, 2} {
		a := Avatar2D(skin(u), 16)
		if a.Bounds().Dx() != 16 || a.NRGBAAt(0, 0) != blue || a.NRGBAAt(1, 1) != blue || a.NRGBAAt(2, 2) != red || a.NRGBAAt(15, 15) != red {
			t.Fatalf("u=%d: %v %v %v", u, a.NRGBAAt(0, 0), a.NRGBAAt(2, 2), a.NRGBAAt(15, 15))
		}
	}
	if a := Avatar2D(skin(1), 100); a.Bounds().Dx() != 100 || a.NRGBAAt(99, 99) != red {
		t.Fatal("sizes that are not a multiple of 8")
	}
}

func TestAvatar3D(t *testing.T) {
	a := Avatar3D(skin(1), 170)
	shaded := func(c color.NRGBA, f float64) color.NRGBA { return shade(c, f) }
	if c := a.NRGBAAt(45, 110); c != shaded(red, 0.86) {
		t.Fatalf("the face, on the left: %v", c)
	}
	if c := a.NRGBAAt(125, 110); c != shaded(yellow, 0.7) {
		t.Fatalf("the left side, on the right: %v", c)
	}
	if c := a.NRGBAAt(85, 40); c != green {
		t.Fatalf("the top: %v", c)
	}
	if a.NRGBAAt(2, 2).A != 0 || a.NRGBAAt(167, 167).A != 0 {
		t.Fatal("the corners stay transparent")
	}
}

func TestSkinPreview(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	fill(img, 1, 8, 8, 8, 8, red)      // face
	fill(img, 1, 24, 8, 8, 8, blue)    // back of the head
	fill(img, 1, 44, 20, 4, 12, green) // right arm front
	s := Normalize(img)
	p := SkinPreview(s, false, 64) // 2 pixels per skin pixel
	if p.Bounds().Dx() != 72 || p.Bounds().Dy() != 64 {
		t.Fatalf("size %v", p.Bounds())
	}
	if p.NRGBAAt(8, 0) != red || p.NRGBAAt(40+8, 0) != blue {
		t.Fatalf("front face %v, back of head %v", p.NRGBAAt(8, 0), p.NRGBAAt(48, 0))
	}
	if p.NRGBAAt(0, 16) != green {
		t.Fatalf("the right arm is on the viewer's left from the front: %v", p.NRGBAAt(0, 16))
	}
	slim := SkinPreview(s, true, 64)
	if slim.NRGBAAt(0, 16).A != 0 || slim.NRGBAAt(2, 16) != green {
		t.Fatal("slim arms are 3 pixels wide")
	}
}

func TestCapePreview(t *testing.T) {
	cape := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	fill(cape, 1, 1, 1, 10, 16, red)
	p := CapePreview(cape, 32)
	if p.Bounds().Dx() != 20 || p.NRGBAAt(0, 0) != red || p.NRGBAAt(19, 31) != red {
		t.Fatalf("cape: %v %v", p.Bounds(), p.NRGBAAt(0, 0))
	}
}

func TestEncodeWebP(t *testing.T) {
	a := Avatar2D(skin(1), 32)
	data, err := Encode(a, WebP)
	if err != nil {
		t.Fatal(err)
	}
	back, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{0, 0}, {10, 10}, {31, 31}} {
		if r, g, b, al := back.At(p.X, p.Y).RGBA(); color.NRGBAModel.Convert(color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(al)}) != a.NRGBAAt(p.X, p.Y) {
			t.Fatalf("lossless at %v", p)
		}
	}
	if _, err := Encode(Placeholder(), PNG); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRendersOnceAndEvicts(t *testing.T) {
	s := NewService(2, 10)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			b, err := s.Get(context.Background(), "k", func() ([]byte, error) { calls.Add(1); return []byte("12345"), nil })
			if err != nil || string(b) != "12345" {
				t.Error(b, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("rendered %d times", calls.Load())
	}
	for i := range 3 { // 3 × 5 bytes over a 10-byte budget: the oldest goes
		_, _ = s.Get(context.Background(), fmt.Sprint(i), func() ([]byte, error) { return []byte("abcde"), nil })
	}
	if _, ok := s.lookup("k"); ok {
		t.Fatal("the least recently used result is evicted")
	}
	if _, ok := s.lookup("2"); !ok {
		t.Fatal("the newest stays")
	}
	if _, err := s.Get(context.Background(), "bad", func() ([]byte, error) { return nil, fmt.Errorf("broken") }); err == nil {
		t.Fatal("errors are returned, not cached")
	}
}
