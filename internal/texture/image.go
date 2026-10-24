package texture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

// Hard limits, whatever the settings say. Every accepted shape has height ≤ width,
// so one upload decodes at most MaxWidth² pixels (16 MiB as 8-bit RGBA).
const (
	MaxWidth    = 2048
	MaxUploadKB = 8192
)

// checked is an accepted image, re-encoded.
type checked struct {
	png           []byte
	hash          string // SHA-256 of png, hex
	width, height int
}

// checkImage validates an uploaded PNG for the texture type and re-encodes it, which
// drops every ancillary chunk (metadata, text, animation). The header is read first,
// so a tiny file claiming huge dimensions is refused before anything is decoded
// (a decompression bomb).
func (s *Service) checkImage(ctx context.Context, data []byte, typ tex.Type, maxWidth int) (*checked, error) {
	bad := func(format string, a ...any) error {
		return &validate.FieldError{Field: "file", Message: fmt.Sprintf(format, a...)}
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, bad("not a PNG image")
	}
	w, h := cfg.Width, cfg.Height
	switch {
	case w > maxWidth:
		return nil, bad("at most %d pixels wide", maxWidth)
	case w == 0 || h == 0 || w%64 != 0 || h%32 != 0:
		return nil, bad("the width must be a multiple of 64 and the height of 32 (got %d×%d)", w, h)
	case typ == tex.TypeCape && w != 2*h:
		return nil, bad("a cape must be twice as wide as it is high (got %d×%d)", w, h)
	case typ == tex.TypeAlex && w != h:
		return nil, bad("a slim (Alex) skin must be square (got %d×%d)", w, h)
	case typ == tex.TypeSteve && w != h && w != 2*h:
		return nil, bad("a skin must be square or twice as wide as it is high (got %d×%d)", w, h)
	}

	select { // decoding costs memory: a few at a time
	case s.decoding <- struct{}{}:
		defer func() { <-s.decoding }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, bad("the PNG image is damaged")
	}
	switch img.(type) {
	case *image.NRGBA, *image.Paletted, *image.Gray, *image.RGBA:
	default:
		// 16-bit and other layouts become 8-bit RGBA, which every game client reads.
		n := image.NewNRGBA(img.Bounds())
		draw.Draw(n, n.Bounds(), img, img.Bounds().Min, draw.Src)
		img = n
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(buf.Bytes())
	return &checked{png: buf.Bytes(), hash: hex.EncodeToString(sum[:]), width: w, height: h}, nil
}
