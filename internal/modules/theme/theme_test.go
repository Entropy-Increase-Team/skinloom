package theme

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
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
		if c.want == http.StatusOK && rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", c.path, rec.Header().Get("X-Content-Type-Options"))
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

func TestResolveAddsCustomCSSAndJS(t *testing.T) {
	r := builtin(t)
	css, js := "body { outline: 1px solid red }", "console.log('hi')"
	got := r.Resolve(snapshotWith(t, map[string]string{"theme.custom_css": mustJSON(t, css), "theme.custom_js": mustJSON(t, js)}))
	if n := len(got.CSS); n != 2 || got.CSS[1] != CustomCSSURL(css) {
		t.Fatalf("css %v", got.CSS)
	}
	if len(got.JS) != 1 || got.JS[0] != CustomJSURL(js) {
		t.Fatalf("js %v", got.JS)
	}
	if got := r.Resolve(snapshotWith(t, nil)); got.JS == nil || len(got.JS) != 0 {
		t.Fatalf("no custom js: %#v", got.JS) // an empty array for the frontend, not null
	}

	h := CustomHandler(func() string { return css }, func() string { return js })
	for _, c := range []struct{ url, body, typ string }{
		{CustomCSSURL(css), css, "text/css; charset=utf-8"},
		{CustomJSURL(js), js, "text/javascript; charset=utf-8"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != c.body || rec.Header().Get("Content-Type") != c.typ ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
			rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("current %s: %d %q %v", c.url, rec.Code, rec.Body, rec.Header())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, CustomJSURL("old"), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != js || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("stale name: %d %v", rec.Code, rec.Header())
	}
	for _, u := range []string{"/themes-assets/_custom/x.html", "/themes-assets/_custom/x"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", u, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	CustomHandler(func() string { return css }, func() string { return "" }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, CustomJSURL(js), nil))
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
		"not a theme":      `{"id":"x","kind":"plugin","version":"1"}`,
		"no version":       `{"id":"x","kind":"theme"}`,
		"missing css":      `{"id":"x","kind":"theme","version":"1","theme":{"css":"nope.css"}}`,
		"bad json":         `{`,
		"bad id":           `{"id":"_custom","kind":"theme","version":"1"}`,
		"unknown layout":   `{"id":"x","kind":"theme","version":"1","theme":{"layout":{"auth":"fullscreen"}}}`,
		"unknown block":    `{"id":"x","kind":"theme","version":"1","theme":{"home":[{"type":"carousel"}]}}`,
		"unsafe token":     `{"id":"x","kind":"theme","version":"1","theme":{"tokens":{"light":{"primary":"red;}"}}}}`,
		"bad colorScheme":  `{"id":"x","kind":"theme","version":"1","theme":{"colorScheme":"sepia"}}`,
		"self extends":     `{"id":"x","kind":"theme","version":"1","extends":"x"}`,
		"missing asset":    `{"id":"x","kind":"theme","version":"1","theme":{"tokens":{"viewer":{"background":"assets/nope.webp"}}}}`,
		"js in screenshot": `{"id":"x","kind":"theme","version":"1","screenshot":"a.js"}`,
		"bad setting type": `{"id":"x","kind":"theme","version":"1","theme":{"settings":{"a":{"type":"script"}}}}`,
	}
	for name, manifest := range cases {
		fsys := fstest.MapFS{"x/plugin.json": {Data: []byte(manifest)}, "x/a.js": {Data: []byte("alert(1)")}}
		if err := NewRegistry().LoadDir(fsys, "test"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestLocalThemesExtendsAssetsAndSettings(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")

	// 1. Valid local theme extending default, with screenshot, relative assets, colorScheme, layout & settings.
	myDir := filepath.Join(themesDir, "my-site")
	if err := os.MkdirAll(filepath.Join(myDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(myDir, "screenshot.webp"), "webp")
	writeFile(t, filepath.Join(myDir, "theme.css"), ".sl-header { border-bottom-width: 2px; }")
	writeFile(t, filepath.Join(myDir, "assets/sky.webp"), "sky")
	writeFile(t, filepath.Join(myDir, "assets/hero.webp"), "hero")
	writeFile(t, filepath.Join(myDir, "assets/badge.svg"), "<svg></svg>")
	writeFile(t, filepath.Join(myDir, "assets/evil.js"), "alert(1)")
	writeFile(t, filepath.Join(myDir, "plugin.json"), `{
		"id": "my-site",
		"kind": "theme",
		"version": "1.0.0",
		"name": {"zh-CN": "我的站", "en": "My Site"},
		"apiVersion": 1,
		"extends": "default",
		"screenshot": "screenshot.webp",
		"theme": {
			"colorScheme": "dark",
			"tokens": {
				"light": {"primary": "oklch(0.55 0.15 150)"},
				"dark": {"primary": "oklch(0.75 0.13 150)"},
				"fonts": {"display": "'My Display', var(--sl-font-sans)"},
				"viewer": {"background": "url(assets/sky.webp) center / cover"}
			},
			"css": "theme.css",
			"layout": {"user": "tabs", "texture": "stacked"},
			"home": [{"type": "hero", "props": {"layout": "banner", "background": "assets/hero.webp"}}],
			"settings": {
				"accent": {"type": "color", "label": {"zh-CN": "主色"}, "token": "primary"},
				"heroImage": {"type": "image", "label": "Hero", "default": "assets/hero.webp"},
				"density": {"type": "choice", "options": ["comfortable", "compact"], "default": "comfortable"},
				"compactHeader": {"type": "boolean", "default": true}
			}
		}
	}`)

	// 2. Broken local theme (duplicate built-in id) and multi-level extends.
	dupDir := filepath.Join(themesDir, "dup-default")
	_ = os.MkdirAll(dupDir, 0o755)
	writeFile(t, filepath.Join(dupDir, "plugin.json"), `{"id":"default","kind":"theme","version":"1.0.0"}`)

	grandDir := filepath.Join(themesDir, "grandchild")
	_ = os.MkdirAll(grandDir, 0o755)
	writeFile(t, filepath.Join(grandDir, "plugin.json"), `{"id":"grandchild","kind":"theme","version":"1.0.0","extends":"my-site"}`)

	r := builtin(t)
	r.SetLocalDir(themesDir, nil)

	// Valid theme is installed; broken ones are listed with Error but not returned by Get.
	my, ok := r.Get("my-site")
	if !ok {
		t.Fatal("my-site not loaded")
	}
	if _, ok := r.Get("grandchild"); ok {
		t.Fatal("multi-level extends must be rejected")
	}
	def, _ := r.Get("default")
	if def.Source != "builtin" {
		t.Fatal("local theme overwrote built-in default")
	}
	all := r.List()
	if len(all) != 5 { // default, brocade, my-site + 2 broken
		t.Fatalf("List len = %d, want 5", len(all))
	}

	// Local asset URL uses content hash, not Version.
	if strings.Contains(my.ScreenshotURL, "/1.0.0/") || !strings.HasPrefix(my.ScreenshotURL, "/themes-assets/my-site/") {
		t.Fatalf("screenshot URL = %q", my.ScreenshotURL)
	}
	// CSS includes parent (default) first, then child (my-site).
	if len(my.CSSFiles) != 2 || !strings.HasPrefix(my.CSSFiles[0], "/themes-assets/default/") || !strings.HasPrefix(my.CSSFiles[1], "/themes-assets/my-site/") {
		t.Fatalf("inherited CSS = %v", my.CSSFiles)
	}

	// AssetHandler serves allowed extensions, sandboxes SVG, and blocks .js.
	h := r.AssetHandler()
	svgURL := AssetURL(my, "assets/badge.svg")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, svgURL, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'self'; sandbox" {
		t.Fatalf("svg: %d csp=%q", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AssetURL(my, "assets/evil.js"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("evil.js served with status %d", rec.Code)
	}

	// Resolve applies inherited tokens, rewritten relative URLs, and theme.settings.
	snap := snapshotWith(t, map[string]string{
		"theme.active":   `"my-site"`,
		"theme.settings": `{"my-site":{"accent":"oklch(0.6 0.16 200)","density":"compact"}}`,
	})
	got := r.Resolve(snap)
	if got.ColorScheme != "dark" || got.Layout["user"] != "tabs" || got.Layout["texture"] != "stacked" || got.Layout["auth"] != "card" {
		t.Fatalf("resolved layout/colorScheme: %+v", got)
	}
	if got.Tokens.Light["primary"] != "oklch(0.6 0.16 200)" || got.Tokens.Dark["primary"] != "oklch(0.6 0.16 200)" {
		t.Fatalf("token-bound setting not applied: %v / %v", got.Tokens.Light["primary"], got.Tokens.Dark["primary"])
	}
	if got.Tokens.Light["background"] != def.Theme.Tokens.Light["background"] {
		t.Fatalf("inherited token missing: %v", got.Tokens.Light["background"])
	}
	if !strings.Contains(got.Tokens.Viewer["background"], "/themes-assets/my-site/") {
		t.Fatalf("viewer.background relative asset not rewritten: %q", got.Tokens.Viewer["background"])
	}
	if got.Settings["density"] != "compact" || got.Settings["compactHeader"] != "true" || !strings.HasPrefix(got.Settings["heroImage"], "/themes-assets/my-site/") {
		t.Fatalf("resolved settings = %+v", got.Settings)
	}
	if bg, _ := got.Home[0].Props["background"].(string); !strings.HasPrefix(bg, "/themes-assets/my-site/") {
		t.Fatalf("hero background prop not rewritten: %q", bg)
	}

	// Editing a local file changes the content hash on Reload without changing Version.
	oldCSS := my.CSSFiles[1]
	writeFile(t, filepath.Join(myDir, "theme.css"), ".sl-header { border-bottom-width: 4px; }")
	r.Reload()
	myAfter, _ := r.Get("my-site")
	if myAfter.CSSFiles[1] == oldCSS {
		t.Fatalf("content hash did not change after file edit: %q", myAfter.CSSFiles[1])
	}
}

func TestScaffoldAndCheck(t *testing.T) {
	r := builtin(t)
	dir := t.TempDir()
	created, err := Scaffold(dir, "indigo-custom", "default")
	if err != nil {
		t.Fatal(err)
	}
	th, warnings, err := Check(os.DirFS(created), r)
	if err != nil || th.ID != "indigo-custom" || len(warnings) != 0 {
		t.Fatalf("Check scaffolded theme: th=%v warnings=%v err=%v", th, warnings, err)
	}

	// Low-contrast override triggers a warning from Check.
	lowContrast := fstest.MapFS{
		"plugin.json": {Data: []byte(`{
			"id": "low-contrast",
			"kind": "theme",
			"version": "0.1.0",
			"extends": "default",
			"theme": {
				"tokens": {
					"light": {"primary": "#f0f0f0", "primary-foreground": "#ffffff"}
				}
			}
		}`)},
	}
	_, warnings, err = Check(lowContrast, r)
	if err != nil || len(warnings) == 0 {
		t.Fatalf("want contrast warnings, got warnings=%v err=%v", warnings, err)
	}
}

func TestLocalThemeRejectsAlphabeticallySortedGrandchildAndSkipsHiddenHash(t *testing.T) {
	dir := t.TempDir()
	aDir := filepath.Join(dir, "a-parent")
	_ = os.MkdirAll(aDir, 0o755)
	writeFile(t, filepath.Join(aDir, "plugin.json"), `{
		"id": "a-parent",
		"kind": "theme",
		"version": "1.0.0",
		"extends": "default"
	}`)
	hBefore := hashFS(os.DirFS(aDir))
	_ = os.MkdirAll(filepath.Join(aDir, ".git"), 0o755)
	writeFile(t, filepath.Join(aDir, ".DS_Store"), "noise")
	writeFile(t, filepath.Join(aDir, ".git", "HEAD"), "ref: refs/heads/main\n")
	if hAfter := hashFS(os.DirFS(aDir)); hAfter != hBefore {
		t.Fatalf("hashFS changed after hidden files added: %q -> %q", hBefore, hAfter)
	}

	bDir := filepath.Join(dir, "b-grand")
	_ = os.MkdirAll(bDir, 0o755)
	writeFile(t, filepath.Join(bDir, "plugin.json"), `{
		"id": "b-grand",
		"kind": "theme",
		"version": "1.0.0",
		"extends": "a-parent"
	}`)

	r := builtin(t)
	r.SetLocalDir(dir, nil)

	if _, ok := r.Get("a-parent"); !ok {
		t.Fatal("a-parent should load")
	}
	if _, ok := r.Get("b-grand"); ok {
		t.Fatal("b-grand extending a-parent (even sorting after it) must be rejected")
	}
	var brokenErr string
	for _, th := range r.List() {
		if th.ID == "b-grand" {
			brokenErr = th.Error
		}
	}
	if !strings.Contains(brokenErr, "single-level") {
		t.Fatalf("expected single-level inheritance error in List(), got %q", brokenErr)
	}
}

func TestValidateDeclaredSettingsImageDefaultError(t *testing.T) {
	r := builtin(t)
	bad := fstest.MapFS{
		"plugin.json": {Data: []byte(`{
			"id": "bad-img-default",
			"kind": "theme",
			"version": "0.1.0",
			"extends": "default",
			"theme": {
				"settings": {
					"hero": {
						"type": "image",
						"label": {"en": "Hero"},
						"default": "https://example.com/a;b.png"
					}
				}
			}
		}`)},
	}
	_, _, err := Check(bad, r)
	if err == nil {
		t.Fatal("expected error for image default with forbidden CSS token characters")
	}
	if strings.Contains(err.Error(), "%!w") || !strings.Contains(err.Error(), "forbidden characters") {
		t.Fatalf("unexpected error formatting: %v", err)
	}
}

func TestLevel5ThemeOverridesTrustGateAndPluginBlocks(t *testing.T) {
	dir := t.TempDir()
	thDir := filepath.Join(dir, "prism")
	_ = os.MkdirAll(filepath.Join(thDir, "web"), 0o755)
	writeFile(t, filepath.Join(thDir, "web", "index.js"), `export default { overrides: { "site.footer": () => null } };`)
	writeFile(t, filepath.Join(thDir, "web", "index.css"), `.prism-footer { opacity: 0.9; }`)
	writeFile(t, filepath.Join(thDir, "plugin.json"), `{
		"id": "prism",
		"kind": "theme",
		"version": "1.0.0",
		"name": {"en": "Prism"},
		"apiVersion": 1,
		"web": "web/index.js",
		"theme": {
			"overrides": ["site.header", "site.footer"],
			"tokens": {"light": {"primary": "#111"}, "dark": {"primary": "#eee"}}
		}
	}`)

	r := NewRegistry()
	r.SetLocalDir(dir, nil)
	loaded, ok := r.Get("prism")
	if !ok {
		t.Fatalf("expected prism theme loaded, got broken=%+v", r.List())
	}
	if len(loaded.Overrides) != 2 || loaded.WebEntry == "" {
		t.Fatalf("expected overrides and webEntry, got overrides=%v web=%q", loaded.Overrides, loaded.WebEntry)
	}

	snap := snapshotWith(t, map[string]string{"theme.active": `"prism"`})
	untrusted := r.ResolveTheme(snap, "", nil)
	if untrusted.WebEntry != "" || len(untrusted.Overrides) != 0 {
		t.Fatalf("expected untrusted theme web entry stripped, got %+v", untrusted)
	}
	trusted := r.ResolveTheme(snap, "", func(id string) bool { return id == "prism" })
	if trusted.WebEntry != loaded.WebEntry || len(trusted.Overrides) != 2 {
		t.Fatalf("expected trusted web entry & overrides, got %+v", trusted)
	}

	// Web script asset serving enforces trust callback (403 untrusted vs 200 trusted).
	recBlocked := httptest.NewRecorder()
	r.AssetHandlerWithTrust(nil).ServeHTTP(recBlocked, httptest.NewRequest(http.MethodGet, loaded.WebEntry, nil))
	if recBlocked.Code != http.StatusForbidden {
		t.Fatalf("untrusted theme JS = %d, want 403", recBlocked.Code)
	}
	recOK := httptest.NewRecorder()
	r.AssetHandlerWithTrust(func(id string) bool { return id == "prism" }).ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, loaded.WebEntry, nil))
	if recOK.Code != http.StatusOK || !strings.Contains(recOK.Body.String(), "site.footer") {
		t.Fatalf("trusted theme JS = %d %s", recOK.Code, recOK.Body.String())
	}

	// Invalid override target rejected cleanly.
	bad := fstest.MapFS{
		"plugin.json": {Data: []byte(`{"id":"bad-over","kind":"theme","version":"1.0.0","apiVersion":1,"theme":{"overrides":["unknown.spot"]}}`)},
	}
	if _, _, err := Check(bad, r); err == nil {
		t.Fatal("expected unknown override target rejection")
	}

	// Plugin blocks register/validate/unregister cleanly.
	RegisterPluginBlocks("spotlight-plug", []BlockType{{Type: "spotlight"}})
	t.Cleanup(func() { UnregisterPluginBlocks("spotlight-plug") })
	if err := ValidateHome([]any{map[string]any{"type": "spotlight", "props": map[string]any{"title": "Hi"}}}); err != nil {
		t.Fatalf("ValidateHome plugin block failed: %v", err)
	}
	localized := LocalizeBlocks([]Block{{Type: "spotlight", Props: map[string]any{"title": "Hi"}}}, "en", nil)
	if localized[0].Props["title"] != "Hi" {
		t.Fatalf("expected plugin block props preserved: %+v", localized)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
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

func TestAssetHandlerCustomWebAndPluginBlockNormalization(t *testing.T) {
	r := builtin(t)
	local := fstest.MapFS{
		"plugin.json": {Data: []byte(`{
			"id": "custom-web",
			"kind": "theme",
			"version": "1.0.0",
			"name": {"en": "Custom Web"},
			"apiVersion": 1,
			"web": "dist/index.js"
		}`)},
		"dist/index.js": {Data: []byte(`export default {};`)},
	}
	th, _, err := Check(local, r)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	r.Put(th)

	untrusted := r.AssetHandlerWithTrust(func(string) bool { return false })
	recForbid := httptest.NewRecorder()
	untrusted.ServeHTTP(recForbid, httptest.NewRequest("GET", th.WebEntry, nil))
	if recForbid.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for untrusted custom web entry, got %d", recForbid.Code)
	}

	trusted := r.AssetHandlerWithTrust(func(id string) bool { return id == "custom-web" })
	recOK := httptest.NewRecorder()
	trusted.ServeHTTP(recOK, httptest.NewRequest("GET", th.WebEntry, nil))
	if recOK.Code != http.StatusOK || !strings.Contains(recOK.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("expected 200 JS for trusted dist/index.js, got %d ctype=%q", recOK.Code, recOK.Header().Get("Content-Type"))
	}

	// RegisterPluginBlocks normalizes nil Props to []Prop{} (marshals as [] not null) and ignores built-in names.
	RegisterPluginBlocks("block-test", []BlockType{
		{Type: "hero"},
		{Type: "custom-widget"},
	})
	t.Cleanup(func() { UnregisterPluginBlocks("block-test") })

	all := AllBlockTypes()
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"custom-widget","props":[]`) {
		t.Fatalf("expected non-null empty props slice for plugin block: %s", string(raw))
	}
	if err := ValidateBlocks([]Block{{Type: "custom-widget", Props: map[string]any{"x": strings.Repeat("a", maxPluginBlockBytes+1)}}}); err == nil {
		t.Fatal("expected error when schema-less plugin block exceeds size cap")
	}
}

func TestRegistryWatchAllowsStagingRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	r := builtin(t)
	r.SetLocalDir(dir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan struct{}, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Watch(ctx, func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Give fsnotify watcher time to arm on the root directory.
	time.Sleep(100 * time.Millisecond)

	// Simulate ExtractPackageFiles: create hidden .extract-* staging dir, write manifest,
	// sleep briefly so any faulty watcher would have grabbed a handle, then rename to final theme dir.
	stage1, err := os.MkdirTemp(dir, ".extract-demo-theme-*")
	if err != nil {
		t.Fatalf("MkdirTemp stage1: %v", err)
	}
	writeFile(t, filepath.Join(stage1, "plugin.json"), `{
		"id": "demo-theme",
		"kind": "theme",
		"version": "1.0.0",
		"extends": "default"
	}`)
	time.Sleep(80 * time.Millisecond)

	dest := filepath.Join(dir, "demo-theme")
	if err := os.Rename(stage1, dest); err != nil {
		t.Fatalf("Rename staging dir while Watch active (Windows lock regression): %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if th, ok := r.Get("demo-theme"); ok && th.Version == "1.0.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Watch did not reload demo-theme v1.0.0 in time")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Simulate upgrade swap (rename existing watched theme to .bak-*, rename new staging dir into place, remove backup).
	stage2, err := os.MkdirTemp(dir, ".extract-demo-theme-*")
	if err != nil {
		t.Fatalf("MkdirTemp stage2: %v", err)
	}
	writeFile(t, filepath.Join(stage2, "plugin.json"), `{
		"id": "demo-theme",
		"kind": "theme",
		"version": "1.0.1",
		"extends": "default"
	}`)
	bak := filepath.Join(dir, ".bak-demo-theme")
	if err := os.Rename(dest, bak); err != nil {
		t.Fatalf("Rename active theme to backup while Watch active: %v", err)
	}
	if err := os.Rename(stage2, dest); err != nil {
		t.Fatalf("Rename upgraded theme into place while Watch active: %v", err)
	}
	if err := os.RemoveAll(bak); err != nil {
		t.Fatalf("RemoveAll backup dir while Watch active: %v", err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for {
		if th, ok := r.Get("demo-theme"); ok && th.Version == "1.0.1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Watch did not reload upgraded demo-theme v1.0.1 in time")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Simulate uninstall removal.
	if err := os.RemoveAll(dest); err != nil {
		t.Fatalf("RemoveAll theme dir while Watch active: %v", err)
	}
}

func TestResolveSyncsWarningFromHighlightOverride(t *testing.T) {
	r := builtin(t)

	// Overriding highlight mirrors onto warning when warning is not explicitly overridden.
	snap := snapshotWith(t, map[string]string{
		"theme.active": `"default"`,
		"theme.overrides": `{
			"light": {"highlight": "oklch(0.75 0.16 65)", "highlight-foreground": "oklch(0.2 0.04 50)"},
			"dark": {"highlight": "oklch(0.84 0.15 70)", "warning": "oklch(0.78 0.18 40)"}
		}`,
	})
	got := r.Resolve(snap)
	if got.Tokens.Light["warning"] != "oklch(0.75 0.16 65)" {
		t.Fatalf("light warning did not mirror highlight override: got %q", got.Tokens.Light["warning"])
	}
	if got.Tokens.Light["warning-foreground"] != "oklch(0.2 0.04 50)" {
		t.Fatalf("light warning-foreground did not mirror highlight-foreground override: got %q", got.Tokens.Light["warning-foreground"])
	}
	// Explicit warning override must be preserved.
	if got.Tokens.Dark["warning"] != "oklch(0.78 0.18 40)" {
		t.Fatalf("dark explicit warning override was overwritten: got %q", got.Tokens.Dark["warning"])
	}
}
