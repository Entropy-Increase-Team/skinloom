// Package render draws avatars (a skin's head, flat or as a cube) and previews (the
// skin seen from the front and the back, or a cape) for /avatar and /preview. All
// drawing is nearest-neighbour: skins are pixel art, and Blessing Skin's smoothed
// resize blurred them.
package render

import (
	"bytes"
	"cmp"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"slices"

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

// SkinPreview draws the skin from an angled front view and an angled back view side by
// side (matching Blessing Skin's 3D oblique angle with nearest-neighbour pixel art),
// height pixels tall, within a 9:8 canvas.
func SkinPreview(skin *image.NRGBA, slim bool, height int) *image.NRGBA {
	w := int(math.Round(float64(height) * 9 / 8))
	out := image.NewNRGBA(image.Rect(0, 0, w, height))
	u := unit(skin)
	scale := float64(height) / 36.0
	cy := float64(height)/2.0 + 0.485*scale
	cxFront := 10.75 * scale
	cxBack := 29.75 * scale

	baseBoxes, overlayBoxes := playerBoxes(slim)
	const beta = 25 * math.Pi / 180
	renderAngledFigure(out, skin, u, baseBoxes, overlayBoxes, 45*math.Pi/180, beta, scale, cxFront, cy)
	renderAngledFigure(out, skin, u, baseBoxes, overlayBoxes, 225*math.Pi/180, beta, scale, cxBack, cy)
	return out
}

type vec3 struct{ x, y, z float64 }

func (v vec3) add(o vec3) vec3    { return vec3{v.x + o.x, v.y + o.y, v.z + o.z} }
func (v vec3) mul(s float64) vec3 { return vec3{v.x * s, v.y * s, v.z * s} }

type boxSpec struct {
	x0, x1, y0, y1, z0, z1 float64
	u, v, tw, th, td       int
}

func (b boxSpec) expand(d float64, u, v int) boxSpec {
	return boxSpec{b.x0 - d, b.x1 + d, b.y0 - d, b.y1 + d, b.z0 - d, b.z1 + d, u, v, b.tw, b.th, b.td}
}

func playerBoxes(slim bool) (base, overlay []boxSpec) {
	aw := 4
	if slim {
		aw = 3
	}
	faw := float64(aw)
	head := boxSpec{-4, 4, 24, 32, -4, 4, 0, 0, 8, 8, 8}
	torso := boxSpec{-4, 4, 12, 24, -2, 2, 16, 16, 8, 12, 4}
	rightArm := boxSpec{-4 - faw, -4, 12, 24, -2, 2, 40, 16, aw, 12, 4}
	leftArm := boxSpec{4, 4 + faw, 12, 24, -2, 2, 32, 48, aw, 12, 4}
	rightLeg := boxSpec{-4, 0, 0, 12, -2, 2, 0, 16, 4, 12, 4}
	leftLeg := boxSpec{0, 4, 0, 12, -2, 2, 16, 48, 4, 12, 4}

	base = []boxSpec{head, torso, rightArm, leftArm, rightLeg, leftLeg}
	overlay = []boxSpec{
		head.expand(0.5, 32, 0),
		torso.expand(0.25, 16, 32),
		rightArm.expand(0.25, 40, 32),
		leftArm.expand(0.25, 48, 48),
		rightLeg.expand(0.25, 0, 32),
		leftLeg.expand(0.25, 0, 48),
	}
	return base, overlay
}

type faceSpec struct {
	o, du, dv, normal vec3
	tx, ty, tw, th    int
	shade             float64
}

func boxFaces(b boxSpec) [6]faceSpec {
	dx, dy, dz := b.x1-b.x0, b.y1-b.y0, b.z1-b.z0
	return [6]faceSpec{
		{ // front (+Z)
			vec3{b.x0, b.y1, b.z1}, vec3{dx, 0, 0}, vec3{0, -dy, 0}, vec3{0, 0, 1},
			b.u + b.td, b.v + b.td, b.tw, b.th, 0.86,
		},
		{ // back (-Z)
			vec3{b.x1, b.y1, b.z0}, vec3{-dx, 0, 0}, vec3{0, -dy, 0}, vec3{0, 0, -1},
			b.u + b.tw + 2*b.td, b.v + b.td, b.tw, b.th, 0.86,
		},
		{ // left side of player (+X)
			vec3{b.x1, b.y1, b.z1}, vec3{0, 0, -dz}, vec3{0, -dy, 0}, vec3{1, 0, 0},
			b.u + b.tw + b.td, b.v + b.td, b.td, b.th, 0.70,
		},
		{ // right side of player (-X)
			vec3{b.x0, b.y1, b.z0}, vec3{0, 0, dz}, vec3{0, -dy, 0}, vec3{-1, 0, 0},
			b.u, b.v + b.td, b.td, b.th, 0.70,
		},
		{ // top (+Y)
			vec3{b.x0, b.y1, b.z0}, vec3{dx, 0, 0}, vec3{0, 0, dz}, vec3{0, 1, 0},
			b.u + b.td, b.v, b.tw, b.td, 1.0,
		},
		{ // bottom (-Y)
			vec3{b.x0, b.y0, b.z1}, vec3{dx, 0, 0}, vec3{0, 0, -dz}, vec3{0, -1, 0},
			b.u + b.tw + b.td, b.v, b.tw, b.td, 0.55,
		},
	}
}

type projectedQuad struct {
	ox, oy       float64
	dux, duy     float64
	dvx, dvy     float64
	z0, dzu, dzv float64
	centerZ      float64
	tx, ty       int
	tw, th       int
	shade        float64
	x0, y0       int
	x1, y1       int
}

func renderAngledFigure(dst, skin *image.NRGBA, u int, base, overlay []boxSpec, phi, beta, scale, cx, cy float64) {
	sinPhi, cosPhi := math.Sincos(phi)
	sinBeta, cosBeta := math.Sincos(beta)
	project := func(p vec3) (sx, sy, sz float64) {
		yModel := p.y - 16
		x1 := p.x*cosPhi - p.z*sinPhi
		z1 := p.x*sinPhi + p.z*cosPhi
		sx = cx + x1*scale
		sy = cy - (yModel*cosBeta-z1*sinBeta)*scale
		sz = yModel*sinBeta + z1*cosBeta
		return sx, sy, sz
	}

	bounds := dst.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	makeQuad := func(f faceSpec) (projectedQuad, bool) {
		n1z := f.normal.x*sinPhi + f.normal.z*cosPhi
		nCamZ := f.normal.y*sinBeta + n1z*cosBeta
		if nCamZ <= 1e-6 {
			return projectedQuad{}, false
		}
		ox, oy, z0 := project(f.o)
		pxU, pyU, zU := project(f.o.add(f.du))
		pxV, pyV, zV := project(f.o.add(f.dv))
		dux, duy, dzu := pxU-ox, pyU-oy, zU-z0
		dvx, dvy, dzv := pxV-ox, pyV-oy, zV-z0
		det := dux*dvy - duy*dvx
		if math.Abs(det) < 1e-6 {
			return projectedQuad{}, false
		}
		pxUV, pyUV, _ := project(f.o.add(f.du).add(f.dv))
		minX := min(min(ox, pxU), min(pxV, pxUV))
		maxX := max(max(ox, pxU), max(pxV, pxUV))
		minY := min(min(oy, pyU), min(pyV, pyUV))
		maxY := max(max(oy, pyU), max(pyV, pyUV))
		x0 := max(0, int(math.Floor(minX)))
		x1 := min(w-1, int(math.Ceil(maxX)))
		y0 := max(0, int(math.Floor(minY)))
		y1 := min(h-1, int(math.Ceil(maxY)))
		if x0 > x1 || y0 > y1 {
			return projectedQuad{}, false
		}
		_, _, cz := project(f.o.add(f.du.mul(0.5)).add(f.dv.mul(0.5)))
		return projectedQuad{
			ox, oy, dux, duy, dvx, dvy, z0, dzu, dzv, cz,
			f.tx, f.ty, f.tw, f.th, f.shade, x0, y0, x1, y1,
		}, true
	}

	zbuf := make([]float64, w*h)
	for i := range zbuf {
		zbuf[i] = math.Inf(-1)
	}
	sample := func(q projectedQuad, a, b float64) color.NRGBA {
		iu := min(q.tw*u-1, max(0, int(a*float64(q.tw*u))))
		iv := min(q.th*u-1, max(0, int(b*float64(q.th*u))))
		return skin.NRGBAAt(q.tx*u+iu, q.ty*u+iv)
	}
	rasterize := func(q projectedQuad, isOverlay bool) {
		det := q.dux*q.dvy - q.duy*q.dvx
		for py := q.y0; py <= q.y1; py++ {
			ry := float64(py) + 0.5 - q.oy
			rowOff := py * w
			for px := q.x0; px <= q.x1; px++ {
				rx := float64(px) + 0.5 - q.ox
				a := (rx*q.dvy - ry*q.dvx) / det
				b := (q.dux*ry - q.duy*rx) / det
				if a < -1e-5 || a >= 1+1e-5 || b < -1e-5 || b >= 1+1e-5 {
					continue
				}
				z := q.z0 + a*q.dzu + b*q.dzv
				idx := rowOff + px
				if !isOverlay {
					if z > zbuf[idx] {
						zbuf[idx] = z
						dst.SetNRGBA(px, py, shade(sample(q, a, b), q.shade))
					}
					continue
				}
				if z < zbuf[idx]-1e-4 {
					continue
				}
				c := sample(q, a, b)
				if c.A == 0 {
					continue
				}
				blend(dst, px, py, shade(c, q.shade))
			}
		}
	}

	for _, box := range base {
		for _, f := range boxFaces(box) {
			if q, ok := makeQuad(f); ok {
				rasterize(q, false)
			}
		}
	}
	var ovQuads []projectedQuad
	for _, box := range overlay {
		for _, f := range boxFaces(box) {
			if q, ok := makeQuad(f); ok {
				ovQuads = append(ovQuads, q)
			}
		}
	}
	slices.SortFunc(ovQuads, func(a, b projectedQuad) int {
		return cmp.Compare(a.centerZ, b.centerZ)
	})
	for _, q := range ovQuads {
		rasterize(q, true)
	}
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

// WebPMaxPixels is the largest picture served as WebP: the pure-Go encoder takes about
// 15 ms for 256×256 but 55 ms for 512×512, against PNG's 6 to 15 ms, and a render that
// misses the cache should take under 50 ms (design 04 §5.4). Larger pictures, rarely
// asked for, are PNG (about twice the bytes).
const WebPMaxPixels = 300 * 300

// Format is the format to encode a w×h picture in when the client takes WebP.
func Format(webp bool, w, h int) string {
	if webp && w*h <= WebPMaxPixels {
		return WebP
	}
	return PNG
}

// Encode encodes img as PNG or (lossless) WebP.
func Encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == WebP {
		// The fastest level is also the smallest above about 128×128 for nearest-neighbour
		// pixel art (long runs of one colour); below, the default is a little smaller.
		lvl := nativewebp.DefaultCompression
		if b := img.Bounds(); b.Dx()*b.Dy() > 128*128 {
			lvl = nativewebp.BestSpeed
		}
		err = nativewebp.Encode(&buf, img, &nativewebp.Options{CompressionLevel: lvl})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img)
	}
	return buf.Bytes(), err
}
