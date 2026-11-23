package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

func buildHTTPTestSLP(t *testing.T, priv ed25519.PrivateKey) []byte {
	t.Helper()
	files := map[string][]byte{
		"plugin.json": []byte(`{
			"id": "http-pkg",
			"kind": "plugin",
			"version": "1.0.0",
			"name": {"en": "HTTP Pkg"},
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv", "users.ban", "routes.public"],
			"routes": { "public": ["/ping"] },
			"settings": {
				"banner": { "type": "string", "default": "welcome" },
				"webhookSecret": { "type": "secret" }
			}
		}`),
		"server.js": []byte(`
			export default {
				setup(ctx) {
					ctx.log.info("http-pkg ready");
					ctx.http.get("/ping", { auth: "public" }, async () => ({
						banner: await ctx.settings.get("banner"),
						hasSecret: Boolean(await ctx.settings.get("webhookSecret"))
					}));
				}
			};
		`),
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	var cs strings.Builder
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&cs, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	cw, err := zw.Create("checksums.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cw.Write([]byte(cs.String())); err != nil {
		t.Fatal(err)
	}
	if priv != nil {
		sig := ed25519.Sign(priv, []byte(cs.String()))
		sw, err := zw.Create("signature")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sw.Write([]byte(base64.StdEncoding.EncodeToString(sig) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func doMultipartSLP(t *testing.T, h http.Handler, b *browser, targetPath string, slp []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "plugin.slp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(slp); err != nil {
		t.Fatal(err)
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, targetPath, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(CSRFHeader, "1")
	if b != nil && b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: b.cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminPluginPackageEndpointsAndRBAC(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	d.Bus = hook.NewBus(d.Log, 4)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })

	store, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(pub)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:                d.DB.Ent,
		Bus:               d.Bus,
		Settings:          d.Settings,
		Store:             store,
		DataDir:           dir,
		TrustedPublicKeys: []string{pubPEM},
		Log:               d.Log,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr
	setSettings(t, d, `{"captcha.provider": "none"}`)

	h, _ := New(d)
	owner := &browser{t: t, h: h}
	setupSite(t, d, owner)
	regularUser := registerUser(t, h, "user@example.com")
	mod := registerUser(t, h, "mod@example.com")
	if err := d.DB.Ent.User.Update().Where(user.EmailNormalized("mod@example.com")).SetRole(user.RoleAdmin).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	signedSLP := buildHTTPTestSLP(t, priv)
	unsignedSLP := buildHTTPTestSLP(t, nil)

	// Regular user cannot list/inspect.
	if rec := regularUser.do(http.MethodGet, "/api/v1/admin/plugins", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user listing plugins, got %d", rec.Code)
	}

	// Regular admin CAN inspect, CANNOT install.
	if rec := doMultipartSLP(t, h, mod, "/api/v1/admin/plugins/inspect", unsignedSLP, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin inspect failed: %d %s", rec.Code, rec.Body.String())
	} else if !strings.Contains(rec.Body.String(), `"high_risk"`) {
		t.Fatalf("expected high_risk tier in inspect response: %s", rec.Body.String())
	}
	if rec := doMultipartSLP(t, h, mod, "/api/v1/admin/plugins/install", signedSLP, map[string]string{"grantAll": "true"}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when non-superadmin tries install, got %d %s", rec.Code, rec.Body.String())
	}

	// Superadmin installing unsigned without allowUnsigned must fail cleanly (422).
	if rec := doMultipartSLP(t, h, owner, "/api/v1/admin/plugins/install", unsignedSLP, map[string]string{"grantAll": "true"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on unsigned install without allowUnsigned, got %d %s", rec.Code, rec.Body.String())
	}

	// Superadmin installs signed SLP granting only storage.kv.
	rec := doMultipartSLP(t, h, owner, "/api/v1/admin/plugins/install", signedSLP, map[string]string{
		"grantedPermissions": `["storage.kv"]`,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("superadmin install failed: %d %s", rec.Code, rec.Body.String())
	}

	// Regular admin CAN list, get details (with recentLogs), and update settings (secrets preserved).
	if rec := mod.do(http.MethodGet, "/api/v1/admin/plugins", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"http-pkg"`) {
		t.Fatalf("admin list failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mod.do(http.MethodGet, "/api/v1/admin/plugins/http-pkg", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "http-pkg ready") {
		t.Fatalf("admin get failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mod.do(http.MethodPatch, "/api/v1/admin/plugins/http-pkg/settings", `{"banner":"hello","webhookSecret":"top-secret"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings patch failed: %d %s", rec.Code, rec.Body.String())
	} else if strings.Contains(rec.Body.String(), "top-secret") {
		t.Fatalf("secret echoed back in PATCH response: %s", rec.Body.String())
	}
	if rec := mod.do(http.MethodPatch, "/api/v1/admin/plugins/http-pkg/settings", jsonStringify(t, map[string]any{"banner": "hello2", "webhookSecret": settings.Redacted})); rec.Code != http.StatusOK {
		t.Fatalf("settings sentinel patch failed: %d %s", rec.Code, rec.Body.String())
	}

	// Plugin HTTP route confirms updated banner and preserved secret!
	if rec := do(t, h, http.MethodGet, "/api/plugins/http-pkg/ping", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hasSecret":true`) || !strings.Contains(rec.Body.String(), `"banner":"hello2"`) {
		t.Fatalf("plugin route mismatch: %d %s", rec.Code, rec.Body.String())
	}

	// Disable / Enable / Reload by admin.
	if rec := mod.do(http.MethodPost, "/api/v1/admin/plugins/http-pkg/disable", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"disabled"`) {
		t.Fatalf("disable failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mod.do(http.MethodPost, "/api/v1/admin/plugins/http-pkg/enable", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"enabled"`) {
		t.Fatalf("enable failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mod.do(http.MethodPost, "/api/v1/admin/plugins/http-pkg/reload", ""); rec.Code != http.StatusOK {
		t.Fatalf("reload failed: %d %s", rec.Code, rec.Body.String())
	}

	// Regular admin CANNOT uninstall; Superadmin CAN uninstall?purge=true.
	if rec := mod.do(http.MethodDelete, "/api/v1/admin/plugins/http-pkg?purge=true", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when non-superadmin tries uninstall, got %d", rec.Code)
	}
	if rec := owner.do(http.MethodDelete, "/api/v1/admin/plugins/http-pkg?purge=true", ""); rec.Code != http.StatusOK {
		t.Fatalf("superadmin uninstall failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := owner.do(http.MethodGet, "/api/v1/admin/plugins/http-pkg", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after uninstall, got %d", rec.Code)
	}
}

func jsonStringify(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAdminPluginDevReloadAndThemeBootstrapRegressions(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	d.Bus = hook.NewBus(d.Log, 4)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Themes:   d.Themes,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr
	setSettings(t, d, `{"captcha.provider": "none"}`)

	h, _ := New(d)
	owner := &browser{t: t, h: h}
	setupSite(t, d, owner)
	mod := registerUser(t, h, "mod@example.com")
	if err := d.DB.Ent.User.Update().Where(user.EmailNormalized("mod@example.com")).SetRole(user.RoleAdmin).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	// Regular admin receives 403 on dev-reload.
	payload := jsonStringify(t, map[string]any{
		"manifest": map[string]any{
			"id":         "dev-mod",
			"kind":       "plugin",
			"version":    "1.0.0",
			"name":       "Dev",
			"apiVersion": 1,
		},
	})
	if rec := mod.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", payload); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-superadmin dev-reload, got %d", rec.Code)
	}

	// Path traversal in dev-reload files map fails with 422.
	badFilesPayload := jsonStringify(t, map[string]any{
		"manifest": map[string]any{
			"id":         "dev-bad-path",
			"kind":       "plugin",
			"version":    "1.0.0",
			"name":       "DevBad",
			"apiVersion": 1,
		},
		"files": map[string]string{
			"../escape.js": base64.StdEncoding.EncodeToString([]byte("x")),
		},
	})
	if rec := owner.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", badFilesPayload); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on dev-reload path traversal, got %d %s", rec.Code, rec.Body.String())
	}

	// Syntax error in dev-reload serverJs returns 422 (not 500).
	badJSPayload := jsonStringify(t, map[string]any{
		"manifest": map[string]any{
			"id":         "dev-bad-js",
			"kind":       "plugin",
			"version":    "1.0.0",
			"name":       "DevBadJS",
			"apiVersion": 1,
			"server":     "server.js",
		},
		"serverJs": "export default { broken syntax ===",
	})
	if rec := owner.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", badJSPayload); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid dev-reload serverJs, got %d %s", rec.Code, rec.Body.String())
	}

	// Valid dev-reload records audit log and bootstrap deduplicates inactive/active kind:theme entries.
	okPayload := jsonStringify(t, map[string]any{
		"manifest": map[string]any{
			"id":         "dev-audit",
			"kind":       "plugin",
			"version":    "1.0.0",
			"name":       "DevOK",
			"apiVersion": 1,
			"server":     "server.js",
		},
		"serverJs": `export default { setup() {} };`,
	})
	if rec := owner.do(http.MethodPost, "/api/v1/admin/plugins/dev-reload", okPayload); rec.Code != http.StatusOK {
		t.Fatalf("valid dev-reload failed: %d %s", rec.Code, rec.Body.String())
	}
	if cnt, _ := d.DB.Ent.AuditLog.Query().Where(auditlog.Action("plugin.dev_reload")).Count(ctx); cnt != 1 {
		t.Fatalf("expected 1 plugin.dev_reload audit log, got %d", cnt)
	}

	// Inactive kind:theme plugin must not appear in site bootstrap.
	themeManifest := []byte(`{
		"id": "inactive-theme",
		"kind": "theme",
		"version": "1.0.0",
		"name": "Inactive Theme",
		"apiVersion": 1,
		"web": "web/index.js"
	}`)
	if _, err := mgr.LoadFromBytes(ctx, themeManifest, nil, map[string][]byte{
		"web/index.js":  []byte(`export default {};`),
		"web/index.css": []byte(`.x{}`),
	}, nil, true); err != nil {
		t.Fatalf("LoadFromBytes inactive-theme: %v", err)
	}
	recBoot := (&browser{t: t, h: h}).do(http.MethodGet, "/api/v1/site/bootstrap", "")
	if recBoot.Code != http.StatusOK {
		t.Fatalf("bootstrap = %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot Bootstrap
	if err := json.Unmarshal(recBoot.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}
	for _, fp := range boot.Plugins {
		if fp.ID == "inactive-theme" {
			t.Fatalf("inactive kind:theme plugin must not be emitted in bootstrap.Plugins: %+v", fp)
		}
	}

	// CSWSH protection: cookie-authenticated WebSocket upgrade with cross-site Origin must be rejected with 403.
	wsCrossReq := httptest.NewRequest(http.MethodGet, "/api/plugins/dev-audit/ws", nil)
	wsCrossReq.Header.Set("Connection", "Upgrade")
	wsCrossReq.Header.Set("Upgrade", "websocket")
	wsCrossReq.Header.Set("Origin", "https://evil.example")
	wsCrossReq.AddCookie(&http.Cookie{Name: SessionCookie, Value: owner.cookie})
	wsCrossRec := httptest.NewRecorder()
	h.ServeHTTP(wsCrossRec, wsCrossReq)
	if wsCrossRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-origin cookie-authenticated WebSocket upgrade, got %d", wsCrossRec.Code)
	}

	// Same-origin WebSocket upgrade passes csrfGuard (reaching 404 on unregistered route).
	wsSameReq := httptest.NewRequest(http.MethodGet, "/api/plugins/dev-audit/ws", nil)
	wsSameReq.Header.Set("Connection", "Upgrade")
	wsSameReq.Header.Set("Upgrade", "websocket")
	wsSameReq.Header.Set("Origin", "http://example.com")
	wsSameReq.AddCookie(&http.Cookie{Name: SessionCookie, Value: owner.cookie})
	wsSameRec := httptest.NewRecorder()
	h.ServeHTTP(wsSameRec, wsSameReq)
	if wsSameRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (passed csrfGuard) on same-origin WebSocket upgrade, got %d", wsSameRec.Code)
	}
}

func TestAdminMarketEndpointsAndRBAC(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	d.Bus = hook.NewBus(d.Log, 4)
	t.Cleanup(func() { d.Bus.Close(context.Background()) })

	store, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:                d.DB.Ent,
		Bus:               d.Bus,
		Settings:          d.Settings,
		Store:             store,
		DataDir:           dir,
		TrustedPublicKeys: []string{pubPEM},
		Log:               d.Log,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr
	setSettings(t, d, `{"captcha.provider": "none"}`)

	h, _ := New(d)
	owner := &browser{t: t, h: h}
	setupSite(t, d, owner)
	mod := registerUser(t, h, "market-mod@example.com")
	if err := d.DB.Ent.User.Update().Where(user.EmailNormalized("market-mod@example.com")).SetRole(user.RoleAdmin).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	slpBytes := buildHTTPTestSLP(t, priv)
	sum := sha256.Sum256(slpBytes)
	shaHex := hex.EncodeToString(sum[:])

	var blockedState []plugin.RegistryBlocked
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := plugin.RegistryIndex{
			SchemaVersion: 1,
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
			RegistryID:    "official",
			Packages: []plugin.RegistryPackage{
				{
					ID:       "http-pkg",
					Kind:     "plugin",
					Runtime:  "js",
					Category: "security",
					Official: true,
					Name:     plugin.LocalizedText{"en": "HTTP Pkg", "zh-CN": "市场测试插件"},
					Latest:   "1.0.0",
					Versions: []plugin.RegistryVersion{
						{
							Version:     "1.0.0",
							APIVersion:  1,
							Permissions: []string{"storage.kv", "users.ban", "routes.public"},
							Artifacts: map[string]plugin.RegistryArtifact{
								"any": {
									URLs:   []string{"/packages/http-pkg-1.0.0.slp"},
									SHA256: shaHex,
									Size:   int64(len(slpBytes)),
								},
							},
						},
					},
				},
			},
			Blocked: blockedState,
		}
		rawIdx, _ := json.Marshal(idx)
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, rawIdx))
		switch r.URL.Path {
		case "/index.v1.json":
			_, _ = w.Write(rawIdx)
		case "/index.v1.json.sig":
			_, _ = w.Write([]byte(sig))
		case "/packages/http-pkg-1.0.0.slp":
			_, _ = w.Write(slpBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer regSrv.Close()

	mgr.Registry().SetOfficialPublicKey(pubPEM)
	mgr.Registry().SetRegistriesForTest([]settings.RegistrySource{
		{
			ID:       "official",
			Name:     "Official Market",
			URLs:     []string{regSrv.URL},
			Official: true,
			Enabled:  true,
		},
	})

	// Regular admin can list and inspect market packages.
	listRec := mod.do(http.MethodGet, "/api/v1/admin/market/packages?compatibleOnly=true", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /admin/market/packages = %d %s", listRec.Code, listRec.Body.String())
	}
	inspRec := mod.do(http.MethodPost, "/api/v1/admin/market/inspect", `{"registryId":"official","id":"http-pkg"}`)
	if inspRec.Code != http.StatusOK {
		t.Fatalf("POST /admin/market/inspect = %d %s", inspRec.Code, inspRec.Body.String())
	}

	// Regular admin cannot install from market (403); superadmin can (201).
	if rec := mod.do(http.MethodPost, "/api/v1/admin/market/install", `{"registryId":"official","id":"http-pkg","grantAll":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-superadmin market install, got %d", rec.Code)
	}
	instRec := owner.do(http.MethodPost, "/api/v1/admin/market/install", `{"registryId":"official","id":"http-pkg","grantAll":true}`)
	if instRec.Code != http.StatusCreated {
		t.Fatalf("superadmin POST /admin/market/install = %d %s", instRec.Code, instRec.Body.String())
	}

	// Refresh market after adding a block rule disables the plugin automatically.
	blockedState = []plugin.RegistryBlocked{{ID: "http-pkg", Versions: "*", Reason: "malicious test"}}
	refRec := mod.do(http.MethodPost, "/api/v1/admin/market/refresh", "")
	if refRec.Code != http.StatusOK {
		t.Fatalf("POST /admin/market/refresh = %d %s", refRec.Code, refRec.Body.String())
	}
	info, ok := mgr.Get(ctx, "http-pkg")
	if !ok || info.Status != "disabled" {
		t.Fatalf("expected http-pkg disabled after refresh blocklist check, got %+v", info)
	}
}
