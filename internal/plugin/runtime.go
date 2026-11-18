package plugin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

const (
	defaultGuardTimeout = 2 * time.Second
	defaultRouteTimeout = 30 * time.Second
	defaultHookTimeout  = 10 * time.Second
	maxCacheEntries     = 1024
	maxLogEntries       = 200
)

type hookBinding struct {
	Kind  hook.Kind
	Name  string
	Order int
	FnID  int
}

type routeBinding struct {
	Method  string
	Pattern string
	Auth    string // public | user | admin | superadmin
	FnID    int
}

// LogEntry is one captured line emitted through ctx.log.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type cacheItem struct {
	value     any
	expiresAt time.Time
}

type localeContextKey struct{}

// WithRequestLocale attaches the current HTTP/UI locale so ctx.t translates for
// the visitor's language when no explicit locale argument is given.
func WithRequestLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeContextKey{}, locale)
}

func requestLocaleFrom(ctx context.Context) string {
	if ctx == nil {
		return "en"
	}
	if l, ok := ctx.Value(localeContextKey{}).(string); ok && l != "" {
		return l
	}
	return "en"
}

// pluginRuntime owns one enabled plugin's isolate pool, permissions, storage
// backends, routes, hook subscriptions, and circuit breaker.
type pluginRuntime struct {
	id          string
	manifest    *Manifest
	dir         string
	bundleCode  []byte
	staticFiles map[string][]byte // optional in-memory web/assets/locales overlay
	perms       *PermissionSet
	circuit     *CircuitBreaker
	log         *slog.Logger

	kv    *KVStore
	docs  *DocStore
	files *FileStore
	fetch *Fetcher

	siteSettings *settings.Service

	localesMu sync.RWMutex
	locales   map[string]map[string]string

	settingsMu sync.RWMutex
	settings   map[string]any

	cacheMu sync.RWMutex
	cache   map[string]cacheItem

	logsMu sync.Mutex
	logs   []LogEntry

	setupMu        sync.Mutex
	collecting     bool
	collectedHooks []hookBinding
	collectedRoute []routeBinding
	hasOnChange    bool

	routes []routeBinding
	pool   *jsrt.Pool

	emitLocalEvent func(eventName string, payload any)
	onTrip         func(id string, err error)
	closed         atomic.Bool
}

func newPluginRuntime(
	manifest *Manifest,
	dir string,
	bundleCode []byte,
	staticFiles map[string][]byte,
	grantedPerms []string,
	initialSettings map[string]any,
	mgr *Manager,
) (*pluginRuntime, error) {
	quotas := manifest.EffectiveQuotas()
	perms := NewPermissionSet(manifest.Permissions, grantedPerms)

	docs, err := NewDocStore(manifest.ID, mgr.db, manifest.Storage, perms)
	if err != nil {
		return nil, err
	}
	log := mgr.log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("plugin", manifest.ID)

	rt := &pluginRuntime{
		id:           manifest.ID,
		manifest:     manifest,
		dir:          dir,
		bundleCode:   bundleCode,
		staticFiles:  staticFiles,
		perms:        perms,
		circuit:      NewCircuitBreaker(DefaultCircuitThreshold),
		log:          log,
		kv:           NewKVStore(manifest.ID, mgr.db, quotas.KVKeys, perms),
		docs:         docs,
		files:        NewFileStore(manifest.ID, mgr.dataDir, quotas.FilesBytes, perms),
		fetch:        NewFetcher(manifest.HTTP.Allow, perms),
		siteSettings: mgr.settings,
		locales:      loadPluginLocales(dir, staticFiles),
		settings:     mergeDefaultSettings(manifest.Settings, initialSettings),
		cache:        make(map[string]cacheItem),
		emitLocalEvent: func(eventName string, payload any) {
			if mgr.bus != nil {
				hook.EmitDynamic(mgr.bus, eventName, payload)
			}
		},
		onTrip: mgr.handleCircuitTrip,
	}
	return rt, nil
}

func (r *pluginRuntime) initPool(ctx context.Context, eng jsrt.Engine) error {
	if len(r.bundleCode) == 0 {
		return nil
	}
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       r.id,
		Filename: r.manifest.Server,
		Code:     r.bundleCode,
		Limits: jsrt.Limits{
			PoolSize:     jsrt.DefaultPoolSize,
			MemoryLimit:  jsrt.DefaultMemoryLimit,
			MaxStackSize: jsrt.DefaultMaxStackSize,
			Timeout:      defaultGuardTimeout,
		},
		Log:     r.log,
		Prepare: r.prepareInstance,
		OnSetup: func(setupCtx context.Context, inst jsrt.Instance) error {
			return r.runSetupInInstance(setupCtx, inst, true)
		},
		OnWarm: func(warmCtx context.Context, inst jsrt.Instance) error {
			return r.runSetupInInstance(warmCtx, inst, false)
		},
	})
	if err != nil {
		return err
	}
	r.pool = pool
	r.setupMu.Lock()
	r.routes = append([]routeBinding(nil), r.collectedRoute...)
	r.setupMu.Unlock()
	return nil
}

func (r *pluginRuntime) runSetupInInstance(ctx context.Context, inst jsrt.Instance, collect bool) error {
	r.setupMu.Lock()
	r.collecting = collect
	if collect {
		r.collectedHooks = nil
		r.collectedRoute = nil
		r.hasOnChange = false
	}
	r.setupMu.Unlock()

	setupCtx, cancel := context.WithTimeout(ctx, defaultHookTimeout)
	defer cancel()
	_, _, err := inst.InvokeGlobal(setupCtx, "__sl_run_setup")
	return err
}

func (r *pluginRuntime) prepareInstance(_ context.Context, inst jsrt.Instance) error {
	bindings := map[string]jsrt.HostFunc{
		"__sl_setup_hook":     r.hostRegisterHook,
		"__sl_setup_route":    r.hostRegisterRoute,
		"__sl_setup_onChange": r.hostRegisterOnChange,
		"__sl_event_emit":     r.hostEmitEvent,
		"__sl_kv":             r.hostKV,
		"__sl_db":             r.hostDB,
		"__sl_files":          r.hostFiles,
		"__sl_fetch":          r.hostFetch,
		"__sl_settings":       r.hostSettings,
		"__sl_site":           r.hostSite,
		"__sl_crypto":         r.hostCrypto,
		"__sl_cache":          r.hostCache,
		"__sl_i18n":           r.hostI18n,
		"__sl_log":            r.hostLog,
	}
	for name, fn := range bindings {
		if err := inst.RegisterHostFunc(name, fn); err != nil {
			return err
		}
	}
	if _, _, err := inst.Eval(context.Background(), "<plugin-ctx-prelude>", pluginPreludeJS()); err != nil {
		return err
	}
	return nil
}

func (r *pluginRuntime) buildHookRegistrations() ([]hook.Registration, error) {
	r.setupMu.Lock()
	specs := append([]hookBinding(nil), r.collectedHooks...)
	r.setupMu.Unlock()

	regs := make([]hook.Registration, 0, len(specs))
	for _, spec := range specs {
		sp := spec
		switch sp.Kind {
		case hook.KindGuard:
			reg, err := hook.HandleGuardDynamic(sp.Name, sp.Order, func(ctx context.Context, payload any) error {
				return r.invokeGuard(ctx, sp.FnID, payload)
			})
			if err != nil {
				return nil, err
			}
			regs = append(regs, reg)
		case hook.KindFilter:
			reg, err := hook.HandleFilterDynamic(sp.Name, sp.Order, func(ctx context.Context, value, payload any) (any, error) {
				return r.invokeFilter(ctx, sp.FnID, value, payload)
			})
			if err != nil {
				return nil, err
			}
			regs = append(regs, reg)
		case hook.KindEvent:
			if strings.HasPrefix(sp.Name, "plugin:") {
				hook.DeclarePluginEvent(sp.Name)
			}
			reg, err := hook.HandleEventDynamic(sp.Name, sp.Order, func(ctx context.Context, payload any) error {
				return r.invokeEvent(ctx, sp.FnID, payload)
			})
			if err != nil {
				return nil, err
			}
			regs = append(regs, reg)
		}
	}
	return regs, nil
}

func (r *pluginRuntime) invokeJS(ctx context.Context, timeout time.Duration, globalFn string, args ...any) (any, error) {
	if r.closed.Load() || r.pool == nil {
		return nil, errors.New("plugin runtime not active")
	}
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if _, hasDeadline := callCtx.Deadline(); !hasDeadline && timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, timeout)
		defer cancel()
	}

	var out any
	_, err := r.pool.Do(callCtx, func(inst jsrt.Instance) (jsrt.CallStats, error) {
		val, stats, err := inst.InvokeGlobal(callCtx, globalFn, args...)
		out = val
		return stats, err
	})
	if err != nil {
		r.recordRuntimeFailure(err)
		return nil, err
	}
	r.circuit.RecordSuccess()
	return out, nil
}

func (r *pluginRuntime) invokeGuard(ctx context.Context, fnID int, payload any) error {
	res, err := r.invokeJS(ctx, defaultGuardTimeout, "__sl_invoke_handler", fnID, payload)
	if err != nil {
		return err
	}
	if m, ok := res.(map[string]any); ok {
		if rejected, _ := m["__skinloomReject"].(bool); rejected {
			reason, _ := m["reason"].(string)
			if reason == "" {
				reason = "rejected by plugin " + r.id
			}
			return hook.Reject(reason)
		}
	}
	return nil
}

func (r *pluginRuntime) invokeFilter(ctx context.Context, fnID int, value, payload any) (any, error) {
	return r.invokeJS(ctx, defaultGuardTimeout, "__sl_invoke_handler", fnID, value, payload)
}

func (r *pluginRuntime) invokeEvent(ctx context.Context, fnID int, payload any) error {
	_, err := r.invokeJS(ctx, defaultHookTimeout, "__sl_invoke_handler", fnID, payload)
	return err
}

func (r *pluginRuntime) invokeRoute(ctx context.Context, fnID int, req map[string]any) (any, error) {
	return r.invokeJS(ctx, defaultRouteTimeout, "__sl_invoke_handler", fnID, req)
}

func (r *pluginRuntime) invokeLifecycle(ctx context.Context, method string, arg any) error {
	if r.pool == nil || r.closed.Load() {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, defaultHookTimeout)
	defer cancel()
	_, err := r.pool.Do(callCtx, func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, stats, err := inst.InvokeGlobal(callCtx, "__sl_run_lifecycle", method, arg)
		return stats, err
	})
	return err
}

func (r *pluginRuntime) recordRuntimeFailure(err error) {
	if r.closed.Load() {
		return
	}
	if r.circuit.RecordFailure(err) && r.onTrip != nil {
		go r.onTrip(r.id, err)
	}
}

func (r *pluginRuntime) notifySettingsChanged(ctx context.Context, newSettings map[string]any) {
	merged := mergeDefaultSettings(r.manifest.Settings, newSettings)
	r.settingsMu.Lock()
	r.settings = merged
	r.settingsMu.Unlock()

	r.setupMu.Lock()
	listening := r.hasOnChange
	r.setupMu.Unlock()
	if !listening || r.pool == nil || r.closed.Load() {
		return
	}
	_, _ = r.invokeJS(ctx, defaultHookTimeout, "__sl_fire_settings_change", merged)
}

func (r *pluginRuntime) close(drainTimeout time.Duration) {
	if !r.closed.CompareAndSwap(false, true) {
		return
	}
	if r.pool != nil {
		_ = r.pool.DrainAndClose(drainTimeout)
	}
}

// --- Host RPC handlers bridged into JS ctx ---

func (r *pluginRuntime) hostRegisterHook(_ context.Context, args []any) (any, error) {
	if len(args) < 4 {
		return nil, errors.New("registerHook requires (kind, name, order, fnId)")
	}
	kindStr, _ := args[0].(string)
	name, _ := args[1].(string)
	order := int(asFloat(args[2]))
	fnID := int(asFloat(args[3]))

	var kind hook.Kind
	switch kindStr {
	case "guard":
		kind = hook.KindGuard
	case "filter":
		kind = hook.KindFilter
	case "event":
		kind = hook.KindEvent
	default:
		return nil, fmt.Errorf("unknown hook kind %q", kindStr)
	}
	if !r.manifest.HasHook(kind, name) {
		return nil, fmt.Errorf("%w: hook %q (%s) is not declared in plugin.json", ErrPermissionDenied, name, kindStr)
	}

	r.setupMu.Lock()
	defer r.setupMu.Unlock()
	if r.collecting {
		r.collectedHooks = append(r.collectedHooks, hookBinding{
			Kind:  kind,
			Name:  name,
			Order: order,
			FnID:  fnID,
		})
	}
	return nil, nil
}

func (r *pluginRuntime) hostRegisterRoute(_ context.Context, args []any) (any, error) {
	if len(args) < 4 {
		return nil, errors.New("registerRoute requires (method, path, auth, fnId)")
	}
	method := strings.ToUpper(fmt.Sprint(args[0]))
	routePath, _ := args[1].(string)
	authLevel, _ := args[2].(string)
	fnID := int(asFloat(args[3]))
	if authLevel == "" {
		authLevel = "public"
	}
	switch authLevel {
	case "public", "user", "admin", "superadmin":
	default:
		return nil, fmt.Errorf("invalid route auth level %q", authLevel)
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}

	r.setupMu.Lock()
	defer r.setupMu.Unlock()
	if r.collecting {
		r.collectedRoute = append(r.collectedRoute, routeBinding{
			Method:  method,
			Pattern: routePath,
			Auth:    authLevel,
			FnID:    fnID,
		})
	}
	return nil, nil
}

func (r *pluginRuntime) hostRegisterOnChange(_ context.Context, _ []any) (any, error) {
	r.setupMu.Lock()
	r.hasOnChange = true
	r.setupMu.Unlock()
	return nil, nil
}

func (r *pluginRuntime) hostEmitEvent(_ context.Context, args []any) (any, error) {
	if len(args) < 1 {
		return nil, errors.New("events.emit requires event name")
	}
	localName, _ := args[0].(string)
	if localName == "" || strings.Contains(localName, ":") {
		return nil, errors.New("events.emit expects a local event name without prefix")
	}
	var payload any
	if len(args) > 1 {
		payload = args[1]
	}
	if r.emitLocalEvent != nil {
		r.emitLocalEvent("plugin:"+r.id+"."+localName, payload)
	}
	return nil, nil
}

func (r *pluginRuntime) hostKV(ctx context.Context, args []any) (any, error) {
	if len(args) < 2 {
		return nil, errors.New("kv call missing op or key")
	}
	op, _ := args[0].(string)
	key, _ := args[1].(string)
	switch op {
	case "get":
		return r.kv.Get(ctx, key)
	case "set":
		var val any
		var ttl float64
		if len(args) > 2 {
			val = args[2]
		}
		if len(args) > 3 {
			ttl = asFloat(args[3])
		}
		return nil, r.kv.Set(ctx, key, val, ttl)
	case "delete":
		return r.kv.Delete(ctx, key)
	case "list":
		var limit int
		if len(args) > 2 {
			limit = int(asFloat(args[2]))
		}
		return r.kv.List(ctx, key, limit)
	case "incr":
		delta := 1.0
		if len(args) > 2 && args[2] != nil {
			delta = asFloat(args[2])
		}
		return r.kv.Incr(ctx, key, delta)
	default:
		return nil, fmt.Errorf("unknown kv op %q", op)
	}
}

func (r *pluginRuntime) hostDB(ctx context.Context, args []any) (any, error) {
	if len(args) < 2 {
		return nil, errors.New("db call missing op or collection")
	}
	op, _ := args[0].(string)
	collName, _ := args[1].(string)
	coll, err := r.docs.Collection(collName)
	if err != nil {
		return nil, err
	}
	switch op {
	case "insertOne":
		doc, _ := asMap(argAt(args, 2))
		return coll.InsertOne(ctx, doc)
	case "findOne":
		filter, _ := asMap(argAt(args, 2))
		return coll.FindOne(ctx, filter)
	case "find":
		filter, _ := asMap(argAt(args, 2))
		opts := FindOpts{}
		if m, ok := asMap(argAt(args, 3)); ok {
			if l, has := m["limit"]; has {
				opts.Limit = int(asFloat(l))
			}
			if c, has := m["cursor"].(string); has {
				opts.Cursor = c
			}
			if s, ok := m["sort"].(map[string]any); ok {
				opts.Sort = make(map[string]int, len(s))
				for k, v := range s {
					opts.Sort[k] = int(asFloat(v))
				}
			}
		}
		return coll.Find(ctx, filter, opts)
	case "updateOne":
		filter, _ := asMap(argAt(args, 2))
		patch, _ := asMap(argAt(args, 3))
		matched, err := coll.UpdateOne(ctx, filter, patch)
		return map[string]any{"matched": matched, "modified": matched}, err
	case "updateMany":
		filter, _ := asMap(argAt(args, 2))
		patch, _ := asMap(argAt(args, 3))
		cnt, err := coll.UpdateMany(ctx, filter, patch)
		return map[string]any{"matched": cnt, "modified": cnt}, err
	case "deleteOne":
		filter, _ := asMap(argAt(args, 2))
		deleted, err := coll.DeleteOne(ctx, filter)
		return map[string]any{"deleted": deleted}, err
	case "count":
		filter, _ := asMap(argAt(args, 2))
		return coll.Count(ctx, filter)
	default:
		return nil, fmt.Errorf("unknown db op %q", op)
	}
}

func (r *pluginRuntime) hostFiles(ctx context.Context, args []any) (any, error) {
	if len(args) < 2 {
		return nil, errors.New("files call missing op or key")
	}
	op, _ := args[0].(string)
	key, _ := args[1].(string)
	switch op {
	case "put":
		var data []byte
		switch v := argAt(args, 2).(type) {
		case []byte:
			data = v
		case string:
			data = []byte(v)
		case nil:
			data = nil
		default:
			return nil, fmt.Errorf("files.put expects Uint8Array or string, got %T", v)
		}
		return r.files.Put(ctx, key, data)
	case "get":
		data, err := r.files.Get(ctx, key)
		if errors.Is(err, ErrFileNotFound) {
			return nil, nil
		}
		return data, err
	case "delete":
		return r.files.Delete(ctx, key)
	case "url":
		return r.files.URL(key)
	default:
		return nil, fmt.Errorf("unknown files op %q", op)
	}
}

func (r *pluginRuntime) hostFetch(ctx context.Context, args []any) (any, error) {
	rawURL, _ := argAt(args, 0).(string)
	req := FetchRequest{}
	if m, ok := asMap(argAt(args, 1)); ok {
		if meth, ok := m["method"].(string); ok {
			req.Method = meth
		}
		if body, ok := m["body"].(string); ok {
			req.Body = body
		}
		if t, has := m["timeoutMs"]; has {
			req.Timeout = int(asFloat(t))
		}
		if h, ok := m["headers"].(map[string]any); ok {
			req.Headers = make(map[string]string, len(h))
			for k, v := range h {
				req.Headers[k] = fmt.Sprint(v)
			}
		}
	}
	return r.fetch.Fetch(ctx, rawURL, req)
}

func (r *pluginRuntime) hostSettings(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	switch op {
	case "get":
		key, _ := argAt(args, 1).(string)
		return r.settings[key], nil
	case "all":
		return cloneMap(r.settings), nil
	default:
		return nil, fmt.Errorf("unknown settings op %q", op)
	}
}

func (r *pluginRuntime) hostSite(ctx context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	locale, _ := argAt(args, 1).(string)
	if locale == "" {
		locale = requestLocaleFrom(ctx)
	}
	if r.siteSettings == nil {
		switch op {
		case "locales":
			return []string{"zh-CN", "en"}, nil
		case "public":
			return map[string]any{}, nil
		default:
			return "", nil
		}
	}
	snap := r.siteSettings.Current()
	switch op {
	case "name":
		return snap.Localized("site.name", locale), nil
	case "url":
		return snap.String("site.url"), nil
	case "description":
		return snap.Localized("site.description", locale), nil
	case "locales":
		return []string{"zh-CN", "en"}, nil
	case "public":
		out := map[string]any{}
		for _, def := range r.siteSettings.Registry().All() {
			if !def.Public || def.Secret {
				continue
			}
			var val any
			if err := json.Unmarshal(snap.Raw(def.Key), &val); err == nil {
				out[def.Key] = val
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown site op %q", op)
	}
}

func (r *pluginRuntime) hostCrypto(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	switch op {
	case "randomBytes":
		n := int(asFloat(argAt(args, 1)))
		if n < 0 || n > 65536 {
			return nil, errors.New("randomBytes length must be 0..65536")
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		return b, nil
	case "randomString":
		n := int(asFloat(argAt(args, 1)))
		if n <= 0 {
			n = 16
		}
		if n > 256 {
			n = 256
		}
		const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
		raw := make([]byte, n)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		for i := range raw {
			raw[i] = alphabet[int(raw[i])%len(alphabet)]
		}
		return string(raw), nil
	case "sha256":
		sum := sha256.Sum256(argToBytes(argAt(args, 1)))
		return hex.EncodeToString(sum[:]), nil
	case "hmac":
		algo, _ := argAt(args, 1).(string)
		key := argToBytes(argAt(args, 2))
		data := argToBytes(argAt(args, 3))
		var hFn func() hash.Hash
		switch strings.ToLower(algo) {
		case "", "sha256":
			hFn = sha256.New
		case "sha512":
			hFn = sha512.New
		case "sha1":
			hFn = sha1.New
		default:
			return nil, fmt.Errorf("unsupported hmac algorithm %q", algo)
		}
		mac := hmac.New(hFn, key)
		mac.Write(data)
		return hex.EncodeToString(mac.Sum(nil)), nil
	case "timingSafeEqual":
		a := argToBytes(argAt(args, 1))
		b := argToBytes(argAt(args, 2))
		return subtle.ConstantTimeCompare(a, b) == 1, nil
	case "uuid":
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%s-%s-%s-%s-%s",
			hex.EncodeToString(b[0:4]),
			hex.EncodeToString(b[4:6]),
			hex.EncodeToString(b[6:8]),
			hex.EncodeToString(b[8:10]),
			hex.EncodeToString(b[10:16]),
		), nil
	default:
		return nil, fmt.Errorf("unknown crypto op %q", op)
	}
}

func (r *pluginRuntime) hostCache(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	key, _ := argAt(args, 1).(string)
	now := time.Now()
	switch op {
	case "get":
		r.cacheMu.RLock()
		it, ok := r.cache[key]
		r.cacheMu.RUnlock()
		if !ok || (!it.expiresAt.IsZero() && !it.expiresAt.After(now)) {
			if ok {
				r.cacheMu.Lock()
				delete(r.cache, key)
				r.cacheMu.Unlock()
			}
			return nil, nil
		}
		return it.value, nil
	case "set":
		val := argAt(args, 2)
		ttlSec := asFloat(argAt(args, 3))
		var exp time.Time
		if ttlSec > 0 {
			exp = now.Add(time.Duration(ttlSec * float64(time.Second)))
		}
		r.cacheMu.Lock()
		if len(r.cache) >= maxCacheEntries {
			for k, item := range r.cache {
				if !item.expiresAt.IsZero() && !item.expiresAt.After(now) {
					delete(r.cache, k)
				}
			}
			if len(r.cache) >= maxCacheEntries {
				for k := range r.cache {
					delete(r.cache, k)
					break
				}
			}
		}
		r.cache[key] = cacheItem{value: val, expiresAt: exp}
		r.cacheMu.Unlock()
		return nil, nil
	case "delete":
		r.cacheMu.Lock()
		_, existed := r.cache[key]
		delete(r.cache, key)
		r.cacheMu.Unlock()
		return existed, nil
	default:
		return nil, fmt.Errorf("unknown cache op %q", op)
	}
}

func (r *pluginRuntime) hostI18n(ctx context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	if op == "locales" {
		r.localesMu.RLock()
		out := make([]string, 0, len(r.locales))
		for k := range r.locales {
			out = append(out, k)
		}
		r.localesMu.RUnlock()
		sort.Strings(out)
		return out, nil
	}
	key, _ := argAt(args, 1).(string)
	params, _ := asMap(argAt(args, 2))
	locale, _ := argAt(args, 3).(string)
	if locale == "" {
		locale = requestLocaleFrom(ctx)
	}
	return r.translate(key, params, locale), nil
}

func (r *pluginRuntime) translate(key string, params map[string]any, locale string) string {
	r.localesMu.RLock()
	defer r.localesMu.RUnlock()

	tryLocales := []string{}
	if locale != "" {
		tryLocales = append(tryLocales, locale)
		if base, _, ok := strings.Cut(locale, "-"); ok {
			tryLocales = append(tryLocales, base)
		}
	}
	tryLocales = append(tryLocales, "en", "zh-CN")

	var tmpl string
	for _, loc := range tryLocales {
		if m, ok := r.locales[loc]; ok {
			if s, ok := m[key]; ok {
				tmpl = s
				break
			}
		}
	}
	if tmpl == "" {
		tmpl = key
	}
	for k, v := range params {
		s := fmt.Sprint(v)
		tmpl = strings.ReplaceAll(tmpl, "{{"+k+"}}", s)
		tmpl = strings.ReplaceAll(tmpl, "{"+k+"}", s)
	}
	return tmpl
}

func (r *pluginRuntime) hostLog(_ context.Context, args []any) (any, error) {
	level, _ := argAt(args, 0).(string)
	msg, _ := argAt(args, 1).(string)
	switch level {
	case "debug":
		r.log.Debug(msg)
	case "warn":
		r.log.Warn(msg)
	case "error":
		r.log.Error(msg)
	default:
		level = "info"
		r.log.Info(msg)
	}
	r.logsMu.Lock()
	r.logs = append(r.logs, LogEntry{Time: time.Now().UTC(), Level: level, Message: msg})
	if len(r.logs) > maxLogEntries {
		r.logs = r.logs[len(r.logs)-maxLogEntries:]
	}
	r.logsMu.Unlock()
	return nil, nil
}

func (r *pluginRuntime) recentLogs() []LogEntry {
	r.logsMu.Lock()
	defer r.logsMu.Unlock()
	return append([]LogEntry(nil), r.logs...)
}

func loadPluginLocales(dir string, staticFiles map[string][]byte) map[string]map[string]string {
	out := map[string]map[string]string{}
	parseJSON := func(lang string, data []byte) {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		flat := map[string]string{}
		flattenStrings("", raw, flat)
		if len(flat) > 0 {
			out[lang] = flat
		}
	}
	for k, v := range staticFiles {
		clean := strings.TrimPrefix(k, "/")
		if strings.HasPrefix(clean, "locales/") && strings.HasSuffix(clean, ".json") {
			lang := strings.TrimSuffix(strings.TrimPrefix(clean, "locales/"), ".json")
			parseJSON(lang, v)
		}
	}
	if dir != "" {
		locDir := filepath.Join(dir, "locales")
		_ = filepath.WalkDir(locDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			lang := strings.TrimSuffix(d.Name(), ".json")
			parseJSON(lang, data)
			return nil
		})
	}
	return out
}

func flattenStrings(prefix string, m map[string]any, dst map[string]string) {
	for k, v := range m {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			dst[full] = val
		case map[string]any:
			flattenStrings(full, val, dst)
		}
	}
}

func mergeDefaultSettings(schemaMap map[string]any, saved map[string]any) map[string]any {
	out := map[string]any{}
	for k, rawDef := range schemaMap {
		if m, ok := rawDef.(map[string]any); ok {
			if def, has := m["default"]; has {
				out[k] = def
			}
		}
	}
	for k, v := range saved {
		out[k] = v
	}
	return out
}

func argAt(args []any, idx int) any {
	if idx < 0 || idx >= len(args) {
		return nil
	}
	return args[idx]
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}

func asMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func argToBytes(v any) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		return nil
	}
}

func pluginPreludeJS() string {
	return `(() => {
  const handlers = [];
  const settingsListeners = [];
  let nextFnId = 0;

  function registerHandler(fn) {
    const id = nextFnId++;
    handlers[id] = fn;
    return id;
  }

  function decodeBase64(b64) {
    const table = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
    const clean = String(b64 || '').replace(/[^A-Za-z0-9+/=]/g, '');
    const len = clean.length;
    if (len === 0) return new Uint8Array(0);
    let pad = 0;
    if (clean[len - 1] === '=') pad++;
    if (clean[len - 2] === '=') pad++;
    const outLen = ((len * 3) >> 2) - pad;
    const out = new Uint8Array(outLen);
    let p = 0;
    for (let i = 0; i < len; i += 4) {
      const a = table.indexOf(clean[i]);
      const b = table.indexOf(clean[i + 1]);
      const c = clean[i + 2] === '=' ? 0 : table.indexOf(clean[i + 2]);
      const d = clean[i + 3] === '=' ? 0 : table.indexOf(clean[i + 3]);
      const n = (a << 18) | (b << 12) | (c << 6) | d;
      if (p < outLen) out[p++] = (n >> 16) & 255;
      if (p < outLen) out[p++] = (n >> 8) & 255;
      if (p < outLen) out[p++] = n & 255;
    }
    return out;
  }

  globalThis.reject = (reason) => ({
    __skinloomReject: true,
    reason: String(reason ?? 'rejected'),
  });

  function wrapHttpMethod(method) {
    return (path, optsOrFn, maybeFn) => {
      let opts = {};
      let fn = optsOrFn;
      if (typeof optsOrFn !== 'function') {
        opts = optsOrFn || {};
        fn = maybeFn;
      }
      if (typeof fn !== 'function') {
        throw new TypeError('HTTP route handler must be a function');
      }
      const id = registerHandler(fn);
      return __sl_setup_route(method, String(path), String(opts.auth || 'public'), id);
    };
  }

  const ctx = {
    hooks: {
      reject: globalThis.reject,
      guard(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('guard handler must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('guard', String(name), Number(order) || 0, id);
      },
      filter(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('filter handler must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('filter', String(name), Number(order) || 0, id);
      },
    },
    events: {
      on(name, fn, order = 0) {
        if (typeof fn !== 'function') throw new TypeError('event listener must be a function');
        const id = registerHandler(fn);
        return __sl_setup_hook('event', String(name), Number(order) || 0, id);
      },
      emit(localName, payload) {
        return __sl_event_emit(String(localName), payload ?? null);
      },
    },
    http: {
      get: wrapHttpMethod('GET'),
      post: wrapHttpMethod('POST'),
      put: wrapHttpMethod('PUT'),
      patch: wrapHttpMethod('PATCH'),
      delete: wrapHttpMethod('DELETE'),
      route(method, path, optsOrFn, maybeFn) {
        return wrapHttpMethod(String(method).toUpperCase())(path, optsOrFn, maybeFn);
      },
    },
    kv: {
      get: (key) => __sl_kv('get', String(key)),
      set: (key, value, ttlSeconds = 0) => __sl_kv('set', String(key), value ?? null, Number(ttlSeconds) || 0),
      delete: (key) => __sl_kv('delete', String(key)),
      list: (prefix = '', limit = 100) => __sl_kv('list', String(prefix), Number(limit) || 100),
      incr: (key, delta = 1) => __sl_kv('incr', String(key), delta === undefined ? 1 : Number(delta)),
    },
    db: {
      collection(name) {
        const c = String(name);
        return {
          insertOne: (doc) => __sl_db('insertOne', c, doc || {}),
          findOne: (filter = {}) => __sl_db('findOne', c, filter || {}),
          find: (filter = {}, opts = {}) => __sl_db('find', c, filter || {}, opts || {}),
          updateOne: (filter, patch) => __sl_db('updateOne', c, filter || {}, patch || {}),
          updateMany: (filter, patch) => __sl_db('updateMany', c, filter || {}, patch || {}),
          deleteOne: (filter) => __sl_db('deleteOne', c, filter || {}),
          count: (filter = {}) => __sl_db('count', c, filter || {}),
        };
      },
    },
    files: {
      put: (key, data) => __sl_files('put', String(key), data),
      get: (key) => __sl_files('get', String(key)),
      delete: (key) => __sl_files('delete', String(key)),
      url: (key) => __sl_files('url', String(key)),
    },
    async fetch(url, init = {}) {
      const resp = await __sl_fetch(String(url), {
        method: init?.method || 'GET',
        headers: init?.headers || {},
        body: init?.body != null ? String(init.body) : '',
        timeoutMs: Number(init?.timeoutMs) || 0,
      });
      return {
        ok: Boolean(resp.ok),
        status: resp.status,
        statusText: resp.statusText,
        headers: resp.headers || {},
        url: resp.url,
        async text() { return resp.bodyText ?? ''; },
        async json() { return JSON.parse(resp.bodyText ?? 'null'); },
        async arrayBuffer() { return decodeBase64(resp.bodyBase64).buffer; },
      };
    },
    settings: {
      get: (key) => __sl_settings('get', String(key)),
      all: () => __sl_settings('all'),
      onChange(fn) {
        if (typeof fn !== 'function') throw new TypeError('onChange expects a function');
        settingsListeners.push(fn);
        return __sl_setup_onChange();
      },
    },
    site: {
      name: (locale = '') => __sl_site('name', String(locale || '')),
      url: () => __sl_site('url'),
      description: (locale = '') => __sl_site('description', String(locale || '')),
      locales: () => __sl_site('locales'),
      publicSettings: () => __sl_site('public'),
    },
    crypto: {
      randomString: (len = 16) => __sl_crypto('randomString', Number(len) || 16),
      randomBytes: (len) => __sl_crypto('randomBytes', Number(len) || 0),
      sha256: (data) => __sl_crypto('sha256', data),
      hmac: (algo, key, data) => __sl_crypto('hmac', String(algo || 'sha256'), key, data),
      timingSafeEqual: (a, b) => __sl_crypto('timingSafeEqual', a, b),
      uuid: () => __sl_crypto('uuid'),
    },
    t: (key, params, locale = '') => __sl_i18n('t', String(key), params || null, String(locale || '')),
    i18n: {
      t: (key, params, locale = '') => __sl_i18n('t', String(key), params || null, String(locale || '')),
      locales: () => __sl_i18n('locales'),
    },
    log: {
      debug: (...a) => __sl_log('debug', a.map(String).join(' ')),
      info: (...a) => __sl_log('info', a.map(String).join(' ')),
      warn: (...a) => __sl_log('warn', a.map(String).join(' ')),
      error: (...a) => __sl_log('error', a.map(String).join(' ')),
    },
    cache: {
      get: (key) => __sl_cache('get', String(key)),
      set: (key, val, ttlSeconds = 0) => __sl_cache('set', String(key), val ?? null, Number(ttlSeconds) || 0),
      delete: (key) => __sl_cache('delete', String(key)),
    },
  };

  globalThis.fetch = ctx.fetch;
  globalThis.definePlugin = (def) => def;

  globalThis.__sl_run_setup = async () => {
    const mod = globalThis.__jsrt_module_default;
    if (!mod) {
      return;
    }
    if (typeof mod === 'function') {
      await mod(ctx);
      return;
    }
    if (typeof mod.setup === 'function') {
      await mod.setup(ctx);
    }
  };

  globalThis.__sl_invoke_handler = async (id, ...args) => {
    const fn = handlers[id];
    if (typeof fn !== 'function') {
      throw new Error('missing plugin handler #' + id);
    }
    return await fn(...args);
  };

  globalThis.__sl_run_lifecycle = async (method, arg) => {
    const mod = globalThis.__jsrt_module_default;
    if (!mod || typeof mod[method] !== 'function') {
      return null;
    }
    if (arg !== undefined && arg !== null) {
      return await mod[method](ctx, arg);
    }
    return await mod[method](ctx);
  };

  globalThis.__sl_fire_settings_change = async (nextSettings) => {
    for (const fn of settingsListeners) {
      await fn(nextSettings);
    }
  };
})();`
}
