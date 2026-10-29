// Package render draws avatars (a skin's head, flat or as a cube) and previews (the
// skin seen from the front and the back, or a cape) for /avatar and /preview. All
// drawing is nearest-neighbour: skins are pixel art, and Blessing Skin's smoothed
// resize blurred them.
package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"github.com/HugoSmits86/nativewebp"
)

// AvatarSizes are the avatar sizes served (docs/design/02 §2.5); others snap to the
// nearest, so each texture has a bounded number of renders to cache.
var AvatarSizes = []int{16, 24, 32, 48, 64, 96, 100, 128, 256, 512}

// PreviewHeights are the preview heights served.
var PreviewHeights = []int{64, 100, 128, 150, 200, 256, 300, 400, 512}

// Snap returns the allowed value nearest to n (the larger one on a tie), or def for
// n <= 0.
func Snap(n int, allowed []int, def int) int {
	if n <= 0 {
		return def
	}
	best := allowed[0]
	for _, a := range allowed {
		if abs(a-n) < abs(best-n) || (abs(a-n) == abs(best-n) && a > best) {
			best = a
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Normalize returns a skin in the square 64×64 layout (at its own resolution). As in
// Minecraft, a legacy 64×32 skin gets left limbs mirrored from the right ones and an
// overlay area that is entirely opaque counts as empty (old skins filled it with a
// colour); the base layer is drawn opaque, the overlay with its transparency.
func Normalize(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w := b.Dx()
	u := w / 64
	if u < 1 {
		u = 1
	}
	legacy := b.Dy()*2 == w
	out := image.NewNRGBA(image.Rect(0, 0, w, w))
	draw.Draw(out, image.Rect(0, 0, w, b.Dy()), src, b.Min, draw.Src)
	if legacy {
		for _, c := range legacyCopies {
			copyMirrored(out, u, c[0], c[1], c[2], c[3], c[4], c[5])
		}
		clearIfOpaque(out, u, 32, 0, 64, 32)
	}
	for _, r := range [][4]int{{0, 0, 32, 16}, {0, 16, 64, 32}, {16, 48, 48, 64}} {
		setOpaque(out, u, r[0], r[1], r[2], r[3])
	}
	return out
}

// legacyCopies builds the left leg and arm of a 64×32 skin from the right ones, face
// by face and mirrored, exactly as Minecraft converts old skins: x, y, w, h of the
// source face and its destination x, y, in skin pixels.
var legacyCopies = [][6]int{
	{4, 16, 4, 4, 20, 48}, {8, 16, 4, 4, 24, 48}, // leg top, bottom
	{0, 20, 4, 12, 24, 52}, {4, 20, 4, 12, 20, 52}, {8, 20, 4, 12, 16, 52}, {12, 20, 4, 12, 28, 52},
	{44, 16, 4, 4, 36, 48}, {48, 16, 4, 4, 40, 48}, // arm top, bottom
	{40, 20, 4, 12, 40, 52}, {44, 20, 4, 12, 36, 52}, {48, 20, 4, 12, 32, 52}, {52, 20, 4, 12, 44, 52},
}

func copyMirrored(img *image.NRGBA, u, x, y, w, h, dx, dy int) {
	for j := 0; j < h*u; j++ {
		for i := 0; i < w*u; i++ {
			img.SetNRGBA(dx*u+w*u-1-i, dy*u+j, img.NRGBAAt(x*u+i, y*u+j))
		}
	}
}

func clearIfOpaque(img *image.NRGBA, u, x0, y0, x1, y1 int) {
	for y := y0 * u; y < y1*u; y++ {
		for x := x0 * u; x < x1*u; x++ {
			if img.NRGBAAt(x, y).A < 255 {
				return
			}
		}
	}
	draw.Draw(img, image.Rect(x0*u, y0*u, x1*u, y1*u), image.Transparent, image.Point{}, draw.Src)
}

func setOpaque(img *image.NRGBA, u, x0, y0, x1, y1 int) {
	for y := y0 * u; y < y1*u; y++ {
		for x := x0 * u; x < x1*u; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i+3] = 255
		}
	}
}

// part copies a w×h rectangle of the skin at (sx, sy) to (dx, dy) of a figure, all in
// skin pixels.
type part struct{ sx, sy, w, h, dx, dy int }

// limbs lists, for the front or the back, the base and overlay faces of a skin laid
// out as a 16×32 figure. From the front the player's right limbs are on the viewer's
// left; from the back, on the right. Slim (Alex) arms are 3 pixels wide.
func limbs(slim, back bool) (base, overlay []part) {
	a := 4
	if slim {
		a = 3
	}
	// Each face: base x, y and overlay x, y in the texture; w; figure x for the front
	// view and for the back view.
	type face struct{ bx, by, ox, oy, w, h, front, rear int }
	var faces []face
	if !back {
		faces = []face{
			{8, 8, 40, 8, 8, 8, 4, 0},         // head
			{20, 20, 20, 36, 8, 12, 4, 0},     // body
			{44, 20, 44, 36, a, 12, 4 - a, 0}, // right arm
			{36, 52, 52, 52, a, 12, 12, 0},    // left arm
			{4, 20, 4, 36, 4, 12, 4, 0},       // right leg
			{20, 52, 4, 52, 4, 12, 8, 0},      // left leg
		}
	} else {
		faces = []face{
			{24, 8, 56, 8, 8, 8, 0, 4},
			{32, 20, 32, 36, 8, 12, 0, 4},
			{48 + a, 20, 48 + a, 36, a, 12, 0, 12}, // right arm, seen from behind on the right
			{40 + a, 52, 56 + a, 52, a, 12, 0, 4 - a},
			{12, 20, 12, 36, 4, 12, 0, 8},
			{28, 52, 12, 52, 4, 12, 0, 4},
		}
	}
	ys := []int{0, 8, 8, 8, 20, 20}
	for i, f := range faces {
		dx := f.front
		if back {
			dx = f.rear
		}
		base = append(base, part{f.bx, f.by, f.w, f.h, dx, ys[i]})
		overlay = append(overlay, part{f.ox, f.oy, f.w, f.h, dx, ys[i]})
	}
	return base, overlay
}

// blit draws parts of src (u texture pixels per skin pixel) onto dst, scale output
// pixels per skin pixel, offset by ox output pixels; over blends by alpha.
func blit(dst, src *image.NRGBA, u int, parts []part, scale float64, ox int, over bool) {
	for _, p := range parts {
		x0, x1 := ox+int(math.Round(float64(p.dx)*scale)), ox+int(math.Round(float64(p.dx+p.w)*scale))
		y0, y1 := int(math.Round(float64(p.dy)*scale)), int(math.Round(float64(p.dy+p.h)*scale))
		for y := y0; y < y1; y++ {
			ty := p.sy*u + min(p.h*u-1, (y-y0)*p.h*u/max(1, y1-y0))
			for x := x0; x < x1; x++ {
				tx := p.sx*u + min(p.w*u-1, (x-x0)*p.w*u/max(1, x1-x0))
				c := src.NRGBAAt(tx, ty)
				if over {
					blend(dst, x, y, c)
				} else {
					dst.SetNRGBA(x, y, c)
				}
			}
		}
	}
}

// blend draws c over the pixel at x, y.
func blend(dst *image.NRGBA, x, y int, c color.NRGBA) {
	switch c.A {
	case 0:
		return
	case 255:
		dst.SetNRGBA(x, y, c)
		return
	}
	d := dst.NRGBAAt(x, y)
	a := float64(c.A) / 255
	da := float64(d.A) / 255 * (1 - a)
	oa := a + da
	mix := func(s, t uint8) uint8 { return uint8(math.Round((float64(s)*a + float64(t)*da) / oa)) }
	dst.SetNRGBA(x, y, color.NRGBA{mix(c.R, d.R), mix(c.G, d.G), mix(c.B, d.B), uint8(math.Round(oa * 255))})
}

func unit(skin *image.NRGBA) int { return max(1, skin.Bounds().Dx()/64) }

// Avatar2D draws the face (with the hat layer) as a size×size square.
func Avatar2D(skin *image.NRGBA, size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	u := unit(skin)
	scale := float64(size) / 8
	blit(out, skin, u, []part{{8, 8, 8, 8, 0, 0}}, scale, 0, false)
	blit(out, skin, u, []part{{40, 8, 8, 8, 0, 0}}, scale, 0, true)
	return out
}

// Avatar3D draws the head as a cube seen from above, in front and to its left: the
// top, the face and the left side, each with its hat layer, shaded so the edges read.
func Avatar3D(skin *image.NRGBA, size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	u := unit(skin)
	const c30, s30 = 0.8660254, 0.5
	// The cube spans 16·cos30 ≈ 13.9 by 16 cube units on screen; fit it with a margin.
	k := float64(size) / 17
	ox := (float64(size) - 16*c30*k) / 2
	oy := (float64(size)-16*k)/2 + 12*k // screen y of the cube's front-bottom-left corner
	// Screen position of cube point (x right, y up, z away from the viewer), in pixels.
	type face struct {
		origin, du, dv [2]float64 // screen origin and the screen steps of the face's u and v axes
		tex            func(a, b float64) (float64, float64)
		shade          float64
	}
	pt := func(x, y, z float64) [2]float64 {
		return [2]float64{ox + (x+z)*c30*k, oy + (-y+(x-z)*s30)*k}
	}
	sub := func(p, q [2]float64) [2]float64 { return [2]float64{p[0] - q[0], p[1] - q[1]} }
	faces := []face{
		{ // top (y = 8): u along x, v along z; texture 8..16 × 0..8, the front edge at the bottom
			pt(0, 8, 0), sub(pt(1, 8, 0), pt(0, 8, 0)), sub(pt(0, 8, 1), pt(0, 8, 0)),
			func(a, b float64) (float64, float64) { return 8 + a, 8 - b - 1e-9 }, 1.0, // b = 0 is row 7, not the face below
		},
		{ // front (z = 0): u along x, v down from the top; texture 8..16 × 8..16
			pt(0, 8, 0), sub(pt(1, 8, 0), pt(0, 8, 0)), sub(pt(0, 7, 0), pt(0, 8, 0)),
			func(a, b float64) (float64, float64) { return 8 + a, 8 + b }, 0.86,
		},
		{ // the head's left side (x = 8): u along z from the front edge; texture 16..24 × 8..16
			pt(8, 8, 0), sub(pt(8, 8, 1), pt(8, 8, 0)), sub(pt(8, 7, 0), pt(8, 8, 0)),
			func(a, b float64) (float64, float64) { return 16 + a, 8 + b }, 0.7,
		},
	}
	for py := range size {
		for px := range size {
			x, y := float64(px)+0.5, float64(py)+0.5
			for _, f := range faces {
				// Solve origin + a·du + b·dv = (x, y) for a, b in cube units.
				det := f.du[0]*f.dv[1] - f.du[1]*f.dv[0]
				rx, ry := x-f.origin[0], y-f.origin[1]
				a := (rx*f.dv[1] - ry*f.dv[0]) / det
				b := (f.du[0]*ry - f.du[1]*rx) / det
				if a < 0 || a >= 8 || b < 0 || b >= 8 {
					continue
				}
				tx, ty := f.tex(a, b)
				base := skin.NRGBAAt(int(tx*float64(u)), int(ty*float64(u)))
				hat := skin.NRGBAAt(int((tx+32)*float64(u)), int(ty*float64(u)))
				out.SetNRGBA(px, py, shade(base, f.shade))
				blend(out, px, py, shade(hat, f.shade))
			}
		}
	}
	return out
}

func shade(c color.NRGBA, f float64) color.NRGBA {
	return color.NRGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), c.A}
}

// SkinPreview draws the skin from the front and from the back, side by side, height
// pixels tall.
func SkinPreview(skin *image.NRGBA, slim bool, height int) *image.NRGBA {
	scale := float64(height) / 32
	gap := int(math.Round(4 * scale))
	fw := int(math.Round(16 * scale))
	out := image.NewNRGBA(image.Rect(0, 0, 2*fw+gap, height))
	u := unit(skin)
	for i, back := range []bool{false, true} {
		base, overlay := limbs(slim, back)
		blit(out, skin, u, base, scale, i*(fw+gap), false)
		blit(out, skin, u, overlay, scale, i*(fw+gap), true)
	}
	return out
}

// CapePreview draws the outer face of a cape (the side seen from behind), height
// pixels tall.
func CapePreview(cape image.Image, height int) *image.NRGBA {
	src := image.NewNRGBA(cape.Bounds())
	draw.Draw(src, src.Bounds(), cape, cape.Bounds().Min, draw.Src)
	scale := float64(height) / 16
	out := image.NewNRGBA(image.Rect(0, 0, int(math.Round(10*scale)), height))
	blit(out, src, unit(src), []part{{1, 1, 10, 16, 0, 0}}, scale, 0, false)
	return out
}

// Placeholder is a plain grey skin, drawn when there is no skin to draw: no texture,
// a missing file, or a skin the viewer may not see.
func Placeholder() *image.NRGBA {
	skin := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(skin, skin.Bounds(), image.NewUniform(color.NRGBA{0x9c, 0xa3, 0xaf, 0xff}), image.Point{}, draw.Src)
	draw.Draw(skin, image.Rect(32, 0, 64, 16), image.Transparent, image.Point{}, draw.Src) // no hat
	dark := image.NewUniform(color.NRGBA{0x4b, 0x55, 0x63, 0xff})
	draw.Draw(skin, image.Rect(8, 0, 16, 8), image.NewUniform(color.NRGBA{0x6b, 0x72, 0x80, 0xff}), image.Point{}, draw.Src)
	draw.Draw(skin, image.Rect(9, 12, 11, 13), dark, image.Point{}, draw.Src) // eyes
	draw.Draw(skin, image.Rect(13, 12, 15, 13), dark, image.Point{}, draw.Src)
	return skin
}

// Formats an image can be encoded in.
const (
	PNG  = "png"
	WebP = "webp"
)

// Encode encodes img as PNG or (lossless) WebP.
func Encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == WebP {
		err = nativewebp.Encode(&buf, img, nil)
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img)
	}
	return buf.Bytes(), err
}
