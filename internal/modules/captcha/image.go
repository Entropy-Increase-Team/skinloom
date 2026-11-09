package captcha

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand/v2"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
)

// Image size in pixels; the frontend shows it at this size (2x on HiDPI is fine).
const (
	imageW = 180
	imageH = 64
)

var faceOnce = sync.OnceValues(func() (font.Face, error) {
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: 36, DPI: 72, Hinting: font.HintingNone})
})

// render draws text as a PNG: each character rotated, scaled and placed with
// jitter, crossed by curves, the whole image bent by a sine wave, plus speckles.
// Visual randomness uses math/rand; the answer itself comes from crypto/rand.
func render(text string) ([]byte, error) {
	face, err := faceOnce()
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, imageW, imageH))
	bg := color.RGBA{uint8(225 + rand.IntN(30)), uint8(225 + rand.IntN(30)), uint8(225 + rand.IntN(30)), 255}
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	step := float64(imageW-24) / float64(len(text))
	for i, ch := range text {
		glyph := image.NewRGBA(image.Rect(0, 0, 44, 48))
		d := font.Drawer{Dst: glyph, Src: image.NewUniform(darkColor()), Face: face, Dot: fixed.P(8, 38)}
		d.DrawString(string(ch))

		angle := (rand.Float64() - 0.5) * 0.7 // about ±20°
		scale := 0.85 + rand.Float64()*0.3
		cx, cy := 22.0, 24.0
		px := 12 + step*(float64(i)+0.5) + (rand.Float64()-0.5)*6
		py := float64(imageH)/2 + (rand.Float64()-0.5)*10
		sin, cos := math.Sincos(angle)
		a, b, dd, e := scale*cos, -scale*sin, scale*sin, scale*cos
		m := f64.Aff3{a, b, px - a*cx - b*cy, dd, e, py - dd*cx - e*cy}
		draw.BiLinear.Transform(img, m, glyph, glyph.Bounds(), draw.Over, nil)
	}

	for range 2 + rand.IntN(2) { // interference curves through the text
		c := darkColor()
		amp, period, phase := 6+rand.Float64()*10, 60+rand.Float64()*80, rand.Float64()*2*math.Pi
		y0 := float64(imageH)/2 + (rand.Float64()-0.5)*20
		for x := 0; x < imageW; x++ {
			y := int(y0 + amp*math.Sin(2*math.Pi*float64(x)/period+phase))
			img.SetRGBA(x, y, c)
			img.SetRGBA(x, y+1, c)
		}
	}

	out := image.NewRGBA(img.Bounds()) // bend the whole picture
	amp, period, phase := 2+rand.Float64()*3, 40+rand.Float64()*40, rand.Float64()*2*math.Pi
	for x := 0; x < imageW; x++ {
		shift := int(amp * math.Sin(2*math.Pi*float64(x)/period+phase))
		for y := 0; y < imageH; y++ {
			sy := y + shift
			if sy < 0 || sy >= imageH {
				out.SetRGBA(x, y, bg)
				continue
			}
			out.SetRGBA(x, y, img.RGBAAt(x, sy))
		}
	}

	for range imageW * imageH / 25 { // speckles
		out.SetRGBA(rand.IntN(imageW), rand.IntN(imageH), darkColor())
	}

	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func darkColor() color.RGBA {
	return color.RGBA{uint8(rand.IntN(140)), uint8(rand.IntN(140)), uint8(rand.IntN(140)), 255}
}
