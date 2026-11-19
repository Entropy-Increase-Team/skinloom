package plugin

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

func buildPackageBundle(t *testing.T, id, version string, perms []string, priv ed25519.PrivateKey) ([]byte, string) {
	t.Helper()
	permsJSON, _ := json.Marshal(perms)
	files := map[string][]byte{
		"plugin.json": []byte(`{
			"id": "` + id + `",
			"kind": "plugin",
			"version": "` + version + `",
			"name": {"en": "Cluster Demo"},
			"apiVersion": 1,
			"server": "server.js",
			"routes": { "public": ["/info", "/kv"] },
			"permissions": ` + string(permsJSON) + `,
			"settings": {
				"greeting": { "type": "string", "default": "hello", "order": 1 },
				"apiSecret": { "type": "secret", "order": 2 },
				"retries": { "type": "number", "default": 2, "min": 1, "max": 5, "order": 3 }
			}
		}`),
		"server.js": []byte(`
			let upgradedFrom = null;
			export default {
				onUpgrade(ctx, meta) { upgradedFrom = meta && meta.from; },
				setup(ctx) {
					ctx.http.get("/info", { auth: "public" }, async () => {
						const greeting = await ctx.settings.get("greeting");
						const secret = await ctx.settings.get("apiSecret");
						return {
							version: "` + version + `",
							greeting,
							hasSecret: Boolean(secret),
							secretLen: String(secret || "").length,
							upgradedFrom
						};
					});
					ctx.http.get("/kv", { auth: "public" }, async () => {
						await ctx.kv.set("k1", { v: "` + version + `" }, 0);
						return { saved: true, kv: await ctx.kv.get("k1") };
					});
				}
			};
		`),
	}
	slp := buildTestSLP(t, files, priv, false, false)
	return slp, id
}

func TestInstallUpgradeSecretSettingsAndPurge(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()

	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "pkg_mgr.db", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatal(err)
	}

	store, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	bus := hook.NewBus(log, 4)
	defer bus.Close(ctx)

	priv, pubPEM := testKeypairPEM(t)
	mgr, err := NewManager(ManagerDeps{
		DB:                database.Ent,
		Bus:               bus,
		Store:             store,
		DataDir:           dir,
		TrustedPublicKeys: []string{pubPEM},
		Log:               log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Shutdown(ctx) }()

	// Unsigned install without AllowUnsigned must fail.
	v1Unsigned, id := buildPackageBundle(t, "cluster-demo", "1.0.0", []string{"storage.kv", "users.ban"}, nil)
	if _, err := mgr.InstallPackage(ctx, v1Unsigned, InstallOptions{GrantAllDeclared: true, Enable: true}); !errors.Is(err, ErrUntrustedPackage) {
		t.Fatalf("expected ErrUntrustedPackage, got %v", err)
	}

	// Inspect & install signed v1 package.
	v1Signed, _ := buildPackageBundle(t, id, "1.0.0", []string{"storage.kv", "users.ban"}, priv)
	inspect1, err := mgr.InspectPackage(ctx, v1Signed, "en")
	if err != nil {
		t.Fatalf("InspectPackage v1: %v", err)
	}
	if !inspect1.Verification.Trusted || !inspect1.RequiresHighRiskConfirm || inspect1.IsUpgrade {
		t.Fatalf("unexpected inspect v1: %+v", inspect1)
	}

	info1, err := mgr.InstallPackage(ctx, v1Signed, InstallOptions{
		GrantedPermissions: []string{"storage.kv"},
		Enable:             true,
	})
	if err != nil {
		t.Fatalf("InstallPackage v1: %v", err)
	}
	if info1.PackageSHA256 == "" || info1.Status != "enabled" {
		t.Fatalf("unexpected info1: %+v", info1)
	}
	if _, err := store.Stat(ctx, info1.PackageSHA256); err != nil {
		t.Fatalf("expected .slp stored under SHA256 key: %v", err)
	}

	// Save secret + normal setting, then verify redaction + sentinel preservation.
	if err := mgr.UpdateSettings(ctx, id, map[string]any{
		"greeting":  "hi",
		"apiSecret": "super-secret-token",
	}); err != nil {
		t.Fatalf("UpdateSettings secret: %v", err)
	}
	sv, err := mgr.PluginSettingsView(ctx, id, "en")
	if err != nil {
		t.Fatalf("PluginSettingsView: %v", err)
	}
	if _, leaked := sv.Values["apiSecret"]; leaked {
		t.Fatalf("secret leaked in settings values map: %+v", sv.Values)
	}
	var secretField *PluginSettingFieldView
	for i := range sv.Fields {
		if sv.Fields[i].Key == "apiSecret" {
			secretField = &sv.Fields[i]
		}
	}
	if secretField == nil || !secretField.Secret || !secretField.IsSet || secretField.Value != nil {
		t.Fatalf("unexpected secret field view: %+v", secretField)
	}

	// Update with sentinel "********" must keep existing secret intact!
	if err := mgr.UpdateSettings(ctx, id, map[string]any{
		"greeting":  "hola",
		"apiSecret": settings.Redacted,
	}); err != nil {
		t.Fatalf("UpdateSettings redacted sentinel: %v", err)
	}

	// Invalid range must fail.
	if err := mgr.UpdateSettings(ctx, id, map[string]any{"retries": 99}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("expected ErrInvalidSettings for out-of-range retries, got %v", err)
	}

	callInfo := func() map[string]any {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/plugins/cluster-demo/info", nil)
		mgr.ServePluginAPI(rec, req, id, "/info", Viewer{}, "en")
		if rec.Code != http.StatusOK {
			t.Fatalf("route /info status %d body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	got1 := callInfo()
	if got1["greeting"] != "hola" || got1["hasSecret"] != true || int(got1["secretLen"].(float64)) != len("super-secret-token") {
		t.Fatalf("unexpected route v1 response (secret should survive sentinel update): %+v", got1)
	}

	// Trigger KV write before upgrade to test preservation across upgrade & purge behavior.
	recKV := httptest.NewRecorder()
	mgr.ServePluginAPI(recKV, httptest.NewRequest(http.MethodGet, "/api/plugins/cluster-demo/kv", nil), id, "/kv", Viewer{}, "en")
	if recKV.Code != http.StatusOK {
		t.Fatalf("route /kv status %d", recKV.Code)
	}

	// Upgrade to v2 and confirm old Store blob is pruned and onUpgrade ran!
	v2Signed, _ := buildPackageBundle(t, id, "2.0.0", []string{"storage.kv", "http.fetch"}, priv)
	inspect2, err := mgr.InspectPackage(ctx, v2Signed, "en")
	if err != nil {
		t.Fatalf("InspectPackage v2: %v", err)
	}
	if !inspect2.IsUpgrade || inspect2.CurrentVersion != "1.0.0" {
		t.Fatalf("expected upgrade metadata, got %+v", inspect2)
	}

	info2, err := mgr.InstallPackage(ctx, v2Signed, InstallOptions{
		GrantedPermissions: []string{"storage.kv", "http.fetch"},
		Enable:             true,
	})
	if err != nil {
		t.Fatalf("InstallPackage v2: %v", err)
	}
	if info2.PackageSHA256 == info1.PackageSHA256 {
		t.Fatal("expected different archive SHA256 for v2")
	}
	if _, err := store.Stat(ctx, info1.PackageSHA256); err == nil {
		t.Fatal("expected replaced v1 .slp blob to be removed from Store")
	}
	if info2.Usage == nil || info2.Usage.KVKeys != 1 {
		t.Fatalf("expected KV usage count 1 after upgrade, got %+v", info2.Usage)
	}
	got2 := callInfo()
	if got2["version"] != "2.0.0" || got2["upgradedFrom"] != "1.0.0" || got2["hasSecret"] != true {
		t.Fatalf("unexpected route v2 response: %+v", got2)
	}

	// Uninstall with purge=false removes files & blob, keeps KV.
	if err := mgr.Uninstall(ctx, id, false); err != nil {
		t.Fatalf("Uninstall(purge=false): %v", err)
	}
	if cnt, _ := database.Ent.PluginKV.Query().Count(ctx); cnt != 1 {
		t.Fatalf("expected KV preserved when purge=false, count=%d", cnt)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", id)); !os.IsNotExist(err) {
		t.Fatalf("expected plugin extract dir removed on uninstall, stat err=%v", err)
	}
	if _, err := store.Stat(ctx, info2.PackageSHA256); err == nil {
		t.Fatal("expected v2 .slp blob deleted from Store after uninstall")
	}

	// Reinstall then Uninstall with purge=true clears KV too.
	if _, err := mgr.InstallPackage(ctx, v2Signed, InstallOptions{GrantAllDeclared: true, Enable: true}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Uninstall(ctx, id, true); err != nil {
		t.Fatalf("Uninstall(purge=true): %v", err)
	}
	if cnt, _ := database.Ent.PluginKV.Query().Count(ctx); cnt != 0 {
		t.Fatalf("expected KV wiped when purge=true, count=%d", cnt)
	}
}

func TestMultiReplicaSyncClusterAndThemeInstall(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sharedDir := t.TempDir()
	nodeADir := filepath.Join(sharedDir, "nodeA")
	nodeBDir := filepath.Join(sharedDir, "nodeB")

	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "cluster.db", DataDir: sharedDir})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(ctx, database, sharedDir, log); err != nil {
		t.Fatal(err)
	}

	sharedStore, err := storage.NewLocal(filepath.Join(sharedDir, "shared-store"))
	if err != nil {
		t.Fatal(err)
	}

	priv, pubPEM := testKeypairPEM(t)

	themesB := theme.NewRegistry()
	themesB.SetLocalDir(filepath.Join(nodeBDir, "themes"), log)

	busA := hook.NewBus(log, 2)
	defer busA.Close(ctx)
	busB := hook.NewBus(log, 2)
	defer busB.Close(ctx)

	mgrA, err := NewManager(ManagerDeps{
		DB: database.Ent, Bus: busA, Store: sharedStore, DataDir: nodeADir,
		TrustedPublicKeys: []string{pubPEM}, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgrA.Shutdown(ctx) }()

	mgrB, err := NewManager(ManagerDeps{
		DB: database.Ent, Bus: busB, Store: sharedStore, Themes: themesB, DataDir: nodeBDir,
		TrustedPublicKeys: []string{pubPEM}, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgrB.Shutdown(ctx) }()

	// Install a plugin on Node A; Node B pulls .slp from shared Store via SyncCluster.
	pkgSLP, id := buildPackageBundle(t, "sync-test", "1.0.0", []string{"storage.kv"}, priv)
	if _, err := mgrA.InstallPackage(ctx, pkgSLP, InstallOptions{GrantAllDeclared: true, Enable: true}); err != nil {
		t.Fatalf("Node A InstallPackage: %v", err)
	}
	if err := mgrB.SyncCluster(ctx); err != nil {
		t.Fatalf("Node B SyncCluster: %v", err)
	}
	rec := httptest.NewRecorder()
	mgrB.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/sync-test/info", nil), id, "/info", Viewer{}, "en")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"1.0.0"`) {
		t.Fatalf("Node B did not activate synced plugin: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Update settings on Node A; Node B syncs without cold rebuild.
	if err := mgrA.UpdateSettings(ctx, id, map[string]any{"greeting": "synced-greeting"}); err != nil {
		t.Fatal(err)
	}
	if err := mgrB.SyncCluster(ctx); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	mgrB.ServePluginAPI(rec2, httptest.NewRequest(http.MethodGet, "/api/plugins/sync-test/info", nil), id, "/info", Viewer{}, "en")
	if !strings.Contains(rec2.Body.String(), `"greeting":"synced-greeting"`) {
		t.Fatalf("Node B did not hot-sync setting: %s", rec2.Body.String())
	}

	// Install a valid theme .slp on Node A; Node B extracts theme + registers trusted web!
	themeSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "mint-theme",
			"kind": "theme",
			"version": "1.0.0",
			"name": {"en": "Mint Theme"},
			"apiVersion": 1,
			"web": "web.js",
			"theme": {
				"colorScheme": "light",
				"css": "theme.css",
				"tokens": { "light": { "background": "#ffffff", "foreground": "#111111", "primary": "#0f766e" } }
			}
		}`),
		"theme.css": []byte(`:root { --accent: #0f766e; }`),
		"web.js":    []byte(`export default { id: "mint-theme" };`),
	}, priv, false, false)
	if _, err := mgrA.InstallPackage(ctx, themeSLP, InstallOptions{GrantAllDeclared: true, Enable: true}); err != nil {
		t.Fatalf("InstallPackage theme: %v", err)
	}
	if err := mgrB.SyncCluster(ctx); err != nil {
		t.Fatal(err)
	}
	if !mgrB.HasTrustedThemeWeb("mint-theme") {
		t.Fatal("expected Node B to trust theme web entry for installed .slp theme")
	}
	if th, ok := themesB.Get("mint-theme"); !ok || th.Error != "" {
		t.Fatalf("expected Node B theme registry to load mint-theme cleanly, ok=%v th=%+v", ok, th)
	}

	// Uninstall on Node A removes Node B's synced runtime & extracted dirs.
	if err := mgrA.Uninstall(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := mgrB.SyncCluster(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(nodeBDir, "plugins", id)); !os.IsNotExist(err) {
		t.Fatalf("expected Node B plugin dir removed after remote uninstall, stat=%v", err)
	}
}

func TestPackagesManagerSecurityAndLifecycleRegressions(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()

	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "regressions.db", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	bus := hook.NewBus(log, 2)
	defer bus.Close(ctx)

	themes := theme.NewRegistry()
	themes.SetLocalDir(filepath.Join(dir, "themes"), log)

	mgr, err := NewManager(ManagerDeps{
		DB:      database.Ent,
		Bus:     bus,
		Store:   store,
		Themes:  themes,
		DataDir: dir,
		Log:     log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Shutdown(ctx) }()

	// 1. 40-char plugin ID + nested group secrets + string slice options + cron lease within VARCHAR(40).
	longID := "very-long-plugin-id-with-exact-40-char01"
	if len(longID) != 40 {
		t.Fatalf("expected 40 char plugin ID, got %d", len(longID))
	}
	v1SLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "` + longID + `",
			"kind": "plugin",
			"version": "1.0.0",
			"name": {"en": "Long Plugin"},
			"apiVersion": 1,
			"server": "server.js",
			"routes": { "public": ["/ok"] },
			"settings": {
				"mode": { "type": "select", "options": ["fast", "safe"], "default": "fast", "required": true },
				"oauth": {
					"type": "group",
					"fields": {
						"clientId": { "type": "string", "default": "cid-1" },
						"clientSecret": { "type": "secret", "default": "initial-nested-secret", "required": true }
					}
				},
				"legacySecret": { "type": "secret", "default": "old-secret" }
			}
		}`),
		"server.js": []byte(`export default { setup(ctx) { ctx.http.get("/ok", { auth: "public" }, async () => ({ ok: true, mode: await ctx.settings.get("mode") })); } };`),
	}, nil, false, false)

	info1, err := mgr.InstallPackage(ctx, v1SLP, InstallOptions{AllowUnsigned: true, GrantAllDeclared: true, Enable: true})
	if err != nil {
		t.Fatalf("InstallPackage longID: %v", err)
	}
	// Cron slot claim succeeds even for 40-char plugin ID and Uninstall(purge=true) sweeps it.
	if !mgr.claimCronSlot(ctx, longID, "nightly-job", "202611180000") {
		t.Fatal("claimCronSlot failed for 40-char plugin ID")
	}
	if mgr.claimCronSlot(ctx, longID, "nightly-job", "202611180000") {
		t.Fatal("duplicate claimCronSlot should return false")
	}

	// Nested group secret must NEVER appear in admin view, settings view, or public bootstrap!
	if info1.Settings != nil {
		if _, leaked := info1.Settings["legacySecret"]; leaked {
			t.Fatalf("top-level secret leaked in PluginInfo.Settings: %+v", info1.Settings)
		}
		if oauthMap, ok := info1.Settings["oauth"].(map[string]any); ok {
			if _, leaked := oauthMap["clientSecret"]; leaked {
				t.Fatalf("nested group secret leaked in PluginInfo.Settings: %+v", oauthMap)
			}
		}
	}
	sv, err := mgr.PluginSettingsView(ctx, longID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := sv.Values["oauth"].(map[string]any)["clientSecret"]; leaked {
		t.Fatalf("nested group secret leaked in PluginSettingsView: %+v", sv.Values)
	}
	fps := mgr.FrontendPlugins("en")
	for _, fp := range fps {
		if fp.ID == longID {
			if oauthMap, ok := fp.Settings["oauth"].(map[string]any); ok {
				if _, leaked := oauthMap["clientSecret"]; leaked {
					t.Fatalf("nested group secret leaked in FrontendPlugins bootstrap: %+v", oauthMap)
				}
			}
		}
	}

	// Redact sentinel preserves nested group secret, empty clears non-required fails required, concurrent updates don't clobber!
	if err := mgr.UpdateSettings(ctx, longID, map[string]any{
		"oauth": map[string]any{
			"clientId":     "cid-2",
			"clientSecret": settings.Redacted,
		},
	}); err != nil {
		t.Fatalf("UpdateSettings nested group redacted sentinel: %v", err)
	}
	if err := mgr.UpdateSettings(ctx, longID, map[string]any{"mode": ""}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("expected ErrInvalidSettings clearing required select setting, got %v", err)
	}
	if err := mgr.UpdateSettings(ctx, longID, map[string]any{
		"oauth": map[string]any{"clientSecret": ""},
	}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("expected ErrInvalidSettings clearing required nested secret, got %v", err)
	}
	rowAfter, _ := database.Ent.Plugin.Get(ctx, longID)
	if oauthRow, _ := rowAfter.Settings["oauth"].(map[string]any); oauthRow["clientSecret"] != "initial-nested-secret" || oauthRow["clientId"] != "cid-2" {
		t.Fatalf("unexpected stored oauth group: %+v", oauthRow)
	}

	// Missing manifest fails closed (empty redacted settings).
	if got := redactSettingsMap(nil, rowAfter.Settings); len(got) != 0 {
		t.Fatalf("expected fail-closed empty settings when manifest is nil, got %+v", got)
	}

	// Concurrent updates on independent keys do not clobber each other.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = mgr.UpdateSettings(ctx, longID, map[string]any{"mode": "safe"})
	}()
	go func() {
		defer wg.Done()
		_ = mgr.UpdateSettings(ctx, longID, map[string]any{"oauth": map[string]any{"clientId": "cid-3"}})
	}()
	wg.Wait()
	rowConc, _ := database.Ent.Plugin.Get(ctx, longID)
	if rowConc.Settings["mode"] != "safe" {
		t.Fatalf("concurrent update lost mode change: %+v", rowConc.Settings)
	}

	// 2. Failed upgrade rolls back DB metadata + keeps v1 active cleanly, and removes orphaned blob/dir.
	brokenUpgrade := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "` + longID + `",
			"kind": "plugin",
			"version": "2.0.0",
			"name": {"en": "Long Plugin"},
			"apiVersion": 1,
			"server": "server.js",
			"settings": { "mode": { "type": "string", "default": "fast" } }
		}`),
		"server.js": []byte(`export default { setup() { throw new Error("boom on setup"); } };`),
	}, nil, false, false)
	if _, err := mgr.InstallPackage(ctx, brokenUpgrade, InstallOptions{AllowUnsigned: true, GrantAllDeclared: true, Enable: true}); err == nil {
		t.Fatal("expected broken upgrade to fail")
	}
	rowKeep, _ := database.Ent.Plugin.Get(ctx, longID)
	if rowKeep.Version != "1.0.0" || rowKeep.Status != entplugin.StatusEnabled {
		t.Fatalf("expected DB rolled back to 1.0.0 enabled after failed upgrade, got ver=%s status=%s", rowKeep.Version, rowKeep.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", longID, "2.0.0")); !os.IsNotExist(err) {
		t.Fatalf("expected failed upgrade extract dir cleaned up, stat=%v", err)
	}
	recRoute := httptest.NewRecorder()
	mgr.ServePluginAPI(recRoute, httptest.NewRequest(http.MethodGet, "/api/plugins/"+longID+"/ok", nil), longID, "/ok", Viewer{}, "en")
	if recRoute.Code != http.StatusOK {
		t.Fatalf("expected rolled-back v1 runtime still serving /ok, got %d", recRoute.Code)
	}

	// 3. Uninstall(purge=true) removes __cron lease entries too.
	if err := mgr.Uninstall(ctx, longID, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if cronLeft, _ := database.Ent.PluginKV.Query().Where(pluginkv.Plugin(cronKVPlugin)).Count(ctx); cronLeft != 0 {
		t.Fatalf("expected __cron lease swept on purge uninstall, left=%d", cronLeft)
	}
}

func buildNativeTestSLP(t *testing.T, id, version, platform string, binBytes []byte, priv ed25519.PrivateKey) []byte {
	t.Helper()
	goos, goarch, _ := strings.Cut(platform, "/")
	binName := "bin/" + id + "_" + goos + "_" + goarch
	if goos == "windows" {
		binName += ".exe"
	}
	files := map[string][]byte{
		"plugin.json": []byte(`{
			"id": "` + id + `",
			"kind": "plugin",
			"runtime": "native",
			"version": "` + version + `",
			"name": {"en": "Native Sync Demo"},
			"apiVersion": 1,
			"native": {
				"protocolVersion": 1,
				"executable": "bin/` + id + `",
				"platforms": ["` + platform + `"]
			},
			"permissions": ["storage.kv"]
		}`),
		binName: binBytes,
	}
	return buildTestSLP(t, files, priv, false, false)
}

func TestNativeTrustPolicyAndMultiArchSyncCluster(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sharedDir := t.TempDir()
	nodeADir := filepath.Join(sharedDir, "nodeA")
	nodeBDir := filepath.Join(sharedDir, "nodeB")

	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "native_cluster.db", DataDir: sharedDir})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(ctx, database, sharedDir, log); err != nil {
		t.Fatal(err)
	}

	sharedStore, err := storage.NewLocal(filepath.Join(sharedDir, "shared-store"))
	if err != nil {
		t.Fatal(err)
	}

	trustedPriv, trustedPubPEM := testKeypairPEM(t)
	untrustedPriv, _ := testKeypairPEM(t)

	busA := hook.NewBus(log, 2)
	defer busA.Close(ctx)
	busB := hook.NewBus(log, 2)
	defer busB.Close(ctx)

	// Fake driver factory so we can test multi-arch SyncCluster on any host OS
	// without actually executing a foreign-arch ELF binary.
	var activatedDirs []string
	var mu sync.Mutex
	fakeFactory := func(mf *Manifest, dir string) Driver {
		mu.Lock()
		activatedDirs = append(activatedDirs, mf.ID+":"+dir)
		mu.Unlock()
		return &fakeNativeDriver{}
	}

	// Node A: production mode (AllowUnsignedNative: false), linux/amd64
	mgrA, err := NewManager(ManagerDeps{
		DB:                  database.Ent,
		Bus:                 busA,
		Store:               sharedStore,
		DataDir:             nodeADir,
		HostPlatform:        "linux/amd64",
		TrustedPublicKeys:   []string{trustedPubPEM},
		AllowUnsignedNative: false,
		NativeDriverFactory: fakeFactory,
		Log:                 log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgrA.Shutdown(ctx) }()

	// 1. Production mode hard-rejects unsigned AND unknown-signer native .slp even if AllowUnsigned=true.
	unsignedAMD64 := buildNativeTestSLP(t, "multi-native", "1.0.0", "linux/amd64", []byte("elf-amd64"), nil)
	inspectUnsigned, err := mgrA.InspectPackage(ctx, unsignedAMD64, "en")
	if err != nil {
		t.Fatalf("InspectPackage unsigned native: %v", err)
	}
	if inspectUnsigned.Runtime != RuntimeNative || !inspectUnsigned.NativeInstallBlocked || inspectUnsigned.NativeBlockReason == "" || !inspectUnsigned.HasServer {
		t.Fatalf("expected production native block in inspect result, got %+v", inspectUnsigned)
	}
	if _, err := mgrA.InstallPackage(ctx, unsignedAMD64, InstallOptions{AllowUnsigned: true, GrantAllDeclared: true, Enable: true}); !errors.Is(err, ErrUnsignedNativeForbidden) {
		t.Fatalf("expected ErrUnsignedNativeForbidden for unsigned native in prod, got %v", err)
	}
	untrustedAMD64 := buildNativeTestSLP(t, "multi-native", "1.0.0", "linux/amd64", []byte("elf-amd64"), untrustedPriv)
	if _, err := mgrA.InstallPackage(ctx, untrustedAMD64, InstallOptions{AllowUnsigned: true, GrantAllDeclared: true, Enable: true}); !errors.Is(err, ErrUnsignedNativeForbidden) {
		t.Fatalf("expected ErrUnsignedNativeForbidden for untrusted-key native in prod, got %v", err)
	}

	// 2. Wrong architecture single-platform native package is flagged incompatible and refused on install.
	signedARM64 := buildNativeTestSLP(t, "multi-native", "1.0.0", "linux/arm64", []byte("elf-arm64"), trustedPriv)
	inspectWrongArch, err := mgrA.InspectPackage(ctx, signedARM64, "en")
	if err != nil {
		t.Fatalf("InspectPackage arm64 on amd64: %v", err)
	}
	if inspectWrongArch.PlatformCompatible || inspectWrongArch.HostPlatform != "linux/amd64" {
		t.Fatalf("expected PlatformCompatible=false on amd64 host, got %+v", inspectWrongArch)
	}
	if _, err := mgrA.InstallPackage(ctx, signedARM64, InstallOptions{GrantAllDeclared: true, Enable: true}); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage installing arm64 package on amd64 node, got %v", err)
	}

	// 3. Trusted signed linux/amd64 package installs cleanly on Node A and records platform_packages + registry_source.
	signedAMD64 := buildNativeTestSLP(t, "multi-native", "1.0.0", "linux/amd64", []byte("elf-amd64"), trustedPriv)
	infoA, err := mgrA.InstallPackage(ctx, signedAMD64, InstallOptions{
		GrantAllDeclared: true,
		Enable:           true,
		Source:           "market",
		RegistrySource:   "https://registry.skinloom.test/index.v1.json",
	})
	if err != nil {
		t.Fatalf("InstallPackage signed amd64: %v", err)
	}
	shaAMD64 := infoA.PackageSHA256
	if infoA.PlatformPackages["linux/amd64"] != shaAMD64 {
		t.Fatalf("expected platform_packages[linux/amd64]=%s, got %+v", shaAMD64, infoA.PlatformPackages)
	}

	// 4. Node B (linux/arm64) runs SyncCluster: sharedStore only has amd64 package, so
	// Node B falls back to PlatformPackageFetcher -> verifies Ed25519 signature ->
	// caches arm64 blob in sharedStore -> records platform_packages[linux/arm64] -> activates!
	fetchedCalls := 0
	mgrB, err := NewManager(ManagerDeps{
		DB:                  database.Ent,
		Bus:                 busB,
		Store:               sharedStore,
		DataDir:             nodeBDir,
		HostPlatform:        "linux/arm64",
		TrustedPublicKeys:   []string{trustedPubPEM},
		AllowUnsignedNative: false,
		NativeDriverFactory: fakeFactory,
		PlatformPackageFetcher: func(_ context.Context, registrySource, pluginID, version, platform string) ([]byte, error) {
			fetchedCalls++
			if registrySource != "https://registry.skinloom.test/index.v1.json" || pluginID != "multi-native" || version != "1.0.0" || platform != "linux/arm64" {
				t.Fatalf("unexpected fetcher args: src=%q id=%q ver=%q plat=%q", registrySource, pluginID, version, platform)
			}
			return signedARM64, nil
		},
		Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgrB.Shutdown(ctx) }()

	if err := mgrB.SyncCluster(ctx); err != nil {
		t.Fatalf("Node B SyncCluster: %v", err)
	}
	if fetchedCalls != 1 {
		t.Fatalf("expected 1 PlatformPackageFetcher call, got %d", fetchedCalls)
	}
	rowAfter, err := database.Ent.Plugin.Get(ctx, "multi-native")
	if err != nil {
		t.Fatal(err)
	}
	shaARM64 := rowAfter.PlatformPackages["linux/arm64"]
	if shaARM64 == "" || shaARM64 == shaAMD64 || rowAfter.PlatformPackages["linux/amd64"] != shaAMD64 {
		t.Fatalf("expected both platform_packages preserved, got %+v", rowAfter.PlatformPackages)
	}
	if _, err := sharedStore.Stat(ctx, shaARM64); err != nil {
		t.Fatalf("expected fetched arm64 blob cached in sharedStore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nodeBDir, "plugins", "multi-native", "1.0.0", "bin", "multi-native_linux_arm64")); err != nil {
		t.Fatalf("expected arm64 binary extracted on Node B: %v", err)
	}

	// 5. Uninstall removes BOTH amd64 and arm64 blobs from sharedStore.
	if err := mgrA.Uninstall(ctx, "multi-native", true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := sharedStore.Stat(ctx, shaAMD64); err == nil {
		t.Fatal("expected amd64 blob removed after uninstall")
	}
	if _, err := sharedStore.Stat(ctx, shaARM64); err == nil {
		t.Fatal("expected arm64 blob removed after uninstall")
	}

	// 6. Dev mode (AllowUnsignedNative: true) permits unsigned native only when AllowUnsigned=true.
	mgrDev, err := NewManager(ManagerDeps{
		DB:                  database.Ent,
		Bus:                 busA,
		Store:               sharedStore,
		DataDir:             t.TempDir(),
		HostPlatform:        "linux/amd64",
		AllowUnsignedNative: true,
		NativeDriverFactory: fakeFactory,
		Log:                 log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgrDev.Shutdown(ctx) }()

	if _, err := mgrDev.InstallPackage(ctx, unsignedAMD64, InstallOptions{AllowUnsigned: false, GrantAllDeclared: true, Enable: true}); !errors.Is(err, ErrUntrustedPackage) {
		t.Fatalf("expected ErrUntrustedPackage in dev mode when AllowUnsigned=false, got %v", err)
	}
	if _, err := mgrDev.InstallPackage(ctx, unsignedAMD64, InstallOptions{AllowUnsigned: true, GrantAllDeclared: true, Enable: true}); err != nil {
		t.Fatalf("expected unsigned native install to succeed in dev mode with AllowUnsigned=true, got %v", err)
	}
}

type fakeNativeDriver struct{}

func (fakeNativeDriver) Init(_ context.Context, _ DriverInitConfig) (*DriverRegistrations, error) {
	return &DriverRegistrations{}, nil
}
func (fakeNativeDriver) Lifecycle(context.Context, string, any) error           { return nil }
func (fakeNativeDriver) InvokeGuard(context.Context, string, string, any) error { return nil }
func (fakeNativeDriver) InvokeFilter(context.Context, string, string, any, any) (any, error) {
	return nil, nil
}
func (fakeNativeDriver) InvokeEvent(context.Context, string, string, any) error { return nil }
func (fakeNativeDriver) InvokeHTTP(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}
func (fakeNativeDriver) InvokeCron(context.Context, string, string) error { return nil }
func (fakeNativeDriver) InvokeService(context.Context, string, string, string, ...any) (any, error) {
	return nil, nil
}
func (fakeNativeDriver) InvokeExtension(context.Context, string, string, string, ...any) (any, error) {
	return nil, nil
}
func (fakeNativeDriver) NotifySettings(context.Context, map[string]any) error { return nil }
func (fakeNativeDriver) ProxyHandler() http.Handler                           { return nil }
func (fakeNativeDriver) Stats() DriverStats {
	return DriverStats{Runtime: RuntimeNative, Native: &NativeStats{Running: true}}
}
func (fakeNativeDriver) DrainAndClose(time.Duration) error { return nil }
