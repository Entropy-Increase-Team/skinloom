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

func TestBuiltinThemes(t *testing.T) {
	r := builtin(t)
	for _, id := range []string{"default", "brocade"} {
		th, ok := r.Get(id)
		if !ok {
			t.Fatalf("%s not installed", id)
		}
		got := r.Resolve(snapshotWith(t, map[string]string{"theme.active": `"` + id + `"`}))
		if got.ID != id || got.Layout["auth"] != cmpOr(th.Theme.Layout["auth"], "card") || len(got.Layout) != len(Layouts) {
			t.Errorf("%s resolved as %+v", id, got)
		}
	}
	if got := r.Resolve(snapshotWith(t, map[string]string{"theme.active": `"brocade"`})); got.Layout["auth"] != "split" || got.Layout["skinlib"] != "compact" {
		t.Fatalf("brocade layout %v", got.Layout)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func TestResolveAddsCustomCSS(t *testing.T) {
	r := builtin(t)
	css := "body { outline: 1px solid red }"
	got := r.Resolve(snapshotWith(t, map[string]string{"theme.custom_css": mustJSON(t, css)}))
	if n := len(got.CSS); n != 2 || got.CSS[1] != CustomCSSURL(css) {
		t.Fatalf("css %v", got.CSS)
	}

	h := CustomCSSHandler(func() string { return css })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, CustomCSSURL(css), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != css || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" ||
		rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("current: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, CustomCSSURL("old"), nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("stale name: %d %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	CustomCSSHandler(func() string { return "" }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, CustomCSSURL(css), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("none: %d", rec.Code)
	}
}

func TestValidateOverrides(t *testing.T) {
	good := `{"light":{"primary":"oklch(0.6 0.2 300)"},"dark":{"primary":"#336699"},"viewer":{"background":"transparent"}}`
	if err := ValidateOverrides(decode(t, good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"light":{"primary":"red;}body{display:none"}}`,
		`{"light":{"ring":"a</style>"}}`,
		`{"light":{"Primary":"red"}}`,
		`{"light":{"primary":1}}`,
		`{"sepia":{"primary":"red"}}`,
		`{"light":"red"}`,
	} {
		if err := ValidateOverrides(decode(t, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestLoadRejectsBadManifests(t *testing.T) {
	cases := map[string]string{
		"not a theme":    `{"id":"x","kind":"plugin","version":"1"}`,
		"no version":     `{"id":"x","kind":"theme"}`,
		"missing css":    `{"id":"x","kind":"theme","version":"1","theme":{"css":"nope.css"}}`,
		"bad json":       `{`,
		"bad id":         `{"id":"_custom","kind":"theme","version":"1"}`,
		"unknown layout": `{"id":"x","kind":"theme","version":"1","theme":{"layout":{"auth":"fullscreen"}}}`,
		"unknown block":  `{"id":"x","kind":"theme","version":"1","theme":{"home":[{"type":"carousel"}]}}`,
		"unsafe token":   `{"id":"x","kind":"theme","version":"1","theme":{"tokens":{"light":{"primary":"red;}"}}}}`,
	}
	for name, manifest := range cases {
		fsys := fstest.MapFS{"x/plugin.json": {Data: []byte(manifest)}}
		if err := NewRegistry().LoadDir(fsys, "test"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
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
