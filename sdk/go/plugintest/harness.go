// Package plugintest provides an in-memory mock SkinLoom host and isolated
// SQLite database harness so Tier 2 native Go plugins can be unit-tested with
// standard `go test` without launching subprocesses.
package plugintest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
	"github.com/Entropy-Increase-Team/skinloom/sdk/go/plugindb"
)

const testProxyToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// Option customizes a plugintest Harness before Plugin.Setup runs.
type Option func(*harnessConfig)

type harnessConfig struct {
	pluginID     string
	version      string
	settings     map[string]any
	enforcePerms bool
	permissions  []string
	enableSQL    bool
	hostSetup    []func(*MockHost)
}

// WithPluginID overrides the plugin ID (default: "test-plugin").
func WithPluginID(id string) Option {
	return func(c *harnessConfig) { c.pluginID = id }
}

// WithVersion overrides the plugin version (default: "0.1.0").
func WithVersion(v string) Option {
	return func(c *harnessConfig) { c.version = v }
}

// WithSettings supplies initial plugin settings passed to Init.
func WithSettings(settings map[string]any) Option {
	return func(c *harnessConfig) { c.settings = settings }
}

// WithPermissions restricts the mock host to strictly enforce only the listed
// permissions. When omitted, all permissions are allowed for convenience.
func WithPermissions(perms ...string) Option {
	return func(c *harnessConfig) {
		c.enforcePerms = true
		c.permissions = append([]string(nil), perms...)
	}
}

// WithSQL provisions an isolated in-memory SQLite database (with "plg_<id>_" prefix)
// and hands it to the plugin during Init.
func WithSQL() Option {
	return func(c *harnessConfig) { c.enableSQL = true }
}

// WithHost applies a custom mutator to the MockHost before Plugin.Setup runs.
func WithHost(fn func(*MockHost)) Option {
	return func(c *harnessConfig) {
		if fn != nil {
			c.hostSetup = append(c.hostSetup, fn)
		}
	}
}

// Harness runs a native Go plugin completely in-process against a MockHost.
type Harness struct {
	t        testing.TB
	srv      *plugin.Server
	host     *MockHost
	initResp *pluginv1.InitResponse
	db       *plugindb.DB
}

// New initializes p inside an in-memory SkinLoom test host and fails t if Setup fails.
func New(t testing.TB, p plugin.Plugin, opts ...Option) *Harness {
	t.Helper()

	cfg := harnessConfig{
		pluginID: "test-plugin",
		version:  "0.1.0",
		settings: map[string]any{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	host := NewMockHost()
	host.Settings = cfg.settings
	if cfg.enforcePerms {
		host.setPermissions(true, cfg.permissions)
	}
	for _, fn := range cfg.hostSetup {
		fn(host)
	}

	var protoDB *pluginv1.DatabaseConfig
	var testDB *plugindb.DB
	wantsSQL := cfg.enableSQL
	if !wantsSQL && cfg.enforcePerms {
		for _, perm := range cfg.permissions {
			if perm == "storage.sql" {
				wantsSQL = true
				break
			}
		}
	}
	if wantsSQL {
		memDB, dbCfg, err := plugindb.OpenMemory(cfg.pluginID)
		if err != nil {
			t.Fatalf("plugintest: open memory db: %v", err)
		}
		testDB = memDB
		protoDB = &pluginv1.DatabaseConfig{
			Driver: dbCfg.Driver,
			Dsn:    dbCfg.DSN,
			Prefix: dbCfg.Prefix,
			Schema: dbCfg.Schema,
		}
	}

	srv := plugin.NewServer(p)
	srv.SetHostClient(host.Client())

	settingsRaw, _ := json.Marshal(cfg.settings)
	initResp, err := srv.Init(context.Background(), &pluginv1.InitRequest{
		PluginId:           cfg.pluginID,
		Version:            cfg.version,
		SettingsJson:       settingsRaw,
		GrantedPermissions: cfg.permissions,
		DataDir:            t.TempDir(),
		RuntimeDir:         t.TempDir(),
		Database:           protoDB,
		ProxyToken:         testProxyToken,
	})
	if err != nil {
		if testDB != nil {
			_ = testDB.Close()
		}
		srv.Close()
		t.Fatalf("plugintest: Init failed: %v", err)
	}

	h := &Harness{
		t:        t,
		srv:      srv,
		host:     host,
		initResp: initResp,
		db:       testDB,
	}
	t.Cleanup(func() {
		srv.Close()
		if testDB != nil {
			_ = testDB.Close()
		}
	})
	return h
}

// Host returns the underlying MockHost for seeding data and asserting side effects.
func (h *Harness) Host() *MockHost { return h.host }

// Server returns the underlying sdk/go *plugin.Server.
func (h *Harness) Server() *plugin.Server { return h.srv }

// InitResponse returns the protobuf InitResponse emitted by Setup.
func (h *Harness) InitResponse() *pluginv1.InitResponse { return h.initResp }

// DB returns the test harness's handle to the plugin's isolated in-memory SQLite DB.
func (h *Harness) DB() *plugindb.DB { return h.db }

// Lifecycle invokes an optional lifecycle hook ("onInstall", "onEnable", "onDisable",
// "onUpgrade", "onUninstall").
func (h *Harness) Lifecycle(ctx context.Context, event string, payload any) error {
	var raw []byte
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	resp, err := h.srv.Lifecycle(ctx, &pluginv1.LifecycleRequest{Event: event, PayloadJson: raw})
	if err != nil {
		return err
	}
	if resp.GetError() != "" {
		return fmt.Errorf("%s", resp.GetError())
	}
	return nil
}

// TriggerGuard runs all registered guard hooks matching hookName in registration order.
// Returns *plugin.RejectionError when rejected.
func (h *Harness) TriggerGuard(ctx context.Context, hookName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, hk := range h.initResp.GetHooks() {
		if hk.GetKind() == "guard" && hk.GetName() == hookName {
			resp, err := h.srv.InvokeGuard(ctx, &pluginv1.InvokeGuardRequest{
				HookName:    hookName,
				HandlerId:   hk.GetHandlerId(),
				PayloadJson: raw,
			})
			if err != nil {
				return err
			}
			if resp.GetRejected() {
				return plugin.Reject(resp.GetReason())
			}
		}
	}
	return nil
}

// TriggerFilter pipes value through all registered filter hooks matching hookName.
func (h *Harness) TriggerFilter(ctx context.Context, hookName string, value, payload any) (json.RawMessage, error) {
	valRaw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	ctxRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cur := valRaw
	for _, hk := range h.initResp.GetHooks() {
		if hk.GetKind() == "filter" && hk.GetName() == hookName {
			resp, err := h.srv.InvokeFilter(ctx, &pluginv1.InvokeFilterRequest{
				HookName:    hookName,
				HandlerId:   hk.GetHandlerId(),
				ValueJson:   cur,
				ContextJson: ctxRaw,
			})
			if err != nil {
				return nil, err
			}
			if len(resp.GetValueJson()) > 0 {
				cur = resp.GetValueJson()
			}
		}
	}
	return json.RawMessage(cur), nil
}

// TriggerEvent invokes all registered event hooks matching hookName.
func (h *Harness) TriggerEvent(ctx context.Context, hookName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, hk := range h.initResp.GetHooks() {
		if hk.GetKind() == "event" && hk.GetName() == hookName {
			if _, err := h.srv.InvokeEvent(ctx, &pluginv1.InvokeEventRequest{
				HookName:    hookName,
				HandlerId:   hk.GetHandlerId(),
				PayloadJson: raw,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// DoHTTP dispatches a JSON HTTP request to the matching non-stream route.
func (h *Harness) DoHTTP(ctx context.Context, req *plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	subpath := normalizeTestPath(req.Path)
	actualSegs := splitTestSegments(subpath)

	var matched *pluginv1.RouteRegistration
	var params map[string]string
	for _, rt := range h.initResp.GetRoutes() {
		if rt.GetStreamProxy() {
			continue
		}
		p, ok := matchTestSegments(splitTestSegments(rt.GetPattern()), actualSegs)
		if !ok {
			continue
		}
		if rt.GetMethod() == "*" || strings.EqualFold(rt.GetMethod(), "ANY") || strings.EqualFold(rt.GetMethod(), method) {
			matched = rt
			params = p
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("no matching plugin route for %s %s", method, subpath)
	}
	for k, v := range req.Params {
		params[k] = v
	}

	var viewer *pluginv1.ViewerInfo
	if req.User != nil && req.User.Authenticated {
		viewer = &pluginv1.ViewerInfo{
			Authenticated: true,
			UserId:        req.User.UserID,
			Email:         req.User.Email,
			Nickname:      req.User.Nickname,
			Role:          req.User.Role,
		}
	}

	resp, err := h.srv.InvokeHTTP(ctx, &pluginv1.InvokeHTTPRequest{
		HandlerId: matched.GetHandlerId(),
		Method:    method,
		Path:      subpath,
		Params:    params,
		Query:     req.Query,
		Headers:   req.Headers,
		Body:      req.Body,
		Locale:    req.Locale,
		User:      viewer,
	})
	if err != nil {
		return nil, err
	}
	if resp.GetRejected() {
		return nil, plugin.Reject(resp.GetRejectReason())
	}
	return &plugin.HTTPResponse{
		Status:  int(resp.GetStatus()),
		Headers: resp.GetHeaders(),
		RawBody: resp.GetBody(),
	}, nil
}

// DoStreamHTTP dispatches an HTTP request directly into the plugin's local
// streaming proxy handler with the host's valid X-SkinLoom-Proxy-Token injected.
func (h *Harness) DoStreamHTTP(r *http.Request) *httptest.ResponseRecorder {
	clone := r.Clone(r.Context())
	if clone.Header.Get(plugin.HeaderProxyToken) == "" {
		clone.Header.Set(plugin.HeaderProxyToken, testProxyToken)
	}
	rec := httptest.NewRecorder()
	h.srv.StreamHTTPHandler().ServeHTTP(rec, clone)
	return rec
}

// TriggerCron runs the registered cron job with the given name.
func (h *Harness) TriggerCron(ctx context.Context, jobName string) error {
	for _, cr := range h.initResp.GetCrons() {
		if cr.GetName() == jobName {
			_, err := h.srv.InvokeCron(ctx, &pluginv1.InvokeCronRequest{
				JobName:   jobName,
				HandlerId: cr.GetHandlerId(),
			})
			return err
		}
	}
	return fmt.Errorf("cron job %q not registered", jobName)
}

// CallService invokes an exposed cross-plugin RPC service.
func (h *Harness) CallService(ctx context.Context, serviceName, callerID string, args ...any) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	resp, err := h.srv.InvokeService(ctx, &pluginv1.InvokeServiceRequest{
		ServiceName:    serviceName,
		HandlerId:      serviceName,
		ArgsJson:       raw,
		CallerPluginId: callerID,
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.GetResultJson()), nil
}

// UpdateSettings pushes updated settings into the plugin and triggers Settings.OnChange.
func (h *Harness) UpdateSettings(ctx context.Context, next map[string]any) error {
	h.host.mu.Lock()
	h.host.Settings = next
	h.host.mu.Unlock()
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = h.srv.NotifySettingsChanged(ctx, &pluginv1.NotifySettingsChangedRequest{SettingsJson: raw})
	return err
}

func normalizeTestPath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func splitTestSegments(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func matchTestSegments(pat, actual []string) (map[string]string, bool) {
	if len(pat) == 1 && pat[0] == "*" {
		return map[string]string{"*": strings.Join(actual, "/")}, true
	}
	if len(pat) > 1 && pat[len(pat)-1] == "*" {
		prefix := pat[:len(pat)-1]
		if len(actual) < len(prefix) {
			return nil, false
		}
		params, ok := matchTestSegments(prefix, actual[:len(prefix)])
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
