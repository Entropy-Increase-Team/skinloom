package render

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
)

const maxProviderRenderBytes = 4 << 20

var (
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	riffMagic = []byte("RIFF")
	webpMagic = []byte("WEBP")
)

// AvatarRequest describes an avatar render delegated to a custom RenderProvider.
type AvatarRequest struct {
	SkinPNG []byte
	Size    int
	Is3D    bool
	Format  string // "png" | "webp"
}

// PreviewRequest describes a skin or cape preview render delegated to a custom RenderProvider.
type PreviewRequest struct {
	TexturePNG  []byte
	TextureType string // "steve" | "alex" | "cape"
	Height      int
	Format      string // "png" | "webp"
}

// Provider is a hot-swappable 2D/3D avatar and texture preview renderer registered
// by an authorized native plugin with the "provider.render" permission.
type Provider interface {
	RenderAvatar(ctx context.Context, req AvatarRequest) ([]byte, error)
	RenderPreview(ctx context.Context, req PreviewRequest) ([]byte, error)
}

type renderProviderEntry struct {
	owner    string
	provider Provider
}

type renderProviderSlot struct {
	entry atomic.Pointer[renderProviderEntry]
}

// SetProvider atomically activates a custom RenderProvider under owner and clears
// cached renders so subsequent requests pick up the new renderer immediately.
func (s *Service) SetProvider(owner string, p Provider) {
	if p == nil {
		s.provider.entry.Store(nil)
	} else {
		s.provider.entry.Store(&renderProviderEntry{owner: owner, provider: p})
	}
	s.InvalidateCache()
}

// ClearProvider reverts to the built-in renderer if and only if owner currently
// holds the active RenderProvider slot.
func (s *Service) ClearProvider(owner string) {
	for {
		cur := s.provider.entry.Load()
		if cur == nil || cur.owner != owner {
			return
		}
		if s.provider.entry.CompareAndSwap(cur, nil) {
			s.InvalidateCache()
			return
		}
	}
}

// ProviderOwner returns the active custom RenderProvider owner, or "" for built-in.
func (s *Service) ProviderOwner() string {
	if cur := s.provider.entry.Load(); cur != nil {
		return cur.owner
	}
	return ""
}

// ProviderCacheTag returns "<owner>:g<generation>" when a custom RenderProvider is
// active (or "" for built-in), ensuring cache keys and ETags rotate on every
// provider hot-swap, reload, or explicit cache invalidation.
func (s *Service) ProviderCacheTag() string {
	if cur := s.provider.entry.Load(); cur != nil && cur.owner != "" {
		return fmt.Sprintf("%s:g%d", cur.owner, s.generation.Load())
	}
	return ""
}

// ActiveProvider returns the currently registered custom RenderProvider, or nil.
func (s *Service) ActiveProvider() Provider {
	if cur := s.provider.entry.Load(); cur != nil {
		return cur.provider
	}
	return nil
}

// InvalidateCache clears all cached render outputs and bumps the cache generation.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation.Add(1)
	s.items = map[string]*listElement{}
	s.order.Init()
	s.size = 0
}

func validProviderOutput(data []byte, format string) bool {
	if len(data) == 0 || len(data) > maxProviderRenderBytes {
		return false
	}
	switch format {
	case "webp":
		return len(data) >= 12 && bytes.Equal(data[:4], riffMagic) && bytes.Equal(data[8:12], webpMagic)
	case "", "png":
		if len(data) >= 8 && bytes.Equal(data[:8], pngMagic) {
			return true
		}
		if len(data) >= 12 && bytes.Equal(data[:4], riffMagic) && bytes.Equal(data[8:12], webpMagic) {
			return true
		}
		return false
	default:
		return false
	}
}

// RenderAvatarWithProvider delegates to the active RenderProvider if present,
// falling back to builtin if no provider is active or the provider returns an error.
func (s *Service) RenderAvatarWithProvider(
	ctx context.Context,
	req AvatarRequest,
	builtin func() ([]byte, error),
) ([]byte, error) {
	out, _, err := s.RenderAvatarWithProviderStatus(ctx, req, builtin)
	return out, err
}

// RenderAvatarWithProviderStatus behaves like RenderAvatarWithProvider and also
// reports whether the returned bytes are cacheable under the active provider key
// (false when a custom provider was active but failed and fell back to builtin).
func (s *Service) RenderAvatarWithProviderStatus(
	ctx context.Context,
	req AvatarRequest,
	builtin func() ([]byte, error),
) (data []byte, cacheable bool, err error) {
	if p := s.ActiveProvider(); p != nil && len(req.SkinPNG) > 0 {
		out, pErr := safeCallAvatarProvider(ctx, p, req)
		if pErr == nil && validProviderOutput(out, req.Format) {
			return out, true, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		b, bErr := builtin()
		return b, false, bErr
	}
	b, bErr := builtin()
	return b, true, bErr
}

// RenderPreviewWithProvider delegates to the active RenderProvider if present,
// falling back to builtin if no provider is active or the provider returns an error.
func (s *Service) RenderPreviewWithProvider(
	ctx context.Context,
	req PreviewRequest,
	builtin func() ([]byte, error),
) ([]byte, error) {
	out, _, err := s.RenderPreviewWithProviderStatus(ctx, req, builtin)
	return out, err
}

// RenderPreviewWithProviderStatus behaves like RenderPreviewWithProvider and also
// reports whether the returned bytes are cacheable under the active provider key.
func (s *Service) RenderPreviewWithProviderStatus(
	ctx context.Context,
	req PreviewRequest,
	builtin func() ([]byte, error),
) (data []byte, cacheable bool, err error) {
	if p := s.ActiveProvider(); p != nil && len(req.TexturePNG) > 0 {
		out, pErr := safeCallPreviewProvider(ctx, p, req)
		if pErr == nil && validProviderOutput(out, req.Format) {
			return out, true, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		b, bErr := builtin()
		return b, false, bErr
	}
	b, bErr := builtin()
	return b, true, bErr
}

func safeCallAvatarProvider(ctx context.Context, p Provider, req AvatarRequest) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("render provider panic: %v", r)
		}
	}()
	return p.RenderAvatar(ctx, req)
}

func safeCallPreviewProvider(ctx context.Context, p Provider, req PreviewRequest) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("render provider panic: %v", r)
		}
	}()
	return p.RenderPreview(ctx, req)
}
