// Package plugin is the official Go SDK for building Tier 2 SkinLoom native plugins.
//
// A native plugin implements the Plugin interface (Setup) and calls plugin.Serve(p)
// from main(). Optional lifecycle hooks (OnInstaller, OnEnabler, OnDisabler,
// OnUpgrader, OnUninstaller, HealthChecker) and infrastructure provider interfaces
// (StorageProvider, SearchProvider, RenderProvider) are automatically discovered
// and wired over bidirectional gRPC.
package plugin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	"github.com/Entropy-Increase-Team/skinloom/sdk/go/plugindb"
)

const (
	// ProtocolVersion is the native plugin gRPC protocol version expected by SkinLoom host.
	ProtocolVersion = 1

	// PluginName is the hashicorp/go-plugin dispense key shared with SkinLoom host.
	PluginName = "skinloom"

	// HeaderProxyToken carries the ephemeral 256-bit shared secret sent by SkinLoom's
	// local ReverseProxy on streaming HTTP/WebSocket requests.
	HeaderProxyToken = "X-SkinLoom-Proxy-Token"

	maxGRPCMsgBytes = 32 * 1024 * 1024
)

// Handshake matches SkinLoom's NativeHandshake configuration.
var Handshake = hplugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   "SKINLOOM_NATIVE_PLUGIN",
	MagicCookieValue: "skinloom-plugin-v1",
}

// Plugin is the core interface every Tier 2 native Go plugin implements.
type Plugin interface {
	Setup(ctx *Context) error
}

// Optional lifecycle interfaces.
type (
	OnInstaller interface {
		OnInstall(ctx context.Context) error
	}
	OnEnabler interface {
		OnEnable(ctx context.Context) error
	}
	OnDisabler interface {
		OnDisable(ctx context.Context) error
	}
	OnUpgrader interface {
		OnUpgrade(ctx context.Context, fromVersion string) error
	}
	OnUninstaller interface {
		OnUninstall(ctx context.Context, purge bool) error
	}
	HealthChecker interface {
		HealthCheck(ctx context.Context) error
	}
)

// StorageEntry represents one object key returned by StorageProvider.List.
type StorageEntry struct {
	Key     string
	ModTime time.Time
}

// StorageProvider allows a native plugin with "provider.storage" permission to
// supply SkinLoom's texture object storage backend.
type StorageProvider interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) (data []byte, modTime time.Time, found bool, err error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (modTime time.Time, size int64, exists bool, err error)
	List(ctx context.Context, cursor string, limit int) (entries []StorageEntry, nextCursor string, err error)
	URL(ctx context.Context, key string) (string, error)
}

// SearchDoc represents a texture document indexed by SearchProvider.
type SearchDoc struct {
	ID         int64
	Name       string
	Type       string
	Hash       string
	UploaderID int64
	Public     bool
	Likes      int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SearchQuery represents a texture search query sent to SearchProvider.
type SearchQuery struct {
	Query          string
	Category       string
	UploaderID     int64
	IncludePrivate bool
	Sort           string
	Cursor         string
	Limit          int
}

// SearchResult carries ordered texture IDs returned by SearchProvider.
type SearchResult struct {
	TextureIDs []int64
	NextCursor string
	Total      int64
}

// SearchProvider allows a native plugin with "provider.search" permission to
// supply SkinLoom's texture search engine.
type SearchProvider interface {
	Index(ctx context.Context, doc SearchDoc) error
	Delete(ctx context.Context, textureID int64) error
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
}

// RenderAvatarRequest holds avatar rendering inputs.
type RenderAvatarRequest struct {
	SkinPNG []byte
	Size    int
	Is3D    bool
	Format  string // "png" | "webp"
}

// RenderPreviewRequest holds full-body / cape preview rendering inputs.
type RenderPreviewRequest struct {
	TexturePNG  []byte
	TextureType string // "steve" | "alex" | "cape"
	Height      int
	Format      string // "png" | "webp"
}

// RenderProvider allows a native plugin with "provider.render" permission to
// supply SkinLoom's 2D/3D skin renderer.
type RenderProvider interface {
	RenderAvatar(ctx context.Context, req RenderAvatarRequest) (imageBytes []byte, format string, err error)
	RenderPreview(ctx context.Context, req RenderPreviewRequest) (imageBytes []byte, format string, err error)
}

// Serve runs p as a hashicorp/go-plugin gRPC server until the host shuts down
// the subprocess. Call this directly from main().
func Serve(p Plugin) {
	srv := NewServer(p)
	defer srv.Close()

	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]hplugin.Plugin{
			PluginName: &grpcPlugin{server: srv},
		},
		GRPCServer: func(opts []grpc.ServerOption) *grpc.Server {
			opts = append(opts,
				grpc.MaxRecvMsgSize(maxGRPCMsgBytes),
				grpc.MaxSendMsgSize(maxGRPCMsgBytes),
			)
			return grpc.NewServer(opts...)
		},
	})
}

type grpcPlugin struct {
	hplugin.NetRPCUnsupportedPlugin
	server *Server
}

func (g *grpcPlugin) GRPCServer(broker *hplugin.GRPCBroker, s *grpc.Server) error {
	g.server.SetGRPCBroker(broker)
	pluginv1.RegisterPluginServiceServer(s, g.server)
	if a := g.server.StorageAdapter(); a != nil {
		pluginv1.RegisterStorageProviderServiceServer(s, a)
	}
	if a := g.server.SearchAdapter(); a != nil {
		pluginv1.RegisterSearchProviderServiceServer(s, a)
	}
	if a := g.server.RenderAdapter(); a != nil {
		pluginv1.RegisterRenderProviderServiceServer(s, a)
	}
	return nil
}

func (g *grpcPlugin) GRPCClient(context.Context, *hplugin.GRPCBroker, *grpc.ClientConn) (any, error) {
	return nil, errors.New("sdk/go grpcPlugin is server-side only")
}

type streamRouteEntry struct {
	method  string
	pattern string
	handler http.Handler
}

// Server adapts a Go Plugin implementation onto the SkinLoom PluginService gRPC
// contract. It is used internally by Serve and directly by sdk/go/plugintest.
type Server struct {
	pluginv1.UnimplementedPluginServiceServer

	plugin    Plugin
	startedAt time.Time

	broker     *hplugin.GRPCBroker
	hostClient pluginv1.HostServiceClient // optional pre-wired client (for tests)
	hostConn   *grpc.ClientConn

	mu                   sync.RWMutex
	ctx                  *Context
	db                   *plugindb.DB
	guards               map[string]GuardFunc
	filters              map[string]FilterFunc
	events               map[string]EventFunc
	httpHandlers         map[string]HTTPHandler
	crons                map[string]CronFunc
	services             map[string]ServiceFunc
	authVerifiers        map[string]AuthVerifyFunc
	passwordVerifiers    map[string]PasswordVerifyFunc
	captchaVerifiers     map[string]CaptchaVerifyFunc
	cdnPurgers           map[string]CDNPurgeFunc
	yggMetaProvider      YggdrasilMetaFunc
	staticYggMeta        *YggdrasilMetaExtension
	hookRegs             []*pluginv1.HookRegistration
	routeRegs            []*pluginv1.RouteRegistration
	cronRegs             []*pluginv1.CronRegistration
	serviceNames         []string
	authRegs             []*pluginv1.AuthProviderRegistration
	passwordVerifierRegs []*pluginv1.PasswordVerifierRegistration
	captchaRegs          []*pluginv1.NamedProviderRegistration
	cdnRegs              []*pluginv1.NamedProviderRegistration
	streamRoutes         []streamRouteEntry
	customStreamHandler  http.Handler
	seq                  int

	proxyToken  string
	proxyLn     net.Listener
	proxyServer *http.Server
	proxySock   string
}

// NewServer creates a Server wrapping p.
func NewServer(p Plugin) *Server {
	return &Server{
		plugin:    p,
		startedAt: time.Now().UTC(),
	}
}

// SetGRPCBroker injects the hashicorp/go-plugin GRPCBroker used to dial HostService.
func (s *Server) SetGRPCBroker(b *hplugin.GRPCBroker) {
	s.broker = b
}

// SetHostClient injects a pre-built HostServiceClient (used by sdk/go/plugintest).
func (s *Server) SetHostClient(h pluginv1.HostServiceClient) {
	s.hostClient = h
}

// Context returns the initialized *Context after Init completes.
func (s *Server) Context() *Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctx
}

// StorageAdapter returns a gRPC StorageProviderServiceServer if the plugin implements StorageProvider.
func (s *Server) StorageAdapter() pluginv1.StorageProviderServiceServer {
	if sp, ok := s.plugin.(StorageProvider); ok {
		return &storageProviderAdapter{impl: sp}
	}
	return nil
}

// SearchAdapter returns a gRPC SearchProviderServiceServer if the plugin implements SearchProvider.
func (s *Server) SearchAdapter() pluginv1.SearchProviderServiceServer {
	if sp, ok := s.plugin.(SearchProvider); ok {
		return &searchProviderAdapter{impl: sp}
	}
	return nil
}

// RenderAdapter returns a gRPC RenderProviderServiceServer if the plugin implements RenderProvider.
func (s *Server) RenderAdapter() pluginv1.RenderProviderServiceServer {
	if rp, ok := s.plugin.(RenderProvider); ok {
		return &renderProviderAdapter{impl: rp}
	}
	return nil
}

// Close stops any local HTTP/WS stream proxy server, closes the isolated DB,
// and closes the broker connection.
func (s *Server) Close() {
	s.mu.Lock()
	ps := s.proxyServer
	ln := s.proxyLn
	sock := s.proxySock
	db := s.db
	hc := s.hostConn
	s.proxyServer = nil
	s.proxyLn = nil
	s.proxySock = ""
	s.db = nil
	s.hostConn = nil
	s.mu.Unlock()

	if ps != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = ps.Shutdown(ctx)
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	if sock != "" {
		_ = os.Remove(sock)
	}
	if db != nil {
		_ = db.Close()
	}
	if hc != nil {
		_ = hc.Close()
	}
}

func (s *Server) resetRegistrations() {
	s.guards = map[string]GuardFunc{}
	s.filters = map[string]FilterFunc{}
	s.events = map[string]EventFunc{}
	s.httpHandlers = map[string]HTTPHandler{}
	s.crons = map[string]CronFunc{}
	s.services = map[string]ServiceFunc{}
	s.authVerifiers = map[string]AuthVerifyFunc{}
	s.passwordVerifiers = map[string]PasswordVerifyFunc{}
	s.captchaVerifiers = map[string]CaptchaVerifyFunc{}
	s.cdnPurgers = map[string]CDNPurgeFunc{}
	s.yggMetaProvider = nil
	s.staticYggMeta = nil
	s.hookRegs = nil
	s.routeRegs = nil
	s.cronRegs = nil
	s.serviceNames = nil
	s.authRegs = nil
	s.passwordVerifierRegs = nil
	s.captchaRegs = nil
	s.cdnRegs = nil
	s.streamRoutes = nil
	s.customStreamHandler = nil
	s.seq = 0
}

func (s *Server) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s_%d", prefix, s.seq)
}

func (s *Server) addGuard(name string, order int, fn GuardFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("guard")
	s.guards[id] = fn
	s.hookRegs = append(s.hookRegs, &pluginv1.HookRegistration{
		Kind:      "guard",
		Name:      name,
		Order:     int32(order),
		HandlerId: id,
	})
}

func (s *Server) addFilter(name string, order int, fn FilterFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("filter")
	s.filters[id] = fn
	s.hookRegs = append(s.hookRegs, &pluginv1.HookRegistration{
		Kind:      "filter",
		Name:      name,
		Order:     int32(order),
		HandlerId: id,
	})
}

func (s *Server) addEvent(name string, order int, fn EventFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("event")
	s.events[id] = fn
	s.hookRegs = append(s.hookRegs, &pluginv1.HookRegistration{
		Kind:      "event",
		Name:      name,
		Order:     int32(order),
		HandlerId: id,
	})
}

func (s *Server) addHTTPRoute(method, pattern string, cfg routeConfig, fn HTTPHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("http")
	s.httpHandlers[id] = fn
	s.routeRegs = append(s.routeRegs, &pluginv1.RouteRegistration{
		Method:         strings.ToUpper(strings.TrimSpace(method)),
		Pattern:        normalizePattern(pattern),
		Auth:           cfg.auth,
		HandlerId:      id,
		ForwardHeaders: append([]string(nil), cfg.forwardHeaders...),
		OpenCors:       cfg.openCORS,
		WebhookNoCsrf:  cfg.webhookNoCSRF,
	})
}

func (s *Server) addStreamRoute(method, pattern string, cfg routeConfig, handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normMethod := strings.ToUpper(strings.TrimSpace(method))
	if normMethod == "" {
		normMethod = "ANY"
	}
	normPattern := normalizePattern(pattern)
	s.routeRegs = append(s.routeRegs, &pluginv1.RouteRegistration{
		Method:         normMethod,
		Pattern:        normPattern,
		Auth:           cfg.auth,
		StreamProxy:    true,
		ForwardHeaders: append([]string(nil), cfg.forwardHeaders...),
		OpenCors:       cfg.openCORS,
		WebhookNoCsrf:  cfg.webhookNoCSRF,
	})
	if handler != nil {
		s.streamRoutes = append(s.streamRoutes, streamRouteEntry{
			method:  normMethod,
			pattern: normPattern,
			handler: handler,
		})
	}
}

func (s *Server) setCustomStreamHandler(h http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customStreamHandler = h
}

func (s *Server) addCron(name, expr string, fn CronFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID("cron")
	s.crons[id] = fn
	s.cronRegs = append(s.cronRegs, &pluginv1.CronRegistration{
		Name:      name,
		Expr:      expr,
		HandlerId: id,
	})
}

func (s *Server) addService(name string, fn ServiceFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.services[name]; !exists {
		s.serviceNames = append(s.serviceNames, name)
	}
	s.services[name] = fn
}

func (s *Server) addAuthProvider(def AuthProviderDef, verify AuthVerifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var hid string
	if verify != nil {
		hid = s.nextID("auth")
		s.authVerifiers[hid] = verify
	}
	mode := def.Mode
	if mode == "" {
		if verify != nil {
			mode = "credentials"
		} else {
			mode = "redirect"
		}
	}
	s.authRegs = append(s.authRegs, &pluginv1.AuthProviderRegistration{
		Id:        def.ID,
		Name:      def.Name,
		Icon:      def.Icon,
		AuthUrl:   def.AuthURL,
		Mode:      mode,
		HandlerId: hid,
	})
}

func (s *Server) addPasswordVerifier(algo string, verify PasswordVerifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hid := s.nextID("pw")
	s.passwordVerifiers[hid] = verify
	s.passwordVerifierRegs = append(s.passwordVerifierRegs, &pluginv1.PasswordVerifierRegistration{
		Algo:      algo,
		HandlerId: hid,
	})
}

func (s *Server) addCaptchaProvider(id, name string, verify CaptchaVerifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hid := s.nextID("captcha")
	s.captchaVerifiers[hid] = verify
	s.captchaRegs = append(s.captchaRegs, &pluginv1.NamedProviderRegistration{
		Id:        id,
		Name:      name,
		HandlerId: hid,
	})
}

func (s *Server) addCDNProvider(id, name string, purge CDNPurgeFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hid := s.nextID("cdn")
	s.cdnPurgers[hid] = purge
	s.cdnRegs = append(s.cdnRegs, &pluginv1.NamedProviderRegistration{
		Id:        id,
		Name:      name,
		HandlerId: hid,
	})
}

func (s *Server) setStaticYggdrasilMeta(ext YggdrasilMetaExtension) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := ext
	s.staticYggMeta = &cp
}

func (s *Server) setYggdrasilMetaProvider(fn YggdrasilMetaFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.yggMetaProvider = fn
}

// Init implements pluginv1.PluginServiceServer.
func (s *Server) Init(_ context.Context, req *pluginv1.InitRequest) (*pluginv1.InitResponse, error) {
	if s.plugin == nil {
		return nil, status.Error(codes.FailedPrecondition, "nil plugin implementation")
	}

	s.mu.Lock()
	prevConn := s.hostConn
	prevDB := s.db
	prevSrv := s.proxyServer
	prevLn := s.proxyLn
	prevSock := s.proxySock
	s.hostConn = nil
	s.db = nil
	s.proxyServer = nil
	s.proxyLn = nil
	s.proxySock = ""
	s.proxyToken = ""
	s.resetRegistrations()
	s.mu.Unlock()
	if prevSrv != nil {
		_ = prevSrv.Close()
	}
	if prevLn != nil {
		_ = prevLn.Close()
	}
	if prevSock != "" {
		_ = os.Remove(prevSock)
	}
	if prevDB != nil {
		_ = prevDB.Close()
	}
	if prevConn != nil {
		_ = prevConn.Close()
	}

	var openedConn *grpc.ClientConn
	var pdb *plugindb.DB
	cleanupOnErr := func() {
		if pdb != nil {
			_ = pdb.Close()
		}
		if openedConn != nil {
			_ = openedConn.Close()
		}
		s.mu.Lock()
		if s.db == pdb {
			s.db = nil
		}
		if s.hostConn == openedConn {
			s.hostConn = nil
		}
		s.mu.Unlock()
	}

	host := s.hostClient
	if host == nil && s.broker != nil && req.GetHostBrokerId() != 0 {
		conn, err := s.broker.DialWithOptions(
			req.GetHostBrokerId(),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxGRPCMsgBytes),
				grpc.MaxCallSendMsgSize(maxGRPCMsgBytes),
			),
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "dial host broker: %v", err)
		}
		openedConn = conn
		s.mu.Lock()
		s.hostConn = conn
		s.mu.Unlock()
		host = pluginv1.NewHostServiceClient(conn)
	}

	if dbc := req.GetDatabase(); dbc != nil && dbc.GetDsn() != "" {
		opened, err := plugindb.Open(plugindb.Config{
			Driver: dbc.GetDriver(),
			DSN:    dbc.GetDsn(),
			Prefix: dbc.GetPrefix(),
			Schema: dbc.GetSchema(),
		})
		if err != nil {
			cleanupOnErr()
			return nil, status.Errorf(codes.Internal, "open isolated plugin db: %v", err)
		}
		pdb = opened
	}

	settingsMap := map[string]any{}
	if len(req.GetSettingsJson()) > 0 {
		_ = json.Unmarshal(req.GetSettingsJson(), &settingsMap)
	}

	pctx := newContext(s, req, host, pdb, settingsMap)
	s.mu.Lock()
	s.ctx = pctx
	s.db = pdb
	s.mu.Unlock()

	if err := s.plugin.Setup(pctx); err != nil {
		cleanupOnErr()
		return nil, status.Errorf(codes.Unknown, "plugin Setup failed: %v", err)
	}

	s.mu.RLock()
	resp := &pluginv1.InitResponse{
		Hooks:             append([]*pluginv1.HookRegistration(nil), s.hookRegs...),
		Routes:            append([]*pluginv1.RouteRegistration(nil), s.routeRegs...),
		Crons:             append([]*pluginv1.CronRegistration(nil), s.cronRegs...),
		Services:          append([]string(nil), s.serviceNames...),
		AuthProviders:     append([]*pluginv1.AuthProviderRegistration(nil), s.authRegs...),
		PasswordVerifiers: append([]*pluginv1.PasswordVerifierRegistration(nil), s.passwordVerifierRegs...),
		CaptchaProviders:  append([]*pluginv1.NamedProviderRegistration(nil), s.captchaRegs...),
		CdnProviders:      append([]*pluginv1.NamedProviderRegistration(nil), s.cdnRegs...),
	}
	if s.yggMetaProvider != nil {
		resp.HasYggdrasilMetaProvider = true
		resp.YggdrasilMetaHandlerId = "yggdrasil_meta"
	}
	if s.staticYggMeta != nil {
		metaBytes, _ := json.Marshal(s.staticYggMeta.Meta)
		resp.StaticYggdrasilMeta = &pluginv1.YggdrasilMetaExtension{
			SkinDomains: append([]string(nil), s.staticYggMeta.SkinDomains...),
			MetaJson:    metaBytes,
		}
	}
	hasStream := len(s.streamRoutes) > 0 || s.customStreamHandler != nil
	s.mu.RUnlock()

	// Always subscribe to host settings pushes so pctx.Settings.Get() stays fresh
	// even when the plugin does not register an explicit Settings.OnChange callback.
	resp.ListensSettingsChange = true

	_, hasStorage := s.plugin.(StorageProvider)
	_, hasSearch := s.plugin.(SearchProvider)
	_, hasRender := s.plugin.(RenderProvider)
	if hasStorage || hasSearch || hasRender {
		resp.Providers = &pluginv1.ProviderCapabilities{
			Storage: hasStorage,
			Search:  hasSearch,
			Render:  hasRender,
		}
	}

	if hasStream {
		proxyInfo, err := s.startStreamProxy(req.GetProxySocketPath(), req.GetProxyToken())
		if err != nil {
			cleanupOnErr()
			return nil, status.Errorf(codes.Internal, "start stream proxy: %v", err)
		}
		resp.HttpProxy = proxyInfo
	}

	return resp, nil
}

// StreamHTTPHandler returns the authenticated local HTTP handler serving stream
// routes (also usable directly by sdk/go/plugintest without binding a socket).
func (s *Server) StreamHTTPHandler() http.Handler {
	return http.HandlerFunc(s.serveStreamHTTP)
}

func (s *Server) startStreamProxy(socketPath, token string) (*pluginv1.HTTPProxyInfo, error) {
	if len(strings.TrimSpace(token)) < 32 {
		return nil, errors.New("host did not provide a valid proxy token")
	}

	var ln net.Listener
	var network, addr, sockFile string
	var err error
	if socketPath != "" && runtime.GOOS != "windows" {
		_ = os.Remove(socketPath)
		ln, err = net.Listen("unix", socketPath)
		if err == nil {
			_ = os.Chmod(socketPath, 0o600)
			network = "unix"
			addr = socketPath
			sockFile = socketPath
		}
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			network = "tcp"
			addr = ln.Addr().String()
		}
	}
	if err != nil {
		return nil, err
	}

	httpSrv := &http.Server{
		Handler:           s.StreamHTTPHandler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	s.mu.Lock()
	s.proxyToken = token
	s.proxyLn = ln
	s.proxyServer = httpSrv
	s.proxySock = sockFile
	s.mu.Unlock()

	go func() {
		_ = httpSrv.Serve(ln)
	}()

	return &pluginv1.HTTPProxyInfo{
		Enabled: true,
		Network: network,
		Address: addr,
		Token:   token,
	}, nil
}

func (s *Server) serveStreamHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	expected := s.proxyToken
	custom := s.customStreamHandler
	routes := append([]streamRouteEntry(nil), s.streamRoutes...)
	s.mu.RUnlock()

	if expected != "" {
		got := r.Header.Get(HeaderProxyToken)
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			http.Error(w, "invalid proxy token", http.StatusForbidden)
			return
		}
	}

	if custom != nil {
		custom.ServeHTTP(w, r)
		return
	}

	subpath := normalizePattern(r.URL.Path)
	actualSegs := splitSegments(subpath)
	pathMatched := false
	for _, rt := range routes {
		params, ok := matchSegments(splitSegments(rt.pattern), actualSegs)
		if !ok {
			continue
		}
		pathMatched = true
		if rt.method == "*" || strings.EqualFold(rt.method, "ANY") || strings.EqualFold(rt.method, r.Method) {
			reqCtx := context.WithValue(r.Context(), streamParamsKey{}, params)
			rt.handler.ServeHTTP(w, r.WithContext(reqCtx))
			return
		}
	}
	if pathMatched {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.NotFound(w, r)
}

// Lifecycle implements pluginv1.PluginServiceServer.
func (s *Server) Lifecycle(ctx context.Context, req *pluginv1.LifecycleRequest) (*pluginv1.LifecycleResponse, error) {
	var err error
	switch req.GetEvent() {
	case "onInstall":
		if p, ok := s.plugin.(OnInstaller); ok {
			err = p.OnInstall(ctx)
		}
	case "onEnable":
		if p, ok := s.plugin.(OnEnabler); ok {
			err = p.OnEnable(ctx)
		}
	case "onDisable":
		if p, ok := s.plugin.(OnDisabler); ok {
			err = p.OnDisable(ctx)
		}
	case "onUpgrade":
		if p, ok := s.plugin.(OnUpgrader); ok {
			var payload struct {
				From string `json:"from"`
			}
			_ = json.Unmarshal(req.GetPayloadJson(), &payload)
			err = p.OnUpgrade(ctx, payload.From)
		}
	case "onUninstall":
		if p, ok := s.plugin.(OnUninstaller); ok {
			var payload struct {
				Purge bool `json:"purge"`
			}
			_ = json.Unmarshal(req.GetPayloadJson(), &payload)
			err = p.OnUninstall(ctx, payload.Purge)
		}
	}
	if err != nil {
		return &pluginv1.LifecycleResponse{Error: err.Error()}, nil
	}
	return &pluginv1.LifecycleResponse{}, nil
}

// InvokeGuard implements pluginv1.PluginServiceServer.
func (s *Server) InvokeGuard(ctx context.Context, req *pluginv1.InvokeGuardRequest) (*pluginv1.InvokeGuardResponse, error) {
	s.mu.RLock()
	fn := s.guards[req.GetHandlerId()]
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "guard handler %q not found", req.GetHandlerId())
	}
	if err := fn(ctx, json.RawMessage(req.GetPayloadJson())); err != nil {
		if rej, ok := IsRejection(err); ok {
			return &pluginv1.InvokeGuardResponse{Rejected: true, Reason: rej.Reason}, nil
		}
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.InvokeGuardResponse{}, nil
}

// InvokeFilter implements pluginv1.PluginServiceServer.
func (s *Server) InvokeFilter(ctx context.Context, req *pluginv1.InvokeFilterRequest) (*pluginv1.InvokeFilterResponse, error) {
	s.mu.RLock()
	fn := s.filters[req.GetHandlerId()]
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "filter handler %q not found", req.GetHandlerId())
	}
	out, err := fn(ctx, json.RawMessage(req.GetValueJson()), json.RawMessage(req.GetContextJson()))
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if out == nil {
		return &pluginv1.InvokeFilterResponse{ValueJson: req.GetValueJson()}, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "marshal filter result: %v", err)
	}
	return &pluginv1.InvokeFilterResponse{ValueJson: raw}, nil
}

// InvokeEvent implements pluginv1.PluginServiceServer.
func (s *Server) InvokeEvent(ctx context.Context, req *pluginv1.InvokeEventRequest) (*pluginv1.InvokeEventResponse, error) {
	s.mu.RLock()
	fn := s.events[req.GetHandlerId()]
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "event handler %q not found", req.GetHandlerId())
	}
	if err := fn(ctx, json.RawMessage(req.GetPayloadJson())); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.InvokeEventResponse{}, nil
}

// InvokeHTTP implements pluginv1.PluginServiceServer.
func (s *Server) InvokeHTTP(ctx context.Context, req *pluginv1.InvokeHTTPRequest) (*pluginv1.InvokeHTTPResponse, error) {
	s.mu.RLock()
	fn := s.httpHandlers[req.GetHandlerId()]
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "http handler %q not found", req.GetHandlerId())
	}
	hreq := &HTTPRequest{
		Method:  req.GetMethod(),
		Path:    req.GetPath(),
		Params:  req.GetParams(),
		Query:   req.GetQuery(),
		Headers: req.GetHeaders(),
		Body:    req.GetBody(),
		Locale:  req.GetLocale(),
	}
	if u := req.GetUser(); u != nil && u.GetAuthenticated() {
		hreq.User = &Viewer{
			Authenticated: true,
			UserID:        u.GetUserId(),
			Email:         u.GetEmail(),
			Nickname:      u.GetNickname(),
			Role:          u.GetRole(),
		}
	}
	resp, err := fn(ctx, hreq)
	if err != nil {
		if rej, ok := IsRejection(err); ok {
			return &pluginv1.InvokeHTTPResponse{Rejected: true, RejectReason: rej.Reason}, nil
		}
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if resp == nil {
		return &pluginv1.InvokeHTTPResponse{Status: http.StatusNoContent}, nil
	}
	st := resp.Status
	if st == 0 {
		st = http.StatusOK
	}
	bodyBytes := resp.RawBody
	if len(bodyBytes) == 0 && resp.Body != nil {
		raw, mErr := json.Marshal(resp.Body)
		if mErr != nil {
			return nil, status.Errorf(codes.Unknown, "marshal http response: %v", mErr)
		}
		bodyBytes = raw
	}
	return &pluginv1.InvokeHTTPResponse{
		Status:  int32(st),
		Headers: resp.Headers,
		Body:    bodyBytes,
	}, nil
}

// InvokeCron implements pluginv1.PluginServiceServer.
func (s *Server) InvokeCron(ctx context.Context, req *pluginv1.InvokeCronRequest) (*pluginv1.InvokeCronResponse, error) {
	s.mu.RLock()
	fn := s.crons[req.GetHandlerId()]
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "cron handler %q not found", req.GetHandlerId())
	}
	if err := fn(ctx); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.InvokeCronResponse{}, nil
}

// InvokeService implements pluginv1.PluginServiceServer.
func (s *Server) InvokeService(ctx context.Context, req *pluginv1.InvokeServiceRequest) (*pluginv1.InvokeServiceResponse, error) {
	s.mu.RLock()
	fn := s.services[req.GetServiceName()]
	if fn == nil {
		fn = s.services[req.GetHandlerId()]
	}
	s.mu.RUnlock()
	if fn == nil {
		return nil, status.Errorf(codes.NotFound, "service %q not found", req.GetServiceName())
	}
	var args []json.RawMessage
	if len(req.GetArgsJson()) > 0 {
		if err := json.Unmarshal(req.GetArgsJson(), &args); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid service args: %v", err)
		}
	}
	out, err := fn(ctx, req.GetCallerPluginId(), args)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if out == nil {
		return &pluginv1.InvokeServiceResponse{}, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "marshal service result: %v", err)
	}
	return &pluginv1.InvokeServiceResponse{ResultJson: raw}, nil
}

// InvokeExtension implements pluginv1.PluginServiceServer.
func (s *Server) InvokeExtension(ctx context.Context, req *pluginv1.InvokeExtensionRequest) (*pluginv1.InvokeExtensionResponse, error) {
	var args []json.RawMessage
	if len(req.GetPayloadJson()) > 0 {
		if err := json.Unmarshal(req.GetPayloadJson(), &args); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid extension payload: %v", err)
		}
	}
	var out any
	var err error

	s.mu.RLock()
	switch req.GetKind() {
	case "auth":
		fn := s.authVerifiers[req.GetHandlerId()]
		s.mu.RUnlock()
		if fn == nil {
			return nil, status.Errorf(codes.NotFound, "auth handler %q not found", req.GetHandlerId())
		}
		var input map[string]any
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &input)
		}
		out, err = fn(ctx, input)
	case "password_verifier":
		fn := s.passwordVerifiers[req.GetHandlerId()]
		s.mu.RUnlock()
		if fn == nil {
			return nil, status.Errorf(codes.NotFound, "password verifier %q not found", req.GetHandlerId())
		}
		var storedHash, pw string
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &storedHash)
		}
		if len(args) > 1 {
			_ = json.Unmarshal(args[1], &pw)
		}
		out, err = fn(ctx, storedHash, pw)
	case "captcha":
		fn := s.captchaVerifiers[req.GetHandlerId()]
		s.mu.RUnlock()
		if fn == nil {
			return nil, status.Errorf(codes.NotFound, "captcha handler %q not found", req.GetHandlerId())
		}
		var ans CaptchaAnswer
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &ans)
		}
		out, err = fn(ctx, ans)
	case "cdn":
		fn := s.cdnPurgers[req.GetHandlerId()]
		s.mu.RUnlock()
		if fn == nil {
			return nil, status.Errorf(codes.NotFound, "cdn purger %q not found", req.GetHandlerId())
		}
		var urls []string
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &urls)
		}
		err = fn(ctx, urls)
	case "yggdrasil_meta":
		fn := s.yggMetaProvider
		s.mu.RUnlock()
		if fn == nil {
			return nil, status.Error(codes.NotFound, "yggdrasil meta provider not registered")
		}
		out, err = fn(ctx)
	default:
		s.mu.RUnlock()
		return nil, status.Errorf(codes.InvalidArgument, "unknown extension kind %q", req.GetKind())
	}

	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if out == nil {
		return &pluginv1.InvokeExtensionResponse{}, nil
	}
	raw, mErr := json.Marshal(out)
	if mErr != nil {
		return nil, status.Errorf(codes.Unknown, "marshal extension result: %v", mErr)
	}
	return &pluginv1.InvokeExtensionResponse{ResultJson: raw}, nil
}

// NotifySettingsChanged implements pluginv1.PluginServiceServer.
func (s *Server) NotifySettingsChanged(ctx context.Context, req *pluginv1.NotifySettingsChangedRequest) (*pluginv1.NotifySettingsChangedResponse, error) {
	s.mu.RLock()
	pctx := s.ctx
	s.mu.RUnlock()
	if pctx != nil {
		if err := pctx.updateSettings(ctx, req.GetSettingsJson()); err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}
	}
	return &pluginv1.NotifySettingsChangedResponse{}, nil
}

// HealthCheck implements pluginv1.PluginServiceServer.
func (s *Server) HealthCheck(ctx context.Context, _ *pluginv1.HealthCheckRequest) (*pluginv1.HealthCheckResponse, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	uptime := int64(time.Since(s.startedAt).Seconds())
	if hc, ok := s.plugin.(HealthChecker); ok {
		if err := hc.HealthCheck(ctx); err != nil {
			return &pluginv1.HealthCheckResponse{
				Healthy:        false,
				Message:        err.Error(),
				MemoryRssBytes: ms.Sys,
				UptimeSeconds:  uptime,
			}, nil
		}
	}
	return &pluginv1.HealthCheckResponse{
		Healthy:        true,
		Message:        "ok",
		MemoryRssBytes: ms.Sys,
		UptimeSeconds:  uptime,
	}, nil
}

// --- Separate Infrastructure Provider gRPC Adapters (avoids Delete name clash) ---

type storageProviderAdapter struct {
	pluginv1.UnimplementedStorageProviderServiceServer
	impl StorageProvider
}

func (a *storageProviderAdapter) Put(ctx context.Context, req *pluginv1.StoragePutRequest) (*pluginv1.Empty, error) {
	if err := a.impl.Put(ctx, req.GetKey(), req.GetData()); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.Empty{}, nil
}

func (a *storageProviderAdapter) Get(ctx context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageGetResponse, error) {
	data, mt, found, err := a.impl.Get(ctx, req.GetKey())
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if !found {
		return &pluginv1.StorageGetResponse{Found: false}, nil
	}
	return &pluginv1.StorageGetResponse{
		Found:           true,
		Data:            data,
		ModTimeUnixNano: mt.UTC().UnixNano(),
	}, nil
}

func (a *storageProviderAdapter) Delete(ctx context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.Empty, error) {
	if err := a.impl.Delete(ctx, req.GetKey()); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.Empty{}, nil
}

func (a *storageProviderAdapter) Stat(ctx context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageStatResponse, error) {
	mt, size, exists, err := a.impl.Stat(ctx, req.GetKey())
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if !exists {
		return &pluginv1.StorageStatResponse{Exists: false}, nil
	}
	return &pluginv1.StorageStatResponse{
		Exists:          true,
		ModTimeUnixNano: mt.UTC().UnixNano(),
		Size:            size,
	}, nil
}

func (a *storageProviderAdapter) List(ctx context.Context, req *pluginv1.StorageListRequest) (*pluginv1.StorageListResponse, error) {
	entries, next, err := a.impl.List(ctx, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	out := make([]*pluginv1.StorageEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &pluginv1.StorageEntry{
			Key:             e.Key,
			ModTimeUnixNano: e.ModTime.UTC().UnixNano(),
		})
	}
	return &pluginv1.StorageListResponse{Entries: out, NextCursor: next}, nil
}

func (a *storageProviderAdapter) URL(ctx context.Context, req *pluginv1.StorageKeyRequest) (*pluginv1.StorageURLResponse, error) {
	u, err := a.impl.URL(ctx, req.GetKey())
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.StorageURLResponse{Url: u}, nil
}

type searchProviderAdapter struct {
	pluginv1.UnimplementedSearchProviderServiceServer
	impl SearchProvider
}

func (a *searchProviderAdapter) Index(ctx context.Context, req *pluginv1.SearchIndexRequest) (*pluginv1.Empty, error) {
	d := req.GetDoc()
	if d == nil {
		return nil, status.Error(codes.InvalidArgument, "missing search doc")
	}
	err := a.impl.Index(ctx, SearchDoc{
		ID:         d.GetId(),
		Name:       d.GetName(),
		Type:       d.GetType(),
		Hash:       d.GetHash(),
		UploaderID: d.GetUploaderId(),
		Public:     d.GetPublic(),
		Likes:      int(d.GetLikes()),
		CreatedAt:  time.Unix(d.GetCreatedAtUnix(), 0).UTC(),
		UpdatedAt:  time.Unix(d.GetUpdatedAtUnix(), 0).UTC(),
	})
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.Empty{}, nil
}

func (a *searchProviderAdapter) Delete(ctx context.Context, req *pluginv1.SearchDeleteRequest) (*pluginv1.Empty, error) {
	if err := a.impl.Delete(ctx, req.GetTextureId()); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.Empty{}, nil
}

func (a *searchProviderAdapter) Search(ctx context.Context, req *pluginv1.SearchQueryRequest) (*pluginv1.SearchQueryResponse, error) {
	res, err := a.impl.Search(ctx, SearchQuery{
		Query:          req.GetQuery(),
		Category:       req.GetCategory(),
		UploaderID:     req.GetUploaderId(),
		IncludePrivate: req.GetIncludePrivate(),
		Sort:           req.GetSort(),
		Cursor:         req.GetCursor(),
		Limit:          int(req.GetLimit()),
	})
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &pluginv1.SearchQueryResponse{
		TextureIds: res.TextureIDs,
		NextCursor: res.NextCursor,
		Total:      res.Total,
	}, nil
}

type renderProviderAdapter struct {
	pluginv1.UnimplementedRenderProviderServiceServer
	impl RenderProvider
}

func (a *renderProviderAdapter) RenderAvatar(ctx context.Context, req *pluginv1.RenderAvatarRequest) (*pluginv1.RenderImageResponse, error) {
	img, format, err := a.impl.RenderAvatar(ctx, RenderAvatarRequest{
		SkinPNG: req.GetSkinPng(),
		Size:    int(req.GetSize()),
		Is3D:    req.GetIs_3D(),
		Format:  req.GetFormat(),
	})
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if format == "" {
		format = req.GetFormat()
	}
	return &pluginv1.RenderImageResponse{ImageBytes: img, Format: format}, nil
}

func (a *renderProviderAdapter) RenderPreview(ctx context.Context, req *pluginv1.RenderPreviewRequest) (*pluginv1.RenderImageResponse, error) {
	img, format, err := a.impl.RenderPreview(ctx, RenderPreviewRequest{
		TexturePNG:  req.GetTexturePng(),
		TextureType: req.GetTextureType(),
		Height:      int(req.GetHeight()),
		Format:      req.GetFormat(),
	})
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if format == "" {
		format = req.GetFormat()
	}
	return &pluginv1.RenderImageResponse{ImageBytes: img, Format: format}, nil
}

// --- Route pattern helpers (matching host internal/plugin/manager.go) ---

func normalizePattern(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func splitSegments(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func matchSegments(pat, actual []string) (map[string]string, bool) {
	if len(pat) == 1 && pat[0] == "*" {
		return map[string]string{"*": strings.Join(actual, "/")}, true
	}
	if len(pat) > 1 && pat[len(pat)-1] == "*" {
		prefix := pat[:len(pat)-1]
		if len(actual) < len(prefix) {
			return nil, false
		}
		params, ok := matchSegments(prefix, actual[:len(prefix)])
		if !ok {
			return nil, false
		}
		params["*"] = strings.Join(actual[len(prefix):], "/")
		return params, true
	}
	if len(pat) != len(actual) {
		return nil, false
	}
	params := map[string]string{}
	for i := range pat {
		seg := pat[i]
		switch {
		case strings.HasPrefix(seg, ":"):
			params[seg[1:]] = actual[i]
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") && len(seg) > 2:
			params[seg[1:len(seg)-1]] = actual[i]
		case seg == actual[i]:
		default:
			return nil, false
		}
	}
	return params, true
}
