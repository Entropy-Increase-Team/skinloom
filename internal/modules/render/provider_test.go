package render

import (
	"context"
	"errors"
	"testing"
)

type stubRenderProvider struct {
	avatarOut  []byte
	previewOut []byte
	err        error
}

func (s *stubRenderProvider) RenderAvatar(_ context.Context, _ AvatarRequest) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.avatarOut, nil
}

func (s *stubRenderProvider) RenderPreview(_ context.Context, _ PreviewRequest) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.previewOut, nil
}

func TestRenderServiceProviderHotSwapAndFallback(t *testing.T) {
	ctx := context.Background()
	svc := NewService(2, 1<<20)

	// Populate cache first; installing a provider must invalidate it.
	_, err := svc.Get(ctx, "k1", func() ([]byte, error) { return []byte("cached"), nil })
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := svc.lookup("k1"); !ok {
		t.Fatalf("expected k1 in cache")
	}

	validPNG := append([]byte("\x89PNG\r\n\x1a\n"), []byte("custom-png-payload")...)
	validWebP := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("custom-webp-payload")...)
	p1 := &stubRenderProvider{avatarOut: validPNG, previewOut: validWebP}
	tag0 := svc.ProviderCacheTag()
	svc.SetProvider("plugin:ren-a", p1)
	tag1 := svc.ProviderCacheTag()
	if tag0 == tag1 {
		t.Fatalf("expected ProviderCacheTag to rotate on SetProvider: %q == %q", tag0, tag1)
	}
	if _, ok := svc.lookup("k1"); ok {
		t.Fatalf("expected cache invalidated after SetProvider")
	}
	if svc.ProviderOwner() != "plugin:ren-a" {
		t.Fatalf("ProviderOwner = %q", svc.ProviderOwner())
	}

	gotAv, err := svc.RenderAvatarWithProvider(ctx, AvatarRequest{SkinPNG: []byte("png"), Size: 64, Format: PNG}, func() ([]byte, error) {
		return []byte("builtin-avatar"), nil
	})
	if err != nil || string(gotAv) != string(validPNG) {
		t.Fatalf("RenderAvatarWithProvider = (%q, %v)", gotAv, err)
	}

	gotPr, err := svc.RenderPreviewWithProvider(ctx, PreviewRequest{TexturePNG: []byte("png"), TextureType: "steve", Height: 200, Format: WebP}, func() ([]byte, error) {
		return []byte("builtin-preview"), nil
	})
	if err != nil || string(gotPr) != string(validWebP) {
		t.Fatalf("RenderPreviewWithProvider = (%q, %v)", gotPr, err)
	}

	// Invalid image header from provider must fall back to built-in and mark cacheable=false.
	svc.SetProvider("plugin:ren-bad-bytes", &stubRenderProvider{avatarOut: []byte("not-a-png")})
	gotBadBytes, cacheable, err := svc.GetWithCacheable(ctx, "fallback-key", func() ([]byte, bool, error) {
		return svc.RenderAvatarWithProviderStatus(ctx, AvatarRequest{SkinPNG: []byte("png"), Format: PNG}, func() ([]byte, error) {
			return []byte("builtin-avatar"), nil
		})
	})
	if err != nil || string(gotBadBytes) != "builtin-avatar" || cacheable {
		t.Fatalf("expected non-cacheable builtin fallback on bad magic bytes, got (%q, cacheable=%v, err=%v)", gotBadBytes, cacheable, err)
	}
	if _, ok := svc.lookup("fallback-key"); ok {
		t.Fatalf("fallback render must not be stored in LRU cache")
	}

	// On provider error, gracefully fall back to built-in.
	svc.SetProvider("plugin:ren-b", &stubRenderProvider{err: errors.New("boom")})
	gotFb, err := svc.RenderAvatarWithProvider(ctx, AvatarRequest{SkinPNG: []byte("png")}, func() ([]byte, error) {
		return []byte("builtin-avatar"), nil
	})
	if err != nil || string(gotFb) != "builtin-avatar" {
		t.Fatalf("expected builtin fallback on error, got (%q, %v)", gotFb, err)
	}

	// Stale owner clear does not remove plugin:ren-b.
	svc.ClearProvider("plugin:ren-a")
	if svc.ProviderOwner() != "plugin:ren-b" {
		t.Fatalf("stale ClearProvider removed active provider: %q", svc.ProviderOwner())
	}
	svc.ClearProvider("plugin:ren-b")
	if svc.ProviderOwner() != "" {
		t.Fatalf("expected cleared provider, got %q", svc.ProviderOwner())
	}
}
