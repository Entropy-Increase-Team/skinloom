package theme

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

func builtin(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	if err := r.LoadDir(themes.Builtin, "builtin"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAssetHandler(t *testing.T) {
	r := builtin(t)
	h := r.AssetHandler()
	def, _ := r.Get(DefaultID)
	base := "/themes-assets/default/" + def.Version + "/"
	cases := []struct {
		path      string
		want      int
		immutable bool
	}{
		{base + "theme.css", http.StatusOK, true},
		{"/themes-assets/default/0.0.0-old/theme.css", http.StatusOK, false},
		{base + "plugin.json", http.StatusNotFound, false},
		{base + "./plugin.json", http.StatusNotFound, false},
		{base + "../default/plugin.json", http.StatusNotFound, false},
		// Regression: directories were listed.
		{base, http.StatusNotFound, false},
		{base + ".", http.StatusNotFound, false},
		{"/themes-assets/nope/1/theme.css", http.StatusNotFound, false},
		{"/themes-assets/default", http.StatusNotFound, false},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.want)
			continue
		}
		cc := rec.Header().Get("Cache-Control")
		if c.immutable && cc != "public, max-age=31536000, immutable" {
			t.Errorf("%s: Cache-Control %q, want immutable", c.path, cc)
		}
		if c.want == http.StatusOK && !c.immutable && cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q, want no-cache for a stale version", c.path, cc)
		}
	}
}

func TestResolveAppliesOnlySafeOverrides(t *testing.T) {
	r := builtin(t)
	snap := snapshotWith(t, map[string]string{
		"theme.overrides": `{"light":{"primary":"red","border":"x;}body{display:none","ring":"a</style>"},"dark":{"primary":"blue"}}`,
	})
	got := r.Resolve(snap)
	if got.Tokens.Light["primary"] != "red" || got.Tokens.Dark["primary"] != "blue" {
		t.Fatalf("safe overrides not applied: %v / %v", got.Tokens.Light["primary"], got.Tokens.Dark["primary"])
	}
	def, _ := r.Get(DefaultID)
	if got.Tokens.Light["border"] != def.Theme.Tokens.Light["border"] || got.Tokens.Light["ring"] != def.Theme.Tokens.Light["ring"] {
		t.Fatal("unsafe override values must be dropped")
	}
	if def.Theme.Tokens.Light["primary"] == "red" {
		t.Fatal("Resolve must not mutate the installed theme")
	}
}

func TestResolveHomeBlocks(t *testing.T) {
	r := builtin(t)
	def, _ := r.Get(DefaultID)
	if got := r.Resolve(snapshotWith(t, nil)); len(got.Home) != len(def.Theme.Home) {
		t.Fatalf("default home: %d blocks, want %d", len(got.Home), len(def.Theme.Home))
	}
	got := r.Resolve(snapshotWith(t, map[string]string{"home.blocks": `[{"type":"hero"}]`}))
	if len(got.Home) != 1 || got.Home[0].Type != "hero" {
		t.Fatalf("home.blocks override not applied: %+v", got.Home)
	}
}

func TestLoadRejectsBadManifests(t *testing.T) {
	cases := map[string]string{
		"not a theme": `{"id":"x","kind":"plugin","version":"1"}`,
		"no version":  `{"id":"x","kind":"theme"}`,
		"missing css": `{"id":"x","kind":"theme","version":"1","theme":{"css":"nope.css"}}`,
		"bad json":    `{`,
	}
	for name, manifest := range cases {
		fsys := fstest.MapFS{"x/plugin.json": {Data: []byte(manifest)}}
		if err := NewRegistry().LoadDir(fsys, "test"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// snapshotWith builds a settings snapshot from raw JSON values without a database.
func snapshotWith(t *testing.T, values map[string]string) *settings.Snapshot {
	t.Helper()
	raw := map[string]json.RawMessage{}
	for k, v := range values {
		raw[k] = json.RawMessage(v)
	}
	return settings.NewSnapshot(settings.NewRegistry(), 1, raw)
}
