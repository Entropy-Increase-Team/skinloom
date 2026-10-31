package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
)

// Custom CSS reaches every page: an administrator may see it but only a super
// administrator (or the admin token) may change it (BS S16). It is served as a
// stylesheet of its own, listed in the bootstrap after the theme's.
func TestCustomCSSIsForSuperadmins(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	owner := &browser{t: t, h: h}
	setupSite(t, d, owner)
	admin := registerUser(t, h, "mod@example.com")
	if err := d.DB.Ent.User.Update().Where(user.EmailNormalized("mod@example.com")).SetRole(user.RoleAdmin).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	css := `{"theme.custom_css": "body { outline: 1px solid red }", "site.name": {"en": "Mine"}}`
	rec := admin.do("PATCH", "/api/v1/admin/settings", css)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"location":"body.theme.custom_css"`) {
		t.Fatalf("admin: %d %s", rec.Code, rec.Body)
	}
	if d.Settings.Current().IsSet("site.name") {
		t.Fatal("a refused save changed the other keys")
	}
	if rec := admin.do("PATCH", "/api/v1/admin/settings", `{"site.name": {"en": "Mine"}}`); rec.Code != http.StatusOK {
		t.Fatalf("admin, other keys: %d %s", rec.Code, rec.Body)
	}
	if rec := owner.do("PATCH", "/api/v1/admin/settings", css); rec.Code != http.StatusOK {
		t.Fatalf("super administrator: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PATCH", "/api/v1/admin/settings", "Bearer "+testToken, `{"theme.custom_css": "a { color: red }"}`); rec.Code != http.StatusOK {
		t.Fatalf("admin token: %d %s", rec.Code, rec.Body)
	}

	var b Bootstrap
	_ = json.Unmarshal(do(t, h, "GET", "/api/v1/site/bootstrap", "", "").Body.Bytes(), &b)
	if len(b.Theme.CSS) != 2 || b.Theme.CSS[1] != theme.CustomCSSURL("a { color: red }") {
		t.Fatalf("bootstrap css %v", b.Theme.CSS)
	}
	if rec := do(t, h, "GET", b.Theme.CSS[1], "", ""); rec.Code != http.StatusOK || rec.Body.String() != "a { color: red }" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("stylesheet: %d %q", rec.Code, rec.Body)
	}

	var list SettingsList
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/settings", "").Body.Bytes(), &list)
	for _, s := range list.Settings {
		if s.Key == "theme.custom_css" && (!s.Superadmin || s.Value != "a { color: red }") {
			t.Fatalf("custom css view %+v", s)
		}
	}
}

func TestAppearanceAPI(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken

	var themes []ThemeView
	_ = json.Unmarshal(do(t, h, "GET", "/api/v1/admin/themes", auth, "").Body.Bytes(), &themes)
	if len(themes) != 2 || themes[0].ID != "brocade" || themes[0].Layout["auth"] != "split" || len(themes[0].Home) == 0 ||
		themes[0].Tokens.Light["primary"] == "" || !themes[1].Active {
		t.Fatalf("themes %+v", themes)
	}
	var blocks []theme.BlockType
	_ = json.Unmarshal(do(t, h, "GET", "/api/v1/admin/blocks", auth, "").Body.Bytes(), &blocks)
	if len(blocks) != len(theme.BlockTypes) {
		t.Fatalf("block types %+v", blocks)
	}

	// home.blocks is checked against the catalog.
	rec := do(t, h, "PATCH", "/api/v1/admin/settings", auth, `{"home.blocks": [{"type": "carousel"}]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unknown type") {
		t.Fatalf("unknown block: %d %s", rec.Code, rec.Body)
	}
	home := `{"home.blocks": [
		{"type": "markdown", "props": {"content": {"zh-CN": "**你好**", "en": "**hi**"}}},
		{"type": "hero", "props": {"title": {"zh-CN": "欢迎", "en": "Welcome"}}}
	], "theme.active": "brocade", "site.copyright": {"en": "Run by *us*"}, "site.logo": "/logo.png", "seo.keywords": {"en": "skins, minecraft"}}`
	if rec := do(t, h, "PATCH", "/api/v1/admin/settings", auth, home); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PATCH", "/api/v1/admin/settings", auth, `{"site.favicon": "javascript:alert(1)"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("script favicon: %d", rec.Code)
	}

	// The bootstrap gives pages the blocks in the visitor's language, Markdown rendered.
	var b Bootstrap
	_ = json.Unmarshal(do(t, h, "GET", "/api/v1/site/bootstrap?lang=en", "", "").Body.Bytes(), &b)
	if b.Theme.ID != "brocade" || len(b.Theme.Home) != 2 || b.Theme.Home[0].Props["contentHtml"] != "<p><strong>hi</strong></p>\n" ||
		b.Theme.Home[1].Props["title"] != "Welcome" {
		t.Fatalf("home %+v", b.Theme.Home)
	}
	if b.Site.CopyrightHTML != "<p>Run by <em>us</em></p>\n" || b.Site.Logo != "/logo.png" || b.Site.Keywords != "skins, minecraft" {
		t.Fatalf("site %+v", b.Site)
	}

	rec = do(t, h, "GET", "/api/v1/site/stats", "", "")
	var s SiteStats
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if rec.Code != http.StatusOK || s != (SiteStats{}) || rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body)
	}
}
