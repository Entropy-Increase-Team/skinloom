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

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

const (
	defaultGuardTimeout = 2 * time.Second
	defaultRouteTimeout = 30 * time.Second
	defaultHookTimeout  = 10 * time.Second
	defaultCronTimeout  = 5 * time.Minute
	maxCacheEntries     = 1024
	maxLogEntries       = 200
)

type hookBinding struct {
	Kind      hook.Kind
	Name      string
	Order     int
	FnID      int
	HandlerID string
}

type routeBinding struct {
	Method      string `json:"method"`
	Pattern     string `json:"pattern"`
	Auth        string `json:"auth"` // public | user | admin | superadmin
	StreamProxy bool   `json:"streamProxy,omitempty"`
	FnID        int    `json:"-"`
	HandlerID   string `json:"-"`
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
		return "zh-CN"
	}
	if l, ok := ctx.Value(localeContextKey{}).(string); ok && l != "" {
		return l
	}
	return "zh-CN"
}

// pluginRuntime owns one enabled plugin's isolate pool, permissions, storage
// backends, routes, hook subscriptions, and circuit breaker.
type pluginRuntime struct {
	id          string
	rev         string
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

	db           *ent.Client
	bus          *hook.Bus
	siteSettings *settings.Service
	accounts     *account.Service
	players      *player.Service
	textures     *texture.Service
	notify       *notify.Service
	mailer       *mail.Mailer
	captcha      *captcha.Service
	cdn          *cdn.Service
	yggdrasil    *yggdrasil.Service

	localesMu sync.RWMutex
	locales   map[string]map[string]string

	settingsMu    sync.RWMutex
	settings      map[string]any
	rawSettings   map[string]any
	packageSHA256 string
	grantedPerms  []string

	cacheMu sync.RWMutex
	cache   map[string]cacheItem

	logsMu sync.Mutex
	logs   []LogEntry

	setupMu        sync.Mutex
	collecting     bool
	collectedHooks []hookBinding
	collectedRoute []routeBinding
	authProviders  []authProviderBinding
	verifiers      []passwordVerifierBinding
	captchas       []captchaBinding
	cdns           []cdnBinding
	yggMetas       []yggMetaBinding
	crons          []cronBinding
	services       []serviceBinding
	hasOnChange    bool

	storageProvider storage.Store
	searchProvider  texture.SearchProvider
	renderProvider  render.Provider
	dbConfig        *PluginDBConfig

	routes []routeBinding
	driver Driver

	emitLocalEvent func(eventName string, payload any)
	callService    func(ctx context.Context, targetID, svcName string, args ...any) (any, error)
	onTrip         func(id string, rt *pluginRuntime, err error)
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
	perms := NewPermissionSet(manifest.EffectivePermissions(), grantedPerms)

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
		rev:          manifest.Version,
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
		db:           mgr.db,
		bus:          mgr.bus,
		siteSettings: mgr.settings,
		accounts:     mgr.accounts,
		players:      mgr.players,
		textures:     mgr.textures,
		notify:       mgr.notify,
		mailer:       mgr.mailer,
		captcha:      mgr.captcha,
		cdn:          mgr.cdn,
		yggdrasil:    mgr.yggdrasil,
		locales:      loadPluginLocales(dir, staticFiles),
		settings:     mergeDefaultSettings(manifest.Settings, initialSettings),
		rawSettings:  cloneMap(initialSettings),
		grantedPerms: append([]string(nil), grantedPerms...),
		cache:        make(map[string]cacheItem),
		emitLocalEvent: func(eventName string, payload any) {
			if mgr.bus != nil {
				hook.EmitDynamic(mgr.bus, eventName, payload)
			}
		},
		callService: func(ctx context.Context, targetID, svcName string, args ...any) (any, error) {
			return mgr.CallService(ctx, manifest.ID, targetID, svcName, args...)
		},
		onTrip: mgr.handleCircuitTrip,
	}
	return rt, nil
}

func (r *pluginRuntime) initDriver(ctx context.Context, drv Driver, dbCfg *PluginDBConfig, dataDir, runtimeDir string) error {
	r.driver = drv
	r.dbConfig = dbCfg
	if drv == nil {
		return nil
	}
	regs, err := drv.Init(ctx, DriverInitConfig{
		Runtime:    r,
		Settings:   r.currentSettingsCopy(),
		DBConfig:   dbCfg,
		DataDir:    dataDir,
		RuntimeDir: runtimeDir,
	})
	if err != nil {
		return err
	}
	if regs == nil {
		return nil
	}
	r.setupMu.Lock()
	r.collectedHooks = append([]hookBinding(nil), regs.Hooks...)
	r.collectedRoute = append([]routeBinding(nil), regs.Routes...)
	r.routes = append([]routeBinding(nil), regs.Routes...)
	r.authProviders = append([]authProviderBinding(nil), regs.AuthProviders...)
	r.verifiers = append([]passwordVerifierBinding(nil), regs.PasswordVerifiers...)
	r.captchas = append([]captchaBinding(nil), regs.CaptchaProviders...)
	r.cdns = append([]cdnBinding(nil), regs.CDNProviders...)
	r.yggMetas = append([]yggMetaBinding(nil), regs.YggdrasilMetas...)
	r.crons = append([]cronBinding(nil), regs.Crons...)
	r.services = append([]serviceBinding(nil), regs.Services...)
	r.hasOnChange = regs.HasSettingsOnChange
	r.storageProvider = regs.StorageProvider
	r.searchProvider = regs.SearchProvider
	r.renderProvider = regs.RenderProvider
	r.setupMu.Unlock()
	return nil
}

func (r *pluginRuntime) currentSettingsCopy() map[string]any {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	return cloneMap(r.settings)
}

func (r *pluginRuntime) buildHookRegistrations() ([]hook.Registration, error) {
	r.setupMu.Lock()
	specs := append([]hookBinding(nil), r.collectedHooks...)
	r.setupMu.Unlock()

	regs := make([]hook.Registration, 0, len(specs))
	for _, spec := range specs {
		sp := spec
		hid := resolveHandlerID(sp.HandlerID, sp.FnID)
		switch sp.Kind {
		case hook.KindGuard:
			reg, err := hook.HandleGuardDynamic(sp.Name, sp.Order, func(ctx context.Context, payload any) error {
				return r.invokeGuard(ctx, hid, sp.Name, payload)
			})
			if err != nil {
				return nil, err
			}
			regs = append(regs, reg)
		case hook.KindFilter:
			reg, err := hook.HandleFilterDynamic(sp.Name, sp.Order, func(ctx context.Context, value, payload any) (any, error) {
				return r.invokeFilter(ctx, hid, sp.Name, value, payload)
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
				return r.invokeEvent(ctx, hid, sp.Name, payload)
			})
			if err != nil {
				return nil, err
			}
			regs = append(regs, reg)
		}
	}
	return regs, nil
}

func (r *pluginRuntime) invokeGuard(ctx context.Context, handlerID, hookName string, payload any) error {
	if r.closed.Load() || r.driver == nil {
		return errors.New("plugin runtime not active")
	}
	return r.driver.InvokeGuard(ctx, handlerID, hookName, payload)
}

func (r *pluginRuntime) invokeFilter(ctx context.Context, handlerID, hookName string, value, payload any) (any, error) {
	if r.closed.Load() || r.driver == nil {
		return nil, errors.New("plugin runtime not active")
	}
	return r.driver.InvokeFilter(ctx, handlerID, hookName, value, payload)
}

func (r *pluginRuntime) invokeEvent(ctx context.Context, handlerID, hookName string, payload any) error {
	if r.closed.Load() || r.driver == nil {
		return errors.New("plugin runtime not active")
	}
	return r.driver.InvokeEvent(ctx, handlerID, hookName, payload)
}

func (r *pluginRuntime) invokeRoute(ctx context.Context, handlerID string, req map[string]any) (any, error) {
	if r.closed.Load() || r.driver == nil {
		return nil, errors.New("plugin runtime not active")
	}
	return r.driver.InvokeHTTP(ctx, handlerID, req)
}

func (r *pluginRuntime) invokeCron(ctx context.Context, handlerID, jobName string) error {
	if r.closed.Load() || r.driver == nil {
		return errors.New("plugin runtime not active")
	}
	return r.driver.InvokeCron(ctx, handlerID, jobName)
}

func (r *pluginRuntime) invokeService(ctx context.Context, handlerID, serviceName, callerID string, args ...any) (any, error) {
	if r.closed.Load() || r.driver == nil {
		return nil, errors.New("plugin runtime not active")
	}
	return r.driver.InvokeService(ctx, handlerID, serviceName, callerID, args...)
}

func (r *pluginRuntime) invokeExtension(ctx context.Context, kind, id, handlerID string, args ...any) (any, error) {
	if r.closed.Load() || r.driver == nil {
		return nil, errors.New("plugin runtime not active")
	}
	return r.driver.InvokeExtension(ctx, kind, id, handlerID, args...)
}

func (r *pluginRuntime) invokeLifecycle(ctx context.Context, method string, arg any) error {
	if r.driver == nil || r.closed.Load() {
		return nil
	}
	return r.driver.Lifecycle(ctx, method, arg)
}

func (r *pluginRuntime) recordRuntimeFailure(err error) {
	if r.closed.Load() {
		return
	}
	if r.circuit.RecordFailure(err) && r.onTrip != nil {
		go r.onTrip(r.id, r, err)
	}
}

func (r *pluginRuntime) notifySettingsChanged(ctx context.Context, newSettings map[string]any) {
	merged := mergeDefaultSettings(r.manifest.Settings, newSettings)
	r.settingsMu.Lock()
	r.rawSettings = cloneMap(newSettings)
	r.settings = merged
	r.settingsMu.Unlock()

	r.setupMu.Lock()
	listening := r.hasOnChange
	r.setupMu.Unlock()
	// Always push updated settings to Tier 2 native plugins so their in-process
	// Settings cache stays fresh even when no OnChange callback is registered.
	if (!listening && r.manifest.EffectiveRuntime() != RuntimeNative) || r.driver == nil || r.closed.Load() {
		return
	}
	_ = r.driver.NotifySettings(ctx, cloneMap(merged))
}

func (r *pluginRuntime) close(drainTimeout time.Duration) {
	if !r.closed.CompareAndSwap(false, true) {
		return
	}
	if r.driver != nil {
		_ = r.driver.DrainAndClose(drainTimeout)
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
		const maxUnbiased = 248 // 62 * 4: reject >= 248 to avoid modulo bias
		out := make([]byte, n)
		buf := make([]byte, n*2)
		filled := 0
		for filled < n {
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			for _, b := range buf {
				if b >= maxUnbiased {
					continue
				}
				out[filled] = alphabet[int(b)%len(alphabet)]
				filled++
				if filled == n {
					break
				}
			}
		}
		return string(out), nil
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
				if cur, exists := r.cache[key]; exists && !cur.expiresAt.IsZero() && !cur.expiresAt.After(now) {
					delete(r.cache, key)
				}
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
	addUnique := func(cand string) {
		if cand == "" {
			return
		}
		for _, existing := range tryLocales {
			if existing == cand {
				return
			}
		}
		tryLocales = append(tryLocales, cand)
	}
	keys := make([]string, 0, len(r.locales))
	for k := range r.locales {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	norm := normalizeLocaleTag(locale)
	if norm != "" {
		addUnique(norm)
		for _, k := range keys {
			if strings.EqualFold(normalizeLocaleTag(k), norm) {
				addUnique(k)
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(norm), "-")
		if base != "" {
			addUnique(base)
			if base == "zh" {
				addUnique("zh-CN")
			}
			for _, k := range keys {
				kb, _, _ := strings.Cut(strings.ToLower(normalizeLocaleTag(k)), "-")
				if kb == base {
					addUnique(k)
				}
			}
		}
	}
	addUnique("zh-CN")
	addUnique("en")

	var tmpl string
	for _, loc := range tryLocales {
		if m, ok := r.locales[loc]; ok {
			if s, ok := m[key]; ok && s != "" {
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
	r.appendLog(level, msg)
	return nil, nil
}

func (r *pluginRuntime) appendLog(level, msg string) {
	msg = strings.TrimRight(msg, "\r\n")
	if msg == "" {
		return
	}
	if len(msg) > 4096 {
		msg = msg[:4096] + "..."
	}
	switch strings.ToLower(level) {
	case "debug", "trace":
		level = "debug"
		r.log.Debug(msg)
	case "warn", "warning":
		level = "warn"
		r.log.Warn(msg)
	case "error":
		level = "error"
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
		m, ok := rawDef.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(strVal(m["type"])))
		if typ == "group" {
			subSchema, _ := m["fields"].(map[string]any)
			subSaved, _ := saved[k].(map[string]any)
			subMerged := mergeDefaultSettings(subSchema, subSaved)
			if len(subMerged) > 0 {
				out[k] = subMerged
			}
			continue
		}
		if def, has := m["default"]; has {
			out[k] = cloneJSONValue(def)
		}
	}
	for k, v := range saved {
		if rawDef, ok := schemaMap[k].(map[string]any); ok {
			if strings.EqualFold(strVal(rawDef["type"]), "group") {
				continue
			}
		}
		out[k] = cloneJSONValue(v)
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
