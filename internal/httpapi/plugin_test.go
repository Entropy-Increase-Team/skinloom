package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/gateway"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

func TestPluginEndToEndHTTPAPI(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	var logBuf bytes.Buffer
	d.Log = slog.New(slog.NewTextHandler(&logBuf, nil))
	d.Bus = hook.NewBus(d.Log, 4)
	t.Cleanup(func() {
		d.Bus.Close(context.Background())
		if t.Failed() {
			t.Logf("logs:\n%s", logBuf.String())
		}
	})
	d.Accounts.SetHooks(d.Bus)
	d.Textures.SetHooks(d.Bus)
	d.Players.SetHooks(d.Bus)
	d.Reports.SetHooks(d.Bus)
	d.Yggdrasil.SetHooks(d.Bus)
	setSettings(t, d, `{"captcha.provider": "none"}`)

	q := jobs.New(d.DB.Ent, d.Log)
	q.Register(hook.OutboxJobKind, d.Bus.Deliver)
	d.Accounts.SetJobNotifier(q.Kick)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Notify:   d.Notify,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	manifest := []byte(`{
		"id": "invite-code",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Invite Code",
		"apiVersion": 1,
		"server": "server.js",
		"permissions": ["storage.docs", "storage.kv"],
		"hooks": {
			"guard": ["auth.register"],
			"filter": ["user.badges"],
			"event": ["user.registered"]
		},
		"storage": {
			"collections": {
				"codes": {
					"indexes": {
						"code": "string:unique",
						"usedBy": "number"
					}
				}
			}
		}
	}`)
	serverJS := []byte(`
		export default {
			setup(ctx) {
				const codes = ctx.db.collection("codes");

				ctx.hooks.guard("auth.register", async (p) => {
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return reject("invite_required");
					const row = await codes.findOne({ code });
					if (!row) return reject("invite_invalid");
					if (row.usedBy != null) return reject("invite_used");
				});

				ctx.events.on("user.registered", async (p) => {
					if (p.method === "setup") return;
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return;
					const hasSecret = Boolean(p.form && p.form.passwordSecret);
					await codes.updateOne({ code }, { usedBy: p.userId, hasSecret });
					await ctx.kv.set("lastRegistered:" + p.userId, { method: p.method, hasSecret });
				});

				ctx.hooks.filter("user.badges", (badges, u) => {
					const out = Array.isArray(badges) ? [...badges] : [];
					if (u.userId > 1) {
						out.push({ text: "Invited", color: "#2563eb" });
					}
					return out;
				});

				ctx.http.post("/codes", { auth: "admin" }, async (req) => {
					const code = String(req.body?.code || "");
					const doc = await codes.insertOne({ code, usedBy: null });
					return { status: 201, body: doc };
				});

				ctx.http.get("/codes/:code", { auth: "public" }, async (req) => {
					const doc = await codes.findOne({ code: req.params.code });
					if (!doc) return { status: 404, body: { error: "not found" } };
					return doc;
				});
			}
		};
	`)
	staticFiles := map[string][]byte{
		"web/widget.js": []byte(`export const widget = "invite-code";`),
	}
	if _, err := mgr.LoadFromBytes(ctx, manifest, serverJS, staticFiles, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)

	// 1. CSRF check on /api/plugins/* when using browser session cookie
	noCSRFBrowser := &browser{t: t, h: h, cookie: admin.cookie, headers: map[string]string{CSRFHeader: ""}}
	noCSRFRec := noCSRFBrowser.do(http.MethodPost, "/api/plugins/invite-code/codes", `{"code":"JOIN-2026"}`)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 missing CSRF header on /api/plugins/*, got %d (%s)", noCSRFRec.Code, noCSRFRec.Body.String())
	}

	// 2. Admin creates an invite code via /api/plugins/invite-code/codes
	recCreate := admin.do("POST", "/api/plugins/invite-code/codes", `{"code":"JOIN-2026"}`)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("POST /api/plugins/invite-code/codes = %d: %s", recCreate.Code, recCreate.Body.String())
	}

	// 3. Registration without inviteCode is rejected cleanly with RFC 9457 422
	guest := &browser{t: t, h: h}
	recNoCode := guest.do("POST", "/api/v1/auth/register", `{"email":"alice@example.com","nickname":"Alice","password":"alice-pass-1"}`)
	if recNoCode.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on missing inviteCode, got %d: %s", recNoCode.Code, recNoCode.Body.String())
	}
	if !strings.Contains(recNoCode.Body.String(), "invite_required") {
		t.Fatalf("expected invite_required in 422 Problem Details, got %s", recNoCode.Body.String())
	}

	// 4. Registration with valid inviteCode succeeds, passing redacted form into user.registered
	recOK := guest.do("POST", "/api/v1/auth/register", `{
		"email": "alice@example.com",
		"nickname": "Alice",
		"password": "alice-pass-1",
		"form": { "inviteCode": "JOIN-2026", "passwordSecret": "do-not-leak" }
	}`)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid invite registration, got %d: %s", recOK.Code, recOK.Body.String())
	}
	var regUser UserView
	if err := json.Unmarshal(recOK.Body.Bytes(), &regUser); err != nil {
		t.Fatalf("decode registered user: %v", err)
	}

	// Deliver outbox event user.registered synchronously in test
	if _, err := q.RunDue(ctx); err != nil {
		t.Fatalf("q.RunDue: %v", err)
	}

	// 5. Verify code is marked usedBy == regUser.ID and sensitive form field was redacted
	recCode := guest.do("GET", "/api/plugins/invite-code/codes/JOIN-2026", "")
	if recCode.Code != http.StatusOK {
		t.Fatalf("GET /api/plugins/invite-code/codes/JOIN-2026 = %d: %s", recCode.Code, recCode.Body.String())
	}
	var codeDoc map[string]any
	if err := json.Unmarshal(recCode.Body.Bytes(), &codeDoc); err != nil {
		t.Fatal(err)
	}
	if codeDoc["usedBy"] != float64(regUser.ID) {
		t.Fatalf("expected usedBy=%d, got %#v", regUser.ID, codeDoc["usedBy"])
	}
	if codeDoc["hasSecret"] != false {
		t.Fatalf("expected passwordSecret redacted from user.registered form, got %#v", codeDoc["hasSecret"])
	}

	// 6. Reusing the same invite code fails with 422 invite_used
	guest2 := &browser{t: t, h: h}
	recDup := guest2.do("POST", "/api/v1/auth/register", `{
		"email": "bob@example.com",
		"nickname": "Bob",
		"password": "bob-pass-123",
		"form": { "inviteCode": "JOIN-2026" }
	}`)
	if recDup.Code != http.StatusUnprocessableEntity || !strings.Contains(recDup.Body.String(), "invite_used") {
		t.Fatalf("expected 422 invite_used on second registration, got %d: %s", recDup.Code, recDup.Body.String())
	}

	// 7. GET /api/v1/auth/me reflects custom badge added by user.badges filter
	recMe := guest.do("GET", "/api/v1/auth/me", "")
	if recMe.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/me = %d: %s", recMe.Code, recMe.Body.String())
	}
	var me UserView
	if err := json.Unmarshal(recMe.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if len(me.Badges) != 1 || me.Badges[0].Text != "Invited" || me.Badges[0].Color != "#2563eb" {
		t.Fatalf("expected badge [{Invited #2563eb}], got %#v", me.Badges)
	}

	// 8. Static plugin assets served at /plugins-assets/{id}/* with nosniff
	recAsset := guest.do("GET", "/plugins-assets/invite-code/web/widget.js", "")
	if recAsset.Code != http.StatusOK || recAsset.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET /plugins-assets/... failed: code=%d nosniff=%q body=%s",
			recAsset.Code, recAsset.Header().Get("X-Content-Type-Options"), recAsset.Body.String())
	}
}

func TestPluginDevReload(t *testing.T) {
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })
	setSettings(t, d, `{"captcha.provider": "none"}`)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Notify:   d.Notify,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	h, _ := New(d)
	superadmin := &browser{t: t, h: h}
	setupSite(t, d, superadmin)

	// Normal member must get 403 Forbidden.
	member := &browser{t: t, h: h}
	recReg := member.do(http.MethodPost, "/api/v1/auth/register", `{"email":"devuser@example.com","nickname":"DevUser","password":"dev-pass-123"}`)
	if recReg.Code != http.StatusOK {
		t.Fatalf("register member: %d %s", recReg.Code, recReg.Body.String())
	}

	payloadV1, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":          "dev-hot",
			"kind":        "plugin",
			"version":     "0.1.0",
			"name":        "Dev Hot",
			"apiVersion":  1,
			"server":      "server.js",
			"web":         "web/index.js",
			"permissions": []string{"routes.public"},
		},
		"serverJs": `export default { setup(ctx) { ctx.http.get("/ping", { auth: "public" }, () => ({ version: "v1" })); } };`,
		"files": map[string]string{
			"web/index.js": base64.StdEncoding.EncodeToString([]byte(`export const v = 1;`)),
		},
	})

	// Anonymous request must fail with 401.
	if rec := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "", string(payloadV1)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous dev-reload = %d, want 401", rec.Code)
	}

	// Regular user session must fail with 403.
	if rec := member.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", string(payloadV1)); rec.Code != http.StatusForbidden {
		t.Fatalf("member dev-reload = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	// Admin bearer token loads v1 cleanly.
	recV1 := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "Bearer "+testToken, string(payloadV1))
	if recV1.Code != http.StatusOK {
		t.Fatalf("v1 dev-reload = %d: %s", recV1.Code, recV1.Body.String())
	}

	recPing1 := (&browser{t: t, h: h}).do(http.MethodGet, "/api/plugins/dev-hot/ping", "")
	if recPing1.Code != http.StatusOK || !strings.Contains(recPing1.Body.String(), `"v1"`) {
		t.Fatalf("GET /api/plugins/dev-hot/ping v1 = %d %s", recPing1.Code, recPing1.Body.String())
	}

	// Superadmin session swaps runtime to v2 atomically without server restart.
	payloadV2, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":          "dev-hot",
			"kind":        "plugin",
			"version":     "0.2.0",
			"name":        "Dev Hot",
			"apiVersion":  1,
			"server":      "server.js",
			"web":         "web/index.js",
			"permissions": []string{"routes.public"},
		},
		"serverJs": `export default { setup(ctx) { ctx.http.get("/ping", { auth: "public" }, () => ({ version: "v2" })); } };`,
		"files": map[string]string{
			"web/index.js": base64.StdEncoding.EncodeToString([]byte(`export const v = 2;`)),
		},
	})
	recV2 := superadmin.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", string(payloadV2))
	if recV2.Code != http.StatusOK {
		t.Fatalf("v2 dev-reload = %d: %s", recV2.Code, recV2.Body.String())
	}

	recPing2 := (&browser{t: t, h: h}).do(http.MethodGet, "/api/plugins/dev-hot/ping", "")
	if recPing2.Code != http.StatusOK || !strings.Contains(recPing2.Body.String(), `"v2"`) {
		t.Fatalf("GET /api/plugins/dev-hot/ping v2 = %d %s", recPing2.Code, recPing2.Body.String())
	}
	recAsset2 := (&browser{t: t, h: h}).do(http.MethodGet, "/plugins-assets/dev-hot/web/index.js", "")
	if recAsset2.Code != http.StatusOK || !strings.Contains(recAsset2.Body.String(), `v = 2`) {
		t.Fatalf("GET /plugins-assets/dev-hot/web/index.js = %d %s", recAsset2.Code, recAsset2.Body.String())
	}
}

func TestBootstrapPluginsMenusFormsAndThemePreview(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })
	setSettings(t, d, `{"captcha.provider": "none"}`)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	manifest := []byte(`{
		"id": "ui-ext",
		"kind": "plugin",
		"version": "1.0.0",
		"name": {"en": "UI Ext", "zh-CN": "UI 扩展"},
		"apiVersion": 1,
		"web": "web/index.js",
		"slots": ["site.header.actions"],
		"menus": [
			{"area": "explore", "path": "/board", "title": {"en": "Board"}, "order": 20},
			{"area": "user", "path": "/invites", "title": {"en": "Invites"}, "order": 10},
			{"area": "admin", "path": "/codes", "title": {"en": "Codes"}, "role": "superadmin"}
		],
		"forms": {
			"auth.register": [{"name": "inviteCode", "type": "text", "label": {"en": "Invite Code"}, "required": true}]
		},
		"settings": {
			"publicHint": {"type": "string", "default": "hello"},
			"apiSecret": {"type": "secret", "default": "top-secret-never-leak"}
		}
	}`)
	staticFiles := map[string][]byte{
		"web/index.js":    []byte(`export default {};`),
		"web/index.css":   []byte(`.ui-ext { color: red; }`),
		"locales/en.json": []byte(`{"hello":"world"}`),
	}
	if _, err := mgr.LoadFromBytes(ctx, manifest, nil, staticFiles, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	h, _ := New(d)
	superadmin := &browser{t: t, h: h}
	setupSite(t, d, superadmin)

	// Guest bootstrap sees plugin metadata, filtered explore/user menus, non-secret settings, forms, and not active preview.
	guest := &browser{t: t, h: h}
	recGuest := guest.do(http.MethodGet, "/api/v1/site/bootstrap", "")
	if recGuest.Code != http.StatusOK {
		t.Fatalf("guest bootstrap = %d %s", recGuest.Code, recGuest.Body.String())
	}
	if strings.Contains(recGuest.Body.String(), "top-secret-never-leak") {
		t.Fatalf("secret plugin setting leaked into bootstrap: %s", recGuest.Body.String())
	}
	var gBoot Bootstrap
	if err := json.Unmarshal(recGuest.Body.Bytes(), &gBoot); err != nil {
		t.Fatal(err)
	}
	if len(gBoot.Plugins) != 1 || gBoot.Plugins[0].Entry == "" || gBoot.Plugins[0].Style == "" || gBoot.Plugins[0].Messages["hello"] != "world" {
		t.Fatalf("unexpected Bootstrap.Plugins: %+v", gBoot.Plugins)
	}
	if len(gBoot.Forms["auth.register"]) != 1 || gBoot.Forms["auth.register"][0].Name != "inviteCode" {
		t.Fatalf("unexpected Bootstrap.Forms: %+v", gBoot.Forms)
	}
	if len(gBoot.Menus["admin"]) != 0 {
		t.Fatalf("guest should not receive admin plugin menus: %+v", gBoot.Menus["admin"])
	}

	// Superadmin receives admin menus and can toggle session-scoped theme preview cookie.
	recPreview := superadmin.do(http.MethodPost, "/api/v1/admin/themes/preview", `{"themeId":"brocade"}`)
	if recPreview.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/admin/themes/preview = %d %s", recPreview.Code, recPreview.Body.String())
	}
	var previewCookie *http.Cookie
	for _, c := range recPreview.Result().Cookies() {
		if c.Name == "skinloom_theme_preview" {
			previewCookie = c
		}
	}
	if previewCookie == nil || previewCookie.Value != "brocade" || !previewCookie.HttpOnly {
		t.Fatalf("expected HttpOnly skinloom_theme_preview=brocade cookie, got %+v", previewCookie)
	}
	reqAdminBoot := httptest.NewRequest(http.MethodGet, "/api/v1/site/bootstrap", nil)
	reqAdminBoot.AddCookie(&http.Cookie{Name: SessionCookie, Value: superadmin.cookie})
	reqAdminBoot.AddCookie(&http.Cookie{Name: previewCookie.Name, Value: previewCookie.Value})
	recAdminBoot := httptest.NewRecorder()
	h.ServeHTTP(recAdminBoot, reqAdminBoot)
	var aBoot Bootstrap
	if err := json.Unmarshal(recAdminBoot.Body.Bytes(), &aBoot); err != nil {
		t.Fatal(err)
	}
	if !aBoot.Theme.Previewing || aBoot.Theme.ID != "brocade" {
		t.Fatalf("expected previewing brocade for admin session, got %+v", aBoot.Theme)
	}
	if len(aBoot.Menus["admin"]) != 1 || aBoot.Menus["admin"][0].Href != "/admin/p/ui-ext/codes" {
		t.Fatalf("expected superadmin plugin menu /admin/p/ui-ext/codes, got %+v", aBoot.Menus["admin"])
	}

	// Guest bootstrap remains on default theme even if client forges preview cookie!
	reqGuestForged := httptest.NewRequest(http.MethodGet, "/api/v1/site/bootstrap", nil)
	reqGuestForged.AddCookie(&http.Cookie{Name: "skinloom_theme_preview", Value: "brocade"})
	recGuest2 := httptest.NewRecorder()
	h.ServeHTTP(recGuest2, reqGuestForged)
	var gBoot2 Bootstrap
	_ = json.Unmarshal(recGuest2.Body.Bytes(), &gBoot2)
	if gBoot2.Theme.Previewing || gBoot2.Theme.ID == "brocade" {
		t.Fatalf("guest must not honor preview cookie: %+v", gBoot2.Theme)
	}

	// Clearing preview cookie restores active theme for admin.
	recClear := superadmin.do(http.MethodPost, "/api/v1/admin/themes/preview", `{"themeId":""}`)
	if recClear.Code != http.StatusOK {
		t.Fatalf("clear preview = %d %s", recClear.Code, recClear.Body.String())
	}
	var clearedCookie *http.Cookie
	for _, c := range recClear.Result().Cookies() {
		if c.Name == "skinloom_theme_preview" {
			clearedCookie = c
		}
	}
	if clearedCookie == nil || clearedCookie.MaxAge >= 0 {
		t.Fatalf("expected expired clear cookie, got %+v", clearedCookie)
	}
	recAdminCleared := superadmin.do(http.MethodGet, "/api/v1/site/bootstrap", "")
	var aCleared Bootstrap
	_ = json.Unmarshal(recAdminCleared.Body.Bytes(), &aCleared)
	if aCleared.Theme.Previewing || aCleared.Theme.ID != "default" {
		t.Fatalf("expected default theme after clear preview, got %+v", aCleared.Theme)
	}
}

func TestPluginGuardPropagatesRequestLocale(t *testing.T) {
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })
	d.Accounts.SetHooks(d.Bus)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registration": "open"}`)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Notify:   d.Notify,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	payload, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":         "i18n-guard",
			"kind":       "plugin",
			"version":    "1.0.0",
			"name":       map[string]string{"zh-CN": "多语言拦截器", "en": "i18n Guard"},
			"apiVersion": 1,
			"server":     "server.js",
			"hooks":      map[string]any{"guard": []string{"auth.register"}},
		},
		"serverJs": `export default {
			setup(ctx) {
				ctx.hooks.guard("auth.register", async () => reject(await ctx.t("denied")));
			}
		};`,
		"files": map[string]string{
			"locales/zh-CN.json": base64.StdEncoding.EncodeToString([]byte(`{"denied":"需要有效邀请码"}`)),
			"locales/en.json":    base64.StdEncoding.EncodeToString([]byte(`{"denied":"Valid invitation code required"}`)),
		},
	})
	if rec := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "Bearer "+testToken, string(payload)); rec.Code != http.StatusOK {
		t.Fatalf("dev-reload failed: %d %s", rec.Code, rec.Body.String())
	}

	// Request with locale=zh-CN cookie receives Chinese guard rejection
	reqZh := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"zh@example.com","nickname":"ZhUser","password":"password123"}`))
	reqZh.Header.Set("Content-Type", "application/json")
	reqZh.Header.Set(CSRFHeader, "1")
	reqZh.AddCookie(&http.Cookie{Name: "locale", Value: "zh-CN"})
	recZh := httptest.NewRecorder()
	h.ServeHTTP(recZh, reqZh)
	if recZh.Code != http.StatusUnprocessableEntity || !strings.Contains(recZh.Body.String(), "需要有效邀请码") {
		t.Fatalf("expected Chinese guard rejection, got %d: %s", recZh.Code, recZh.Body.String())
	}

	// Request with locale=en cookie receives English guard rejection
	reqEn := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"en@example.com","nickname":"EnUser","password":"password123"}`))
	reqEn.Header.Set("Content-Type", "application/json")
	reqEn.Header.Set(CSRFHeader, "1")
	reqEn.AddCookie(&http.Cookie{Name: "locale", Value: "en"})
	recEn := httptest.NewRecorder()
	h.ServeHTTP(recEn, reqEn)
	if recEn.Code != http.StatusUnprocessableEntity || !strings.Contains(recEn.Body.String(), "Valid invitation code required") {
		t.Fatalf("expected English guard rejection, got %d: %s", recEn.Code, recEn.Body.String())
	}
}

func buildTestSLP(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var checksums strings.Builder
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
		fsum := sha256.Sum256([]byte(content))
		checksums.WriteString(hex.EncodeToString(fsum[:]) + "  " + name + "\n")
	}
	cw, err := zw.Create("checksums.txt")
	if err != nil {
		t.Fatalf("zip create checksums.txt: %v", err)
	}
	if _, err := cw.Write([]byte(checksums.String())); err != nil {
		t.Fatalf("zip write checksums.txt: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestPluginPackageNotExposedViaTexturesEndpoint(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })

	texStore, err := storage.NewLocal(filepath.Join(dir, "textures"))
	if err != nil {
		t.Fatalf("texStore: %v", err)
	}
	pkgStore, err := storage.NewLocal(filepath.Join(dir, "plugin-packages"))
	if err != nil {
		t.Fatalf("pkgStore: %v", err)
	}

	slpBytes, slpSHA := buildTestSLP(t, map[string]string{
		"plugin.json": `{"id":"priv-pkg","kind":"plugin","version":"1.0.0","name":"Priv","apiVersion":1,"server":"server.js"}`,
		"server.js":   `export default { setup() {} };`,
	})
	// Simulate an unmigrated v1.1 .slp blob left in the public texture directory.
	if err := texStore.Put(ctx, slpSHA, slpBytes); err != nil {
		t.Fatalf("seed legacy texStore: %v", err)
	}

	h, _ := New(d)
	// Defense-in-depth: /textures/{hash} must refuse non-PNG blobs (such as ZIP .slp archives).
	if rec := get(t, h, nil, "/textures/"+slpSHA); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /textures/%s = %d, want 404 for non-PNG .slp archive", slpSHA, rec.Code)
	}

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:          d.DB.Ent,
		Bus:         d.Bus,
		Settings:    d.Settings,
		Store:       pkgStore,
		LegacyStore: texStore,
		DataDir:     dir,
		Log:         d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })

	// InstallPackage must write into private pkgStore and NOT into public texStore.
	slp2Bytes, slp2SHA := buildTestSLP(t, map[string]string{
		"plugin.json": `{"id":"fresh-pkg","kind":"plugin","version":"1.0.0","name":"Fresh","apiVersion":1,"server":"server.js"}`,
		"server.js":   `export default { setup() {} };`,
	})
	if _, err := mgr.InstallPackage(ctx, slp2Bytes, plugin.InstallOptions{Enable: true, Source: "upload", AllowUnsigned: true}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if _, err := pkgStore.Stat(ctx, slp2SHA); err != nil {
		t.Fatalf("expected %s in private pkgStore: %v", slp2SHA, err)
	}
	if _, err := texStore.Stat(ctx, slp2SHA); err == nil {
		t.Fatalf("plugin archive %s leaked into public texStore", slp2SHA)
	}

	// Simulate a DB plugin row pointing to legacy slpSHA and verify Start() migrates it out of texStore.
	if _, err := d.DB.Ent.Plugin.Create().
		SetID("priv-pkg").
		SetKind("plugin").
		SetVersion("1.0.0").
		SetStatus("disabled").
		SetSource("upload").
		SetPackageSha256(slpSHA).
		Save(ctx); err != nil {
		t.Fatalf("seed legacy plugin row: %v", err)
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := pkgStore.Stat(ctx, slpSHA); err != nil {
		t.Fatalf("expected legacy archive migrated to pkgStore: %v", err)
	}
	if _, err := texStore.Stat(ctx, slpSHA); err == nil {
		t.Fatalf("expected legacy archive removed from public texStore after migration")
	}
}

func TestPluginPublicRoutesEndToEnd(t *testing.T) {
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	// Manifest validation must reject missing permission and reserved host routes.
	for _, bad := range []string{
		`{"id":"bad-perm","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"routes":{"public":["/mcskin/{name}.json"]}}`,
		`{"id":"bad-api-root","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api"]}}`,
		`{"id":"bad-api-wild","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api/*"]}}`,
		`{"id":"bad-api-v1","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api/v1/hijack"]}}`,
		`{"id":"bad-api-ygg","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api/yggdrasil/hijack"]}}`,
		`{"id":"bad-api-plg","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api/plugins/hijack"]}}`,
		`{"id":"bad-oauth","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/oauth/start"]}}`,
		`{"id":"bad-csl","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/steve.json"]}}`,
		`{"id":"bad-root","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/"]}}`,
	} {
		if _, err := plugin.ParseManifest([]byte(bad)); err == nil {
			t.Errorf("expected ParseManifest to reject %s", bad)
		}
	}
	// Non-reserved /api/<custom>/* sub-trees (e.g. MUA Union protocol) are allowed.
	if _, err := plugin.ParseManifest([]byte(`{"id":"ok-union","kind":"plugin","version":"1.0.0","name":"Union","apiVersion":1,"permissions":["routes.public"],"routes":{"public":["/api/union/*"]}}`)); err != nil {
		t.Fatalf("expected ParseManifest to accept /api/union/*, got %v", err)
	}

	h, _ := New(d)
	gw := gateway.New(h, d.Log, nil)
	gw.SetPublicRouteMatcher(mgr.HasPublicRoute)
	setupSite(t, d, &browser{t: t, h: h})

	payload, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":          "alt-proto",
			"kind":        "plugin",
			"version":     "1.0.0",
			"name":        "Alt Protocol",
			"apiVersion":  1,
			"server":      "server.js",
			"permissions": []string{"routes.public"},
			"routes":      map[string]any{"public": []string{"/mcskin/{name}.json"}},
		},
		"serverJs": `export default {
			setup(ctx) {
				ctx.http.get("/mcskin/{name}.json", { auth: "public" }, (req) => ({
					player: req.params.name,
					protocol: "alt"
				}));
			}
		};`,
	})
	if rec := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "Bearer "+testToken, string(payload)); rec.Code != http.StatusOK {
		t.Fatalf("dev-reload alt-proto: %d %s", rec.Code, rec.Body.String())
	}

	// Routed cleanly through Gateway (proving non-Reserved root paths reach plugin routes).
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcskin/Steve.json", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"player":"Steve"`) {
		t.Fatalf("GET /mcskin/Steve.json via Gateway = %d %s", rec.Code, rec.Body.String())
	}

	// Second plugin attempting to claim an overlapping public route must fail activation.
	conflictPayload, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":          "alt-proto-dup",
			"kind":        "plugin",
			"version":     "1.0.0",
			"name":        "Dup",
			"apiVersion":  1,
			"server":      "server.js",
			"permissions": []string{"routes.public"},
			"routes":      map[string]any{"public": []string{"/mcskin/*"}},
		},
		"serverJs": `export default { setup() {} };`,
	})
	if recConf := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "Bearer "+testToken, string(conflictPayload)); recConf.Code == http.StatusOK {
		t.Fatalf("expected conflicting routes.public activation to fail, got 200")
	}
}

func TestPluginAuthProviderEndToEnd(t *testing.T) {
	d, dir := newTestDepsDir(t)
	d.Bus = hook.NewBus(d.Log, 2)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })
	setSettings(t, d, `{"captcha.provider": "none", "auth.registration": "open"}`)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Accounts: d.Accounts,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	payload, _ := json.Marshal(map[string]any{
		"manifest": map[string]any{
			"id":          "forum-sso",
			"kind":        "plugin",
			"version":     "1.0.0",
			"name":        "Forum SSO",
			"apiVersion":  1,
			"server":      "server.js",
			"permissions": []string{"auth.provider"},
		},
		"serverJs": `export default {
			setup(ctx) {
				ctx.auth.registerProvider(
					{ id: "forum", name: "Community Forum", authUrl: "/api/plugins/forum-sso/start", mode: "redirect" },
					async (input) => {
						if (input.code !== "valid-code") {
							throw new Error("bad code");
						}
						return {
							subject: "forum-uid-99",
							email: "forum99@example.com",
							emailVerified: false,
							nickname: "ForumHero",
							next: input.next || "/user/closet"
						};
					}
				);
			}
		};`,
	})
	if rec := do(t, h, http.MethodPost, "/api/v1/admin/plugins/dev-reload", "Bearer "+testToken, string(payload)); rec.Code != http.StatusOK {
		t.Fatalf("dev-reload forum-sso: %d %s", rec.Code, rec.Body.String())
	}

	// 1. Bootstrap must expose registered plugin auth provider.
	recBoot := get(t, h, nil, "/api/v1/site/bootstrap")
	if recBoot.Code != http.StatusOK {
		t.Fatalf("bootstrap = %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot Bootstrap
	if err := json.Unmarshal(recBoot.Body.Bytes(), &boot); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if len(boot.Auth.Providers) != 1 || boot.Auth.Providers[0].ID != "forum" || boot.Auth.Providers[0].Name != "Community Forum" {
		t.Fatalf("unexpected bootstrap auth.providers: %+v", boot.Auth.Providers)
	}

	// 2. POST /api/v1/auth/providers/forum/verify without CSRF header must be rejected (403).
	reqNoCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/auth/providers/forum/verify", strings.NewReader(`{"input":{"code":"valid-code"}}`))
	reqNoCSRF.Header.Set("Content-Type", "application/json")
	recNoCSRF := httptest.NewRecorder()
	h.ServeHTTP(recNoCSRF, reqNoCSRF)
	if recNoCSRF.Code != http.StatusForbidden {
		t.Fatalf("POST verify without CSRF = %d, want 403", recNoCSRF.Code)
	}

	// 3. First-time user via POST verify opens pending signup flow with skinloom_social cookie.
	userBrowser := &browser{t: t, h: h}
	recVerify := userBrowser.do(http.MethodPost, "/api/v1/auth/providers/forum/verify", `{"input":{"code":"valid-code"}}`)
	if recVerify.Code != http.StatusOK || !strings.Contains(recVerify.Body.String(), `"signup_required"`) {
		t.Fatalf("first verify = %d %s", recVerify.Code, recVerify.Body.String())
	}
	var socialCookie *http.Cookie
	for _, c := range recVerify.Result().Cookies() {
		if c.Name == SocialCookie {
			socialCookie = c
		}
	}
	if socialCookie == nil || socialCookie.Value == "" {
		t.Fatalf("expected %s cookie from verify, got %+v", SocialCookie, recVerify.Result().Cookies())
	}
	// GET /api/v1/auth/social/pending resolves plugin provider display name & type.
	reqPending := httptest.NewRequest(http.MethodGet, "/api/v1/auth/social/pending", nil)
	reqPending.AddCookie(socialCookie)
	recPending := httptest.NewRecorder()
	h.ServeHTTP(recPending, reqPending)
	if recPending.Code != http.StatusOK || !strings.Contains(recPending.Body.String(), `"Community Forum"`) || !strings.Contains(recPending.Body.String(), `"plugin"`) {
		t.Fatalf("social pending = %d %s", recPending.Code, recPending.Body.String())
	}
	// Complete registration via /api/v1/auth/social/pending/register.
	reqReg := httptest.NewRequest(http.MethodPost, "/api/v1/auth/social/pending/register", strings.NewReader(`{"email":"forum99@example.com","nickname":"ForumHero"}`))
	reqReg.Header.Set("Content-Type", "application/json")
	reqReg.Header.Set(CSRFHeader, "1")
	reqReg.AddCookie(socialCookie)
	recReg := httptest.NewRecorder()
	h.ServeHTTP(recReg, reqReg)
	if recReg.Code != http.StatusOK {
		t.Fatalf("social register = %d %s", recReg.Code, recReg.Body.String())
	}

	// 4. Returning user via browser redirect GET /api/v1/auth/providers/forum/callback receives session cookie and redirect.
	cbBrowser := &browser{t: t, h: h}
	recCB := cbBrowser.do(http.MethodGet, "/api/v1/auth/providers/forum/callback?code=valid-code&next=/user/closet", "")
	if recCB.Code != http.StatusFound || recCB.Header().Get("Location") != "/user/closet" {
		t.Fatalf("GET callback = %d loc=%q", recCB.Code, recCB.Header().Get("Location"))
	}
	recMe := cbBrowser.do(http.MethodGet, "/api/v1/auth/me", "")
	if recMe.Code != http.StatusOK || !strings.Contains(recMe.Body.String(), `"ForumHero"`) {
		t.Fatalf("GET /api/v1/auth/me after callback = %d %s", recMe.Code, recMe.Body.String())
	}

	// 5. Failed callback redirects cleanly to /auth/login?social_error=failed.
	recFail := (&browser{t: t, h: h}).do(http.MethodGet, "/api/v1/auth/providers/forum/callback?code=wrong", "")
	if recFail.Code != http.StatusFound || recFail.Header().Get("Location") != "/auth/login?social_error=failed" {
		t.Fatalf("GET failed callback = %d loc=%q", recFail.Code, recFail.Header().Get("Location"))
	}
}
