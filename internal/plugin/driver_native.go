package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
)

const (
	// NativePluginName is the hashicorp/go-plugin plugin map key for SkinLoom Tier 2 plugins.
	NativePluginName = "skinloom"

	// HeaderProxyToken carries the ephemeral 256-bit shared secret between host
	// ReverseProxy and a native plugin's local HTTP/WebSocket listener.
	HeaderProxyToken = "X-SkinLoom-Proxy-Token"

	defaultNativeHealthInterval = 5 * time.Second
	defaultNativeInitialBackoff = 500 * time.Millisecond
	defaultNativeMaxBackoff     = 30 * time.Second
	maxNativeGRPCMsgBytes       = 32 * 1024 * 1024
)

// NativeHandshake defines the protocol handshake shared by SkinLoom host and sdk/go.
var NativeHandshake = hplugin.HandshakeConfig{
	ProtocolVersion:  CurrentNativeProtocolVersion,
	MagicCookieKey:   "SKINLOOM_NATIVE_PLUGIN",
	MagicCookieValue: "skinloom-plugin-v1",
}

// NativeServerImpl groups the gRPC server implementations hosted inside a native plugin process.
type NativeServerImpl struct {
	Plugin  pluginv1.PluginServiceServer
	Storage pluginv1.StorageProviderServiceServer
	Search  pluginv1.SearchProviderServiceServer
	Render  pluginv1.RenderProviderServiceServer
}

// NativeGRPCPlugin implements hashicorp/go-plugin's GRPCPlugin for SkinLoom Tier 2 plugins.
type NativeGRPCPlugin struct {
	hplugin.NetRPCUnsupportedPlugin
	Impl *NativeServerImpl
}

func (p *NativeGRPCPlugin) GRPCServer(broker *hplugin.GRPCBroker, s *grpc.Server) error {
	if p.Impl == nil || p.Impl.Plugin == nil {
		return errors.New("native plugin server requires PluginService implementation")
	}
	if aware, ok := p.Impl.Plugin.(interface{ SetGRPCBroker(*hplugin.GRPCBroker) }); ok {
		aware.SetGRPCBroker(broker)
	}
	pluginv1.RegisterPluginServiceServer(s, p.Impl.Plugin)
	if p.Impl.Storage != nil {
		pluginv1.RegisterStorageProviderServiceServer(s, p.Impl.Storage)
	}
	if p.Impl.Search != nil {
		pluginv1.RegisterSearchProviderServiceServer(s, p.Impl.Search)
	}
	if p.Impl.Render != nil {
		pluginv1.RegisterRenderProviderServiceServer(s, p.Impl.Render)
	}
	return nil
}

func (p *NativeGRPCPlugin) GRPCClient(_ context.Context, broker *hplugin.GRPCBroker, cc *grpc.ClientConn) (any, error) {
	return &nativeGRPCClientBundle{
		broker:  broker,
		plugin:  pluginv1.NewPluginServiceClient(cc),
		storage: pluginv1.NewStorageProviderServiceClient(cc),
		search:  pluginv1.NewSearchProviderServiceClient(cc),
		render:  pluginv1.NewRenderProviderServiceClient(cc),
	}, nil
}

type nativeGRPCClientBundle struct {
	broker  *hplugin.GRPCBroker
	plugin  pluginv1.PluginServiceClient
	storage pluginv1.StorageProviderServiceClient
	search  pluginv1.SearchProviderServiceClient
	render  pluginv1.RenderProviderServiceClient
}

// NativeDriverOptions tunes subprocess launch and crash supervision behavior.
type NativeDriverOptions struct {
	Executable     string // optional override; otherwise resolved from manifest.native.executable
	ExtraArgs      []string
	ExtraEnv       []string
	HealthInterval time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type nativeProxyHandle struct {
	network    string
	address    string
	token      string
	socketFile string
	proxy      *httputil.ReverseProxy
	transport  *http.Transport
}

func (p *nativeProxyHandle) close() {
	if p == nil {
		return
	}
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	if p.socketFile != "" {
		_ = os.Remove(p.socketFile)
	}
}

type streamProxyMetaKey struct{}

type streamProxyMeta struct {
	PluginID  string
	Locale    string
	RequestID string
	Viewer    Viewer
}

type nativeProcessHandle struct {
	client     *hplugin.Client
	plugin     pluginv1.PluginServiceClient
	storage    pluginv1.StorageProviderServiceClient
	search     pluginv1.SearchProviderServiceClient
	render     pluginv1.RenderProviderServiceClient
	brokerMu   sync.Mutex
	brokerSrv  *grpc.Server
	proxy      *nativeProxyHandle
	pid        int
	startedAt  time.Time
	socketPath string
	inflight   sync.WaitGroup
}

func (h *nativeProcessHandle) drainAndClose(timeout time.Duration) {
	if h == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		h.inflight.Wait()
		close(done)
	}()
	if timeout > 0 {
		t := time.NewTimer(timeout)
		select {
		case <-done:
			t.Stop()
		case <-t.C:
		}
	}
	h.close()
}

func (h *nativeProcessHandle) close() {
	if h == nil {
		return
	}
	h.brokerMu.Lock()
	srv := h.brokerSrv
	h.brokerSrv = nil
	h.brokerMu.Unlock()
	if srv != nil {
		srv.Stop()
	}
	if h.proxy != nil {
		h.proxy.close()
	}
	if h.client != nil {
		h.client.Kill()
	}
	if h.socketPath != "" {
		_ = os.Remove(h.socketPath)
	}
}

// NativeDriver runs a Tier 2 Go native plugin in an isolated OS child process
// managed by hashicorp/go-plugin, providing bidirectional gRPC, exponential
// backoff crash supervision, smooth hot-upgrade draining, and optional local
// HTTP/WebSocket streaming reverse proxy.
type NativeDriver struct {
	opts NativeDriverOptions

	rt         *pluginRuntime
	dbConfig   *PluginDBConfig
	dataDir    string
	runtimeDir string
	executable string

	mu         sync.RWMutex
	handle     *nativeProcessHandle
	running    bool
	restarts   int
	lastMemRSS uint64
	hasProxy   bool

	callMu   sync.RWMutex
	draining bool
	inflight sync.WaitGroup

	stopOnce  sync.Once
	stopSup   chan struct{}
	supCtx    context.Context
	supCancel context.CancelFunc
	wakeSup   chan struct{}
	supWg     sync.WaitGroup
}

// NewNativeDriver creates a NativeDriver with the given options.
func NewNativeDriver(opts NativeDriverOptions) *NativeDriver {
	if opts.HealthInterval <= 0 {
		opts.HealthInterval = defaultNativeHealthInterval
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = defaultNativeInitialBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultNativeMaxBackoff
	}
	supCtx, supCancel := context.WithCancel(context.Background())
	return &NativeDriver{
		opts:      opts,
		stopSup:   make(chan struct{}),
		supCtx:    supCtx,
		supCancel: supCancel,
		wakeSup:   make(chan struct{}, 1),
	}
}

func (d *NativeDriver) Init(ctx context.Context, cfg DriverInitConfig) (*DriverRegistrations, error) {
	if cfg.Runtime == nil || cfg.Runtime.manifest == nil {
		return nil, errors.New("native driver requires plugin runtime and manifest")
	}
	if err := cfg.Runtime.perms.Check(PermNativeExec); err != nil {
		return nil, err
	}
	d.rt = cfg.Runtime
	d.dbConfig = cfg.DBConfig
	d.dataDir = cfg.DataDir
	d.runtimeDir = cfg.RuntimeDir

	exePath := d.opts.Executable
	if exePath == "" {
		var err error
		exePath, err = d.rt.manifest.ResolveNativeExecutable(d.rt.dir, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return nil, err
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(exePath, 0o755); err != nil {
			return nil, fmt.Errorf("chmod native executable %s: %w", exePath, err)
		}
	}
	d.executable = exePath

	handle, initResp, err := d.launchProcess(ctx, cfg.Settings)
	if err != nil {
		return nil, err
	}
	regs, err := d.buildRegistrations(initResp)
	if err != nil {
		handle.close()
		return nil, err
	}

	d.mu.Lock()
	d.handle = handle
	d.running = true
	d.hasProxy = handle.proxy != nil
	d.mu.Unlock()

	d.supWg.Add(1)
	go d.supervise()

	return regs, nil
}

func (d *NativeDriver) launchProcess(ctx context.Context, settingsMap map[string]any) (*nativeProcessHandle, *pluginv1.InitResponse, error) {
	cmd := exec.Command(d.executable, d.opts.ExtraArgs...)
	cmd.Env = d.buildChildEnv()

	hcLog := newSlogHCLogger(d.rt)
	stdoutW := newLineLogWriter(d.rt, "info")
	stderrW := newLineLogWriter(d.rt, "warn")

	client := hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig: NativeHandshake,
		Plugins: map[string]hplugin.Plugin{
			NativePluginName: &NativeGRPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolGRPC},
		Logger:           hcLog,
		SyncStdout:       stdoutW,
		SyncStderr:       stderrW,
		StartTimeout:     15 * time.Second,
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxNativeGRPCMsgBytes),
				grpc.MaxCallSendMsgSize(maxNativeGRPCMsgBytes),
			),
		},
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("start native plugin %q: %w", d.rt.id, err)
	}
	raw, err := rpcClient.Dispense(NativePluginName)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense native plugin %q: %w", d.rt.id, err)
	}
	bundle, ok := raw.(*nativeGRPCClientBundle)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("unexpected dispensed client type %T", raw)
	}

	handle := &nativeProcessHandle{
		client:    client,
		plugin:    bundle.plugin,
		storage:   bundle.storage,
		search:    bundle.search,
		render:    bundle.render,
		startedAt: time.Now().UTC(),
	}
	if rc := client.ReattachConfig(); rc != nil {
		handle.pid = rc.Pid
	}

	// Start reverse HostService on go-plugin's GRPCBroker.
	brokerID := bundle.broker.NextId()
	hostSrv := newHostGRPCServer(d.rt)
	go bundle.broker.AcceptAndServe(brokerID, func(opts []grpc.ServerOption) *grpc.Server {
		opts = append(opts,
			grpc.MaxRecvMsgSize(maxNativeGRPCMsgBytes),
			grpc.MaxSendMsgSize(maxNativeGRPCMsgBytes),
		)
		srv := grpc.NewServer(opts...)
		pluginv1.RegisterHostServiceServer(srv, hostSrv)
		handle.brokerMu.Lock()
		handle.brokerSrv = srv
		handle.brokerMu.Unlock()
		return srv
	})

	// Prepare optional HTTP proxy socket path & 256-bit shared token.
	var proxySocketPath string
	var proxyToken string
	wantsProxy := d.rt.manifest.Native != nil && d.rt.manifest.Native.HTTPProxy && d.rt.perms.Has(PermHTTPStream)
	if wantsProxy {
		proxyToken = randomHexString(32) // 256 bits
		if runtime.GOOS != "windows" {
			sock, sErr := d.allocateUnixSocketPath()
			if sErr != nil {
				handle.close()
				return nil, nil, fmt.Errorf("allocate unix proxy socket for %q: %w", d.rt.id, sErr)
			}
			proxySocketPath = sock
			handle.socketPath = sock
		}
	}

	settingsJSON, _ := json.Marshal(settingsMap)
	var protoDB *pluginv1.DatabaseConfig
	if d.dbConfig != nil && d.rt.perms.Has(PermStorageSQL) {
		protoDB = &pluginv1.DatabaseConfig{
			Driver: d.dbConfig.Driver,
			Dsn:    d.dbConfig.DSN,
			Prefix: d.dbConfig.Prefix,
			Schema: d.dbConfig.Schema,
		}
	}

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	initResp, err := handle.plugin.Init(initCtx, &pluginv1.InitRequest{
		PluginId:           d.rt.id,
		Version:            d.rt.manifest.Version,
		SettingsJson:       settingsJSON,
		GrantedPermissions: append([]string(nil), d.rt.grantedPerms...),
		DataDir:            d.dataDir,
		RuntimeDir:         d.runtimeDir,
		Database:           protoDB,
		HostBrokerId:       brokerID,
		ProxySocketPath:    proxySocketPath,
		ProxyToken:         proxyToken,
	})
	if err != nil {
		handle.close()
		return nil, nil, fmt.Errorf("native plugin %q Init failed: %w", d.rt.id, err)
	}

	if hp := initResp.GetHttpProxy(); hp != nil && hp.GetEnabled() {
		if !wantsProxy {
			handle.close()
			return nil, nil, fmt.Errorf("%w: plugin attempted to enable httpProxy without declaration and %q permission", ErrPermissionDenied, PermHTTPStream)
		}
		ph, pErr := d.buildProxyHandle(hp, proxySocketPath, proxyToken)
		if pErr != nil {
			handle.close()
			return nil, nil, pErr
		}
		handle.proxy = ph
	}

	return handle, initResp, nil
}

func (d *NativeDriver) allocateUnixSocketPath() (string, error) {
	nonce := randomHexString(8)
	baseDir := d.runtimeDir
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "skinloom-run")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	cand := filepath.Join(baseDir, fmt.Sprintf("plugin-%s-%s.sock", d.rt.id, nonce))
	// Unix domain socket sun_path is capped at 104 bytes on macOS and 108 on Linux.
	if len(cand) > 96 {
		shortDir := filepath.Join(os.TempDir(), "sl-run")
		if err := os.MkdirAll(shortDir, 0o700); err != nil {
			return "", err
		}
		cand = filepath.Join(shortDir, fmt.Sprintf("p-%s.sock", nonce))
	}
	_ = os.Remove(cand)
	return cand, nil
}

func (d *NativeDriver) buildProxyHandle(info *pluginv1.HTTPProxyInfo, expectedSocket, defaultToken string) (*nativeProxyHandle, error) {
	network := strings.ToLower(strings.TrimSpace(info.GetNetwork()))
	addr := strings.TrimSpace(info.GetAddress())
	if len(defaultToken) < 32 {
		return nil, errors.New("native httpProxy requires at least a 128-bit host secret token")
	}
	if reported := strings.TrimSpace(info.GetToken()); reported != "" && subtle.ConstantTimeCompare([]byte(reported), []byte(defaultToken)) != 1 {
		return nil, errors.New("native httpProxy reported token does not match host-issued token")
	}
	token := defaultToken

	var socketFile string
	switch network {
	case "unix":
		if runtime.GOOS == "windows" {
			return nil, errors.New("unix socket proxy is not supported on windows")
		}
		if expectedSocket == "" {
			return nil, errors.New("unix socket proxy requested without host-assigned path")
		}
		if filepath.Clean(addr) != filepath.Clean(expectedSocket) {
			return nil, fmt.Errorf("native httpProxy unix socket %q does not match host-assigned path", addr)
		}
		if addr == "" || !filepath.IsAbs(addr) {
			return nil, fmt.Errorf("invalid unix socket address %q", addr)
		}
		socketFile = expectedSocket
	case "tcp":
		if runtime.GOOS != "windows" {
			return nil, errors.New("tcp httpProxy is only permitted on windows; non-windows hosts must use the host-assigned unix socket")
		}
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid tcp proxy address %q: %w", addr, err)
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("native httpProxy tcp address %q must bind strictly to loopback", addr)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("invalid tcp proxy port in %q", addr)
		}
	default:
		return nil, fmt.Errorf("unsupported httpProxy network %q (want unix or tcp)", network)
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		DisableCompression:    false,
	}

	targetURL := &url.URL{Scheme: "http", Host: "plugin.local"}
	rp := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // immediate flushing for SSE and streaming payloads
		ErrorLog:      log.New(io.Discard, "", 0),
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.Out.Host = "plugin.local"
			// Strip sensitive host credentials, untrusted forwarded headers, and any caller X-SkinLoom-*.
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")
			for k := range pr.Out.Header {
				if strings.HasPrefix(strings.ToLower(k), "x-skinloom-") {
					pr.Out.Header.Del(k)
				}
			}
			// Inject host-verified metadata AFTER ReverseProxy hop-by-hop stripping so a client
			// Connection: header cannot strip X-SkinLoom-* or X-SkinLoom-Proxy-Token.
			if meta, ok := pr.In.Context().Value(streamProxyMetaKey{}).(streamProxyMeta); ok {
				pr.Out.Header.Set("X-SkinLoom-Plugin-ID", meta.PluginID)
				pr.Out.Header.Set("X-SkinLoom-Locale", meta.Locale)
				if meta.RequestID != "" {
					pr.Out.Header.Set("X-Request-ID", meta.RequestID)
				}
				if meta.Viewer.Authenticated {
					pr.Out.Header.Set("X-SkinLoom-Authenticated", "1")
					pr.Out.Header.Set("X-SkinLoom-User-ID", strconv.FormatInt(meta.Viewer.UserID, 10))
					pr.Out.Header.Set("X-SkinLoom-User-Role", meta.Viewer.Role)
					if meta.Viewer.Email != "" {
						pr.Out.Header.Set("X-SkinLoom-User-Email", meta.Viewer.Email)
					}
					if meta.Viewer.Nickname != "" {
						pr.Out.Header.Set("X-SkinLoom-User-Nickname", meta.Viewer.Nickname)
					}
				}
			}
			pr.Out.Header.Set(HeaderProxyToken, token)
		},
		ModifyResponse: func(resp *http.Response) error {
			// Prevent plugins from setting cookies, widening CORS, or overriding host security headers.
			stripPluginUnsafeHeaders(resp.Header)
			resp.Header.Set("X-Content-Type-Options", "nosniff")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if d.rt != nil && err != nil {
				d.rt.appendLog("error", "native plugin stream upstream error: "+err.Error())
			}
			d.triggerSupervisor()
			writePluginProblem(w, http.StatusBadGateway, "native plugin stream upstream unavailable")
		},
	}

	return &nativeProxyHandle{
		network:    network,
		address:    addr,
		token:      token,
		socketFile: socketFile,
		proxy:      rp,
		transport:  transport,
	}, nil
}

func (d *NativeDriver) buildRegistrations(resp *pluginv1.InitResponse) (*DriverRegistrations, error) {
	regs := &DriverRegistrations{
		HasSettingsOnChange: resp.GetListensSettingsChange(),
	}

	for _, hk := range resp.GetHooks() {
		var kind hook.Kind
		switch hk.GetKind() {
		case "guard":
			kind = hook.KindGuard
		case "filter":
			kind = hook.KindFilter
		case "event":
			kind = hook.KindEvent
		default:
			return nil, fmt.Errorf("unknown hook kind %q from native plugin", hk.GetKind())
		}
		if !d.rt.manifest.HasHook(kind, hk.GetName()) {
			return nil, fmt.Errorf("%w: hook %q (%s) is not declared in plugin.json", ErrPermissionDenied, hk.GetName(), hk.GetKind())
		}
		hid := hk.GetHandlerId()
		if hid == "" {
			hid = hk.GetKind() + ":" + hk.GetName()
		}
		regs.Hooks = append(regs.Hooks, hookBinding{
			Kind:      kind,
			Name:      hk.GetName(),
			Order:     int(hk.GetOrder()),
			HandlerID: hid,
		})
	}

	for _, rt := range resp.GetRoutes() {
		method := strings.ToUpper(strings.TrimSpace(rt.GetMethod()))
		if method == "" {
			method = "ANY"
		}
		pattern := strings.TrimSpace(rt.GetPattern())
		if !strings.HasPrefix(pattern, "/") {
			pattern = "/" + pattern
		}
		authLevel := strings.TrimSpace(rt.GetAuth())
		if authLevel == "" {
			authLevel = "public"
		}
		switch authLevel {
		case "public", "user", "admin", "superadmin":
		default:
			return nil, fmt.Errorf("invalid route auth level %q", authLevel)
		}
		if rt.GetStreamProxy() {
			if err := d.rt.perms.Check(PermHTTPStream); err != nil {
				return nil, err
			}
		}
		if rt.GetWebhookNoCsrf() && authLevel != "public" {
			return nil, fmt.Errorf("route %s %s: webhookNoCsrf is only allowed on public routes", method, pattern)
		}
		var fwdHeaders []string
		seenFwd := make(map[string]bool, len(rt.GetForwardHeaders()))
		for _, h := range rt.GetForwardHeaders() {
			lh := strings.ToLower(strings.TrimSpace(h))
			if lh == "" || lh == "cookie" || lh == "proxy-authorization" || strings.HasPrefix(lh, "x-skinloom-") || seenFwd[lh] {
				continue
			}
			seenFwd[lh] = true
			fwdHeaders = append(fwdHeaders, lh)
		}
		regs.Routes = append(regs.Routes, routeBinding{
			Method:         method,
			Pattern:        pattern,
			Auth:           authLevel,
			StreamProxy:    rt.GetStreamProxy(),
			ForwardHeaders: fwdHeaders,
			OpenCORS:       rt.GetOpenCors(),
			WebhookNoCSRF:  rt.GetWebhookNoCsrf(),
			HandlerID:      rt.GetHandlerId(),
		})
	}

	for _, cr := range resp.GetCrons() {
		if err := d.rt.perms.Check(PermCron); err != nil {
			return nil, err
		}
		sched, err := ParseCron(cr.GetExpr())
		if err != nil {
			return nil, err
		}
		hid := cr.GetHandlerId()
		if hid == "" {
			hid = cr.GetName()
		}
		regs.Crons = append(regs.Crons, cronBinding{
			Name:      cr.GetName(),
			Expr:      cr.GetExpr(),
			Schedule:  sched,
			HandlerID: hid,
		})
	}

	for _, svc := range resp.GetServices() {
		if !containsString(d.rt.manifest.Services, svc) {
			return nil, fmt.Errorf("%w: service %q is not declared in plugin.json services", ErrPermissionDenied, svc)
		}
		regs.Services = append(regs.Services, serviceBinding{Name: svc, HandlerID: svc})
	}

	for _, ap := range resp.GetAuthProviders() {
		if err := d.rt.perms.Check(PermAuthProvider); err != nil {
			return nil, err
		}
		id := strings.TrimSpace(ap.GetId())
		if id == "" {
			return nil, errors.New("auth provider id is required")
		}
		name := strings.TrimSpace(ap.GetName())
		if name == "" {
			name = id
		}
		authURL := strings.TrimSpace(ap.GetAuthUrl())
		if authURL != "" {
			if !(strings.HasPrefix(authURL, "/") && !strings.HasPrefix(authURL, "//") && !strings.HasPrefix(authURL, "/\\")) &&
				!strings.HasPrefix(authURL, "https://") && !strings.HasPrefix(authURL, "http://") {
				return nil, errors.New("auth provider authUrl must be a site path or http(s) URL")
			}
		}
		mode := strings.TrimSpace(ap.GetMode())
		if mode == "" {
			mode = "credentials"
		}
		if mode != "credentials" && mode != "redirect" {
			return nil, fmt.Errorf("invalid auth provider mode %q", mode)
		}
		regs.AuthProviders = append(regs.AuthProviders, authProviderBinding{
			Def: account.AuthProviderDef{
				ID:      id,
				Name:    name,
				Icon:    strings.TrimSpace(ap.GetIcon()),
				AuthURL: authURL,
				Mode:    mode,
			},
			VerifyFn:  -1,
			HandlerID: ap.GetHandlerId(),
		})
	}

	for _, pv := range resp.GetPasswordVerifiers() {
		if err := d.rt.perms.Check(PermAuthPassword); err != nil {
			return nil, err
		}
		hid := pv.GetHandlerId()
		if hid == "" {
			hid = pv.GetAlgo()
		}
		regs.PasswordVerifiers = append(regs.PasswordVerifiers, passwordVerifierBinding{
			Algo:      pv.GetAlgo(),
			HandlerID: hid,
		})
	}

	for _, cp := range resp.GetCaptchaProviders() {
		if err := d.rt.perms.Check(PermCaptchaProvider); err != nil {
			return nil, err
		}
		hid := cp.GetHandlerId()
		if hid == "" {
			hid = cp.GetId()
		}
		regs.CaptchaProviders = append(regs.CaptchaProviders, captchaBinding{
			ID:        cp.GetId(),
			Name:      cp.GetName(),
			HandlerID: hid,
		})
	}

	for _, c := range resp.GetCdnProviders() {
		if err := d.rt.perms.Check(PermCDNProvider); err != nil {
			return nil, err
		}
		hid := c.GetHandlerId()
		if hid == "" {
			hid = c.GetId()
		}
		regs.CDNProviders = append(regs.CDNProviders, cdnBinding{
			ID:        c.GetId(),
			Name:      c.GetName(),
			HandlerID: hid,
		})
	}

	if resp.GetStaticYggdrasilMeta() != nil {
		if err := d.rt.perms.Check(PermYggdrasilExtend); err != nil {
			return nil, err
		}
		ext := &yggdrasil.MetaExtension{
			SkinDomains: append([]string(nil), resp.GetStaticYggdrasilMeta().GetSkinDomains()...),
		}
		if len(resp.GetStaticYggdrasilMeta().GetMetaJson()) > 0 {
			_ = json.Unmarshal(resp.GetStaticYggdrasilMeta().GetMetaJson(), &ext.Meta)
		}
		regs.YggdrasilMetas = append(regs.YggdrasilMetas, yggMetaBinding{FnID: -1, Static: ext})
	}
	if resp.GetHasYggdrasilMetaProvider() {
		if err := d.rt.perms.Check(PermYggdrasilExtend); err != nil {
			return nil, err
		}
		hid := resp.GetYggdrasilMetaHandlerId()
		if hid == "" {
			hid = "yggdrasil_meta"
		}
		regs.YggdrasilMetas = append(regs.YggdrasilMetas, yggMetaBinding{FnID: -1, HandlerID: hid})
	}

	if prov := resp.GetProviders(); prov != nil {
		if prov.GetStorage() {
			if err := d.rt.perms.Check(PermProviderStorage); err != nil {
				return nil, err
			}
			regs.StorageProvider = &grpcStorageProvider{driver: d}
		}
		if prov.GetSearch() {
			if err := d.rt.perms.Check(PermProviderSearch); err != nil {
				return nil, err
			}
			regs.SearchProvider = &grpcSearchProvider{driver: d}
		}
		if prov.GetRender() {
			if err := d.rt.perms.Check(PermProviderRender); err != nil {
				return nil, err
			}
			regs.RenderProvider = &grpcRenderProvider{driver: d}
		}
	}

	return regs, nil
}

func (d *NativeDriver) buildChildEnv() []string {
	keep := map[string]bool{
		"PATH": true, "HOME": true, "USERPROFILE": true, "TMP": true, "TEMP": true, "TMPDIR": true,
		"SYSTEMROOT": true, "COMSPEC": true, "WINDIR": true, "TZ": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	}
	env := make([]string, 0, 16)
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if ok && keep[strings.ToUpper(k)] {
			env = append(env, kv)
		}
	}
	env = append(env,
		"SKINLOOM_PLUGIN_ID="+d.rt.id,
		"SKINLOOM_PLUGIN_VERSION="+d.rt.manifest.Version,
		"SKINLOOM_DATA_DIR="+d.dataDir,
	)
	env = append(env, d.opts.ExtraEnv...)
	return env
}

func (d *NativeDriver) beginCall() (*nativeProcessHandle, func(), error) {
	d.callMu.RLock()
	if d.draining {
		d.callMu.RUnlock()
		return nil, nil, errors.New("native plugin is draining")
	}
	d.inflight.Add(1)
	d.callMu.RUnlock()

	d.mu.RLock()
	h := d.handle
	running := d.running
	if h != nil && running && !h.client.Exited() {
		h.inflight.Add(1)
	} else {
		h = nil
	}
	d.mu.RUnlock()
	if h == nil {
		d.inflight.Done()
		d.triggerSupervisor()
		return nil, nil, errors.New("native plugin process is unavailable")
	}
	return h, func() {
		h.inflight.Done()
		d.inflight.Done()
	}, nil
}

func (d *NativeDriver) withCall(ctx context.Context, timeout time.Duration, fn func(context.Context, *nativeProcessHandle) error) error {
	h, done, err := d.beginCall()
	if err != nil {
		return err
	}
	defer done()

	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, timeout)
		defer cancel()
	}

	err = fn(callCtx, h)
	if err != nil {
		if isNativeTransportFailure(err) || h.client.Exited() {
			markedNow := false
			d.mu.Lock()
			if d.handle == h && d.running {
				d.running = false
				markedNow = true
			}
			d.mu.Unlock()
			if markedNow {
				d.rt.recordRuntimeFailure(err)
			}
			d.triggerSupervisor()
		}
		return unwrapGRPCError(err)
	}
	d.rt.circuit.RecordSuccess()
	return nil
}

func isNativeTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return true
	}
	return st.Code() == codes.Unavailable
}

func (d *NativeDriver) isDraining() bool {
	d.callMu.RLock()
	defer d.callMu.RUnlock()
	return d.draining
}

func (d *NativeDriver) triggerSupervisor() {
	select {
	case d.wakeSup <- struct{}{}:
	default:
	}
}

func (d *NativeDriver) supervise() {
	defer d.supWg.Done()
	ticker := time.NewTicker(d.opts.HealthInterval)
	defer ticker.Stop()

	backoff := d.opts.InitialBackoff
	for {
		select {
		case <-d.stopSup:
			return
		case <-ticker.C:
		case <-d.wakeSup:
		}
		if d.isDraining() {
			return
		}

		d.mu.RLock()
		h := d.handle
		running := d.running
		d.mu.RUnlock()

		healthy := false
		var failErr error
		alreadyMarkedDown := !running
		if h != nil && running && !h.client.Exited() {
			hcCtx, cancel := context.WithTimeout(d.supCtx, 3*time.Second)
			resp, err := h.plugin.HealthCheck(hcCtx, &pluginv1.HealthCheckRequest{})
			cancel()
			if d.isDraining() {
				return
			}
			if err == nil && resp.GetHealthy() {
				healthy = true
				backoff = d.opts.InitialBackoff
				d.mu.Lock()
				d.lastMemRSS = resp.GetMemoryRssBytes()
				d.mu.Unlock()
			} else {
				if err != nil {
					failErr = err
				} else {
					failErr = fmt.Errorf("health check unhealthy: %s", resp.GetMessage())
				}
			}
		} else {
			failErr = errors.New("native plugin subprocess exited unexpectedly")
		}

		if healthy {
			continue
		}

		d.mu.Lock()
		if d.handle == h {
			if d.running {
				d.running = false
				alreadyMarkedDown = false
			}
		}
		d.mu.Unlock()

		d.rt.appendLog("error", fmt.Sprintf("native plugin crash/unhealthy detected: %v", failErr))
		if !alreadyMarkedDown {
			d.rt.recordRuntimeFailure(failErr)
		}

		_, tripped, _, _ := d.rt.circuit.State()
		if tripped || d.isDraining() {
			return
		}

		// Exponential backoff before relaunching subprocess.
		sleepTimer := time.NewTimer(backoff)
		select {
		case <-d.stopSup:
			sleepTimer.Stop()
			return
		case <-sleepTimer.C:
		}
		if d.isDraining() {
			return
		}

		restartCtx, cancel := context.WithTimeout(d.supCtx, 15*time.Second)
		newHandle, _, restartErr := d.launchProcess(restartCtx, d.rt.currentSettingsCopy())
		cancel()
		if d.isDraining() {
			if newHandle != nil {
				newHandle.close()
			}
			return
		}
		if restartErr != nil {
			d.rt.appendLog("error", fmt.Sprintf("native plugin restart failed (next backoff %s): %v", backoff, restartErr))
			d.rt.recordRuntimeFailure(restartErr)
			_, tripped, _, _ = d.rt.circuit.State()
			if tripped || d.isDraining() {
				return
			}
			backoff = min(backoff*2, d.opts.MaxBackoff)
			d.triggerSupervisor()
			continue
		}

		d.mu.Lock()
		oldHandle := d.handle
		d.handle = newHandle
		d.running = true
		d.restarts++
		d.hasProxy = newHandle.proxy != nil
		d.mu.Unlock()
		if oldHandle != nil {
			go oldHandle.drainAndClose(defaultDrainTimeout)
		}

		backoff = d.opts.InitialBackoff
		d.rt.circuit.RecordSuccess()
		d.rt.appendLog("info", fmt.Sprintf("native plugin restarted cleanly (pid=%d, restarts=%d)", newHandle.pid, d.restarts))
	}
}

func (d *NativeDriver) Lifecycle(ctx context.Context, method string, payload any) error {
	var raw []byte
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	var resp *pluginv1.LifecycleResponse
	err := d.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.Lifecycle(callCtx, &pluginv1.LifecycleRequest{
			Event:       method,
			PayloadJson: raw,
		})
		return rpcErr
	})
	if err != nil {
		return err
	}
	if resp.GetError() != "" {
		return errors.New(resp.GetError())
	}
	return nil
}

func (d *NativeDriver) InvokeGuard(ctx context.Context, handlerID, hookName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var resp *pluginv1.InvokeGuardResponse
	err = d.withCall(ctx, defaultGuardTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.InvokeGuard(callCtx, &pluginv1.InvokeGuardRequest{
			HookName:    hookName,
			HandlerId:   handlerID,
			PayloadJson: raw,
			Locale:      requestLocaleFrom(ctx),
		})
		return rpcErr
	})
	if err != nil {
		return err
	}
	if resp.GetRejected() {
		reason := resp.GetReason()
		if reason == "" {
			reason = "rejected by plugin " + d.rt.id
		}
		return hook.Reject(reason)
	}
	return nil
}

func (d *NativeDriver) InvokeFilter(ctx context.Context, handlerID, hookName string, value, payload any) (any, error) {
	valRaw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	ctxRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var resp *pluginv1.InvokeFilterResponse
	err = d.withCall(ctx, defaultGuardTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.InvokeFilter(callCtx, &pluginv1.InvokeFilterRequest{
			HookName:    hookName,
			HandlerId:   handlerID,
			ValueJson:   valRaw,
			ContextJson: ctxRaw,
			Locale:      requestLocaleFrom(ctx),
		})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetValueJson()) == 0 {
		return value, nil
	}
	var out any
	if err := json.Unmarshal(resp.GetValueJson(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *NativeDriver) InvokeEvent(ctx context.Context, handlerID, hookName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return d.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		_, rpcErr := h.plugin.InvokeEvent(callCtx, &pluginv1.InvokeEventRequest{
			HookName:    hookName,
			HandlerId:   handlerID,
			PayloadJson: raw,
			Locale:      requestLocaleFrom(ctx),
		})
		return rpcErr
	})
}

func (d *NativeDriver) InvokeHTTP(ctx context.Context, handlerID string, req map[string]any) (any, error) {
	rpcReq := &pluginv1.InvokeHTTPRequest{
		HandlerId: handlerID,
		Method:    fmt.Sprint(req["method"]),
		Path:      fmt.Sprint(req["path"]),
		Locale:    fmt.Sprint(req["locale"]),
	}
	if p, ok := req["params"].(map[string]string); ok {
		rpcReq.Params = p
	}
	if q, ok := req["query"].(map[string]string); ok {
		rpcReq.Query = q
	}
	if h, ok := req["headers"].(map[string]string); ok {
		rpcReq.Headers = h
	}
	if rawBody, ok := req["rawBody"].(string); ok {
		rpcReq.Body = []byte(rawBody)
	}
	if u, ok := req["user"].(map[string]any); ok && u != nil {
		rpcReq.User = &pluginv1.ViewerInfo{
			Authenticated: true,
			UserId:        int64(asFloat(u["id"])),
			Email:         strVal(u["email"]),
			Nickname:      strVal(u["nickname"]),
			Role:          strVal(u["role"]),
		}
	}

	var resp *pluginv1.InvokeHTTPResponse
	err := d.withCall(ctx, defaultRouteTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.InvokeHTTP(callCtx, rpcReq)
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	if resp.GetRejected() {
		return nil, hook.Reject(resp.GetRejectReason())
	}

	statusCode := int(resp.GetStatus())
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	hdrs := make(map[string]any, len(resp.GetHeaders()))
	contentType := ""
	for k, v := range resp.GetHeaders() {
		hdrs[k] = v
		if strings.EqualFold(k, "Content-Type") {
			contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(v, ";", 2)[0]))
		}
	}
	var body any
	if raw := resp.GetBody(); len(raw) > 0 {
		isJSON := contentType == "" || contentType == "application/json" || strings.HasSuffix(contentType, "+json")
		if isJSON {
			if err := json.Unmarshal(raw, &body); err != nil {
				body = append([]byte(nil), raw...)
			}
		} else {
			body = append([]byte(nil), raw...)
		}
	}
	return map[string]any{
		"status":  statusCode,
		"headers": hdrs,
		"body":    body,
	}, nil
}

func (d *NativeDriver) InvokeCron(ctx context.Context, handlerID, jobName string) error {
	return d.withCall(ctx, defaultCronTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		_, rpcErr := h.plugin.InvokeCron(callCtx, &pluginv1.InvokeCronRequest{
			JobName:   jobName,
			HandlerId: handlerID,
		})
		return rpcErr
	})
}

func (d *NativeDriver) InvokeService(ctx context.Context, handlerID, serviceName, callerID string, args ...any) (any, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	var resp *pluginv1.InvokeServiceResponse
	err = d.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.InvokeService(callCtx, &pluginv1.InvokeServiceRequest{
			ServiceName:    serviceName,
			HandlerId:      handlerID,
			ArgsJson:       raw,
			CallerPluginId: callerID,
		})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetResultJson()) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(resp.GetResultJson(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *NativeDriver) InvokeExtension(ctx context.Context, kind, id, handlerID string, args ...any) (any, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	timeout := defaultHookTimeout
	if kind == "captcha" || kind == "yggdrasil_meta" {
		timeout = defaultGuardTimeout
	}
	var resp *pluginv1.InvokeExtensionResponse
	err = d.withCall(ctx, timeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		var rpcErr error
		resp, rpcErr = h.plugin.InvokeExtension(callCtx, &pluginv1.InvokeExtensionRequest{
			Kind:        kind,
			Id:          id,
			HandlerId:   handlerID,
			PayloadJson: raw,
		})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetResultJson()) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(resp.GetResultJson(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *NativeDriver) NotifySettings(ctx context.Context, newSettings map[string]any) error {
	raw, err := json.Marshal(newSettings)
	if err != nil {
		return err
	}
	return d.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		_, rpcErr := h.plugin.NotifySettingsChanged(callCtx, &pluginv1.NotifySettingsChangedRequest{
			SettingsJson: raw,
		})
		return rpcErr
	})
}

// ProxyHandler returns an HTTP/WebSocket streaming ReverseProxy handler backed
// by the active native plugin's local Unix socket / loopback listener, or nil
// when httpProxy is disabled.
func (d *NativeDriver) ProxyHandler() http.Handler {
	d.mu.RLock()
	has := d.hasProxy
	d.mu.RUnlock()
	if !has {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, done, err := d.beginCall()
		if err != nil {
			writePluginProblem(w, http.StatusServiceUnavailable, "native plugin stream unavailable")
			return
		}
		defer done()
		if h.proxy == nil || h.proxy.proxy == nil {
			writePluginProblem(w, http.StatusNotFound, "native plugin stream proxy disabled")
			return
		}
		h.proxy.proxy.ServeHTTP(w, r)
	})
}

func (d *NativeDriver) Stats() DriverStats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ns := &NativeStats{
		Running:        d.running,
		Restarts:       d.restarts,
		MemoryRSSBytes: d.lastMemRSS,
		HTTPProxy:      d.hasProxy,
	}
	if d.handle != nil {
		ns.PID = d.handle.pid
		ns.StartedAt = d.handle.startedAt
		if d.running && !d.handle.startedAt.IsZero() {
			ns.UptimeSeconds = int64(time.Since(d.handle.startedAt).Seconds())
		}
	}
	if d.dbConfig != nil {
		ns.DBDriver = d.dbConfig.Driver
		ns.DBPrefix = d.dbConfig.Prefix
	}
	return DriverStats{
		Runtime: RuntimeNative,
		Native:  ns,
	}
}

func (d *NativeDriver) DrainAndClose(timeout time.Duration) error {
	d.callMu.Lock()
	already := d.draining
	d.draining = true
	d.callMu.Unlock()

	d.stopOnce.Do(func() {
		if d.supCancel != nil {
			d.supCancel()
		}
		close(d.stopSup)
	})
	if already {
		return nil
	}
	d.supWg.Wait()

	// Wait for all in-flight RPCs and streaming HTTP/WS requests up to timeout.
	waitDone := make(chan struct{})
	go func() {
		d.inflight.Wait()
		close(waitDone)
	}()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	select {
	case <-waitDone:
		timer.Stop()
	case <-timer.C:
		if d.rt != nil {
			d.rt.appendLog("warn", fmt.Sprintf("native plugin drain timed out after %s; killing subprocess", timeout))
		}
	}

	d.mu.Lock()
	h := d.handle
	d.handle = nil
	d.running = false
	d.mu.Unlock()
	if h != nil {
		h.close()
	}
	return nil
}

var _ Driver = (*NativeDriver)(nil)

func randomHexString(numBytes int) string {
	b := make([]byte, numBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// lineLogWriter streams subprocess stdout/stderr lines into pluginRuntime logs.
type lineLogWriter struct {
	rt    *pluginRuntime
	level string
	mu    sync.Mutex
	buf   []byte
}

func newLineLogWriter(rt *pluginRuntime, level string) *lineLogWriter {
	return &lineLogWriter{rt: rt, level: level}
}

func (w *lineLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	var lines []string
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(bytes.TrimRight(w.buf[:idx], "\r"))
		lines = append(lines, line)
		w.buf = w.buf[idx+1:]
	}
	if len(w.buf) > 8192 {
		lines = append(lines, string(w.buf))
		w.buf = w.buf[:0]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" && w.rt != nil {
			w.rt.appendLog(w.level, line)
		}
	}
	return len(p), nil
}

// slogHCLogger bridges hashicorp/go-plugin's internal hclog into pluginRuntime logs.
type slogHCLogger struct {
	rt    *pluginRuntime
	name  string
	level atomic.Int32
}

func newSlogHCLogger(rt *pluginRuntime) hclog.Logger {
	l := &slogHCLogger{rt: rt}
	l.level.Store(int32(hclog.Info))
	return l
}

func (l *slogHCLogger) emit(lvl hclog.Level, msg string, args ...any) {
	if lvl < hclog.Level(l.level.Load()) || l.rt == nil {
		return
	}
	var sb strings.Builder
	if l.name != "" {
		sb.WriteString("[")
		sb.WriteString(l.name)
		sb.WriteString("] ")
	}
	sb.WriteString(msg)
	for i := 0; i+1 < len(args); i += 2 {
		sb.WriteString(" ")
		sb.WriteString(fmt.Sprint(args[i]))
		sb.WriteString("=")
		sb.WriteString(fmt.Sprint(args[i+1]))
	}
	switch {
	case lvl >= hclog.Error:
		l.rt.appendLog("error", sb.String())
	case lvl >= hclog.Warn:
		l.rt.appendLog("warn", sb.String())
	case lvl >= hclog.Info:
		l.rt.appendLog("info", sb.String())
	default:
		l.rt.appendLog("debug", sb.String())
	}
}

func (l *slogHCLogger) Log(level hclog.Level, msg string, args ...any) { l.emit(level, msg, args...) }
func (l *slogHCLogger) Trace(msg string, args ...any)                  { l.emit(hclog.Trace, msg, args...) }
func (l *slogHCLogger) Debug(msg string, args ...any)                  { l.emit(hclog.Debug, msg, args...) }
func (l *slogHCLogger) Info(msg string, args ...any)                   { l.emit(hclog.Info, msg, args...) }
func (l *slogHCLogger) Warn(msg string, args ...any)                   { l.emit(hclog.Warn, msg, args...) }
func (l *slogHCLogger) Error(msg string, args ...any)                  { l.emit(hclog.Error, msg, args...) }
func (l *slogHCLogger) IsTrace() bool                                  { return hclog.Level(l.level.Load()) <= hclog.Trace }
func (l *slogHCLogger) IsDebug() bool                                  { return hclog.Level(l.level.Load()) <= hclog.Debug }
func (l *slogHCLogger) IsInfo() bool                                   { return hclog.Level(l.level.Load()) <= hclog.Info }
func (l *slogHCLogger) IsWarn() bool                                   { return hclog.Level(l.level.Load()) <= hclog.Warn }
func (l *slogHCLogger) IsError() bool                                  { return hclog.Level(l.level.Load()) <= hclog.Error }
func (l *slogHCLogger) ImpliedArgs() []any                             { return nil }
func (l *slogHCLogger) With(_ ...any) hclog.Logger                     { return l }
func (l *slogHCLogger) Name() string                                   { return l.name }
func (l *slogHCLogger) Named(name string) hclog.Logger {
	next := &slogHCLogger{rt: l.rt, name: name}
	next.level.Store(l.level.Load())
	return next
}
func (l *slogHCLogger) ResetNamed(name string) hclog.Logger { return l.Named(name) }
func (l *slogHCLogger) SetLevel(level hclog.Level)          { l.level.Store(int32(level)) }
func (l *slogHCLogger) GetLevel() hclog.Level               { return hclog.Level(l.level.Load()) }
func (l *slogHCLogger) StandardLogger(*hclog.StandardLoggerOptions) *log.Logger {
	return log.New(io.Discard, "", 0)
}
func (l *slogHCLogger) StandardWriter(*hclog.StandardLoggerOptions) io.Writer {
	return io.Discard
}
