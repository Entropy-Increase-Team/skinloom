package plugin_test

import (
	"context"
	"testing"
	"time"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
)

type multiProviderPlugin struct {
	blobs   map[string][]byte
	indexed []plugin.SearchDoc
}

func (p *multiProviderPlugin) Setup(_ *plugin.Context) error {
	p.blobs = map[string][]byte{}
	return nil
}

func (p *multiProviderPlugin) Put(_ context.Context, key string, data []byte) error {
	p.blobs[key] = append([]byte(nil), data...)
	return nil
}

func (p *multiProviderPlugin) Get(_ context.Context, key string) ([]byte, time.Time, bool, error) {
	b, ok := p.blobs[key]
	return b, time.Now().UTC(), ok, nil
}

func (p *multiProviderPlugin) Delete(_ context.Context, key string) error {
	delete(p.blobs, key)
	return nil
}

func (p *multiProviderPlugin) Stat(_ context.Context, key string) (time.Time, int64, bool, error) {
	b, ok := p.blobs[key]
	return time.Now().UTC(), int64(len(b)), ok, nil
}

func (p *multiProviderPlugin) List(_ context.Context, _ string, _ int) ([]plugin.StorageEntry, string, error) {
	var out []plugin.StorageEntry
	for k := range p.blobs {
		out = append(out, plugin.StorageEntry{Key: k, ModTime: time.Now().UTC()})
	}
	return out, "", nil
}

func (p *multiProviderPlugin) URL(_ context.Context, key string) (string, error) {
	return "https://cdn.example/" + key, nil
}

func (p *multiProviderPlugin) Index(_ context.Context, doc plugin.SearchDoc) error {
	p.indexed = append(p.indexed, doc)
	return nil
}

type separateSearch struct{ p *multiProviderPlugin }

func (s *separateSearch) Setup(_ *plugin.Context) error { return nil }

func (s *separateSearch) Index(ctx context.Context, doc plugin.SearchDoc) error {
	return s.p.Index(ctx, doc)
}
func (s *separateSearch) Delete(_ context.Context, _ int64) error { return nil }
func (s *separateSearch) Search(_ context.Context, _ plugin.SearchQuery) (plugin.SearchResult, error) {
	return plugin.SearchResult{TextureIDs: []int64{101}, Total: 1}, nil
}

func (p *multiProviderPlugin) RenderAvatar(_ context.Context, req plugin.RenderAvatarRequest) ([]byte, string, error) {
	return append([]byte("AVATAR:"), req.SkinPNG...), "png", nil
}

func (p *multiProviderPlugin) RenderPreview(_ context.Context, req plugin.RenderPreviewRequest) ([]byte, string, error) {
	return append([]byte("PREVIEW:"), req.TexturePNG...), "webp", nil
}

func TestServerAdaptersAndHealthCheck(t *testing.T) {
	ctx := context.Background()
	p := &multiProviderPlugin{}
	srv := plugin.NewServer(p)
	defer srv.Close()

	resp, err := srv.Init(ctx, &pluginv1.InitRequest{PluginId: "provider-demo", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !resp.GetProviders().GetStorage() || !resp.GetProviders().GetRender() {
		t.Fatalf("expected storage + render capabilities, got %+v", resp.GetProviders())
	}

	// Storage adapter
	st := srv.StorageAdapter()
	if st == nil {
		t.Fatal("expected non-nil StorageAdapter")
	}
	if _, err := st.Put(ctx, &pluginv1.StoragePutRequest{Key: "abc", Data: []byte("hello")}); err != nil {
		t.Fatalf("Storage Put: %v", err)
	}
	got, err := st.Get(ctx, &pluginv1.StorageKeyRequest{Key: "abc"})
	if err != nil || !got.GetFound() || string(got.GetData()) != "hello" {
		t.Fatalf("Storage Get: got=%+v err=%v", got, err)
	}
	u, err := st.URL(ctx, &pluginv1.StorageKeyRequest{Key: "abc"})
	if err != nil || u.GetUrl() != "https://cdn.example/abc" {
		t.Fatalf("Storage URL: got=%+v err=%v", u, err)
	}

	// Render adapter
	rn := srv.RenderAdapter()
	if rn == nil {
		t.Fatal("expected non-nil RenderAdapter")
	}
	av, err := rn.RenderAvatar(ctx, &pluginv1.RenderAvatarRequest{SkinPng: []byte("png"), Size: 64, Format: "png"})
	if err != nil || string(av.GetImageBytes()) != "AVATAR:png" {
		t.Fatalf("RenderAvatar: got=%+v err=%v", av, err)
	}

	// HealthCheck reports memory & healthy
	hc, err := srv.HealthCheck(ctx, &pluginv1.HealthCheckRequest{})
	if err != nil || !hc.GetHealthy() || hc.GetMemoryRssBytes() == 0 {
		t.Fatalf("HealthCheck: got=%+v err=%v", hc, err)
	}

	// Search adapter
	searchSrv := plugin.NewServer(&separateSearch{p: p})
	defer searchSrv.Close()
	if _, err := searchSrv.Init(ctx, &pluginv1.InitRequest{PluginId: "search-demo", Version: "1.0.0"}); err != nil {
		t.Fatalf("searchSrv Init: %v", err)
	}
	sa := searchSrv.SearchAdapter()
	if sa == nil {
		t.Fatal("expected non-nil SearchAdapter")
	}
	if _, err := sa.Index(ctx, &pluginv1.SearchIndexRequest{Doc: &pluginv1.TextureSearchDoc{Id: 101, Name: "Alex"}}); err != nil {
		t.Fatalf("Search Index: %v", err)
	}
	sr, err := sa.Search(ctx, &pluginv1.SearchQueryRequest{Query: "Alex"})
	if err != nil || len(sr.GetTextureIds()) != 1 || sr.GetTextureIds()[0] != 101 {
		t.Fatalf("Search Query: got=%+v err=%v", sr, err)
	}
}
