package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	"github.com/Entropy-Increase-Team/skinloom/sdk/go/plugindb"
)

// Auth levels supported by SkinLoom plugin HTTP routes.
const (
	AuthPublic     = "public"
	AuthUser       = "user"
	AuthAdmin      = "admin"
	AuthSuperadmin = "superadmin"
)

// RejectionError indicates a clean user-facing rejection from a Guard hook or HTTP handler.
type RejectionError struct {
	Reason string
}

func (e *RejectionError) Error() string { return e.Reason }

// Reject returns a user-facing rejection error recognized by Guard hooks and HTTP routes.
func Reject(reason string) error {
	return &RejectionError{Reason: reason}
}

// IsRejection unwraps err and checks whether it is a *RejectionError.
func IsRejection(err error) (*RejectionError, bool) {
	var rej *RejectionError
	if errors.As(err, &rej) {
		return rej, true
	}
	return nil, false
}

// Viewer describes the authenticated caller on a plugin HTTP or proxied stream request.
type Viewer struct {
	Authenticated bool   `json:"authenticated"`
	UserID        int64  `json:"id"`
	Email         string `json:"email,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	Role          string `json:"role,omitempty"`
}

// ProxyViewer extracts the host-verified Viewer from X-SkinLoom-* headers on a
// locally proxied streaming HTTP/WebSocket request. Supports both session users
// (UserID > 0) and bearer AdminToken superadmin callers (X-SkinLoom-Authenticated: 1, UserID == 0).
func ProxyViewer(r *http.Request) Viewer {
	if r == nil {
		return Viewer{}
	}
	authHeader := strings.TrimSpace(r.Header.Get("X-SkinLoom-Authenticated"))
	rawID := strings.TrimSpace(r.Header.Get("X-SkinLoom-User-ID"))
	if authHeader == "1" {
		uid, _ := strconv.ParseInt(rawID, 10, 64)
		return Viewer{
			Authenticated: true,
			UserID:        uid,
			Email:         r.Header.Get("X-SkinLoom-User-Email"),
			Nickname:      r.Header.Get("X-SkinLoom-User-Nickname"),
			Role:          r.Header.Get("X-SkinLoom-User-Role"),
		}
	}
	if rawID == "" {
		return Viewer{}
	}
	uid, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || uid <= 0 {
		return Viewer{}
	}
	return Viewer{
		Authenticated: true,
		UserID:        uid,
		Email:         r.Header.Get("X-SkinLoom-User-Email"),
		Nickname:      r.Header.Get("X-SkinLoom-User-Nickname"),
		Role:          r.Header.Get("X-SkinLoom-User-Role"),
	}
}

// ProxyLocale returns the request locale forwarded by SkinLoom on a proxied stream request.
func ProxyLocale(r *http.Request) string {
	if r == nil {
		return "en"
	}
	if loc := strings.TrimSpace(r.Header.Get("X-SkinLoom-Locale")); loc != "" {
		return loc
	}
	return "en"
}

type streamParamsKey struct{}

// StreamParams returns matched route parameters (e.g. ":id" or "*") for a
// streaming HTTP request dispatched through Context.HTTP.Stream.
func StreamParams(r *http.Request) map[string]string {
	if r == nil {
		return nil
	}
	if m, ok := r.Context().Value(streamParamsKey{}).(map[string]string); ok {
		return m
	}
	return nil
}

// Hook handler function signatures.
type (
	GuardFunc  func(ctx context.Context, payload json.RawMessage) error
	FilterFunc func(ctx context.Context, value json.RawMessage, payload json.RawMessage) (any, error)
	EventFunc  func(ctx context.Context, payload json.RawMessage) error
	CronFunc   func(ctx context.Context) error

	HTTPHandler func(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error)
	ServiceFunc func(ctx context.Context, callerPluginID string, args []json.RawMessage) (any, error)

	AuthVerifyFunc     func(ctx context.Context, input map[string]any) (*ExternalUserInfo, error)
	PasswordVerifyFunc func(ctx context.Context, storedHash, password string) (bool, error)
	CaptchaVerifyFunc  func(ctx context.Context, answer CaptchaAnswer) (bool, error)
	CDNPurgeFunc       func(ctx context.Context, urls []string) error
	YggdrasilMetaFunc  func(ctx context.Context) (*YggdrasilMetaExtension, error)
)

// HTTPRequest represents an incoming JSON HTTP request dispatched over gRPC.
type HTTPRequest struct {
	Method  string
	Path    string
	Params  map[string]string
	Query   map[string]string
	Headers map[string]string
	Body    []byte
	Locale  string
	User    *Viewer
}

// DecodeJSON unmarshals the request body JSON into dst.
func (r *HTTPRequest) DecodeJSON(dst any) error {
	if len(r.Body) == 0 {
		return errors.New("empty request body")
	}
	return json.Unmarshal(r.Body, dst)
}

// HTTPResponse represents a response returned from a gRPC HTTP route handler.
type HTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    any    // JSON-serialized when RawBody is empty
	RawBody []byte // raw response bytes (takes precedence over Body)
}

// JSON creates a JSON HTTPResponse.
func JSON(status int, body any) *HTTPResponse {
	return &HTTPResponse{
		Status:  status,
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
		Body:    body,
	}
}

// Text creates a plain-text HTTPResponse.
func Text(status int, text string) *HTTPResponse {
	return &HTTPResponse{
		Status:  status,
		Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"},
		RawBody: []byte(text),
	}
}

// NoContent creates a 204 No Content HTTPResponse.
func NoContent() *HTTPResponse {
	return &HTTPResponse{Status: http.StatusNoContent}
}

// Redirect creates a 3xx redirect HTTPResponse.
func Redirect(status int, location string) *HTTPResponse {
	if status < 300 || status >= 400 {
		status = http.StatusFound
	}
	return &HTTPResponse{
		Status:  status,
		Headers: map[string]string{"Location": location},
	}
}

// ExternalUserInfo is returned by an external auth provider callback.
type ExternalUserInfo struct {
	Subject       string `json:"subject"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"emailVerified,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	Next          string `json:"next,omitempty"`
}

// AuthProviderDef declares an external authentication provider.
type AuthProviderDef struct {
	ID      string
	Name    string
	Icon    string
	AuthURL string
	Mode    string // "credentials" | "redirect"
}

// CaptchaAnswer carries the captcha verification payload from the host.
type CaptchaAnswer struct {
	ID       string `json:"id"`
	Value    string `json:"value"`
	RemoteIP string `json:"remoteIp"`
}

// YggdrasilMetaExtension contributes extra skinDomains or meta fields to Yggdrasil metadata.
type YggdrasilMetaExtension struct {
	SkinDomains []string       `json:"skinDomains,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// HookOption configures a hook registration.
type HookOption func(*hookConfig)

type hookConfig struct {
	order int
}

// WithOrder sets the execution priority order for a Guard, Filter, or Event hook.
func WithOrder(order int) HookOption {
	return func(c *hookConfig) { c.order = order }
}

// RouteOption configures an HTTP or streaming route registration.
type RouteOption func(*routeConfig)

type routeConfig struct {
	auth           string
	forwardHeaders []string
	openCORS       bool
	webhookNoCSRF  bool
}

// WithAuth restricts a plugin HTTP route to "public", "user", "admin", or "superadmin".
func WithAuth(level string) RouteOption {
	return func(c *routeConfig) { c.auth = level }
}

// WithForwardHeaders whitelists specific incoming HTTP request header names that
// SkinLoom should pass through into HTTPRequest.Headers (lowercase keys).
// Sensitive host headers (Cookie, X-SkinLoom-*, and host AdminToken Authorization)
// are always stripped by the host.
func WithForwardHeaders(names ...string) RouteOption {
	return func(c *routeConfig) {
		c.forwardHeaders = append(c.forwardHeaders, names...)
	}
}

// WithOpenCORS asks the host to emit Access-Control-Allow-Origin: * (without
// credentials) and answer OPTIONS preflight requests for this route.
func WithOpenCORS() RouteOption {
	return func(c *routeConfig) { c.openCORS = true }
}

// WithWebhookNoCSRF marks a public route as an external machine webhook: the host
// exempts it from the browser cookie CSRF header check and strips Viewer before
// invoking the handler. Only valid when Auth is AuthPublic.
func WithWebhookNoCSRF() RouteOption {
	return func(c *routeConfig) { c.webhookNoCSRF = true }
}

// Context is handed to Plugin.Setup and exposes both registration APIs and
// strongly-typed runtime HostService clients plus the isolated SQL database.
type Context struct {
	pluginID   string
	version    string
	dataDir    string
	runtimeDir string
	granted    map[string]bool
	grantedRaw []string
	db         *plugindb.DB
	host       pluginv1.HostServiceClient
	srv        *Server

	settingsMu       sync.RWMutex
	settings         map[string]any
	settingsOnChange []func(ctx context.Context, settings map[string]any) error

	Hooks     *HooksAPI
	Events    *EventsAPI
	HTTP      *HTTPAPI
	Cron      *CronAPI
	Services  *ServicesAPI
	Auth      *AuthAPI
	Captcha   *CaptchaAPI
	CDN       *CDNAPI
	Yggdrasil *YggdrasilAPI
	Settings  *SettingsAPI

	Users    *UsersClient
	Players  *PlayersClient
	Textures *TexturesClient
	Closet   *ClosetClient
	Score    *ScoreClient
	Notify   *NotifyClient
	Mail     *MailClient
	KV       *KVClient
	Docs     *DocsClient
	Files    *FilesClient
	Fetch    *FetchClient
	Site     *SiteClient
	Audit    *AuditClient
	Cache    *CacheClient
	I18n     *I18nClient
	Log      *LogClient
}

func newContext(
	srv *Server,
	req *pluginv1.InitRequest,
	host pluginv1.HostServiceClient,
	db *plugindb.DB,
	initialSettings map[string]any,
) *Context {
	granted := make(map[string]bool, len(req.GetGrantedPermissions()))
	for _, p := range req.GetGrantedPermissions() {
		granted[p] = true
	}
	if initialSettings == nil {
		initialSettings = map[string]any{}
	}
	c := &Context{
		pluginID:   req.GetPluginId(),
		version:    req.GetVersion(),
		dataDir:    req.GetDataDir(),
		runtimeDir: req.GetRuntimeDir(),
		granted:    granted,
		grantedRaw: append([]string(nil), req.GetGrantedPermissions()...),
		db:         db,
		host:       host,
		srv:        srv,
		settings:   initialSettings,
	}
	c.Hooks = &HooksAPI{ctx: c}
	c.Events = &EventsAPI{ctx: c}
	c.HTTP = &HTTPAPI{ctx: c}
	c.Cron = &CronAPI{ctx: c}
	c.Services = &ServicesAPI{ctx: c}
	c.Auth = &AuthAPI{ctx: c}
	c.Captcha = &CaptchaAPI{ctx: c}
	c.CDN = &CDNAPI{ctx: c}
	c.Yggdrasil = &YggdrasilAPI{ctx: c}
	c.Settings = &SettingsAPI{ctx: c}

	c.Users = &UsersClient{host: host}
	c.Players = &PlayersClient{host: host}
	c.Textures = &TexturesClient{host: host}
	c.Closet = &ClosetClient{host: host}
	c.Score = &ScoreClient{host: host}
	c.Notify = &NotifyClient{host: host}
	c.Mail = &MailClient{host: host}
	c.KV = &KVClient{host: host}
	c.Docs = &DocsClient{host: host}
	c.Files = &FilesClient{host: host}
	c.Fetch = &FetchClient{host: host}
	c.Site = &SiteClient{host: host}
	c.Audit = &AuditClient{host: host}
	c.Cache = &CacheClient{host: host}
	c.I18n = &I18nClient{host: host}
	c.Log = &LogClient{host: host}
	return c
}

// PluginID returns this plugin's manifest identifier.
func (c *Context) PluginID() string { return c.pluginID }

// Version returns this plugin's manifest version.
func (c *Context) Version() string { return c.version }

// DataDir returns the plugin's persistent data directory on the host.
func (c *Context) DataDir() string { return c.dataDir }

// RuntimeDir returns the host's runtime directory (used for Unix sockets etc.).
func (c *Context) RuntimeDir() string { return c.runtimeDir }

// DB returns the isolated three-dialect SQL database handle, or nil when
// "storage.sql" was not requested/provisioned.
func (c *Context) DB() *plugindb.DB { return c.db }

// HasPermission reports whether perm was granted by the site administrator.
func (c *Context) HasPermission(perm string) bool { return c.granted[perm] }

// GrantedPermissions returns a copy of all granted permissions.
func (c *Context) GrantedPermissions() []string { return append([]string(nil), c.grantedRaw...) }

// --- Registration APIs ---

// HooksAPI registers Guard, Filter, and Event hooks during Setup.
type HooksAPI struct{ ctx *Context }

func (a *HooksAPI) Guard(name string, fn GuardFunc, opts ...HookOption) {
	cfg := hookConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	a.ctx.srv.addGuard(name, cfg.order, fn)
}

func (a *HooksAPI) Filter(name string, fn FilterFunc, opts ...HookOption) {
	cfg := hookConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	a.ctx.srv.addFilter(name, cfg.order, fn)
}

func (a *HooksAPI) Event(name string, fn EventFunc, opts ...HookOption) {
	cfg := hookConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	a.ctx.srv.addEvent(name, cfg.order, fn)
}

// EventsAPI subscribes to and emits plugin events.
type EventsAPI struct{ ctx *Context }

func (a *EventsAPI) On(name string, fn EventFunc, opts ...HookOption) {
	a.ctx.Hooks.Event(name, fn, opts...)
}

func (a *EventsAPI) Emit(ctx context.Context, localName string, payload any) error {
	if a.ctx.host == nil {
		return errors.New("host service unavailable")
	}
	var raw []byte
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	_, err := a.ctx.host.EmitEvent(ctx, &pluginv1.EmitEventRequest{
		LocalName:   localName,
		PayloadJson: raw,
	})
	return err
}

// HTTPAPI registers gRPC JSON HTTP routes and local streaming HTTP/WebSocket proxy routes.
type HTTPAPI struct{ ctx *Context }

func (a *HTTPAPI) Route(method, pattern string, fn HTTPHandler, opts ...RouteOption) {
	cfg := routeConfig{auth: AuthPublic}
	for _, opt := range opts {
		opt(&cfg)
	}
	a.ctx.srv.addHTTPRoute(method, pattern, cfg, fn)
}

func (a *HTTPAPI) Get(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route(http.MethodGet, pattern, fn, opts...)
}
func (a *HTTPAPI) Post(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route(http.MethodPost, pattern, fn, opts...)
}
func (a *HTTPAPI) Put(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route(http.MethodPut, pattern, fn, opts...)
}
func (a *HTTPAPI) Patch(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route(http.MethodPatch, pattern, fn, opts...)
}
func (a *HTTPAPI) Delete(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route(http.MethodDelete, pattern, fn, opts...)
}
func (a *HTTPAPI) Any(pattern string, fn HTTPHandler, opts ...RouteOption) {
	a.Route("ANY", pattern, fn, opts...)
}

// Stream registers a streaming HTTP/WebSocket route forwarded directly over the
// local Unix Socket / loopback reverse proxy (requires "http.stream" permission
// and native.httpProxy: true in plugin.json).
func (a *HTTPAPI) Stream(method, pattern string, handler http.Handler, opts ...RouteOption) {
	cfg := routeConfig{auth: AuthPublic}
	for _, opt := range opts {
		opt(&cfg)
	}
	a.ctx.srv.addStreamRoute(method, pattern, cfg, handler)
}

// StreamFunc is a convenience wrapper around Stream taking an http.HandlerFunc.
func (a *HTTPAPI) StreamFunc(method, pattern string, fn http.HandlerFunc, opts ...RouteOption) {
	a.Stream(method, pattern, fn, opts...)
}

// SetStreamHandler replaces the default internal stream router with a custom
// http.Handler (such as a chi.Router or http.ServeMux). Token authentication
// middleware is still automatically enforced before invoking handler.
func (a *HTTPAPI) SetStreamHandler(handler http.Handler) {
	a.ctx.srv.setCustomStreamHandler(handler)
}

// CronAPI registers scheduled jobs (requires "cron.schedule" permission).
type CronAPI struct{ ctx *Context }

func (a *CronAPI) Schedule(name, expr string, fn CronFunc) {
	a.ctx.srv.addCron(name, expr, fn)
}

// ServicesAPI exposes cross-plugin RPC methods and invokes other plugins' services.
type ServicesAPI struct{ ctx *Context }

func (a *ServicesAPI) Expose(name string, fn ServiceFunc) {
	a.ctx.srv.addService(name, fn)
}

func (a *ServicesAPI) Call(ctx context.Context, targetPluginID, serviceName string, args ...any) (json.RawMessage, error) {
	if a.ctx.host == nil {
		return nil, errors.New("host service unavailable")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	resp, err := a.ctx.host.CallPluginService(ctx, &pluginv1.CallPluginServiceRequest{
		TargetPluginId: targetPluginID,
		ServiceName:    serviceName,
		ArgsJson:       raw,
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.GetResultJson()), nil
}

// Discover lists active plugin IDs exposing serviceName that share a declared
// dependency relationship with this plugin (either direction).
func (a *ServicesAPI) Discover(ctx context.Context, serviceName string) ([]string, error) {
	if a.ctx.host == nil {
		return nil, errors.New("host service unavailable")
	}
	resp, err := a.ctx.host.DiscoverPluginServices(ctx, &pluginv1.DiscoverPluginServicesRequest{
		ServiceName: serviceName,
	})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), resp.GetPluginIds()...), nil
}

// AuthAPI registers external authentication providers and custom password hash verifiers.
type AuthAPI struct{ ctx *Context }

func (a *AuthAPI) RegisterProvider(def AuthProviderDef, verify AuthVerifyFunc) {
	a.ctx.srv.addAuthProvider(def, verify)
}

func (a *AuthAPI) RegisterPasswordVerifier(algo string, verify PasswordVerifyFunc) {
	a.ctx.srv.addPasswordVerifier(algo, verify)
}

// CaptchaAPI registers custom captcha providers.
type CaptchaAPI struct{ ctx *Context }

func (a *CaptchaAPI) RegisterProvider(id, name string, verify CaptchaVerifyFunc) {
	a.ctx.srv.addCaptchaProvider(id, name, verify)
}

// CDNAPI registers CDN cache purgers.
type CDNAPI struct{ ctx *Context }

func (a *CDNAPI) RegisterPurger(id, name string, purge CDNPurgeFunc) {
	a.ctx.srv.addCDNProvider(id, name, purge)
}

// YggdrasilAPI extends Yggdrasil metadata and queries Yggdrasil profiles/keys.
type YggdrasilAPI struct{ ctx *Context }

func (a *YggdrasilAPI) ExtendMeta(ext YggdrasilMetaExtension) {
	a.ctx.srv.setStaticYggdrasilMeta(ext)
}

func (a *YggdrasilAPI) RegisterMetaProvider(fn YggdrasilMetaFunc) {
	a.ctx.srv.setYggdrasilMetaProvider(fn)
}

func (a *YggdrasilAPI) PublicKey(ctx context.Context) (string, error) {
	if a.ctx == nil || a.ctx.host == nil {
		return "", errors.New("host service unavailable")
	}
	var out string
	if err := callHostOp(ctx, a.ctx.host.Yggdrasil, "publicKey", nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (a *YggdrasilAPI) LookupName(ctx context.Context, name string) (map[string]any, error) {
	if a.ctx == nil || a.ctx.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out map[string]any
	if err := callHostOp(ctx, a.ctx.host.Yggdrasil, "lookupName", []any{name}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *YggdrasilAPI) LookupProfile(ctx context.Context, uuid, baseURL string) (map[string]any, error) {
	if a.ctx == nil || a.ctx.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out map[string]any
	if err := callHostOp(ctx, a.ctx.host.Yggdrasil, "lookupProfile", []any{uuid, baseURL}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetSigningKey replaces the site's Yggdrasil RSA signing key (>= 4096 bits,
// PKCS#1 or PKCS#8 PEM). Requires the high-risk "yggdrasil.key.write" permission.
func (a *YggdrasilAPI) SetSigningKey(ctx context.Context, pemText string) error {
	if a.ctx == nil || a.ctx.host == nil {
		return errors.New("host service unavailable")
	}
	return callHostOp(ctx, a.ctx.host.Yggdrasil, "setSigningKey", []any{pemText}, nil)
}

// SettingsAPI accesses current plugin settings and subscribes to hot updates.
type SettingsAPI struct{ ctx *Context }

func (a *SettingsAPI) Get(key string) any {
	a.ctx.settingsMu.RLock()
	defer a.ctx.settingsMu.RUnlock()
	return a.ctx.settings[key]
}

func (a *SettingsAPI) GetString(key string) string {
	v := a.Get(key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func (a *SettingsAPI) GetBool(key string) bool {
	b, _ := a.Get(key).(bool)
	return b
}

func (a *SettingsAPI) GetInt(key string) int {
	switch v := a.Get(key).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func (a *SettingsAPI) All() map[string]any {
	a.ctx.settingsMu.RLock()
	defer a.ctx.settingsMu.RUnlock()
	cp := make(map[string]any, len(a.ctx.settings))
	for k, v := range a.ctx.settings {
		cp[k] = v
	}
	return cp
}

// Decode marshals the current settings map into dst (typically a typed config struct).
func (a *SettingsAPI) Decode(dst any) error {
	raw, err := json.Marshal(a.All())
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// OnChange registers a callback invoked whenever an administrator updates plugin settings.
func (a *SettingsAPI) OnChange(fn func(ctx context.Context, settings map[string]any) error) {
	a.ctx.settingsMu.Lock()
	a.ctx.settingsOnChange = append(a.ctx.settingsOnChange, fn)
	a.ctx.settingsMu.Unlock()
}

// Patch validates and persists a partial update to this plugin's own declared
// manifest settings in the host database (preserving omitted secret fields) and
// refreshes the local in-memory settings snapshot synchronously.
func (a *SettingsAPI) Patch(ctx context.Context, patch map[string]any) (map[string]any, error) {
	if a.ctx == nil || a.ctx.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var updated map[string]any
	if err := callHostOp(ctx, a.ctx.host.Settings, "patch", []any{patch}, &updated); err != nil {
		return nil, err
	}
	if updated == nil {
		updated = map[string]any{}
	}
	a.ctx.settingsMu.Lock()
	a.ctx.settings = updated
	a.ctx.settingsMu.Unlock()

	if raw, err := json.Marshal(updated); err == nil {
		var cp map[string]any
		if json.Unmarshal(raw, &cp) == nil && cp != nil {
			return cp, nil
		}
	}
	cp := make(map[string]any, len(updated))
	for k, v := range updated {
		cp[k] = v
	}
	return cp, nil
}

func (c *Context) updateSettings(ctx context.Context, raw []byte) error {
	m := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
	}
	c.settingsMu.Lock()
	c.settings = m
	cbs := append([]func(context.Context, map[string]any) error(nil), c.settingsOnChange...)
	c.settingsMu.Unlock()

	for _, cb := range cbs {
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		if err := cb(ctx, cp); err != nil {
			return err
		}
	}
	return nil
}
