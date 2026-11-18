package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/plugindoc"
	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

const (
	defaultRouteMaxBody = 1 * 1024 * 1024
	hardMaxRouteBody    = 10 * 1024 * 1024
	defaultDrainTimeout = 30 * time.Second
)

// Viewer describes the authenticated caller of /api/plugins/<id>/*.
type Viewer struct {
	Authenticated bool
	UserID        int64
	Email         string
	Nickname      string
	Role          string // user | admin | superadmin
	Admin         bool
	Superadmin    bool
}

// ManagerDeps injects host services required by the plugin runtime.
type ManagerDeps struct {
	DB       *ent.Client
	Bus      *hook.Bus
	Settings *settings.Service
	Notify   *notify.Service
	Engine   jsrt.Engine
	DataDir  string
	Log      *slog.Logger
}

// PluginInfo summarizes an installed plugin's manifest and live runtime state.
type PluginInfo struct {
	ID                 string          `json:"id"`
	Kind               string          `json:"kind"`
	Version            string          `json:"version"`
	Status             string          `json:"status"`
	Error              string          `json:"error,omitempty"`
	Source             string          `json:"source"`
	GrantedPermissions []string        `json:"grantedPermissions"`
	Settings           map[string]any  `json:"settings,omitempty"`
	Manifest           *Manifest       `json:"manifest,omitempty"`
	Routes             []routeBinding  `json:"routes,omitempty"`
	Stats              *jsrt.PoolStats `json:"stats,omitempty"`
	RecentLogs         []LogEntry      `json:"recentLogs,omitempty"`
	CircuitTripped     bool            `json:"circuitTripped,omitempty"`
	ConsecutiveErrors  int             `json:"consecutiveErrors,omitempty"`
}

// Manager coordinates plugin installation, two-phase atomic activation,
// hot enable/disable/uninstall, HTTP route dispatch, static assets, and circuit breaking.
type Manager struct {
	db         *ent.Client
	bus        *hook.Bus
	settings   *settings.Service
	notify     *notify.Service
	dataDir    string
	log        *slog.Logger
	engine     jsrt.Engine
	ownsEngine bool

	mu     sync.RWMutex
	active map[string]*pluginRuntime
	stored map[string]*storedBundle
}

type storedBundle struct {
	manifest    *Manifest
	dir         string
	bundleCode  []byte
	staticFiles map[string][]byte
	sha256      string
	source      string
}

// NewManager creates a plugin manager. If deps.Engine is nil, a dedicated jsrt engine is initialized.
func NewManager(deps ManagerDeps) (*Manager, error) {
	eng := deps.Engine
	owns := false
	if eng == nil {
		var err error
		eng, err = jsrt.NewEngine(jsrt.WithLogger(deps.Log))
		if err != nil {
			return nil, err
		}
		owns = true
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		db:         deps.DB,
		bus:        deps.Bus,
		settings:   deps.Settings,
		notify:     deps.Notify,
		dataDir:    deps.DataDir,
		log:        log,
		engine:     eng,
		ownsEngine: owns,
		active:     map[string]*pluginRuntime{},
		stored:     map[string]*storedBundle{},
	}, nil
}

// Start scans the plugins directory and activates any DB-enabled plugins.
func (m *Manager) Start(ctx context.Context) error {
	if m.db == nil {
		return nil
	}
	rows, err := m.db.Plugin.Query().Order(ent.Asc(entplugin.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	pluginsRoot := filepath.Join(m.dataDir, "plugins")
	for _, row := range rows {
		if row.Kind != entplugin.KindPlugin {
			continue
		}
		if row.Status != entplugin.StatusEnabled {
			continue
		}
		dir := filepath.Join(pluginsRoot, row.ID, row.Version)
		if _, statErr := os.Stat(filepath.Join(dir, "plugin.json")); statErr != nil {
			dir = filepath.Join(pluginsRoot, row.ID)
		}
		manifestRaw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			m.markFailed(ctx, row.ID, fmt.Errorf("missing manifest in %s: %w", dir, err))
			continue
		}
		mf, err := ParseManifest(manifestRaw)
		if err != nil {
			m.markFailed(ctx, row.ID, err)
			continue
		}
		var serverJS []byte
		if mf.Server != "" {
			serverJS, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(mf.Server)))
			if err != nil {
				m.markFailed(ctx, row.ID, fmt.Errorf("read server bundle %q: %w", mf.Server, err))
				continue
			}
		}
		sb := &storedBundle{
			manifest:   mf,
			dir:        dir,
			bundleCode: serverJS,
			sha256:     row.PackageSha256,
			source:     row.Source,
		}
		m.mu.Lock()
		m.stored[mf.ID] = sb
		m.mu.Unlock()
		if err := m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, ""); err != nil {
			m.markFailed(ctx, row.ID, err)
		}
	}
	return nil
}

// LoadFromDir loads an unpacked plugin directory from disk, persists its metadata, and
// optionally enables it atomically.
func (m *Manager) LoadFromDir(ctx context.Context, dir string, enable bool) (*PluginInfo, error) {
	manifestRaw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("read plugin.json: %w", err)
	}
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	var serverJS []byte
	if mf.Server != "" {
		serverJS, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(mf.Server)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", mf.Server, err)
		}
	}
	sum := sha256.Sum256(append(append([]byte{}, manifestRaw...), serverJS...))
	sb := &storedBundle{
		manifest:   mf,
		dir:        dir,
		bundleCode: serverJS,
		sha256:     hex.EncodeToString(sum[:]),
		source:     "local",
	}
	return m.installBundle(ctx, sb, nil, enable)
}

// LoadFromBytes registers an in-memory plugin manifest + JS bundle (+ optional static
// files like web/index.js or locales/en.json), saves DB state, and activates it atomically.
func (m *Manager) LoadFromBytes(
	ctx context.Context,
	manifestRaw []byte,
	serverJS []byte,
	staticFiles map[string][]byte,
	grantedPerms []string,
	enable bool,
) (*PluginInfo, error) {
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if len(serverJS) > jsrt.MaxBundleBytes {
		return nil, jsrt.ErrBundleTooLarge
	}
	if grantedPerms == nil {
		grantedPerms = append([]string(nil), mf.Permissions...)
	}
	sum := sha256.Sum256(append(append([]byte{}, manifestRaw...), serverJS...))
	sb := &storedBundle{
		manifest:    mf,
		bundleCode:  append([]byte(nil), serverJS...),
		staticFiles: staticFiles,
		sha256:      hex.EncodeToString(sum[:]),
		source:      "package",
	}
	return m.installBundle(ctx, sb, grantedPerms, enable)
}

func (m *Manager) installBundle(ctx context.Context, sb *storedBundle, grantedPerms []string, enable bool) (*PluginInfo, error) {
	if grantedPerms == nil {
		grantedPerms = append([]string(nil), sb.manifest.Permissions...)
	}

	var prevVer string
	var existingSettings map[string]any
	isNew := false

	existing, err := m.db.Plugin.Get(ctx, sb.manifest.ID)
	if err != nil {
		if !ent.IsNotFound(err) {
			return nil, err
		}
		isNew = true
	} else {
		prevVer = existing.Version
		existingSettings = existing.Settings
	}

	m.mu.Lock()
	m.stored[sb.manifest.ID] = sb
	m.mu.Unlock()

	now := time.Now().UTC()
	if isNew {
		_, err = m.db.Plugin.Create().
			SetID(sb.manifest.ID).
			SetKind(entplugin.Kind(sb.manifest.Kind)).
			SetVersion(sb.manifest.Version).
			SetStatus(entplugin.StatusDisabled).
			SetGrantedPermissions(grantedPerms).
			SetSettings(mergeDefaultSettings(sb.manifest.Settings, nil)).
			SetError("").
			SetSource(sb.source).
			SetPackageSha256(sb.sha256).
			SetInstalledAt(now).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		existingSettings = mergeDefaultSettings(sb.manifest.Settings, nil)
	} else {
		merged := mergeDefaultSettings(sb.manifest.Settings, existingSettings)
		_, err = m.db.Plugin.UpdateOneID(sb.manifest.ID).
			SetVersion(sb.manifest.Version).
			SetGrantedPermissions(grantedPerms).
			SetSettings(merged).
			SetError("").
			SetSource(sb.source).
			SetPackageSha256(sb.sha256).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		existingSettings = merged
	}

	if enable {
		if err := m.activateBundle(ctx, sb, grantedPerms, existingSettings, isNew, prevVer); err != nil {
			return nil, err
		}
	}
	info, _ := m.Get(ctx, sb.manifest.ID)
	return info, nil
}

// activateBundle performs two-phase atomic registration:
// Phase 1: build runtime, warm isolate pool, run setup(collector) to gather hooks/routes.
// Phase 2: swap active runtime + atomically replace hooks in Bus + mark DB enabled.
func (m *Manager) activateBundle(
	ctx context.Context,
	sb *storedBundle,
	grantedPerms []string,
	settingsMap map[string]any,
	isInstall bool,
	upgradedFrom string,
) error {
	candidate, err := newPluginRuntime(sb.manifest, sb.dir, sb.bundleCode, sb.staticFiles, grantedPerms, settingsMap, m)
	if err != nil {
		m.markFailed(ctx, sb.manifest.ID, err)
		return err
	}

	// Phase 1: compile, warm pool, run setup() across isolates.
	if err := candidate.initPool(ctx, m.engine); err != nil {
		candidate.close(2 * time.Second)
		m.markFailed(ctx, sb.manifest.ID, err)
		return err
	}
	regs, err := candidate.buildHookRegistrations()
	if err != nil {
		candidate.close(2 * time.Second)
		m.markFailed(ctx, sb.manifest.ID, err)
		return err
	}

	// Phase 2: atomic swap into Bus and active route table.
	if m.bus != nil {
		if err := m.bus.Replace("plugin:"+sb.manifest.ID, regs...); err != nil {
			candidate.close(2 * time.Second)
			m.markFailed(ctx, sb.manifest.ID, err)
			return err
		}
	}

	m.mu.Lock()
	old := m.active[sb.manifest.ID]
	m.active[sb.manifest.ID] = candidate
	m.mu.Unlock()

	if old != nil && old != candidate {
		go old.close(defaultDrainTimeout)
	}

	_, err = m.db.Plugin.UpdateOneID(sb.manifest.ID).
		SetStatus(entplugin.StatusEnabled).
		SetError("").
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		m.log.Warn("failed to update plugin enabled status", "plugin", sb.manifest.ID, "err", err)
	}

	// Run lifecycle hooks synchronously on the healthy pool; failures log/fail without
	// leaving partial registrations dangling if severe.
	if isInstall {
		_ = candidate.invokeLifecycle(ctx, "onInstall", nil)
	} else if upgradedFrom != "" && upgradedFrom != sb.manifest.Version {
		_ = candidate.invokeLifecycle(ctx, "onUpgrade", map[string]any{"from": upgradedFrom})
	}
	_ = candidate.invokeLifecycle(ctx, "onEnable", nil)
	return nil
}

// Enable activates an installed plugin cleanly and resets any tripped circuit breaker.
func (m *Manager) Enable(ctx context.Context, id string) error {
	row, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return err
	}
	m.mu.RLock()
	sb := m.stored[id]
	m.mu.RUnlock()
	if sb == nil {
		return fmt.Errorf("plugin %q bundle not loaded", id)
	}
	return m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, "")
}

// Disable atomically removes the plugin from the hook bus and route table, runs
// onDisable, and drains in-flight JS executions.
func (m *Manager) Disable(ctx context.Context, id string) error {
	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}
	m.mu.Lock()
	rt := m.active[id]
	delete(m.active, id)
	m.mu.Unlock()

	if rt != nil {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		rt.close(defaultDrainTimeout)
	}
	_, err := m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusDisabled).
		SetError("").
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

// Uninstall disables the plugin, invokes onUninstall, removes bundle metadata,
// and when purge is true wipes KV, documents, and private files.
func (m *Manager) Uninstall(ctx context.Context, id string, purge bool) error {
	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()

	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}
	if rt != nil {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		_ = rt.invokeLifecycle(ctx, "onUninstall", map[string]any{"purge": purge})
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
		rt.close(defaultDrainTimeout)
	}

	m.mu.Lock()
	delete(m.stored, id)
	m.mu.Unlock()

	if err := m.db.Plugin.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	if purge {
		if _, err := m.db.PluginKV.Delete().Where(pluginkv.Plugin(id)).Exec(ctx); err != nil {
			return err
		}
		if _, err := m.db.PluginDoc.Delete().Where(plugindoc.Plugin(id)).Exec(ctx); err != nil {
			return err
		}
		if m.dataDir != "" {
			_ = os.RemoveAll(filepath.Join(m.dataDir, "plugins-data", id))
		}
	}
	return nil
}

// UpdateSettings updates a plugin's persisted settings and fires ctx.settings.onChange.
func (m *Manager) UpdateSettings(ctx context.Context, id string, next map[string]any) error {
	row, err := m.db.Plugin.UpdateOneID(id).
		SetSettings(cloneMap(next)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		return err
	}
	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()
	if rt != nil {
		rt.notifySettingsChanged(ctx, row.Settings)
	}
	return nil
}

func (m *Manager) handleCircuitTrip(id string, tripErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}

	m.mu.Lock()
	rt := m.active[id]
	delete(m.active, id)
	m.mu.Unlock()

	if rt != nil {
		go rt.close(5 * time.Second)
	}

	errMsg := "circuit breaker tripped"
	if tripErr != nil {
		errMsg = tripErr.Error()
	}
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	m.log.Error("plugin circuit breaker tripped; plugin disabled", "plugin", id, "err", errMsg)
	m.markFailed(ctx, id, errors.New(errMsg))

	if m.db != nil && m.notify != nil {
		admins, err := m.db.User.Query().
			Where(user.RoleEQ(user.RoleSuperadmin), user.StatusEQ(user.StatusActive)).
			All(ctx)
		if err == nil {
			title := fmt.Sprintf("Plugin %s disabled", id)
			if len([]rune(title)) > notify.MaxTitle {
				title = string([]rune(title)[:notify.MaxTitle])
			}
			body := fmt.Sprintf("Plugin `%s` exceeded %d consecutive runtime failures and was automatically disabled.\n\nLast error:\n```\n%s\n```",
				id, DefaultCircuitThreshold, errMsg)
			for _, adm := range admins {
				if _, nErr := m.notify.Send(ctx, adm.ID, title, body, nil); nErr != nil {
					m.log.Warn("failed to notify superadmin of plugin failure", "plugin", id, "userId", adm.ID, "err", nErr)
				}
			}
		}
	}
}

func (m *Manager) markFailed(ctx context.Context, id string, cause error) {
	if m.db == nil || cause == nil {
		return
	}
	msg := cause.Error()
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	_ = m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusFailed).
		SetError(msg).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
}

// Get returns summary info for one installed plugin.
func (m *Manager) Get(ctx context.Context, id string) (*PluginInfo, bool) {
	row, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return nil, false
	}
	m.mu.RLock()
	rt := m.active[id]
	sb := m.stored[id]
	m.mu.RUnlock()

	info := PluginInfo{
		ID:                 row.ID,
		Kind:               string(row.Kind),
		Version:            row.Version,
		Status:             string(row.Status),
		Error:              row.Error,
		Source:             row.Source,
		GrantedPermissions: row.GrantedPermissions,
		Settings:           row.Settings,
	}
	if sb != nil {
		info.Manifest = sb.manifest
	}
	if rt != nil {
		info.Manifest = rt.manifest
		info.Routes = append([]routeBinding(nil), rt.routes...)
		info.RecentLogs = rt.recentLogs()
		if rt.pool != nil {
			st := rt.pool.Stats()
			info.Stats = &st
		}
		consec, tripped, _, _ := rt.circuit.State()
		info.ConsecutiveErrors = consec
		info.CircuitTripped = tripped
	}
	return &info, true
}

// List returns all installed plugins sorted by ID.
func (m *Manager) List(ctx context.Context) ([]PluginInfo, error) {
	rows, err := m.db.Plugin.Query().Order(ent.Asc(entplugin.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PluginInfo, 0, len(rows))
	for _, row := range rows {
		if info, ok := m.Get(ctx, row.ID); ok {
			out = append(out, *info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ServePluginAPI dispatches HTTP requests arriving under /api/plugins/{id}/*.
func (m *Manager) ServePluginAPI(w http.ResponseWriter, r *http.Request, id, subpath string, viewer Viewer, locale string) {
	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()
	if rt == nil || rt.closed.Load() {
		writePluginProblem(w, http.StatusNotFound, "plugin not active")
		return
	}
	if !strings.HasPrefix(subpath, "/") {
		subpath = "/" + subpath
	}
	if subpath != "/" {
		subpath = strings.TrimRight(subpath, "/")
	}

	route, params, pathMatched := matchRoute(rt.routes, r.Method, subpath)
	if route == nil {
		if pathMatched {
			writePluginProblem(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writePluginProblem(w, http.StatusNotFound, "plugin route not found")
		return
	}

	switch route.Auth {
	case "public":
	case "user":
		if !viewer.Authenticated {
			writePluginProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
	case "admin":
		if !viewer.Authenticated {
			writePluginProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !viewer.Admin && !viewer.Superadmin {
			writePluginProblem(w, http.StatusForbidden, "administrators only")
			return
		}
	case "superadmin":
		if !viewer.Authenticated {
			writePluginProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !viewer.Superadmin {
			writePluginProblem(w, http.StatusForbidden, "super administrators only")
			return
		}
	}

	maxBody := rt.manifest.HTTP.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultRouteMaxBody
	}
	if maxBody > hardMaxRouteBody {
		maxBody = hardMaxRouteBody
	}

	var bodyAny any
	var rawBody string
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		limited := io.LimitReader(r.Body, maxBody+1)
		b, err := io.ReadAll(limited)
		if err != nil {
			writePluginProblem(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if int64(len(b)) > maxBody {
			writePluginProblem(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		rawBody = string(b)
		if len(bytes.TrimSpace(b)) > 0 && strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			if err := json.Unmarshal(b, &bodyAny); err != nil {
				writePluginProblem(w, http.StatusBadRequest, "invalid JSON request body")
				return
			}
		} else if len(bytes.TrimSpace(b)) > 0 {
			var probe any
			if json.Unmarshal(b, &probe) == nil {
				bodyAny = probe
			}
		}
	}

	queryMap := make(map[string]string, len(r.URL.Query()))
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			queryMap[k] = vals[0]
		}
	}
	hdrMap := map[string]string{}
	for _, k := range []string{"Content-Type", "Accept", "User-Agent"} {
		if v := r.Header.Get(k); v != "" {
			hdrMap[strings.ToLower(k)] = v
		}
	}

	reqObj := map[string]any{
		"method":  r.Method,
		"path":    subpath,
		"params":  params,
		"query":   queryMap,
		"headers": hdrMap,
		"body":    bodyAny,
		"rawBody": rawBody,
		"locale":  locale,
	}
	if viewer.Authenticated {
		reqObj["user"] = map[string]any{
			"id":       viewer.UserID,
			"email":    viewer.Email,
			"nickname": viewer.Nickname,
			"role":     viewer.Role,
		}
	} else {
		reqObj["user"] = nil
	}

	ctx := WithRequestLocale(r.Context(), locale)
	out, err := rt.invokeRoute(ctx, route.FnID, reqObj)
	if err != nil {
		if errors.Is(err, jsrt.ErrTimeout) {
			writePluginProblem(w, http.StatusGatewayTimeout, "plugin route timed out")
			return
		}
		if rej, ok := hook.AsRejection(err); ok {
			writePluginProblem(w, http.StatusUnprocessableEntity, rej.Reason)
			return
		}
		writePluginProblem(w, http.StatusInternalServerError, err.Error())
		return
	}

	writePluginRouteResponse(w, out)
}

func matchRoute(routes []routeBinding, method, actual string) (*routeBinding, map[string]string, bool) {
	actualSegs := splitPathSegments(actual)
	pathMatched := false
	for idx := range routes {
		rb := &routes[idx]
		params, ok := matchSegments(splitPathSegments(rb.Pattern), actualSegs)
		if !ok {
			continue
		}
		pathMatched = true
		if strings.EqualFold(rb.Method, method) {
			return rb, params, true
		}
	}
	return nil, nil, pathMatched
}

func splitPathSegments(p string) []string {
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

func writePluginRouteResponse(w http.ResponseWriter, out any) {
	status := http.StatusOK
	body := out

	if m, ok := out.(map[string]any); ok {
		if rej, _ := m["__skinloomReject"].(bool); rej {
			reason, _ := m["reason"].(string)
			writePluginProblem(w, http.StatusUnprocessableEntity, reason)
			return
		}
		if s, hasStatus := m["status"]; hasStatus {
			if _, hasBody := m["body"]; hasBody || len(m) <= 3 {
				if code := int(asFloat(s)); code >= 100 && code <= 599 {
					status = code
					if hdrs, ok := m["headers"].(map[string]any); ok {
						for hk, hv := range hdrs {
							if strings.EqualFold(hk, "Set-Cookie") || strings.EqualFold(hk, "Content-Security-Policy") {
								continue
							}
							w.Header().Set(hk, fmt.Sprint(hv))
						}
					}
					body = m["body"]
				}
			}
		}
	}

	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	if str, ok := body.(string); ok && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		_, _ = io.WriteString(w, str)
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

func writePluginProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}

// AssetHandler serves static assets under /plugins-assets/{id}/{rev?}/... and
// plugin-private written files under /plugins-assets/{id}/_files/{key...}.
func (m *Manager) AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/plugins-assets/")
		trimmed = strings.TrimPrefix(trimmed, "/")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			http.NotFound(w, r)
			return
		}
		id, rest := parts[0], parts[1]
		if !pluginIDRe.MatchString(id) && !themeIDRe.MatchString(id) {
			http.NotFound(w, r)
			return
		}

		// Handle plugin-written private files: /plugins-assets/{id}/_files/{key...}
		if strings.HasPrefix(rest, "_files/") {
			if m.dataDir == "" {
				http.NotFound(w, r)
				return
			}
			key := strings.TrimPrefix(rest, "_files/")
			rel, err := validateFileKey(key)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			full := filepath.Join(m.dataDir, "plugins-data", id, filepath.FromSlash(rel))
			data, err := os.ReadFile(full)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "sandbox")
			ctype := safeAssetContentType(rel, data, false)
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "public, max-age=300")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
			return
		}

		m.mu.RLock()
		rt := m.active[id]
		sb := m.stored[id]
		m.mu.RUnlock()

		var dir string
		var overlay map[string][]byte
		var version string
		if rt != nil {
			dir = rt.dir
			overlay = rt.staticFiles
			version = rt.manifest.Version
		} else if sb != nil {
			dir = sb.dir
			overlay = sb.staticFiles
			version = sb.manifest.Version
		} else {
			http.NotFound(w, r)
			return
		}

		// Support both /plugins-assets/{id}/{version}/{path} and /plugins-assets/{id}/{path}.
		assetRel := rest
		versioned := false
		if segs := strings.SplitN(rest, "/", 2); len(segs) == 2 && (segs[0] == version || strings.HasPrefix(segs[0], "v") || strings.Contains(segs[0], ".")) {
			if _, ok := overlay[segs[1]]; ok {
				assetRel = segs[1]
				versioned = true
			} else if dir != "" {
				cleanCheck := path.Clean("/" + segs[1])
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(cleanCheck, "/")))); err == nil {
					assetRel = segs[1]
					versioned = true
				}
			}
		}

		rel, err := validateFileKey(assetRel)
		if err != nil || (!strings.HasPrefix(rel, "web/") && !strings.HasPrefix(rel, "assets/")) {
			http.NotFound(w, r)
			return
		}

		var data []byte
		if b, ok := overlay[rel]; ok {
			data = b
		} else if dir != "" {
			full := filepath.Join(dir, filepath.FromSlash(rel))
			data, err = os.ReadFile(full)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		} else {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", safeAssetContentType(rel, data, strings.HasPrefix(rel, "web/")))
		if versioned {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	})
}

func safeAssetContentType(rel string, data []byte, allowScript bool) string {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".js", ".mjs":
		if !allowScript {
			return "application/octet-stream"
		}
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".html", ".htm", ".xhtml", ".svg", ".xml":
		// Prevent active script execution inside plugin uploaded/static assets.
		return "application/octet-stream"
	}
	if c := mime.TypeByExtension(ext); c != "" {
		return c
	}
	ctype := http.DetectContentType(data)
	if strings.Contains(ctype, "html") {
		return "application/octet-stream"
	}
	return ctype
}

// Shutdown drains all active plugin pools and closes the owned jsrt engine.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	active := make([]*pluginRuntime, 0, len(m.active))
	for id, rt := range m.active {
		if m.bus != nil {
			m.bus.Remove("plugin:" + id)
		}
		active = append(active, rt)
		delete(m.active, id)
	}
	m.mu.Unlock()

	drain := defaultDrainTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline); rem < drain {
			drain = rem
		}
	}
	if drain < 0 {
		drain = 0
	}
	for _, rt := range active {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		rt.close(drain)
	}
	if m.ownsEngine && m.engine != nil {
		return m.engine.Close()
	}
	return nil
}
