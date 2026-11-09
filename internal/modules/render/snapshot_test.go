package render

import (
	"bytes"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var updateSnapshots = flag.Bool("update", false, "rewrite testdata/snapshots/*.png")

// refSkin is a reference skin of w×(w or w/2) pixels: every texel a different colour
// (so a part drawn from the wrong place, or turned, changes the picture) and the
// overlay layer half transparent in a pattern (so blending shows).
func refSkin(w int, legacy bool) *image.NRGBA {
	h := w
	if legacy {
		h = w / 2
	}
	u := w / 64
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			sx, sy := x/u, y/u
			img.SetNRGBA(x, y, color.NRGBA{uint8(sx * 4), uint8(sy * 4), uint8(40 + (sx/8+sy/8)%2*160 + (x%u+y%u)%2*20), 255})
		}
	}
	overlay := []image.Rectangle{image.Rect(32, 0, 64, 16)}
	if !legacy {
		overlay = append(overlay, image.Rect(0, 32, 56, 48), image.Rect(0, 48, 16, 64), image.Rect(48, 48, 64, 64))
	}
	for _, r := range overlay {
		for y := r.Min.Y * u; y < r.Max.Y*u; y++ {
			for x := r.Min.X * u; x < r.Max.X*u; x++ {
				a := uint8(0)
				if (x/u+y/u)%3 == 0 {
					a = 160
				}
				img.SetNRGBA(x, y, color.NRGBA{255 - uint8(x/u*4), 200, uint8(y / u * 4), a})
			}
		}
	}
	return img
}

func refCape() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 4), 90, uint8(y * 8), 255})
		}
	}
	return img
}

// TestRenderSnapshots (design 02 §6 item 5) renders reference skins (classic, slim,
// legacy 64×32, HD 128×128) and a cape and compares each picture with the reviewed
// one in testdata/snapshots: at most 0.5% of the pixels may differ (floating point may
// round an edge pixel differently on another CPU). go test -update rewrites them.
func TestRenderSnapshots(t *testing.T) {
	skins := map[string]*image.NRGBA{
		"classic": Normalize(refSkin(64, false)),
		"legacy":  Normalize(refSkin(64, true)),
		"hd":      Normalize(refSkin(128, false)),
	}
	shots := map[string]*image.NRGBA{"cape-preview": CapePreview(refCape(), 200)}
	for name, s := range skins {
		shots[name+"-avatar2d"] = Avatar2D(s, 100)
		shots[name+"-avatar3d"] = Avatar3D(s, 100)
		shots[name+"-preview"] = SkinPreview(s, false, 200)
	}
	shots["slim-preview"] = SkinPreview(skins["classic"], true, 200)

	dir := filepath.Join("testdata", "snapshots")
	for name, img := range shots {
		file := filepath.Join(dir, name+".png")
		if *updateSnapshots {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		f, err := os.Open(file)
		if err != nil {
			t.Fatalf("%v (go test -run TestRenderSnapshots -update creates it)", err)
		}
		want, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if diff := pixelDiff(img, want); diff > 0.005 {
			t.Errorf("%s: %.2f%% of the pixels differ from %s; look at both and run -update if the change is intended", name, diff*100, file)
		}
	}
}

// pixelDiff is the share of pixels that differ by more than 2 in any channel (1 when
// the sizes differ).
func pixelDiff(a, b image.Image) float64 {
	if a.Bounds() != b.Bounds() {
		return 1
	}
	r := a.Bounds()
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p, q := color.NRGBAModel.Convert(a.At(x, y)).(color.NRGBA), color.NRGBAModel.Convert(b.At(x, y)).(color.NRGBA)
			if far(p.R, q.R) || far(p.G, q.G) || far(p.B, q.B) || far(p.A, q.A) {
				n++
			}
		}
	}
	return float64(n) / float64(r.Dx()*r.Dy())
}

func far(a, b uint8) bool { return abs(int(a)-int(b)) > 2 }

// TestAvatar2DIsBlessingSkins: Blessing Skin's 2D avatar is the 8×8 face with the hat
// layer over it, resized; SkinLoom's is the same picture, scaled without smoothing. So
// at a multiple of the face's texel size every block must be exactly one texel of that
// composite.
func TestAvatar2DIsBlessingSkins(t *testing.T) {
	for _, w := range []int{64, 128} {
		skin := Normalize(refSkin(w, false))
		u := w / 64
		// The face at the skin's own resolution: 8u texels square.
		face := image.NewNRGBA(image.Rect(0, 0, 8*u, 8*u))
		for y := range 8 * u {
			for x := range 8 * u {
				face.SetNRGBA(x, y, skin.NRGBAAt(8*u+x, 8*u+y))
				blend(face, x, y, skin.NRGBAAt(40*u+x, 8*u+y))
			}
		}
		for _, size := range []int{16, 64, 128} {
			got := Avatar2D(skin, size)
			k := size / (8 * u)
			for y := range size {
				for x := range size {
					if g, want := got.NRGBAAt(x, y), face.NRGBAAt(x/k, y/k); g != want {
						t.Fatalf("%dpx skin, size %d: pixel %d,%d = %v, want %v", w, size, x, y, g, want)
					}
				}
			}
		}
	}
}

func TestFormat(t *testing.T) {
	for _, tc := range []struct {
		webp bool
		w, h int
		want string
	}{{true, 100, 100, WebP}, {true, 288, 256, WebP}, {true, 300, 300, WebP}, {true, 512, 512, PNG}, {true, 576, 512, PNG}, {false, 16, 16, PNG}} {
		if got := Format(tc.webp, tc.w, tc.h); got != tc.want {
			t.Errorf("Format(%v, %d, %d) = %s, want %s", tc.webp, tc.w, tc.h, got, tc.want)
		}
	}
}
