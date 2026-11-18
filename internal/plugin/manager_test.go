package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/notification"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

type testEnv struct {
	db     *db.DB
	bus    *hook.Bus
	notify *notify.Service
	mgr    *Manager
	dir    string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "plugin_test.db", DataDir: dir})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatalf("migrate.Up: %v", err)
	}

	bus := hook.NewBus(log, 4)
	t.Cleanup(func() { bus.Close(context.Background()) })

	notifySvc := notify.NewService(database.Ent, log)
	mgr, err := NewManager(ManagerDeps{
		DB:      database.Ent,
		Bus:     bus,
		Notify:  notifySvc,
		DataDir: dir,
		Log:     log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })

	return &testEnv{
		db:     database,
		bus:    bus,
		notify: notifySvc,
		mgr:    mgr,
		dir:    dir,
	}
}

func mustEventually(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestManifestValidation(t *testing.T) {
	valid := `{
		"id": "invite-code",
		"kind": "plugin",
		"version": "1.2.0",
		"name": {"zh-CN": "邀请码", "en": "Invite Code"},
		"apiVersion": 1,
		"server": "dist/server.js",
		"permissions": ["storage.kv", "storage.docs"],
		"http": {"allow": ["api.example.com", "*.trusted.org"]},
		"hooks": {
			"guard": ["auth.register"],
			"filter": ["user.badges"],
			"event": ["user.registered", "plugin:other.done"]
		},
		"storage": {
			"collections": {
				"codes": {
					"indexes": {
						"code": "string:unique",
						"creator": "string",
						"uses": "number",
						"expiresAt": "time"
					}
				}
			}
		}
	}`
	mf, err := ParseManifest([]byte(valid))
	if err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}
	if got := mf.Name.Resolve("zh-CN"); got != "邀请码" {
		t.Fatalf("Resolve(zh-CN) = %q, want 邀请码", got)
	}
	if !mf.HasHook(hook.KindGuard, "auth.register") {
		t.Fatalf("expected HasHook(guard, auth.register) = true")
	}

	cases := []struct {
		name string
		json string
	}{
		{
			name: "bad id",
			json: `{"id":"Invite_Code","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1}`,
		},
		{
			name: "unknown permission",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["sudo.root"]}`,
		},
		{
			name: "unknown guard",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"hooks":{"guard":["no.such.hook"]}}`,
		},
		{
			name: "too many unique slots",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"storage":{"collections":{"c":{"indexes":{"a":"string:unique","b":"string:unique","c":"string:unique"}}}}}`,
		},
		{
			name: "too many string slots",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"storage":{"collections":{"c":{"indexes":{"a":"string","b":"string","c":"string","d":"string"}}}}}`,
		},
		{
			name: "too many number slots",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"storage":{"collections":{"c":{"indexes":{"a":"number","b":"number","c":"number","d":"number"}}}}}`,
		},
		{
			name: "too many time slots",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"storage":{"collections":{"c":{"indexes":{"a":"time","b":"time","c":"time"}}}}}`,
		},
		{
			name: "wildcard star http allow forbidden",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"http":{"allow":["*"]}}`,
		},
		{
			name: "ip literal in http allow forbidden",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"http":{"allow":["127.0.0.1"]}}`,
		},
		{
			name: "server path traversal forbidden",
			json: `{"id":"good-id","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"server":"../outside.js"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.json)); !errors.Is(err, ErrBadManifest) {
				t.Fatalf("expected ErrBadManifest, got %v", err)
			}
		})
	}
}

func TestPermissionEnforcement(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Declare permissions in manifest, but pass empty grantedPerms slice (non-nil) to deny all.
	manifestJSON := []byte(`{
		"id": "perm-denied",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Perm Denied",
		"apiVersion": 1,
		"server": "server.js",
		"storage": {"collections": {"items": {"indexes": {"k": "string"}}}}
	}`)
	serverJS := []byte(`
		export default {
			setup(ctx) {
				ctx.http.get("/kv", { auth: "public" }, async () => await ctx.kv.set("a", 1));
				ctx.http.get("/db", { auth: "public" }, async () => await ctx.db.collection("items").findOne({ k: "x" }));
				ctx.http.get("/file", { auth: "public" }, async () => await ctx.files.put("a.txt", "hi"));
				ctx.http.get("/fetch", { auth: "public" }, async () => await ctx.fetch("https://example.com"));
			}
		};
	`)

	_, err := env.mgr.LoadFromBytes(ctx, manifestJSON, serverJS, nil, []string{}, true)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	for _, path := range []string{"/kv", "/db", "/file", "/fetch"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/plugins/perm-denied"+path, nil)
		env.mgr.ServePluginAPI(rec, req, "perm-denied", path, Viewer{}, "en")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("path %s: expected 500, got %d (%s)", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "permission denied") {
			t.Fatalf("path %s: expected 'permission denied' in body, got %s", path, rec.Body.String())
		}
	}
}

func TestKVStoreOperations(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	permsA := NewPermissionSet([]string{PermStorageKV}, []string{PermStorageKV})
	permsB := NewPermissionSet([]string{PermStorageKV}, []string{PermStorageKV})

	kvA := NewKVStore("plugin-a", env.db.Ent, 3, permsA)
	kvB := NewKVStore("plugin-b", env.db.Ent, 3, permsB)

	if err := kvA.Set(ctx, "cfg:theme", map[string]any{"dark": true}, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := kvB.Set(ctx, "cfg:theme", "isolated", 0); err != nil {
		t.Fatalf("Set B: %v", err)
	}

	gotA, err := kvA.Get(ctx, "cfg:theme")
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	if m, ok := gotA.(map[string]any); !ok || m["dark"] != true {
		t.Fatalf("unexpected Get A: %#v", gotA)
	}
	gotB, _ := kvB.Get(ctx, "cfg:theme")
	if gotB != "isolated" {
		t.Fatalf("expected isolated value for plugin-b, got %#v", gotB)
	}

	// Atomic increment
	n1, err := kvA.Incr(ctx, "cnt:1", 2)
	if err != nil || n1 != 2 {
		t.Fatalf("Incr 1 = %v, %v", n1, err)
	}
	n2, err := kvA.Incr(ctx, "cnt:1", 3)
	if err != nil || n2 != 5 {
		t.Fatalf("Incr 2 = %v, %v", n2, err)
	}

	// TTL expiration
	if err := kvA.Set(ctx, "temp", "val", 0.05); err != nil {
		t.Fatalf("Set temp: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	expiredVal, err := kvA.Get(ctx, "temp")
	if err != nil || expiredVal != nil {
		t.Fatalf("expected expired key to return nil, got %v, %v", expiredVal, err)
	}

	// Key quota (maxKeys = 3; currently have cfg:theme and cnt:1 = 2 keys)
	if err := kvA.Set(ctx, "extra", 1, 0); err != nil {
		t.Fatalf("Set extra: %v", err)
	}
	if err := kvA.Set(ctx, "over-quota", 2, 0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded on 4th key, got %v", err)
	}

	// Value size quota (> 64 KiB)
	bigStr := strings.Repeat("x", MaxPluginKVValBytes)
	if err := kvA.Set(ctx, "cfg:theme", bigStr, 0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded for large value, got %v", err)
	}

	// Prefix list & Delete
	entries, err := kvA.List(ctx, "cnt:", 10)
	if err != nil || len(entries) != 1 || entries[0].Key != "cnt:1" {
		t.Fatalf("List(cnt:) = %#v, %v", entries, err)
	}
	del, err := kvA.Delete(ctx, "cnt:1")
	if err != nil || !del {
		t.Fatalf("Delete(cnt:1) = %v, %v", del, err)
	}
}

func TestDocStoreSlotsAndQueries(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	perms := NewPermissionSet([]string{PermStorageDocs}, []string{PermStorageDocs})

	store, err := NewDocStore("docs-plugin", env.db.Ent, StorageManifest{
		Collections: map[string]CollectionSpec{
			"codes": {
				Indexes: map[string]string{
					"code":      "string:unique",
					"usedBy":    "string:unique",
					"batch":     "string",
					"uses":      "number",
					"expiresAt": "time",
				},
			},
		},
		Quota: QuotaSpec{Docs: 4},
	}, perms)
	if err != nil {
		t.Fatalf("NewDocStore: %v", err)
	}

	coll, err := store.Collection("codes")
	if err != nil {
		t.Fatalf("Collection: %v", err)
	}
	if _, err := store.Collection("unknown"); !errors.Is(err, ErrUnknownCollection) {
		t.Fatalf("expected ErrUnknownCollection, got %v", err)
	}

	t1 := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	t2 := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	tPast := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	_, err = coll.InsertOne(ctx, map[string]any{
		"code": "ALPHA", "usedBy": nil, "batch": "spring-2026", "uses": 10, "expiresAt": t1, "note": "first",
	})
	if err != nil {
		t.Fatalf("InsertOne ALPHA: %v", err)
	}
	_, err = coll.InsertOne(ctx, map[string]any{
		"code": "BETA", "usedBy": nil, "batch": "spring-2026", "uses": 20, "expiresAt": t2,
	})
	if err != nil {
		t.Fatalf("InsertOne BETA (second null unique slot must be allowed): %v", err)
	}
	_, err = coll.InsertOne(ctx, map[string]any{
		"code": "GAMMA", "usedBy": "user-7", "batch": "winter-2026", "uses": 5, "expiresAt": tPast,
	})
	if err != nil {
		t.Fatalf("InsertOne GAMMA: %v", err)
	}

	// Duplicate unique slot fails with ErrUniqueConstraint
	_, err = coll.InsertOne(ctx, map[string]any{
		"code": "ALPHA", "batch": "dup", "uses": 1,
	})
	if !errors.Is(err, ErrUniqueConstraint) {
		t.Fatalf("expected ErrUniqueConstraint on duplicate code, got %v", err)
	}

	// Unindexed filter fails with UnindexedFieldError
	_, err = coll.Find(ctx, map[string]any{"note": "first"}, FindOpts{})
	var unidx *UnindexedFieldError
	if !errors.As(err, &unidx) || unidx.Field != "note" {
		t.Fatalf("expected UnindexedFieldError for 'note', got %v", err)
	}
	// Unindexed sort also fails with UnindexedFieldError
	_, err = coll.Find(ctx, nil, FindOpts{Sort: map[string]int{"note": 1}})
	if !errors.As(err, &unidx) {
		t.Fatalf("expected UnindexedFieldError for unindexed sort, got %v", err)
	}

	// Filter with null on unique slot + $gte number + sort desc + cursor pagination
	page1, err := coll.Find(ctx, map[string]any{"usedBy": nil, "uses": map[string]any{"$gte": 10}}, FindOpts{
		Sort:  map[string]int{"uses": -1},
		Limit: 1,
	})
	if err != nil || len(page1.Items) != 1 || page1.Items[0]["code"] != "BETA" || page1.NextCursor == "" {
		t.Fatalf("page1 unexpected: %#v, err=%v", page1, err)
	}
	page2, err := coll.Find(ctx, map[string]any{"usedBy": nil, "uses": map[string]any{"$gte": 10}}, FindOpts{
		Sort:   map[string]int{"uses": -1},
		Limit:  1,
		Cursor: page1.NextCursor,
	})
	if err != nil || len(page2.Items) != 1 || page2.Items[0]["code"] != "ALPHA" || page2.NextCursor != "" {
		t.Fatalf("page2 unexpected: %#v, err=%v", page2, err)
	}

	// Prefix & $in queries
	prefixRes, err := coll.Find(ctx, map[string]any{"batch": map[string]any{"$prefix": "spring-"}}, FindOpts{})
	if err != nil || len(prefixRes.Items) != 2 {
		t.Fatalf("prefix query failed: %#v, err=%v", prefixRes, err)
	}
	inCnt, err := coll.Count(ctx, map[string]any{"code": map[string]any{"$in": []any{"ALPHA", "GAMMA"}}})
	if err != nil || inCnt != 2 {
		t.Fatalf("Count($in) = %d, err=%v", inCnt, err)
	}

	// UpdateOne & DeleteOne
	updated, err := coll.UpdateOne(ctx, map[string]any{"code": "ALPHA"}, map[string]any{"usedBy": "user-9", "uses": 11})
	if err != nil || !updated {
		t.Fatalf("UpdateOne = %v, %v", updated, err)
	}
	gotAlpha, err := coll.FindOne(ctx, map[string]any{"usedBy": "user-9"})
	if err != nil || gotAlpha["code"] != "ALPHA" {
		t.Fatalf("FindOne after update = %#v, %v", gotAlpha, err)
	}

	// Quota check (Quota.Docs = 4, currently 3 docs)
	deltaDoc, err := coll.InsertOne(ctx, map[string]any{"code": "DELTA", "uses": 1})
	if err != nil {
		t.Fatalf("InsertOne 4th doc: %v", err)
	}
	if _, err := coll.InsertOne(ctx, map[string]any{"code": "EPSILON", "uses": 2}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded on 5th doc, got %v", err)
	}

	// Built-in id field query & delete via native doc_id unique index
	byID, err := coll.FindOne(ctx, map[string]any{"id": deltaDoc["id"]})
	if err != nil || byID == nil || byID["code"] != "DELTA" {
		t.Fatalf("FindOne by built-in id = %#v, %v", byID, err)
	}
	deleted, err := coll.DeleteOne(ctx, map[string]any{"id": deltaDoc["id"]})
	if err != nil || !deleted {
		t.Fatalf("DeleteOne = %v, %v", deleted, err)
	}
}

func TestFileStoreAndAssets(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	perms := NewPermissionSet([]string{PermStorageFiles}, []string{PermStorageFiles})

	fs := NewFileStore("file-plug", env.dir, 64, perms)
	assetURL, err := fs.Put(ctx, "avatars/logo.png", []byte("PNGDATA"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if assetURL != "/plugins-assets/file-plug/_files/avatars/logo.png" {
		t.Fatalf("unexpected url %q", assetURL)
	}
	got, err := fs.Get(ctx, "avatars/logo.png")
	if err != nil || string(got) != "PNGDATA" {
		t.Fatalf("Get = %q, %v", string(got), err)
	}

	// Path traversal attempts rejected
	for _, bad := range []string{"../secret.txt", "a/../secret.txt", "/etc/passwd", `a\b.txt`, ""} {
		if _, err := fs.Put(ctx, bad, []byte("x")); !errors.Is(err, ErrInvalidFileKey) {
			t.Fatalf("key %q: expected ErrInvalidFileKey, got %v", bad, err)
		}
	}

	// Quota exceeded (max 64 bytes)
	if _, err := fs.Put(ctx, "big.bin", bytes.Repeat([]byte("a"), 100)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}

	// Serve private file & bundled static asset via AssetHandler
	manifestJSON := []byte(`{"id":"file-plug","kind":"plugin","version":"2.1.0","name":"Files","apiVersion":1}`)
	static := map[string][]byte{
		"web/index.js": []byte(`export default function(){ return "ok"; }`),
	}
	if _, err := env.mgr.LoadFromBytes(ctx, manifestJSON, nil, static, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	handler := env.mgr.AssetHandler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plugins-assets/file-plug/2.1.0/web/index.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("versioned static asset failed: status=%d cache=%q body=%s",
			rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}

	recFile := httptest.NewRecorder()
	handler.ServeHTTP(recFile, httptest.NewRequest(http.MethodGet, assetURL, nil))
	if recFile.Code != http.StatusOK || recFile.Body.String() != "PNGDATA" || recFile.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("_files asset serve failed: status=%d csp=%q body=%q",
			recFile.Code, recFile.Header().Get("Content-Security-Policy"), recFile.Body.String())
	}

	// Script uploaded into _files/ must never be served with executable application/javascript MIME type
	jsURL, err := fs.Put(ctx, "evil.js", []byte("alert(1)"))
	if err != nil {
		t.Fatalf("Put evil.js: %v", err)
	}
	recJS := httptest.NewRecorder()
	handler.ServeHTTP(recJS, httptest.NewRequest(http.MethodGet, jsURL, nil))
	if recJS.Code != http.StatusOK || recJS.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("_files/evil.js expected application/octet-stream, got status=%d ctype=%q",
			recJS.Code, recJS.Header().Get("Content-Type"))
	}
	_, _ = fs.Delete(ctx, "evil.js")

	if ok, err := fs.Delete(ctx, "avatars/logo.png"); err != nil || !ok {
		t.Fatalf("Delete = %v, %v", ok, err)
	}
}

func TestFetcherSSRFAndAllowlist(t *testing.T) {
	perms := NewPermissionSet([]string{PermHTTPFetch}, []string{PermHTTPFetch})
	f := NewFetcher([]string{"api.example.com", "*.allowed.test"}, perms)

	// Deny hosts not in http.allow
	if _, err := f.Fetch(context.Background(), "https://evil.org/steal", FetchRequest{}); !errors.Is(err, ErrFetchDenied) {
		t.Fatalf("expected ErrFetchDenied for non-allowlisted host, got %v", err)
	}
	// Deny non-HTTP schemes
	if _, err := f.Fetch(context.Background(), "file:///etc/passwd", FetchRequest{}); !errors.Is(err, ErrFetchDenied) {
		t.Fatalf("expected ErrFetchDenied for file:// scheme, got %v", err)
	}

	// SSRF: even if hostname matches allowlist, resolving to loopback / link-local / RFC1918 / IPv6 loopback must be blocked
	fAllowLocal := NewFetcher([]string{"*.ssrf.test", "127.0.0.1", "169.254.169.254", "localhost"}, perms)
	blockedIPs := []struct {
		target string
		ip     string
	}{
		{"https://loop.ssrf.test/x", "127.0.0.1"},
		{"https://meta.ssrf.test/latest/meta-data", "169.254.169.254"},
		{"https://priv.ssrf.test/x", "10.20.30.40"},
		{"https://v6.ssrf.test/x", "::1"},
		{"http://localhost/admin", "127.0.0.1"},
	}
	for _, tc := range blockedIPs {
		fAllowLocal.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP(tc.ip)}}, nil
		}
		_, err := fAllowLocal.Fetch(context.Background(), tc.target, FetchRequest{})
		if !errors.Is(err, ErrFetchDenied) {
			t.Fatalf("target %s -> %s: expected ErrFetchDenied (SSRF block), got %v", tc.target, tc.ip, err)
		}
	}

	// Local httptest server testing 5 MiB cap and redirect blocking to non-allowlisted / loopback IP
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"hello":"world"}`))
		case "/huge":
			_, _ = w.Write(bytes.Repeat([]byte("A"), DefaultFetchMaxRespBytes+10))
		case "/redir-metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		}
	}))
	defer ts.Close()

	// Allow private only in test transport to reach local httptest server for response limit & redirect checks
	fLocal := NewFetcher([]string{"local.allowed.test"}, perms)
	fLocal.allowPrivate = true
	_, srvPort, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	fLocal.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}

	resp, err := fLocal.Fetch(context.Background(), "http://local.allowed.test:"+srvPort+"/ok", FetchRequest{})
	if err != nil || !resp.OK || !strings.Contains(resp.BodyText, `"hello":"world"`) {
		t.Fatalf("local Fetch /ok failed: %#v, err=%v", resp, err)
	}
	if _, err := fLocal.Fetch(context.Background(), "http://local.allowed.test:"+srvPort+"/huge", FetchRequest{}); !errors.Is(err, ErrFetchTooLarge) {
		t.Fatalf("expected ErrFetchTooLarge on >5 MiB response, got %v", err)
	}
	// Even with allowPrivate=true on local transport, redirect host not in http.allow is still denied
	if _, err := fLocal.Fetch(context.Background(), "http://local.allowed.test:"+srvPort+"/redir-metadata", FetchRequest{}); !errors.Is(err, ErrFetchDenied) {
		t.Fatalf("expected redirect to metadata IP to return ErrFetchDenied, got %v", err)
	}
}

func TestPluginLifecycleHooksRoutesAndJS(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	pluginDir := filepath.Join(env.dir, "src", "invite-code")
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"id": "invite-code",
		"kind": "plugin",
		"version": "1.0.0",
		"name": {"en": "Invite Code", "zh-CN": "邀请码"},
		"apiVersion": 1,
		"server": "dist/server.js",
		"permissions": ["storage.kv", "storage.docs"],
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
		},
		"settings": {
			"requireInvite": { "type": "boolean", "default": true }
		}
	}`
	serverJS := `
		let enabledCount = 0;
		export default definePlugin({
			setup(ctx) {
				const codes = ctx.db.collection("codes");

				ctx.settings.onChange(async (next) => {
					await ctx.kv.set("lastSettings", next);
				});

				ctx.hooks.guard("auth.register", async (p) => {
					if (!(await ctx.settings.get("requireInvite"))) return;
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return reject("invite_required");
					const doc = await codes.findOne({ code });
					if (!doc || doc.usedBy != null) return reject("invite_invalid");
				});

				ctx.events.on("user.registered", async (p) => {
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return;
					await codes.updateOne({ code }, { usedBy: p.userId });
					await ctx.kv.incr("stats:redeemed", 1);
				});

				ctx.hooks.filter("user.badges", (badges, u) => {
					const out = Array.isArray(badges) ? [...badges] : [];
					if (u.userId === 42) out.push({ text: "vip-invite", color: "gold" });
					return out;
				});

				ctx.http.post("/codes", { auth: "admin" }, async (req) => {
					const code = (req.body && req.body.code) || ("INV-" + await ctx.crypto.randomString(6));
					const doc = await codes.insertOne({ code, usedBy: null });
					return { status: 201, body: doc };
				});

				ctx.http.get("/stats/:tag", { auth: "public" }, async (req) => {
					const redeemed = await ctx.kv.get("stats:redeemed");
					await ctx.cache.set("lastTag", req.params.tag, 60);
					return {
						tag: req.params.tag,
						redeemed: redeemed || 0,
						hash: await ctx.crypto.sha256("abc"),
						uuid: await ctx.crypto.uuid(),
						cached: await ctx.cache.get("lastTag"),
						siteLocales: await ctx.site.locales(),
						msg: await ctx.t("hello")
					};
				});
			},
			onEnable() {
				enabledCount++;
			}
		});
	`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "dist", "server.js"), []byte(serverJS), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := env.mgr.LoadFromDir(ctx, pluginDir, true)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if info.Status != "enabled" {
		t.Fatalf("expected enabled status, got %q", info.Status)
	}

	// 1. Guard rejects registration without inviteCode
	err = hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{
		Email: "a@example.com",
		Form:  map[string]any{},
	})
	rej, ok := hook.AsRejection(err)
	if !ok || rej.Reason != "invite_required" {
		t.Fatalf("expected Rejection(invite_required), got %v", err)
	}

	// 2. HTTP route auth enforcement: anonymous 401, normal user 403, admin 201
	rec401 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec401, httptest.NewRequest(http.MethodPost, "/api/plugins/invite-code/codes", strings.NewReader(`{"code":"INV-OK"}`)),
		"invite-code", "/codes", Viewer{}, "en")
	if rec401.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauth POST /codes, got %d", rec401.Code)
	}

	rec403 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec403, httptest.NewRequest(http.MethodPost, "/api/plugins/invite-code/codes", strings.NewReader(`{"code":"INV-OK"}`)),
		"invite-code", "/codes", Viewer{Authenticated: true, UserID: 2, Role: "user"}, "en")
	if rec403.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user POST /codes, got %d", rec403.Code)
	}

	reqAdmin := httptest.NewRequest(http.MethodPost, "/api/plugins/invite-code/codes", strings.NewReader(`{"code":"INV-OK"}`))
	reqAdmin.Header.Set("Content-Type", "application/json")
	rec201 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec201, reqAdmin, "invite-code", "/codes", Viewer{Authenticated: true, UserID: 1, Role: "admin", Admin: true}, "en")
	if rec201.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin POST /codes, got %d: %s", rec201.Code, rec201.Body.String())
	}

	// 3. Valid inviteCode passes Guard, and user.registered Event redeems it + updates KV
	if err := hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{
		Email: "b@example.com",
		Form:  map[string]any{"inviteCode": "INV-OK"},
	}); err != nil {
		t.Fatalf("expected INV-OK to pass Guard, got %v", err)
	}

	hook.OnUserRegistered.Emit(env.bus, hook.UserEvent{
		UserID: 42,
		Method: "password",
		Form:   map[string]any{"inviteCode": "INV-OK"},
	})
	mustEventually(t, 5*time.Second, func() bool {
		rec := httptest.NewRecorder()
		env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/invite-code/stats/alpha", nil),
			"invite-code", "/stats/alpha", Viewer{}, "en")
		if rec.Code != http.StatusOK {
			return false
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body["redeemed"] == float64(1) && body["tag"] == "alpha" && body["cached"] == "alpha"
	}, "user.registered event redemption & KV increment")

	// 4. Filter adds badge for user 42
	badges := hook.FilterUserBadges.Apply(ctx, env.bus, []hook.UserBadge{{Text: "early"}}, hook.UserBadgeFilterContext{UserID: 42, Role: "user"})
	if len(badges) != 2 || badges[1].Text != "vip-invite" || badges[1].Color != "gold" {
		t.Fatalf("FilterUserBadges = %#v", badges)
	}

	// 5. UpdateSettings triggers ctx.settings.onChange so registration without code passes
	if err := env.mgr.UpdateSettings(ctx, "invite-code", map[string]any{"requireInvite": false}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if err := hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{Email: "c@example.com"}); err != nil {
		t.Fatalf("expected registration without invite after settings update to pass, got %v", err)
	}

	// 6. Disable removes hooks & routes cleanly
	if err := env.mgr.Disable(ctx, "invite-code"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	rec404 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec404, httptest.NewRequest(http.MethodGet, "/api/plugins/invite-code/stats/alpha", nil),
		"invite-code", "/stats/alpha", Viewer{}, "en")
	if rec404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when disabled, got %d", rec404.Code)
	}
	badgesAfterDisable := hook.FilterUserBadges.Apply(ctx, env.bus, []hook.UserBadge{{Text: "early"}}, hook.UserBadgeFilterContext{UserID: 42, Role: "user"})
	if len(badgesAfterDisable) != 1 {
		t.Fatalf("expected hook removed after disable, got %#v", badgesAfterDisable)
	}

	// 7. Re-enable & Uninstall with purge=true clears KV & Docs
	if err := env.mgr.Enable(ctx, "invite-code"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := env.mgr.Uninstall(ctx, "invite-code", true); err != nil {
		t.Fatalf("Uninstall(purge=true): %v", err)
	}
	kvCount, _ := env.db.Ent.PluginKV.Query().Count(ctx)
	docCount, _ := env.db.Ent.PluginDoc.Query().Count(ctx)
	if kvCount != 0 || docCount != 0 {
		t.Fatalf("expected purged KV/Docs=0, got kv=%d doc=%d", kvCount, docCount)
	}
}

func TestPluginTimeoutAndCircuitBreaker(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create one superadmin so we can assert failure alert notification is delivered.
	adm, err := env.db.Ent.User.Create().
		SetEmail("root@example.com").
		SetEmailNormalized("root@example.com").
		SetNickname("RootAdmin").
		SetPasswordHash("hash").
		SetRole(user.RoleSuperadmin).
		SetStatus(user.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	manifestJSON := []byte(`{
		"id": "faulty-plug",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Faulty",
		"apiVersion": 1,
		"server": "server.js",
		"hooks": {"guard": ["auth.register"]}
	}`)
	serverJS := []byte(`
		export default {
			setup(ctx) {
				ctx.hooks.guard("auth.register", () => {
					while (true) {}
				});
				ctx.http.get("/crash", { auth: "public" }, () => {
					throw new Error("boom");
				});
				ctx.http.get("/ok", { auth: "public" }, () => ({ ok: true }));
			}
		};
	`)

	if _, err := env.mgr.LoadFromBytes(ctx, manifestJSON, serverJS, nil, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	// 1. Guard while(true){} preempts at 2s and recovers pool immediately for /ok
	start := time.Now()
	err = hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{Email: "x@example.com"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected Guard infinite loop to fail")
	}
	if elapsed > 4500*time.Millisecond {
		t.Fatalf("expected 2s guard preemption well under 4.5s, took %v", elapsed)
	}
	// Subsequent healthy request succeeds on healthy/recreated isolate!
	recOK := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recOK, httptest.NewRequest(http.MethodGet, "/api/plugins/faulty-plug/ok", nil),
		"faulty-plug", "/ok", Viewer{}, "en")
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected /ok 200 after timeout recovery, got %d (%s)", recOK.Code, recOK.Body.String())
	}

	// 2. Trigger 5 consecutive failures -> trips CircuitBreaker -> DB failed + Bus removed + Superadmin notified
	for i := 0; i < DefaultCircuitThreshold; i++ {
		rec := httptest.NewRecorder()
		env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/faulty-plug/crash", nil),
			"faulty-plug", "/crash", Viewer{}, "en")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("crash call #%d expected 500, got %d", i+1, rec.Code)
		}
	}

	mustEventually(t, 5*time.Second, func() bool {
		row, err := env.db.Ent.Plugin.Get(ctx, "faulty-plug")
		if err != nil || row.Status != entplugin.StatusFailed {
			return false
		}
		cnt, _ := env.db.Ent.Notification.Query().Where(notification.HasUserWith(user.ID(adm.ID))).Count(ctx)
		return cnt >= 1
	}, "circuit breaker mark failed + superadmin notification")

	// After tripping, hook is unregistered from bus so registration succeeds cleanly
	if err := hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{Email: "x@example.com"}); err != nil {
		t.Fatalf("expected faulty guard removed after circuit trip, got %v", err)
	}
	// Active HTTP route returns 404
	recGone := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recGone, httptest.NewRequest(http.MethodGet, "/api/plugins/faulty-plug/ok", nil),
		"faulty-plug", "/ok", Viewer{}, "en")
	if recGone.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after circuit trip unload, got %d", recGone.Code)
	}

	// Re-enabling restores healthy state
	if err := env.mgr.Enable(ctx, "faulty-plug"); err != nil {
		t.Fatalf("Enable after trip: %v", err)
	}
	recRestored := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recRestored, httptest.NewRequest(http.MethodGet, "/api/plugins/faulty-plug/ok", nil),
		"faulty-plug", "/ok", Viewer{}, "en")
	if recRestored.Code != http.StatusOK {
		t.Fatalf("expected /ok 200 after re-enable, got %d", recRestored.Code)
	}
	_ = jsrt.ErrTimeout
	_ = ent.Plugin{}
}
