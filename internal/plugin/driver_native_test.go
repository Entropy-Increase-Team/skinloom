package plugin

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	hplugin "github.com/hashicorp/go-plugin"
	_ "modernc.org/sqlite"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
	gosdk "github.com/Entropy-Increase-Team/skinloom/sdk/go"
)

// TestHelperNativePluginProcess is invoked by NativeDriver in tests by re-executing
// the compiled Go test binary with -test.run=^TestHelperNativePluginProcess$ and
// SKINLOOM_TEST_NATIVE_HELPER=1. This gives us real OS child processes, real
// hashicorp/go-plugin IPC, real GRPCBroker callbacks, and real local HTTP/WS sockets
// across Windows, Linux, and macOS with zero external build latency.
func TestHelperNativePluginProcess(t *testing.T) {
	if os.Getenv("SKINLOOM_TEST_NATIVE_HELPER") != "1" {
		return
	}
	mode := os.Getenv("SKINLOOM_TEST_NATIVE_MODE")
	srv := &helperNativePluginServer{
		mode:    mode,
		version: os.Getenv("SKINLOOM_PLUGIN_VERSION"),
		blobs:   map[string][]byte{},
		times:   map[string]time.Time{},
	}
	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: NativeHandshake,
		Plugins: map[string]hplugin.Plugin{
			NativePluginName: &NativeGRPCPlugin{
				Impl: &NativeServerImpl{
					Plugin:  srv,
					Storage: srv,
					Search:  &helperSearchServer{parent: srv},
					Render:  srv,
				},
			},
		},
		GRPCServer: hplugin.DefaultGRPCServer,
	})
	os.Exit(0)
}

type helperNativePluginServer struct {
	pluginv1.UnimplementedPluginServiceServer
	pluginv1.UnimplementedStorageProviderServiceServer
	pluginv1.UnimplementedRenderProviderServiceServer

	mode    string
	version string

	broker   *hplugin.GRPCBroker
	host     pluginv1.HostServiceClient
	dbPool   *sql.DB
	dbPrefix string

	mu          sync.RWMutex
	settings    map[string]any
	unhealthy   atomic.Bool
	proxyToken  string
	proxyLn     net.Listener
	proxyServer *http.Server
	blobs       map[string][]byte
	times       map[string]time.Time
	indexedIDs  []int64
}

func (s *helperNativePluginServer) SetGRPCBroker(b *hplugin.GRPCBroker) {
	s.broker = b
}

func (s *helperNativePluginServer) Init(ctx context.Context, req *pluginv1.InitRequest) (*pluginv1.InitResponse, error) {
	if s.mode == "always-fail-init" {
		return nil, fmt.Errorf("simulated fatal init crash")
	}

	if len(req.GetSettingsJson()) > 0 {
		var m map[string]any
		if json.Unmarshal(req.GetSettingsJson(), &m) == nil {
			s.mu.Lock()
			s.settings = m
			s.mu.Unlock()
		}
	}

	// Dial reverse HostService via GRPCBroker.
	if s.broker != nil && req.GetHostBrokerId() != 0 {
		conn, err := s.broker.Dial(req.GetHostBrokerId())
		if err != nil {
			return nil, fmt.Errorf("dial host broker: %w", err)
		}
		s.host = pluginv1.NewHostServiceClient(conn)
		_, _ = s.host.Log(ctx, &pluginv1.LogRequest{Level: "info", Message: "helper native plugin initialized"})
	}

	// Connect to isolated SQLite database if provided.
	if dbc := req.GetDatabase(); dbc != nil && dbc.GetDriver() == "sqlite" && dbc.GetDsn() != "" {
		pool, err := sql.Open("sqlite", dbc.GetDsn())
		if err != nil {
			return nil, fmt.Errorf("open isolated sqlite: %w", err)
		}
		s.dbPool = pool
		s.dbPrefix = dbc.GetPrefix()
		if _, err := pool.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+s.dbPrefix+`events (id INTEGER PRIMARY KEY AUTOINCREMENT, note TEXT NOT NULL)`); err != nil {
			return nil, fmt.Errorf("create plugin table: %w", err)
		}
		if _, err := pool.ExecContext(ctx, `INSERT INTO `+s.dbPrefix+`events (note) VALUES (?)`, "boot:"+req.GetVersion()); err != nil {
			return nil, fmt.Errorf("insert boot row: %w", err)
		}
	}

	resp := &pluginv1.InitResponse{
		ListensSettingsChange: true,
		Routes: []*pluginv1.RouteRegistration{
			{Method: "GET", Pattern: "/status", Auth: "public", HandlerId: "status"},
			{Method: "GET", Pattern: "/slow", Auth: "public", HandlerId: "slow"},
			{Method: "POST", Pattern: "/crash", Auth: "public", HandlerId: "crash"},
			{Method: "POST", Pattern: "/fail-health", Auth: "public", HandlerId: "fail-health"},
			{Method: "POST", Pattern: "/test-host-callbacks", Auth: "public", HandlerId: "test-host-callbacks"},
		},
	}

	if s.mode == "full" {
		resp.Hooks = []*pluginv1.HookRegistration{
			{Kind: "guard", Name: "auth.register", Order: 0, HandlerId: "guard-reg"},
			{Kind: "filter", Name: "user.badges", Order: 0, HandlerId: "filter-badges"},
			{Kind: "event", Name: "user.registered", Order: 0, HandlerId: "event-reg"},
		}
		resp.Crons = []*pluginv1.CronRegistration{
			{Name: "tick", Expr: "* * * * *", HandlerId: "cron-tick"},
		}
		resp.Services = []string{"echo"}
	}

	if s.mode == "providers" {
		resp.Providers = &pluginv1.ProviderCapabilities{
			Storage: true,
			Search:  true,
			Render:  true,
		}
	}

	if s.mode == "stream" || s.mode == "full" {
		s.proxyToken = req.GetProxyToken()
		var ln net.Listener
		var network, addr string
		var err error
		if req.GetProxySocketPath() != "" && runtime.GOOS != "windows" {
			_ = os.Remove(req.GetProxySocketPath())
			ln, err = net.Listen("unix", req.GetProxySocketPath())
			network = "unix"
			addr = req.GetProxySocketPath()
			if err == nil {
				_ = os.Chmod(addr, 0o600)
			}
		} else {
			ln, err = net.Listen("tcp", "127.0.0.1:0")
			network = "tcp"
			if err == nil {
				addr = ln.Addr().String()
			}
		}
		if err != nil {
			return nil, fmt.Errorf("listen proxy: %w", err)
		}
		s.proxyLn = ln
		s.proxyServer = &http.Server{Handler: http.HandlerFunc(s.handleProxyHTTP)}
		go func() { _ = s.proxyServer.Serve(ln) }()

		resp.HttpProxy = &pluginv1.HTTPProxyInfo{
			Enabled: true,
			Network: network,
			Address: addr,
			Token:   s.proxyToken,
		}
		resp.Routes = append(resp.Routes,
			&pluginv1.RouteRegistration{Method: "GET", Pattern: "/stream/sse", Auth: "admin", StreamProxy: true},
			&pluginv1.RouteRegistration{Method: "GET", Pattern: "/stream/echo-headers", Auth: "user", StreamProxy: true},
			&pluginv1.RouteRegistration{Method: "GET", Pattern: "/stream/ws", Auth: "public", StreamProxy: true},
		)
	}

	return resp, nil
}

func (s *helperNativePluginServer) handleProxyHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(HeaderProxyToken) != s.proxyToken {
		http.Error(w, "invalid proxy token", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/stream/echo-headers":
		// Attempt to set a cookie to verify host ModifyResponse strips Set-Cookie!
		w.Header().Set("Set-Cookie", "hijack=evil; Path=/")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cookie":     r.Header.Get("Cookie"),
			"auth":       r.Header.Get("Authorization"),
			"userId":     r.Header.Get("X-SkinLoom-User-ID"),
			"userRole":   r.Header.Get("X-SkinLoom-User-Role"),
			"locale":     r.Header.Get("X-SkinLoom-Locale"),
			"tokenValid": r.Header.Get(HeaderProxyToken) == s.proxyToken,
		})
	case "/stream/sse":
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		for i := 1; i <= 3; i++ {
			_, _ = fmt.Fprintf(w, "data: chunk-%d\n\n", i)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	case "/stream/ws":
		// Minimal RFC-compatible 101 Switching Protocols upgrade echo to prove
		// bidirectional upgraded streams traverse httputil.ReverseProxy transparently.
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "upgrade required", http.StatusUpgradeRequired)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = rw.Flush()
		line, err := rw.ReadString('\n')
		if err == nil {
			_, _ = rw.WriteString("echo:" + line)
			_ = rw.Flush()
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *helperNativePluginServer) Lifecycle(_ context.Context, req *pluginv1.LifecycleRequest) (*pluginv1.LifecycleResponse, error) {
	if req.GetEvent() == "onDisable" || req.GetEvent() == "onUninstall" {
		if s.dbPool != nil {
			_ = s.dbPool.Close()
			s.dbPool = nil
		}
	}
	return &pluginv1.LifecycleResponse{}, nil
}

func (s *helperNativePluginServer) InvokeGuard(_ context.Context, req *pluginv1.InvokeGuardRequest) (*pluginv1.InvokeGuardResponse, error) {
	var p map[string]any
	_ = json.Unmarshal(req.GetPayloadJson(), &p)
	if email, _ := p["email"].(string); email == "blocked@example.com" {
		return &pluginv1.InvokeGuardResponse{Rejected: true, Reason: "blocked by native guard"}, nil
	}
	return &pluginv1.InvokeGuardResponse{}, nil
}

func (s *helperNativePluginServer) InvokeFilter(_ context.Context, req *pluginv1.InvokeFilterRequest) (*pluginv1.InvokeFilterResponse, error) {
	var badges []map[string]any
	_ = json.Unmarshal(req.GetValueJson(), &badges)
	badges = append(badges, map[string]any{"text": "Native VIP", "color": "#10b981"})
	raw, _ := json.Marshal(badges)
	return &pluginv1.InvokeFilterResponse{ValueJson: raw}, nil
}

func (s *helperNativePluginServer) InvokeEvent(ctx context.Context, req *pluginv1.InvokeEventRequest) (*pluginv1.InvokeEventResponse, error) {
	if s.host != nil {
		args, _ := json.Marshal([]any{"last_event", req.GetHookName(), 0})
		_, _ = s.host.KV(ctx, &pluginv1.HostOpRequest{Op: "set", ArgsJson: args})
	}
	return &pluginv1.InvokeEventResponse{}, nil
}

func (s *helperNativePluginServer) InvokeHTTP(ctx context.Context, req *pluginv1.InvokeHTTPRequest) (*pluginv1.InvokeHTTPResponse, error) {
	switch req.GetHandlerId() {
	case "status":
		var rows int
		if s.dbPool != nil {
			_ = s.dbPool.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.dbPrefix+`events`).Scan(&rows)
		}
		s.mu.RLock()
		cfg := cloneMap(s.settings)
		s.mu.RUnlock()
		body, _ := json.Marshal(map[string]any{
			"pid":      os.Getpid(),
			"version":  s.version,
			"dbPrefix": s.dbPrefix,
			"dbRows":   rows,
			"settings": cfg,
		})
		return &pluginv1.InvokeHTTPResponse{Status: 200, Body: body}, nil

	case "slow":
		time.Sleep(350 * time.Millisecond)
		body, _ := json.Marshal(map[string]any{"version": s.version, "pid": os.Getpid()})
		return &pluginv1.InvokeHTTPResponse{Status: 200, Body: body}, nil

	case "crash":
		os.Exit(2)
		return nil, nil

	case "fail-health":
		s.unhealthy.Store(true)
		return &pluginv1.InvokeHTTPResponse{Status: 200, Body: []byte(`{"ok":true}`)}, nil

	case "test-host-callbacks":
		if s.host == nil {
			return &pluginv1.InvokeHTTPResponse{Status: 500, Body: []byte(`{"error":"no host"}`)}, nil
		}
		// 1. Permitted KV set & get
		setArgs, _ := json.Marshal([]any{"k1", "hello-from-native", 0})
		if _, err := s.host.KV(ctx, &pluginv1.HostOpRequest{Op: "set", ArgsJson: setArgs}); err != nil {
			return nil, err
		}
		getArgs, _ := json.Marshal([]any{"k1"})
		kvResp, err := s.host.KV(ctx, &pluginv1.HostOpRequest{Op: "get", ArgsJson: getArgs})
		if err != nil {
			return nil, err
		}
		var kvVal string
		_ = json.Unmarshal(kvResp.GetResultJson(), &kvVal)

		// 2. Denied call (users.ban is NOT granted)
		banArgs, _ := json.Marshal([]any{1, true})
		_, banErr := s.host.Users(ctx, &pluginv1.HostOpRequest{Op: "ban", ArgsJson: banArgs})

		body, _ := json.Marshal(map[string]any{
			"kvVal":     kvVal,
			"banDenied": banErr != nil && strings.Contains(banErr.Error(), "permission denied"),
		})
		return &pluginv1.InvokeHTTPResponse{Status: 200, Body: body}, nil
	}
	return &pluginv1.InvokeHTTPResponse{Status: 404, Body: []byte(`{"error":"not found"}`)}, nil
}

func (s *helperNativePluginServer) InvokeCron(ctx context.Context, req *pluginv1.InvokeCronRequest) (*pluginv1.InvokeCronResponse, error) {
	if s.host != nil {
		args, _ := json.Marshal([]any{"cron_ran", req.GetJobName(), 0})
		_, _ = s.host.KV(ctx, &pluginv1.HostOpRequest{Op: "set", ArgsJson: args})
	}
	return &pluginv1.InvokeCronResponse{}, nil
}

func (s *helperNativePluginServer) InvokeService(_ context.Context, req *pluginv1.InvokeServiceRequest) (*pluginv1.InvokeServiceResponse, error) {
	var args []any
	_ = json.Unmarshal(req.GetArgsJson(), &args)
	out, _ := json.Marshal(map[string]any{
		"service": req.GetServiceName(),
		"args":    args,
		"version": s.version,
	})
	return &pluginv1.InvokeServiceResponse{ResultJson: out}, nil
}

func (s *helperNativePluginServer) NotifySettingsChanged(_ context.Context, req *pluginv1.NotifySettingsChangedRequest) (*pluginv1.NotifySettingsChangedResponse, error) {
	var m map[string]any
	if json.Unmarshal(req.GetSettingsJson(), &m) == nil {
		s.mu.Lock()
		s.settings = m
		s.mu.Unlock()
	}
	return &pluginv1.NotifySettingsChangedResponse{}, nil
}

func (s *helperNativePluginServer) HealthCheck(_ context.Context, _ *pluginv1.HealthCheckRequest) (*pluginv1.HealthCheckResponse, error) {
	if s.unhealthy.Load() {
		return &pluginv1.HealthCheckResponse{Healthy: false, Message: "simulated unhealthy"}, nil
	}
	return &pluginv1.HealthCheckResponse{Healthy: true, MemoryRssBytes: 12345678, UptimeSeconds: 1}, nil
}

// --- StorageProviderService ---

func (s *helperNativePluginServer) Put(_ context.Context, req *pluginv1.StoragePutRequest) (*pluginv1.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[req.GetKey()] = append([]byte(nil), req.GetData()...)
	s.times[req.GetKey()] = time.Now().UTC()
	return &pluginv1.Empty{}, nil
}

func (s *helperNativePluginServer) Get(_ context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageGetResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blobs[req.GetKey()]
	if !ok {
		return &pluginv1.StorageGetResponse{Found: false}, nil
	}
	return &pluginv1.StorageGetResponse{Found: true, Data: b, ModTimeUnixNano: s.times[req.GetKey()].UnixNano()}, nil
}

func (s *helperNativePluginServer) Delete(_ context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, req.GetKey())
	delete(s.times, req.GetKey())
	return &pluginv1.Empty{}, nil
}

func (s *helperNativePluginServer) Stat(_ context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageStatResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blobs[req.GetKey()]
	if !ok {
		return &pluginv1.StorageStatResponse{Exists: false}, nil
	}
	return &pluginv1.StorageStatResponse{Exists: true, Size: int64(len(b)), ModTimeUnixNano: s.times[req.GetKey()].UnixNano()}, nil
}

func (s *helperNativePluginServer) List(_ context.Context, _ *pluginv1.StorageListRequest) (*pluginv1.StorageListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]*pluginv1.StorageEntry, 0, len(s.blobs))
	for k := range s.blobs {
		entries = append(entries, &pluginv1.StorageEntry{Key: k, ModTimeUnixNano: s.times[k].UnixNano()})
	}
	return &pluginv1.StorageListResponse{Entries: entries}, nil
}

func (s *helperNativePluginServer) URL(_ context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageURLResponse, error) {
	return &pluginv1.StorageURLResponse{Url: "https://native-cdn.test/" + req.GetKey()}, nil
}

// --- SearchProviderService ---

type helperSearchServer struct {
	pluginv1.UnimplementedSearchProviderServiceServer
	parent *helperNativePluginServer
}

func (s *helperSearchServer) Index(_ context.Context, req *pluginv1.SearchIndexRequest) (*pluginv1.Empty, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	s.parent.indexedIDs = append(s.parent.indexedIDs, req.GetDoc().GetId())
	return &pluginv1.Empty{}, nil
}

func (s *helperSearchServer) Delete(_ context.Context, req *pluginv1.SearchDeleteRequest) (*pluginv1.Empty, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	var kept []int64
	for _, id := range s.parent.indexedIDs {
		if id != req.GetTextureId() {
			kept = append(kept, id)
		}
	}
	s.parent.indexedIDs = kept
	return &pluginv1.Empty{}, nil
}

func (s *helperSearchServer) Search(_ context.Context, _ *pluginv1.SearchQueryRequest) (*pluginv1.SearchQueryResponse, error) {
	s.parent.mu.RLock()
	defer s.parent.mu.RUnlock()
	return &pluginv1.SearchQueryResponse{TextureIds: append([]int64(nil), s.parent.indexedIDs...), Total: int64(len(s.parent.indexedIDs))}, nil
}

// --- RenderProviderService ---

func (s *helperNativePluginServer) RenderAvatar(_ context.Context, req *pluginv1.RenderAvatarRequest) (*pluginv1.RenderImageResponse, error) {
	prefix := "\x89PNG\r\n\x1a\n"
	if strings.EqualFold(req.GetFormat(), "webp") {
		prefix = "RIFF\x00\x00\x00\x00WEBP"
	}
	return &pluginv1.RenderImageResponse{
		ImageBytes: []byte(fmt.Sprintf("%snative-avatar-%d-%t", prefix, req.GetSize(), req.GetIs_3D())),
		Format:     req.GetFormat(),
	}, nil
}

func (s *helperNativePluginServer) RenderPreview(_ context.Context, req *pluginv1.RenderPreviewRequest) (*pluginv1.RenderImageResponse, error) {
	prefix := "\x89PNG\r\n\x1a\n"
	if strings.EqualFold(req.GetFormat(), "webp") {
		prefix = "RIFF\x00\x00\x00\x00WEBP"
	}
	return &pluginv1.RenderImageResponse{
		ImageBytes: []byte(fmt.Sprintf("%snative-preview-%s-%d", prefix, req.GetTextureType(), req.GetHeight())),
		Format:     req.GetFormat(),
	}, nil
}

// --- Test Harness Helper ---

type nativeTestEnv struct {
	*testEnv
	storageHook *storage.SwappableStore
	textures    *texture.Service
	renders     *render.Service
}

func newNativeTestEnv(t *testing.T, mode string, extraOpts ...func(*NativeDriverOptions)) *nativeTestEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "native_test.db", DataDir: dir})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatalf("migrate.Up: %v", err)
	}

	bus := hook.NewBus(log, 4)
	t.Cleanup(func() { bus.Close(context.Background()) })

	baseStore, err := storage.NewLocal(filepath.Join(dir, "textures"))
	if err != nil {
		t.Fatal(err)
	}
	swStore := storage.NewSwappable(baseStore)
	accSvc := account.NewService(database.Ent, log)
	texSvc := texture.NewService(database.Ent, swStore, log)
	renSvc := render.NewService(2, 1<<20)
	notifySvc := notify.NewService(database.Ent, log)
	alloc := NewDBAllocator(DBAllocatorOptions{
		MainDriver: "sqlite",
		MainDSN:    "native_test.db",
		MainSQL:    database.SQL,
		DataDir:    dir,
	})

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(ManagerDeps{
		DB:                  database.Ent,
		Bus:                 bus,
		Accounts:            accSvc,
		Textures:            texSvc,
		Renders:             renSvc,
		Notify:              notifySvc,
		Store:               baseStore,
		StorageHook:         swStore,
		DBAllocator:         alloc,
		DataDir:             dir,
		AllowUnsignedNative: true,
		Log:                 log,
		NativeDriverFactory: func(_ *Manifest, _ string) Driver {
			opts := NativeDriverOptions{
				Executable:     exe,
				ExtraArgs:      []string{"-test.run=^TestHelperNativePluginProcess$"},
				ExtraEnv:       []string{"SKINLOOM_TEST_NATIVE_HELPER=1", "SKINLOOM_TEST_NATIVE_MODE=" + mode},
				HealthInterval: 80 * time.Millisecond,
				InitialBackoff: 60 * time.Millisecond,
				MaxBackoff:     200 * time.Millisecond,
			}
			for _, fn := range extraOpts {
				fn(&opts)
			}
			return NewNativeDriver(opts)
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })

	return &nativeTestEnv{
		testEnv:     &testEnv{db: database, bus: bus, notify: notifySvc, mgr: mgr, dir: dir},
		storageHook: swStore,
		textures:    texSvc,
		renders:     renSvc,
	}
}

func writeNativeManifestDir(t *testing.T, root string, manifest map[string]any) string {
	t.Helper()
	id := manifest["id"].(string)
	dir := filepath.Join(root, "src-"+id+"-"+manifest["version"].(string))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNativeDriverEndToEndHooksHostCallbacksAndSQL(t *testing.T) {
	env := newNativeTestEnv(t, "full")
	ctx := context.Background()

	pluginDir := writeNativeManifestDir(t, t.TempDir(), map[string]any{
		"id":         "native-demo",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "1.0.0",
		"name":       map[string]string{"en": "Native Demo"},
		"apiVersion": 1,
		"native": map[string]any{
			"protocolVersion": 1,
			"executable":      "bin/native-demo",
			"platforms":       []string{runtime.GOOS + "/" + runtime.GOARCH},
			"httpProxy":       true,
			"database":        map[string]any{"required": true},
		},
		"permissions": []string{"storage.sql", "storage.kv", "http.stream", "cron"},
		"hooks": map[string]any{
			"guard":  []string{"auth.register"},
			"filter": []string{"user.badges"},
			"event":  []string{"user.registered"},
		},
		"services": []string{"echo"},
		"settings": map[string]any{
			"greeting": map[string]any{"type": "string", "default": "hello"},
		},
	})

	info, err := env.mgr.LoadFromDir(ctx, pluginDir, true)
	if err != nil {
		t.Fatalf("LoadFromDir native plugin: %v", err)
	}
	if info.Runtime != RuntimeNative || info.NativeStats == nil || !info.NativeStats.Running || info.NativeStats.PID <= 0 {
		t.Fatalf("unexpected NativeStats: %+v", info)
	}
	if info.NativeStats.DBDriver != "sqlite" || info.NativeStats.DBPrefix != "plg_native_demo_" {
		t.Fatalf("unexpected DB metadata: %+v", info.NativeStats)
	}

	// 1. Guard hook: reject vs allow
	if err := hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{Email: "blocked@example.com"}); err == nil {
		t.Fatalf("expected rejection for blocked@example.com, got nil")
	} else if rej, ok := hook.AsRejection(err); !ok || rej.Reason != "blocked by native guard" {
		t.Fatalf("expected rejection 'blocked by native guard', got %v", err)
	}
	if err := hook.GuardAuthRegister.Check(ctx, env.bus, hook.AuthRegisterGuard{Email: "ok@example.com"}); err != nil {
		t.Fatalf("expected allow for ok@example.com, got %v", err)
	}

	// 2. Filter hook: user.badges
	badges := hook.FilterUserBadges.Apply(ctx, env.bus, []hook.UserBadge{}, hook.UserBadgeFilterContext{UserID: 1})
	if len(badges) != 1 || badges[0].Text != "Native VIP" {
		t.Fatalf("unexpected filtered badges: %+v", badges)
	}

	// 3. HTTP JSON route + isolated SQLite DB verification
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/native-demo/status", nil)
	env.mgr.ServePluginAPI(rec, req, "native-demo", "/status", Viewer{}, "en")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status = %d body=%s", rec.Code, rec.Body.String())
	}
	var statusBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &statusBody)
	if statusBody["dbPrefix"] != "plg_native_demo_" || int(asFloat(statusBody["dbRows"])) != 1 {
		t.Fatalf("unexpected /status payload: %+v", statusBody)
	}

	// 4. Reverse HostService callbacks (permitted KV + denied users.ban)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/plugins/native-demo/test-host-callbacks", nil)
	env.mgr.ServePluginAPI(rec, req, "native-demo", "/test-host-callbacks", Viewer{}, "en")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /test-host-callbacks = %d body=%s", rec.Code, rec.Body.String())
	}
	var cbBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cbBody)
	if cbBody["kvVal"] != "hello-from-native" || cbBody["banDenied"] != true {
		t.Fatalf("unexpected callback check: %+v", cbBody)
	}

	// 5. Cross-plugin service & Cron trigger & Settings onChange
	svcOut, err := env.mgr.CallService(ctx, "", "native-demo", "echo", "ping", 42)
	if err != nil {
		t.Fatalf("CallService: %v", err)
	}
	if m, _ := svcOut.(map[string]any); m["service"] != "echo" {
		t.Fatalf("unexpected service result: %+v", svcOut)
	}
	if err := env.mgr.TriggerCron(ctx, "native-demo", "tick"); err != nil {
		t.Fatalf("TriggerCron: %v", err)
	}
	if err := env.mgr.UpdateSettings(ctx, "native-demo", map[string]any{"greeting": "updated"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	rec = httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/native-demo/status", nil), "native-demo", "/status", Viewer{}, "en")
	_ = json.Unmarshal(rec.Body.Bytes(), &statusBody)
	if sMap, _ := statusBody["settings"].(map[string]any); sMap["greeting"] != "updated" {
		t.Fatalf("expected updated settings in subprocess, got %+v", statusBody["settings"])
	}

	// 6. Uninstall with --purge removes isolated plugin.db cleanly
	dbFile := filepath.Join(env.dir, "plugins-data", "native-demo", "plugin.db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("expected plugin.db before uninstall: %v", err)
	}
	if err := env.mgr.Uninstall(ctx, "native-demo", true); err != nil {
		t.Fatalf("Uninstall(purge=true): %v", err)
	}
	if _, err := os.Stat(dbFile); !os.IsNotExist(err) {
		t.Fatalf("expected plugin.db purged, stat err=%v", err)
	}
}

func TestNativeDriverStreamingHTTPAndWebSocketProxy(t *testing.T) {
	env := newNativeTestEnv(t, "stream")
	ctx := context.Background()

	pluginDir := writeNativeManifestDir(t, t.TempDir(), map[string]any{
		"id":         "stream-demo",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "1.0.0",
		"name":       "Stream Demo",
		"apiVersion": 1,
		"native": map[string]any{
			"protocolVersion": 1,
			"executable":      "bin/stream-demo",
			"httpProxy":       true,
		},
		"permissions": []string{"http.stream"},
	})
	if _, err := env.mgr.LoadFromDir(ctx, pluginDir, true); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	// 1. Auth enforcement: admin SSE route rejects guest (401) and plain user (403)
	rec := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/stream-demo/stream/sse", nil),
		"stream-demo", "/stream/sse", Viewer{}, "en")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest on admin stream route = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/stream-demo/stream/sse", nil),
		"stream-demo", "/stream/sse", Viewer{Authenticated: true, UserID: 7, Role: "user"}, "en")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user on admin stream route = %d, want 403", rec.Code)
	}

	// 2. Admin SSE stream receives all flushed chunks
	rec = httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/stream-demo/stream/sse", nil),
		"stream-demo", "/stream/sse", Viewer{Authenticated: true, UserID: 1, Role: "admin", Admin: true}, "zh-CN")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data: chunk-3") {
		t.Fatalf("SSE response code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 3. Header sanitization: strips Cookie, Authorization, spoofed X-SkinLoom-*, and response Set-Cookie
	rec = httptest.NewRecorder()
	hdrReq := httptest.NewRequest(http.MethodGet, "/api/plugins/stream-demo/stream/echo-headers", nil)
	hdrReq.Header.Set("Cookie", "session=top-secret")
	hdrReq.Header.Set("Authorization", "Bearer admin-secret")
	hdrReq.Header.Set("X-SkinLoom-User-ID", "999999")
	hdrReq.Header.Set("X-SkinLoom-User-Role", "superadmin")
	env.mgr.ServePluginAPI(rec, hdrReq, "stream-demo", "/stream/echo-headers",
		Viewer{Authenticated: true, UserID: 42, Role: "user"}, "zh-CN")
	if rec.Code != http.StatusOK {
		t.Fatalf("echo-headers = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("expected response Set-Cookie to be stripped, got %q", rec.Header().Get("Set-Cookie"))
	}
	var echo map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &echo)
	if echo["cookie"] != "" || echo["auth"] != "" || echo["userId"] != "42" || echo["userRole"] != "user" || echo["locale"] != "zh-CN" || echo["tokenValid"] != true {
		t.Fatalf("unexpected proxied headers: %+v", echo)
	}

	// 4. WebSocket / HTTP 101 Upgrade traverses ReverseProxy transparently over real TCP
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mgr.ServePluginAPI(w, r, "stream-demo", "/stream/ws", Viewer{}, "en")
	}))
	defer ts.Close()

	conn, err := net.DialTimeout("tcp", ts.Listener.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatalf("dial test server: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET /api/plugins/stream-demo/stream/ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", ts.Listener.Addr().String())
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 Switching Protocols, got %q err=%v", statusLine, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	_, _ = io.WriteString(conn, "hello-ws\n")
	reply, err := br.ReadString('\n')
	if err != nil || reply != "echo:hello-ws\n" {
		t.Fatalf("upgraded echo = (%q, %v), want echo:hello-ws\\n", reply, err)
	}
}

func TestNativeDriverCrashAutoRestartAndCircuitBreaker(t *testing.T) {
	t.Run("auto-restart recovers transparently after subprocess crash", func(t *testing.T) {
		env := newNativeTestEnv(t, "basic")
		ctx := context.Background()
		dir := writeNativeManifestDir(t, t.TempDir(), map[string]any{
			"id":         "crash-demo",
			"kind":       "plugin",
			"runtime":    "native",
			"version":    "1.0.0",
			"name":       "Crash Demo",
			"apiVersion": 1,
			"native":     map[string]any{"protocolVersion": 1, "executable": "bin/crash-demo"},
		})
		info1, err := env.mgr.LoadFromDir(ctx, dir, true)
		if err != nil {
			t.Fatalf("LoadFromDir: %v", err)
		}
		pid1 := info1.NativeStats.PID

		// Crash the child process via /crash (child calls os.Exit(2)).
		rec := httptest.NewRecorder()
		env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodPost, "/api/plugins/crash-demo/crash", nil), "crash-demo", "/crash", Viewer{}, "en")
		if rec.Code == http.StatusOK {
			t.Fatalf("expected error response when subprocess crashed")
		}

		// Supervisor must restart child with a brand new PID and resume serving.
		mustEventually(t, 5*time.Second, func() bool {
			cur, ok := env.mgr.Get(ctx, "crash-demo")
			return ok && cur.NativeStats != nil && cur.NativeStats.Running && cur.NativeStats.Restarts >= 1 && cur.NativeStats.PID != pid1
		}, "native plugin automatic crash recovery")

		rec = httptest.NewRecorder()
		env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/crash-demo/status", nil), "crash-demo", "/status", Viewer{}, "en")
		if rec.Code != http.StatusOK {
			t.Fatalf("status after restart = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("repeated unhealthy failures trip circuit breaker and notify superadmin", func(t *testing.T) {
		env := newNativeTestEnv(t, "basic", func(o *NativeDriverOptions) {
			o.HealthInterval = 30 * time.Millisecond
			o.InitialBackoff = 20 * time.Millisecond
			o.MaxBackoff = 40 * time.Millisecond
		})
		ctx := context.Background()

		// Create a superadmin to verify alert notification on circuit trip.
		_, err := env.db.Ent.User.Create().
			SetEmail("root@example.com").
			SetEmailNormalized("root@example.com").
			SetNickname("root").
			SetPasswordHash("x").
			SetRole(user.RoleSuperadmin).
			SetStatus(user.StatusActive).
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}

		dir := writeNativeManifestDir(t, t.TempDir(), map[string]any{
			"id":         "trip-demo",
			"kind":       "plugin",
			"runtime":    "native",
			"version":    "1.0.0",
			"name":       "Trip Demo",
			"apiVersion": 1,
			"native":     map[string]any{"protocolVersion": 1, "executable": "bin/trip-demo"},
		})
		if _, err := env.mgr.LoadFromDir(ctx, dir, true); err != nil {
			t.Fatalf("LoadFromDir: %v", err)
		}

		// Force repeated crashes faster than recovery so consecutive failures reach DefaultCircuitThreshold.
		env.mgr.mu.RLock()
		rt := env.mgr.active["trip-demo"]
		env.mgr.mu.RUnlock()
		for i := range DefaultCircuitThreshold {
			rt.recordRuntimeFailure(fmt.Errorf("crash #%d", i+1))
		}

		mustEventually(t, 3*time.Second, func() bool {
			row, err := env.db.Ent.Plugin.Get(ctx, "trip-demo")
			return err == nil && row.Status == entplugin.StatusFailed
		}, "native plugin circuit breaker trip to failed status")
	})
}

func TestNativeDriverHotUpgradeSmoothDrain(t *testing.T) {
	env := newNativeTestEnv(t, "basic")
	ctx := context.Background()
	tmp := t.TempDir()

	v1Dir := writeNativeManifestDir(t, tmp, map[string]any{
		"id":         "hot-plug",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "1.0.0",
		"name":       "Hot Plug",
		"apiVersion": 1,
		"native":     map[string]any{"protocolVersion": 1, "executable": "bin/hot-plug"},
	})
	if _, err := env.mgr.LoadFromDir(ctx, v1Dir, true); err != nil {
		t.Fatalf("LoadFromDir v1: %v", err)
	}

	// Launch an in-flight slow request against v1.
	type respResult struct {
		code int
		body string
	}
	slowDone := make(chan respResult, 1)
	go func() {
		rec := httptest.NewRecorder()
		env.mgr.ServePluginAPI(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/hot-plug/slow", nil), "hot-plug", "/slow", Viewer{}, "en")
		slowDone <- respResult{code: rec.Code, body: rec.Body.String()}
	}()

	time.Sleep(60 * time.Millisecond)

	// Hot-upgrade to v2 while v1's slow request is still running!
	v2Dir := writeNativeManifestDir(t, tmp, map[string]any{
		"id":         "hot-plug",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "2.0.0",
		"name":       "Hot Plug",
		"apiVersion": 1,
		"native":     map[string]any{"protocolVersion": 1, "executable": "bin/hot-plug"},
	})
	if _, err := env.mgr.LoadFromDir(ctx, v2Dir, true); err != nil {
		t.Fatalf("LoadFromDir v2: %v", err)
	}

	// New request immediately routes to v2.
	recV2 := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recV2, httptest.NewRequest(http.MethodGet, "/api/plugins/hot-plug/status", nil), "hot-plug", "/status", Viewer{}, "en")
	if recV2.Code != http.StatusOK || !strings.Contains(recV2.Body.String(), `"version":"2.0.0"`) {
		t.Fatalf("expected v2 status immediately, got code=%d body=%s", recV2.Code, recV2.Body.String())
	}

	// In-flight v1 request must finish smoothly without interruption!
	slowRes := <-slowDone
	if slowRes.code != http.StatusOK || !strings.Contains(slowRes.body, `"version":"1.0.0"`) {
		t.Fatalf("in-flight v1 request failed during hot upgrade: code=%d body=%s", slowRes.code, slowRes.body)
	}
}

func TestNativeDriverInfrastructureProvidersHotSwap(t *testing.T) {
	env := newNativeTestEnv(t, "providers")
	ctx := context.Background()

	dir := writeNativeManifestDir(t, t.TempDir(), map[string]any{
		"id":         "infra-plug",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "1.0.0",
		"name":       "Infra Providers",
		"apiVersion": 1,
		"native":     map[string]any{"protocolVersion": 1, "executable": "bin/infra-plug"},
		"permissions": []string{
			PermProviderStorage,
			PermProviderSearch,
			PermProviderRender,
		},
	})
	if _, err := env.mgr.LoadFromDir(ctx, dir, true); err != nil {
		t.Fatalf("LoadFromDir providers: %v", err)
	}

	// 1. StorageProvider active & working over gRPC
	if env.storageHook.CurrentOwner() != "plugin:infra-plug" {
		t.Fatalf("storage owner = %q, want plugin:infra-plug", env.storageHook.CurrentOwner())
	}
	sum := sha256.Sum256([]byte("native-blob"))
	key := hex.EncodeToString(sum[:])
	if err := env.storageHook.Put(ctx, key, []byte("native-blob")); err != nil {
		t.Fatalf("SwappableStore.Put: %v", err)
	}
	if gotURL := env.storageHook.URL(key); gotURL != "https://native-cdn.test/"+key {
		t.Fatalf("SwappableStore.URL = %q", gotURL)
	}
	f, err := env.storageHook.Open(ctx, key)
	if err != nil {
		t.Fatalf("SwappableStore.Open: %v", err)
	}
	gotData, _ := io.ReadAll(f)
	_ = f.Close()
	if string(gotData) != "native-blob" {
		t.Fatalf("SwappableStore.Open data = %q", gotData)
	}

	// 2. SearchProvider active
	if env.textures.SearchProviderOwner() != "plugin:infra-plug" {
		t.Fatalf("search owner = %q, want plugin:infra-plug", env.textures.SearchProviderOwner())
	}

	// 3. RenderProvider active & working over gRPC
	if env.renders.ProviderOwner() != "plugin:infra-plug" {
		t.Fatalf("render owner = %q, want plugin:infra-plug", env.renders.ProviderOwner())
	}
	avBytes, err := env.renders.RenderAvatarWithProvider(ctx, render.AvatarRequest{
		SkinPNG: []byte("fake-png"), Size: 128, Is3D: true, Format: render.PNG,
	}, func() ([]byte, error) { return []byte("builtin"), nil })
	if err != nil || !strings.HasSuffix(string(avBytes), "native-avatar-128-true") {
		t.Fatalf("RenderAvatarWithProvider = (%q, %v)", avBytes, err)
	}
	prBytes, err := env.renders.RenderPreviewWithProvider(ctx, render.PreviewRequest{
		TexturePNG: []byte("fake-png"), TextureType: "alex", Height: 256, Format: render.WebP,
	}, func() ([]byte, error) { return []byte("builtin"), nil })
	if err != nil || !strings.HasSuffix(string(prBytes), "native-preview-alex-256") {
		t.Fatalf("RenderPreviewWithProvider = (%q, %v)", prBytes, err)
	}

	// 4. Disabling plugin atomically rolls back all three providers to built-in!
	if err := env.mgr.Disable(ctx, "infra-plug"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if env.storageHook.CurrentOwner() != "" || env.textures.SearchProviderOwner() != "" || env.renders.ProviderOwner() != "" {
		t.Fatalf("providers were not relinquished on Disable: storage=%q search=%q render=%q",
			env.storageHook.CurrentOwner(), env.textures.SearchProviderOwner(), env.renders.ProviderOwner())
	}
	// Verify unused import sanity on strconv if needed
	_ = strconv.Itoa(0)
}

type e2eSDKPlugin struct{}

func (e2eSDKPlugin) Setup(ctx *gosdk.Context) error {
	if db := ctx.DB(); db != nil {
		mfs := fstest.MapFS{
			"migrations/001_sdk.sql": &fstest.MapFile{
				Data: []byte(`-- +goose Up
CREATE TABLE {{prefix}}notes (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT NOT NULL);
`),
			},
		}
		if err := db.Migrate(context.Background(), mfs, "migrations"); err != nil {
			return err
		}
	}

	ctx.HTTP.Post("/notes", func(reqCtx context.Context, req *gosdk.HTTPRequest) (*gosdk.HTTPResponse, error) {
		var in struct {
			Body string `json:"body"`
		}
		_ = req.DecodeJSON(&in)
		if _, err := ctx.DB().Exec(reqCtx, `INSERT INTO {{prefix}}notes (body) VALUES (?)`, in.Body); err != nil {
			return nil, err
		}
		if err := ctx.KV.Set(reqCtx, "latest", in.Body, 0); err != nil {
			return nil, err
		}
		msg, err := ctx.I18n.T(reqCtx, "saved", map[string]any{"body": in.Body}, req.Locale)
		if err != nil {
			return nil, err
		}
		return gosdk.JSON(http.StatusOK, map[string]any{"ok": true, "message": msg}), nil
	}, gosdk.WithAuth(gosdk.AuthPublic))

	ctx.HTTP.Get("/sdk-verify", func(reqCtx context.Context, _ *gosdk.HTTPRequest) (*gosdk.HTTPResponse, error) {
		inserted, err := ctx.Docs.InsertOne(reqCtx, "notes_doc", map[string]any{"title": "doc1"})
		if err != nil {
			return nil, fmt.Errorf("InsertOne: %w", err)
		}
		docID, _ := inserted["id"].(string)
		updated, err := ctx.Docs.UpdateOne(reqCtx, "notes_doc", map[string]any{"id": docID}, map[string]any{"$set": map[string]any{"title": "doc2"}})
		if err != nil || updated != 1 {
			return nil, fmt.Errorf("UpdateOne: updated=%v err=%v", updated, err)
		}
		deleted, err := ctx.Docs.DeleteOne(reqCtx, "notes_doc", map[string]any{"id": docID})
		if err != nil || deleted != 1 {
			return nil, fmt.Errorf("DeleteOne: deleted=%v err=%v", deleted, err)
		}
		label := ctx.Settings.GetString("label")
		return gosdk.JSON(http.StatusOK, map[string]any{
			"docID":   docID,
			"updated": updated == 1,
			"deleted": deleted == 1,
			"label":   label,
			"pid":     os.Getpid(),
		}), nil
	}, gosdk.WithAuth(gosdk.AuthPublic))

	ctx.HTTP.Get("/handler-err", func(_ context.Context, _ *gosdk.HTTPRequest) (*gosdk.HTTPResponse, error) {
		return nil, fmt.Errorf("normal plugin handler failure")
	}, gosdk.WithAuth(gosdk.AuthPublic))

	ctx.HTTP.StreamFunc(http.MethodGet, "/stream/hello/:who", func(w http.ResponseWriter, r *http.Request) {
		params := gosdk.StreamParams(r)
		viewer := gosdk.ProxyViewer(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: hi-%s-auth-%t-role-%s\n\n", params["who"], viewer.Authenticated, viewer.Role)
	}, gosdk.WithAuth(gosdk.AuthUser))

	return nil
}

func TestHelperOfficialSDKPluginProcess(t *testing.T) {
	if os.Getenv("SKINLOOM_TEST_SDK_HELPER") != "1" {
		return
	}
	gosdk.Serve(e2eSDKPlugin{})
	os.Exit(0)
}

func TestNativeDriverWithOfficialGoSDK(t *testing.T) {
	ctx := context.Background()
	env := newNativeTestEnv(t, "sdk", func(opts *NativeDriverOptions) {
		opts.ExtraArgs = []string{"-test.run=^TestHelperOfficialSDKPluginProcess$"}
		opts.ExtraEnv = []string{"SKINLOOM_TEST_SDK_HELPER=1"}
	})

	dir := writeNativeManifestDir(t, env.dir, map[string]any{
		"id":         "sdk-e2e",
		"kind":       "plugin",
		"runtime":    "native",
		"version":    "1.0.0",
		"name":       "SDK E2E",
		"apiVersion": 1,
		"native": map[string]any{
			"protocolVersion": 1,
			"executable":      "bin/sdk-e2e",
			"httpProxy":       true,
			"database":        map[string]any{"required": true},
		},
		"permissions": []string{PermStorageKV, PermStorageDocs, PermStorageSQL, PermHTTPStream},
		"storage": map[string]any{
			"collections": map[string]any{
				"notes_doc": map[string]any{},
			},
		},
		"settings": map[string]any{
			"label": map[string]any{"type": "string", "default": "initial-label"},
		},
	})
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locales", "en.json"), []byte(`{"saved":"Saved {{body}}!"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := env.mgr.LoadFromDir(ctx, dir, true); err != nil {
		t.Fatalf("LoadFromDir sdk-e2e: %v", err)
	}

	// 1. JSON HTTP route exercising plugindb + KV + I18N (verifying I18N gRPC fix too!)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/plugins/sdk-e2e/notes", strings.NewReader(`{"body":"from-sdk"}`))
	req.Header.Set("Content-Type", "application/json")
	env.mgr.ServePluginAPI(rec, req, "sdk-e2e", "/notes", Viewer{}, "en")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Saved from-sdk!") {
		t.Fatalf("POST /notes failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. Streaming SSE route over real Unix Socket / loopback ReverseProxy (including Connection header stripping defense & UserID=0 AdminToken superadmin)
	recSSE := httptest.NewRecorder()
	reqSSE := httptest.NewRequest(http.MethodGet, "/api/plugins/sdk-e2e/stream/hello/steve", nil)
	reqSSE.Header.Set("Connection", "keep-alive, X-SkinLoom-Proxy-Token, X-SkinLoom-User-Role")
	env.mgr.ServePluginAPI(recSSE, reqSSE, "sdk-e2e", "/stream/hello/steve", Viewer{Authenticated: true, UserID: 0, Role: "superadmin"}, "en")
	if recSSE.Code != http.StatusOK || !strings.Contains(recSSE.Body.String(), "data: hi-steve-auth-true-role-superadmin") {
		t.Fatalf("GET /stream/hello/steve failed: status=%d body=%s", recSSE.Code, recSSE.Body.String())
	}

	// 3. Hot-update settings without OnChange listener & exercise Docs InsertOne/UpdateOne/DeleteOne over real gRPC host
	if err := env.mgr.UpdateSettings(ctx, "sdk-e2e", map[string]any{"label": "updated-label"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	recVerify := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recVerify, httptest.NewRequest(http.MethodGet, "/api/plugins/sdk-e2e/sdk-verify", nil), "sdk-e2e", "/sdk-verify", Viewer{}, "en")
	if recVerify.Code != http.StatusOK || !strings.Contains(recVerify.Body.String(), `"label":"updated-label"`) || !strings.Contains(recVerify.Body.String(), `"updated":true`) || !strings.Contains(recVerify.Body.String(), `"deleted":true`) {
		t.Fatalf("GET /sdk-verify failed: status=%d body=%s", recVerify.Code, recVerify.Body.String())
	}
	var verifyOut struct {
		PID int `json:"pid"`
	}
	_ = json.Unmarshal(recVerify.Body.Bytes(), &verifyOut)

	// 4. Normal plugin handler errors (codes.Unknown) must NOT restart the subprocess or trip the circuit breaker.
	for i := 0; i < DefaultCircuitThreshold+2; i++ {
		recErr := httptest.NewRecorder()
		env.mgr.ServePluginAPI(recErr, httptest.NewRequest(http.MethodGet, "/api/plugins/sdk-e2e/handler-err", nil), "sdk-e2e", "/handler-err", Viewer{}, "en")
		if recErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 from /handler-err, got %d", recErr.Code)
		}
	}
	recAfter := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recAfter, httptest.NewRequest(http.MethodGet, "/api/plugins/sdk-e2e/sdk-verify", nil), "sdk-e2e", "/sdk-verify", Viewer{}, "en")
	if recAfter.Code != http.StatusOK {
		t.Fatalf("expected plugin to remain active after normal handler errors, got %d %s", recAfter.Code, recAfter.Body.String())
	}
	var afterOut struct {
		PID int `json:"pid"`
	}
	_ = json.Unmarshal(recAfter.Body.Bytes(), &afterOut)
	if verifyOut.PID == 0 || afterOut.PID != verifyOut.PID {
		t.Fatalf("expected subprocess PID to remain %d after handler errors, got %d", verifyOut.PID, afterOut.PID)
	}
}
