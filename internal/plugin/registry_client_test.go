package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/notification"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

func signIndexJSON(t *testing.T, idx RegistryIndex, priv ed25519.PrivateKey) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, raw)
	return raw, base64.StdEncoding.EncodeToString(sig)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestRegistryClient_FailoverSignatureCacheAndInstall(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)

	offPriv, offPubPEM := testKeypairPEM(t)
	wrongPriv, _ := testKeypairPEM(t)

	// Build a dependency JS plugin package ("base-dep" v1.0.0) and a target JS plugin ("hello-market" v1.2.0)
	depSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "base-dep",
			"kind": "plugin",
			"name": {"zh-CN": "基础依赖", "en": "Base Dependency"},
			"version": "1.0.0",
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv"]
		}`),
		"server.js": []byte(`globalThis.__skinloom_setup = function(ctx) {};`),
	}, offPriv, false, false)

	targetSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "hello-market",
			"kind": "plugin",
			"name": {"zh-CN": "市场问候插件", "en": "Hello Market"},
			"description": {"zh-CN": "来自官方市场的插件", "en": "Plugin from official market"},
			"version": "1.2.0",
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["users.read"],
			"dependencies": {"base-dep": "^1.0.0"}
		}`),
		"server.js": []byte(`
			globalThis.__skinloom_setup = function(ctx) {
				ctx.http.get("/ping", function() { return { status: 200, body: { ok: true, from: "market" } }; });
			};
		`),
	}, offPriv, false, false)

	// Build an incompatible-platform native plugin entry to verify 5-dimensional filtering
	otherPlat := "plan9/386"

	var fallbackHits atomic.Int32
	var currentIndex atomic.Value
	var currentSig atomic.Value

	idx := RegistryIndex{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		RegistryID:    "official",
		Packages: []RegistryPackage{
			{
				ID:       "base-dep",
				Kind:     KindPlugin,
				Runtime:  RuntimeJS,
				Category: "security",
				Official: true,
				Name:     LocalizedText{"zh-CN": "基础依赖", "en": "Base Dependency"},
				Latest:   "1.0.0",
				Versions: []RegistryVersion{
					{
						Version:     "1.0.0",
						APIVersion:  1,
						Permissions: []string{"storage.kv"},
						Artifacts: map[string]RegistryArtifact{
							"any": {
								URLs:      []string{"/packages/base-dep-1.0.0.slp"},
								SHA256:    sha256Hex(depSLP),
								Size:      int64(len(depSLP)),
								Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(offPriv, depSLP)),
							},
						},
					},
				},
			},
			{
				ID:          "hello-market",
				Kind:        KindPlugin,
				Runtime:     RuntimeJS,
				Category:    "score",
				Official:    true,
				Name:        LocalizedText{"zh-CN": "市场问候插件", "en": "Hello Market"},
				Description: LocalizedText{"zh-CN": "来自官方市场的插件", "en": "Plugin from official market"},
				Authors:     []string{"Entropy-Increase-Team"},
				Latest:      "1.2.0",
				Versions: []RegistryVersion{
					{
						Version:      "1.2.0",
						APIVersion:   1,
						Permissions:  []string{"users.read"},
						Dependencies: map[string]string{"base-dep": "^1.0.0"},
						Changelog:    LocalizedText{"zh-CN": "首个正式版本", "en": "Initial release"},
						Artifacts: map[string]RegistryArtifact{
							"any": {
								URLs:      []string{"/packages/hello-market-1.2.0.slp"},
								SHA256:    sha256Hex(targetSLP),
								Size:      int64(len(targetSLP)),
								Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(offPriv, targetSLP)),
							},
						},
					},
				},
			},
			{
				ID:       "only-plan9-native",
				Kind:     KindPlugin,
				Runtime:  RuntimeNative,
				Category: "migration",
				Official: true,
				Name:     LocalizedText{"zh-CN": "异构平台原生插件", "en": "Plan9 Native"},
				Latest:   "1.0.0",
				Versions: []RegistryVersion{
					{
						Version:               "1.0.0",
						APIVersion:            1,
						NativeProtocolVersion: 1,
						Artifacts: map[string]RegistryArtifact{
							otherPlat: {
								URLs:   []string{"/packages/plan9.slp"},
								SHA256: strings.Repeat("a", 64),
								Size:   123,
							},
						},
					},
				},
			},
		},
	}
	rawIdx, sigIdx := signIndexJSON(t, idx, offPriv)
	currentIndex.Store(rawIdx)
	currentSig.Store(sigIdx)

	// Primary mirror returns tampered signature (MITM simulation) -> must fail over to fallback mirror!
	_, badSig := signIndexJSON(t, idx, wrongPriv)
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.v1.json":
			_, _ = w.Write(currentIndex.Load().([]byte))
		case "/index.v1.json.sig":
			_, _ = w.Write([]byte(badSig))
		default:
			http.NotFound(w, r)
		}
	}))
	defer primarySrv.Close()

	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.v1.json":
			fallbackHits.Add(1)
			_, _ = w.Write(currentIndex.Load().([]byte))
		case "/index.v1.json.sig":
			_, _ = w.Write([]byte(currentSig.Load().(string)))
		case "/packages/base-dep-1.0.0.slp":
			_, _ = w.Write(depSLP)
		case "/packages/hello-market-1.2.0.slp":
			_, _ = w.Write(targetSLP)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fallbackSrv.Close()

	reg := env.mgr.Registry()
	reg.SetOfficialPublicKey(offPubPEM)
	reg.SetRegistriesForTest([]settings.RegistrySource{
		{
			ID:       "official",
			Name:     "Official Market",
			URLs:     []string{primarySrv.URL, fallbackSrv.URL},
			Official: true,
			Enabled:  true,
		},
	})

	// 1. Test failover from tampered primary mirror to valid fallback mirror, and 5D filtering.
	res, err := reg.ListPackages(ctx, MarketFilter{
		CompatibleOnly: true,
		Locale:         "zh-CN",
	})
	if err != nil {
		t.Fatalf("ListPackages failed: %v", err)
	}
	if fallbackHits.Load() != 1 {
		t.Fatalf("expected 1 fallback mirror hit, got %d", fallbackHits.Load())
	}
	if len(res.Packages) != 2 {
		t.Fatalf("expected 2 compatible packages (plan9 native filtered out), got %d", len(res.Packages))
	}
	if res.HostEnv.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("unexpected host platform %q", res.HostEnv.Platform)
	}

	// 2. Test 15-minute memory cache (second call should not hit fallbackSrv again).
	if _, err := reg.ListPackages(ctx, MarketFilter{CompatibleOnly: false, Locale: "en"}); err != nil {
		t.Fatalf("cached ListPackages failed: %v", err)
	}
	if fallbackHits.Load() != 1 {
		t.Fatalf("expected cached index (hits=1), got %d", fallbackHits.Load())
	}

	// 3. InspectFromMarket resolves cascade dependency plan ("base-dep" -> "hello-market").
	inspectRes, err := reg.InspectFromMarket(ctx, MarketInspectRequest{
		RegistryID: "official",
		ID:         "hello-market",
	}, "zh-CN")
	if err != nil {
		t.Fatalf("InspectFromMarket failed: %v", err)
	}
	if inspectRes.ID != "hello-market" || inspectRes.Version != "1.2.0" {
		t.Fatalf("unexpected inspect result: %+v", inspectRes)
	}
	if len(inspectRes.DependenciesToInstall) != 1 || inspectRes.DependenciesToInstall[0].ID != "base-dep" {
		t.Fatalf("expected base-dep in DependenciesToInstall, got %+v", inspectRes.DependenciesToInstall)
	}

	// 4. InstallFromMarket cascade-installs base-dep and hello-market atomically.
	installRes, err := reg.InstallFromMarket(ctx, MarketInstallRequest{
		RegistryID: "official",
		ID:         "hello-market",
		GrantAll:   true,
	})
	if err != nil {
		t.Fatalf("InstallFromMarket failed: %v", err)
	}
	if installRes.Plugin.ID != "hello-market" || installRes.Plugin.Status != string(entplugin.StatusEnabled) {
		t.Fatalf("expected hello-market enabled, got %+v", installRes.Plugin)
	}
	if len(installRes.InstalledDependencies) != 1 || installRes.InstalledDependencies[0].ID != "base-dep" {
		t.Fatalf("expected base-dep cascade-installed, got %+v", installRes.InstalledDependencies)
	}

	// 5. Create an active superadmin user and simulate official registry adding hello-market to `blocked`.
	superadmin, err := env.db.Ent.User.Create().
		SetEmail("root@example.com").
		SetEmailNormalized("root@example.com").
		SetNickname("Root").
		SetPasswordHash("x").
		SetRole(user.RoleSuperadmin).
		SetStatus(user.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	idx.Blocked = []RegistryBlocked{
		{
			ID:       "hello-market",
			Versions: ">=1.0.0",
			Reason:   "CVE-2026-9999 malicious payload discovered",
		},
	}
	rawIdx2, sigIdx2 := signIndexJSON(t, idx, offPriv)
	currentIndex.Store(rawIdx2)
	currentSig.Store(sigIdx2)

	refreshRes, err := reg.RefreshAndEnforce(ctx, "zh-CN")
	if err != nil {
		t.Fatalf("RefreshAndEnforce failed: %v", err)
	}
	if len(refreshRes.BlockedActions) != 1 || refreshRes.BlockedActions[0].PluginID != "hello-market" {
		t.Fatalf("expected hello-market in BlockedActions, got %+v", refreshRes.BlockedActions)
	}

	// Verify hello-market is now disabled in DB and runtime
	info, ok := env.mgr.Get(ctx, "hello-market")
	if !ok || info.Status != string(entplugin.StatusDisabled) {
		t.Fatalf("expected hello-market disabled after block enforcement, got %+v", info)
	}

	// Verify audit log and superadmin notification were created
	auditCount, err := env.db.Ent.AuditLog.Query().
		Where(auditlog.Action("plugin.blocked_disabled"), auditlog.TargetID("hello-market")).
		Count(ctx)
	if err != nil || auditCount != 1 {
		t.Fatalf("expected 1 plugin.blocked_disabled audit log, got %d (err=%v)", auditCount, err)
	}

	notifCount, err := env.db.Ent.Notification.Query().
		Where(notification.UserID(superadmin.ID)).
		Count(ctx)
	if err != nil || notifCount == 0 {
		t.Fatalf("expected superadmin notification for blocked plugin, got %d (err=%v)", notifCount, err)
	}

	// Verify attempting to re-install a blocked package from market is rejected with ErrMarketPackageBlocked
	_, err = reg.InstallFromMarket(ctx, MarketInstallRequest{
		RegistryID: "official",
		ID:         "hello-market",
		GrantAll:   true,
	})
	if !errors.Is(err, ErrMarketPackageBlocked) {
		t.Fatalf("expected ErrMarketPackageBlocked when installing blocked plugin, got %v", err)
	}
}

func TestPhaseD5_InfrastructureEndToEndSmoke(t *testing.T) {
	ctx := context.Background()
	env := newNativeTestEnv(t, "stream")
	// Production security policy: forbid unsigned native packages!
	env.mgr.allowUnsignedNative = false
	env.mgr.themes = theme.NewRegistry()

	offPriv, offPubPEM := testKeypairPEM(t)
	hostPlat := runtime.GOOS + "/" + runtime.GOARCH
	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}

	// 1a. Minimal Tier 1 JS plugin .slp (signed by official key)
	jsSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "d5-min-js",
			"kind": "plugin",
			"runtime": "js",
			"name": {"zh-CN": "D5 轻量插件", "en": "D5 Minimal JS"},
			"version": "1.0.0",
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv"]
		}`),
		"server.js": []byte(`
			export default {
				setup(ctx) {
					ctx.http.get("/ping", { auth: "public" }, async () => ({
						status: 200,
						body: { tier: "js", ok: true }
					}));
				}
			};
		`),
	}, offPriv, false, false)

	// 1b. Minimal Tier 2 Native Go plugin split-platform .slp packages (3+ platforms, signed by official key)
	buildNativeSplitSLP := func(osName, archName string, priv ed25519.PrivateKey) []byte {
		ext := ""
		if osName == "windows" {
			ext = ".exe"
		}
		binPath := "bin/d5-min-native-" + osName + "-" + archName + ext
		return buildTestSLP(t, map[string][]byte{
			"plugin.json": []byte(`{
				"id": "d5-min-native",
				"kind": "plugin",
				"runtime": "native",
				"name": {"zh-CN": "D5 原生插件", "en": "D5 Minimal Native"},
				"version": "1.0.0",
				"apiVersion": 1,
				"native": {
					"protocolVersion": 1,
					"executable": "bin/d5-min-native",
					"platforms": ["linux/amd64", "linux/arm64", "windows/amd64", "darwin/arm64", "darwin/amd64"],
					"httpProxy": true,
					"database": {"required": true}
				},
				"permissions": ["native.exec", "storage.sql", "http.stream"]
			}`),
			binPath: []byte("stub-native-binary-" + osName + "-" + archName),
		}, priv, false, false)
	}

	nativeHostSLP := buildNativeSplitSLP(runtime.GOOS, runtime.GOARCH, offPriv)
	nativeLinuxSLP := buildNativeSplitSLP("linux", "amd64", offPriv)
	nativeWinSLP := buildNativeSplitSLP("windows", "amd64", offPriv)
	nativeDarwinSLP := buildNativeSplitSLP("darwin", "arm64", offPriv)

	// Unsigned native package (must be rejected in production mode!)
	unsignedNativeSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "d5-unsigned-native",
			"kind": "plugin",
			"runtime": "native",
			"name": {"en": "Unsigned Native"},
			"version": "1.0.0",
			"apiVersion": 1,
			"native": {
				"protocolVersion": 1,
				"executable": "bin/d5-unsigned-native",
				"platforms": ["` + hostPlat + `"]
			},
			"permissions": ["native.exec"]
		}`),
		"bin/d5-unsigned-native-" + runtime.GOOS + "-" + runtime.GOARCH + exeSuffix: []byte("unsigned-bin"),
	}, nil, false, false)

	// 1c. Level 5 Theme .slp (signed by official key)
	themeSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "d5-level5-theme",
			"kind": "theme",
			"name": {"zh-CN": "D5 五级主题", "en": "D5 Level 5 Theme"},
			"version": "1.0.0",
			"apiVersion": 1,
			"web": "web/index.js"
		}`),
		"theme.json": []byte(`{
			"id": "d5-level5-theme",
			"name": {"zh-CN": "D5 五级主题", "en": "D5 Level 5 Theme"},
			"version": "1.0.0"
		}`),
		"web/index.js":  []byte(`export default { slots: {} };`),
		"web/index.css": []byte(`:root { --radius: 0.75rem; }`),
	}, offPriv, false, false)

	nativeArtifacts := map[string]RegistryArtifact{
		"linux/amd64": {
			URLs:   []string{"/packages/d5-min-native-linux-amd64.slp"},
			SHA256: sha256Hex(nativeLinuxSLP),
			Size:   int64(len(nativeLinuxSLP)),
		},
		"windows/amd64": {
			URLs:   []string{"/packages/d5-min-native-windows-amd64.slp"},
			SHA256: sha256Hex(nativeWinSLP),
			Size:   int64(len(nativeWinSLP)),
		},
		"darwin/arm64": {
			URLs:   []string{"/packages/d5-min-native-darwin-arm64.slp"},
			SHA256: sha256Hex(nativeDarwinSLP),
			Size:   int64(len(nativeDarwinSLP)),
		},
	}
	nativeArtifacts[hostPlat] = RegistryArtifact{
		URLs:   []string{"/packages/d5-min-native-host.slp"},
		SHA256: sha256Hex(nativeHostSLP),
		Size:   int64(len(nativeHostSLP)),
	}

	var currentIndex atomic.Value
	var currentSig atomic.Value
	makeIndex := func(blocked []RegistryBlocked) {
		idx := RegistryIndex{
			SchemaVersion: 1,
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
			RegistryID:    "official",
			Packages: []RegistryPackage{
				{
					ID:       "d5-min-js",
					Kind:     KindPlugin,
					Runtime:  RuntimeJS,
					Category: "security",
					Official: true,
					Name:     LocalizedText{"zh-CN": "D5 轻量插件", "en": "D5 Minimal JS"},
					Latest:   "1.0.0",
					Versions: []RegistryVersion{
						{
							Version:     "1.0.0",
							APIVersion:  1,
							Permissions: []string{"storage.kv"},
							Artifacts: map[string]RegistryArtifact{
								"any": {
									URLs:   []string{"/packages/d5-min-js.slp"},
									SHA256: sha256Hex(jsSLP),
									Size:   int64(len(jsSLP)),
								},
							},
						},
					},
				},
				{
					ID:       "d5-min-native",
					Kind:     KindPlugin,
					Runtime:  RuntimeNative,
					Category: "migration",
					Official: true,
					Name:     LocalizedText{"zh-CN": "D5 原生插件", "en": "D5 Minimal Native"},
					Latest:   "1.0.0",
					Versions: []RegistryVersion{
						{
							Version:               "1.0.0",
							APIVersion:            1,
							NativeProtocolVersion: 1,
							Permissions:           []string{"native.exec", "storage.sql", "http.stream"},
							Artifacts:             nativeArtifacts,
						},
					},
				},
				{
					ID:       "d5-unsigned-native",
					Kind:     KindPlugin,
					Runtime:  RuntimeNative,
					Category: "migration",
					Official: false,
					Name:     LocalizedText{"en": "Unsigned Native"},
					Latest:   "1.0.0",
					Versions: []RegistryVersion{
						{
							Version:               "1.0.0",
							APIVersion:            1,
							NativeProtocolVersion: 1,
							Permissions:           []string{"native.exec"},
							Artifacts: map[string]RegistryArtifact{
								hostPlat: {
									URLs:   []string{"/packages/d5-unsigned-native.slp"},
									SHA256: sha256Hex(unsignedNativeSLP),
									Size:   int64(len(unsignedNativeSLP)),
								},
							},
						},
					},
				},
				{
					ID:       "d5-level5-theme",
					Kind:     KindTheme,
					Runtime:  RuntimeJS,
					Category: "theme",
					Official: true,
					Name:     LocalizedText{"zh-CN": "D5 五级主题", "en": "D5 Level 5 Theme"},
					Latest:   "1.0.0",
					Versions: []RegistryVersion{
						{
							Version:    "1.0.0",
							APIVersion: 1,
							Artifacts: map[string]RegistryArtifact{
								"any": {
									URLs:   []string{"/packages/d5-level5-theme.slp"},
									SHA256: sha256Hex(themeSLP),
									Size:   int64(len(themeSLP)),
								},
							},
						},
					},
				},
			},
			Blocked: blocked,
		}
		raw, sig := signIndexJSON(t, idx, offPriv)
		currentIndex.Store(raw)
		currentSig.Store(sig)
	}
	makeIndex(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.v1.json":
			_, _ = w.Write(currentIndex.Load().([]byte))
		case "/index.v1.json.sig":
			_, _ = w.Write([]byte(currentSig.Load().(string)))
		case "/packages/d5-min-js.slp":
			_, _ = w.Write(jsSLP)
		case "/packages/d5-min-native-host.slp",
			"/packages/d5-min-native-linux-amd64.slp",
			"/packages/d5-min-native-windows-amd64.slp",
			"/packages/d5-min-native-darwin-arm64.slp":
			_, _ = w.Write(nativeHostSLP)
		case "/packages/d5-unsigned-native.slp":
			_, _ = w.Write(unsignedNativeSLP)
		case "/packages/d5-level5-theme.slp":
			_, _ = w.Write(themeSLP)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := env.mgr.Registry()
	reg.SetOfficialPublicKey(offPubPEM)
	reg.SetRegistriesForTest([]settings.RegistrySource{
		{
			ID:       "official",
			Name:     "Official Market",
			URLs:     []string{srv.URL},
			Official: true,
			Enabled:  true,
		},
	})

	// Step 2a: Unsigned native package MUST be rejected in production mode even with AllowUnsigned=true.
	_, err := reg.InstallFromMarket(ctx, MarketInstallRequest{
		RegistryID:    "official",
		ID:            "d5-unsigned-native",
		GrantAll:      true,
		AllowUnsigned: true,
	})
	if !errors.Is(err, ErrUnsignedNativeForbidden) {
		t.Fatalf("expected ErrUnsignedNativeForbidden for unsigned native package in production mode, got %v", err)
	}

	// Step 2b: Install JS plugin, Native Go plugin, and Level 5 Theme from market without restart.
	for _, pkgID := range []string{"d5-min-js", "d5-min-native", "d5-level5-theme"} {
		res, instErr := reg.InstallFromMarket(ctx, MarketInstallRequest{
			RegistryID: "official",
			ID:         pkgID,
			GrantAll:   true,
		})
		if instErr != nil {
			t.Fatalf("InstallFromMarket(%s) failed: %v", pkgID, instErr)
		}
		if res.Plugin.Status != string(entplugin.StatusEnabled) {
			t.Fatalf("expected %s enabled, got %s", pkgID, res.Plugin.Status)
		}
	}

	// Verify JS plugin route responds immediately.
	jsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/d5-min-js/ping", nil)
	jsRec := httptest.NewRecorder()
	env.mgr.ServePluginAPI(jsRec, jsReq, "d5-min-js", "/ping", Viewer{}, "en")
	if jsRec.Code != http.StatusOK || !strings.Contains(jsRec.Body.String(), `"tier":"js"`) {
		t.Fatalf("JS plugin route failed: %d %s", jsRec.Code, jsRec.Body.String())
	}

	// Verify Native Go plugin isolated SQLite database file and stream reverse proxy.
	isoDBPath := filepath.Join(env.dir, "plugins-data", "d5-min-native", "plugin.db")
	if _, statErr := os.Stat(isoDBPath); statErr != nil {
		t.Fatalf("expected isolated SQLite database at %s: %v", isoDBPath, statErr)
	}
	adminViewer := Viewer{Authenticated: true, UserID: 1, Role: "admin", Admin: true}
	natReq := httptest.NewRequest(http.MethodGet, "/api/plugins/d5-min-native/stream/sse", nil)
	natRec := httptest.NewRecorder()
	env.mgr.ServePluginAPI(natRec, natReq, "d5-min-native", "/stream/sse", adminViewer, "en")
	if natRec.Code != http.StatusOK || !strings.Contains(natRec.Body.String(), "data: chunk-1") {
		t.Fatalf("Native plugin stream proxy failed: %d %s", natRec.Code, natRec.Body.String())
	}

	// Step 3: Kill native plugin child process and verify main site + JS plugin stay alive and native child auto-recovers.
	natInfoBefore, ok := env.mgr.Get(ctx, "d5-min-native")
	if !ok || natInfoBefore.NativeStats == nil || natInfoBefore.NativeStats.PID <= 0 {
		t.Fatalf("expected active native PID, got %+v", natInfoBefore)
	}
	oldPID := natInfoBefore.NativeStats.PID
	proc, err := os.FindProcess(oldPID)
	if err != nil {
		t.Fatalf("FindProcess(%d): %v", oldPID, err)
	}
	_ = proc.Kill()

	// Main site / JS plugin remains completely unaffected while native subprocess restarts.
	jsRec2 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(jsRec2, httptest.NewRequest(http.MethodGet, "/api/plugins/d5-min-js/ping", nil), "d5-min-js", "/ping", Viewer{}, "en")
	if jsRec2.Code != http.StatusOK {
		t.Fatalf("JS plugin affected by native subprocess kill: %d", jsRec2.Code)
	}

	// Wait for NativeDriver crash-backoff supervisor to respawn the subprocess and restore stream proxy service.
	deadline := time.Now().Add(8 * time.Second)
	recovered := false
	for time.Now().Before(deadline) {
		infoNow, ok := env.mgr.Get(ctx, "d5-min-native")
		if ok && infoNow.NativeStats != nil && infoNow.NativeStats.Running && infoNow.NativeStats.Restarts >= 1 && infoNow.NativeStats.PID != oldPID {
			rec := httptest.NewRecorder()
			env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/d5-min-native/stream/sse", nil), "d5-min-native", "/stream/sse", adminViewer, "en")
			if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "data: chunk-1") {
				recovered = true
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !recovered {
		t.Fatalf("native plugin did not auto-recover after subprocess kill within timeout")
	}

	// Step 4: Add d5-min-native to index.v1.json `blocked` list and verify automatic disable + superadmin notification.
	superadmin, err := env.db.Ent.User.Create().
		SetEmail("d5-admin@example.com").
		SetEmailNormalized("d5-admin@example.com").
		SetNickname("D5Root").
		SetPasswordHash("x").
		SetRole(user.RoleSuperadmin).
		SetStatus(user.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	makeIndex([]RegistryBlocked{
		{
			ID:       "d5-min-native",
			Versions: "1.0.0",
			Reason:   "D5 simulated emergency block",
		},
	})
	refRes, err := reg.RefreshAndEnforce(ctx, "zh-CN")
	if err != nil {
		t.Fatalf("RefreshAndEnforce failed: %v", err)
	}
	if len(refRes.BlockedActions) != 1 || refRes.BlockedActions[0].PluginID != "d5-min-native" {
		t.Fatalf("expected d5-min-native blocked action, got %+v", refRes.BlockedActions)
	}
	natInfoAfter, ok := env.mgr.Get(ctx, "d5-min-native")
	if !ok || natInfoAfter.Status != string(entplugin.StatusDisabled) {
		t.Fatalf("expected d5-min-native disabled after blocklist patrol, got %+v", natInfoAfter)
	}
	notifCount, err := env.db.Ent.Notification.Query().
		Where(notification.UserID(superadmin.ID)).
		Count(ctx)
	if err != nil || notifCount == 0 {
		t.Fatalf("expected superadmin notification after D5 block enforcement, got %d (err=%v)", notifCount, err)
	}
}

func TestRegistryClient_CrossRegistryTrustAndMultiBlockIsolation(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)

	offPriv, offPubPEM := testKeypairPEM(t)
	commPriv, commPubPEM := testKeypairPEM(t)

	// A package signed by community key, NOT official key.
	commSignedSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "spoof-pkg",
			"kind": "plugin",
			"name": {"en": "Spoof Pkg"},
			"version": "1.0.0",
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["users.ban"]
		}`),
		"server.js": []byte(`globalThis.__skinloom_setup = function() {};`),
	}, commPriv, false, false)

	// An official package that depends on a high-risk community package.
	lowRiskConsumerSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "low-risk-consumer",
			"kind": "plugin",
			"name": {"en": "Low Risk Consumer"},
			"version": "1.0.0",
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv"],
			"dependencies": {"spoof-pkg": "^1.0.0"}
		}`),
		"server.js": []byte(`globalThis.__skinloom_setup = function() {};`),
	}, offPriv, false, false)

	offIdx := RegistryIndex{
		SchemaVersion: 1,
		RegistryID:    "official",
		Packages: []RegistryPackage{
			{
				ID:       "spoof-in-official",
				Kind:     KindPlugin,
				Runtime:  RuntimeJS,
				Official: true,
				Name:     LocalizedText{"en": "Spoof in Official"},
				Latest:   "1.0.0",
				Versions: []RegistryVersion{
					{
						Version:    "1.0.0",
						APIVersion: 1,
						Artifacts: map[string]RegistryArtifact{
							"any": {
								URLs:   []string{"/packages/spoof.slp"},
								SHA256: sha256Hex(commSignedSLP),
								Size:   int64(len(commSignedSLP)),
							},
						},
					},
				},
			},
			{
				ID:       "low-risk-consumer",
				Kind:     KindPlugin,
				Runtime:  RuntimeJS,
				Official: true,
				Name:     LocalizedText{"en": "Low Risk Consumer"},
				Latest:   "1.0.0",
				Versions: []RegistryVersion{
					{
						Version:      "1.0.0",
						APIVersion:   1,
						Permissions:  []string{"storage.kv"},
						Dependencies: map[string]string{"spoof-pkg": "^1.0.0"},
						Artifacts: map[string]RegistryArtifact{
							"any": {
								URLs:   []string{"/packages/consumer.slp"},
								SHA256: sha256Hex(lowRiskConsumerSLP),
								Size:   int64(len(lowRiskConsumerSLP)),
							},
						},
					},
				},
			},
		},
		// Two separate block rules for the same package ID must both be enforced after merge.
		Blocked: []RegistryBlocked{
			{ID: "multi-blocked", Versions: "1.0.0", Reason: "CVE-A"},
			{ID: "multi-blocked", Versions: "1.2.0", Reason: "CVE-B"},
		},
	}
	offRaw, offSig := signIndexJSON(t, offIdx, offPriv)

	commIdx := RegistryIndex{
		SchemaVersion: 1,
		RegistryID:    "community",
		Packages: []RegistryPackage{
			{
				ID:      "spoof-pkg",
				Kind:    KindPlugin,
				Runtime: RuntimeJS,
				Name:    LocalizedText{"en": "Community Pkg"},
				Latest:  "1.0.0",
				Versions: []RegistryVersion{
					{
						Version:     "1.0.0",
						APIVersion:  1,
						Permissions: []string{"users.ban"},
						Artifacts: map[string]RegistryArtifact{
							"any": {
								URLs:   []string{"/packages/spoof.slp"},
								SHA256: sha256Hex(commSignedSLP),
								Size:   int64(len(commSignedSLP)),
							},
						},
					},
				},
			},
		},
	}
	commRaw, commSig := signIndexJSON(t, commIdx, commPriv)

	// Unverified third-party registry attempting to inject a fake block rule against low-risk-consumer.
	unverifiedIdx := RegistryIndex{
		SchemaVersion: 1,
		RegistryID:    "unverified",
		Blocked: []RegistryBlocked{
			{ID: "low-risk-consumer", Versions: "*", Reason: "Fake DoS block rule"},
		},
	}
	unverifiedRaw, _ := json.Marshal(unverifiedIdx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official/index.v1.json":
			_, _ = w.Write(offRaw)
		case "/official/index.v1.json.sig":
			_, _ = w.Write([]byte(offSig))
		case "/community/index.v1.json":
			_, _ = w.Write(commRaw)
		case "/community/index.v1.json.sig":
			_, _ = w.Write([]byte(commSig))
		case "/unverified/index.v1.json":
			_, _ = w.Write(unverifiedRaw)
		case "/official/packages/spoof.slp", "/community/packages/spoof.slp":
			_, _ = w.Write(commSignedSLP)
		case "/official/packages/consumer.slp":
			_, _ = w.Write(lowRiskConsumerSLP)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := env.mgr.Registry()
	reg.SetOfficialPublicKey(offPubPEM)
	reg.SetRegistriesForTest([]settings.RegistrySource{
		{ID: "official", Name: "Official", URLs: []string{srv.URL + "/official"}, Official: true, Enabled: true},
		{ID: "community", Name: "Community", URLs: []string{srv.URL + "/community"}, PublicKey: commPubPEM, Enabled: true},
		{ID: "unverified", Name: "Unverified", URLs: []string{srv.URL + "/unverified"}, Enabled: true},
	})

	// 1. Package signed by community key MUST NOT be trusted when fetched from official registry!
	spoofInspect, err := reg.InspectFromMarket(ctx, MarketInspectRequest{
		RegistryID: "official",
		ID:         "spoof-in-official",
	}, "en")
	if err != nil {
		t.Fatalf("InspectFromMarket(spoof-in-official): %v", err)
	}
	if spoofInspect.Verification.Trusted {
		t.Fatalf("community public key must not be trusted for packages in official registry: %+v", spoofInspect.Verification)
	}

	// 2. Same package fetched from community registry IS trusted via community's scoped publicKey.
	commInspect, err := reg.InspectFromMarket(ctx, MarketInspectRequest{
		RegistryID: "community",
		ID:         "spoof-pkg",
	}, "en")
	if err != nil {
		t.Fatalf("InspectFromMarket(community/spoof-pkg): %v", err)
	}
	if !commInspect.Verification.Trusted || commInspect.Verification.Official {
		t.Fatalf("expected community package trusted (non-official), got %+v", commInspect.Verification)
	}

	// 3. Low-risk package depending on high-risk dependency propagates RequiresHighRiskConfirm = true,
	// and unverified registry's fake block rule is ignored.
	consumerInspect, err := reg.InspectFromMarket(ctx, MarketInspectRequest{
		RegistryID: "official",
		ID:         "low-risk-consumer",
	}, "en")
	if err != nil {
		t.Fatalf("InspectFromMarket(low-risk-consumer): %v", err)
	}
	if !consumerInspect.RequiresHighRiskConfirm {
		t.Fatalf("expected RequiresHighRiskConfirm=true due to high-risk cascade dependency")
	}

	// 4. Merged block rules for multi-blocked match both 1.0.0 and 1.2.0, but not 1.1.0.
	_, _, _, blocked, _ := reg.FetchAllIndexes(ctx, false)
	rule, ok := blocked["multi-blocked"]
	if !ok {
		t.Fatalf("expected merged block rule for multi-blocked")
	}
	if !rule.MatchesVersion("1.0.0") || !rule.MatchesVersion("1.2.0") || rule.MatchesVersion("1.1.0") {
		t.Fatalf("unexpected merged block rule matching behavior: %+v", rule)
	}
}

func TestRegistryClient_ResolveArtifactURLSafety(t *testing.T) {
	base := "https://registry.skinloom.cn/dist"
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "packages/a/1.0.0/a.slp", want: "https://registry.skinloom.cn/dist/packages/a/1.0.0/a.slp"},
		{raw: "/packages/a/1.0.0/a.slp", want: "https://registry.skinloom.cn/dist/packages/a/1.0.0/a.slp"},
		{raw: "https://cdn.example.com/a.slp", want: "https://cdn.example.com/a.slp"},
		{raw: "//evil.internal/a.slp", want: ""},
		{raw: "file:///etc/passwd", want: ""},
		{raw: "ftp://files.example.com/a.slp", want: ""},
		{raw: "javascript:alert(1)", want: ""},
		{raw: "", want: ""},
	}
	for _, tc := range cases {
		got := resolveArtifactURL(base, tc.raw)
		if got != tc.want {
			t.Errorf("resolveArtifactURL(%q, %q) = %q, want %q", base, tc.raw, got, tc.want)
		}
	}
}

func TestMatchesMarketFilter_UtilityGroup(t *testing.T) {
	cases := []struct {
		pkgCat string
		want   bool
	}{
		{pkgCat: "utility", want: true},
		{pkgCat: "storage", want: true},
		{pkgCat: "render", want: true},
		{pkgCat: "search", want: true},
		{pkgCat: "security", want: false},
		{pkgCat: "migration", want: false},
	}
	for _, tc := range cases {
		v := MarketPackageView{
			ID:       "pkg-" + tc.pkgCat,
			Kind:     KindPlugin,
			Runtime:  RuntimeJS,
			Category: tc.pkgCat,
		}
		if got := matchesMarketFilter(v, MarketFilter{Category: "utility"}); got != tc.want {
			t.Errorf("matchesMarketFilter(category=%q, filter=utility) = %v, want %v", tc.pkgCat, got, tc.want)
		}
	}
}
