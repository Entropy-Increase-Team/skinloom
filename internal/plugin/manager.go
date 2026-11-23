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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/plugindoc"
	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

const (
	defaultRouteMaxBody = 1 * 1024 * 1024
	hardMaxRouteBody    = 10 * 1024 * 1024
	defaultDrainTimeout = 30 * time.Second
)

// Viewer describes the authenticated caller of /api/plugins/<id>/*.
type Viewer struct {
	Authenticated     bool
	UserID            int64
	Email             string
	Nickname          string
	Role              string // user | admin | superadmin
	Admin             bool
	Superadmin        bool
	HasHostAuthHeader bool // true when request carried host AdminToken or host Bearer (slo_/slp_/slr_)
}

// PlatformPackageFetcher downloads a platform-specific .slp archive from a
// registry source during multi-arch SyncCluster when the shared storage.Store
// does not yet contain a package for this node's GOOS/GOARCH.
type PlatformPackageFetcher func(ctx context.Context, registrySource, pluginID, version, platform string) ([]byte, error)

// ManagerDeps injects host services required by the plugin runtime.
type ManagerDeps struct {
	DB                     *ent.Client
	Bus                    *hook.Bus
	Settings               *settings.Service
	Accounts               *account.Service
	Players                *player.Service
	Textures               *texture.Service
	Renders                *render.Service
	Notify                 *notify.Service
	Mailer                 *mail.Mailer
	Captcha                *captcha.Service
	CDN                    *cdn.Service
	Yggdrasil              *yggdrasil.Service
	Store                  storage.Store
	LegacyStore            storage.Store // optional v1.1 texture store fallback for one-time .slp migration
	StorageHook            *storage.SwappableStore
	Themes                 *theme.Registry
	Engine                 jsrt.Engine
	DBAllocator            *DBAllocator
	DataDir                string
	HostPlatform           string // optional override (defaults to runtime.GOOS+"/"+runtime.GOARCH)
	TrustedPublicKeys      []string
	AllowUnsignedNative    bool
	NativeDriverFactory    func(manifest *Manifest, dir string) Driver
	PlatformPackageFetcher PlatformPackageFetcher
	Log                    *slog.Logger
}

// PluginInfo summarizes an installed plugin's manifest and live runtime state.
type PluginInfo struct {
	ID                 string            `json:"id"`
	Kind               string            `json:"kind"`
	Runtime            string            `json:"runtime,omitempty"`
	Version            string            `json:"version"`
	Status             string            `json:"status"`
	Error              string            `json:"error,omitempty"`
	Source             string            `json:"source"`
	RegistrySource     string            `json:"registrySource,omitempty"`
	PackageSHA256      string            `json:"packageSha256,omitempty"`
	PlatformPackages   map[string]string `json:"platformPackages,omitempty"`
	GrantedPermissions []string          `json:"grantedPermissions"`
	Settings           map[string]any    `json:"settings,omitempty"`
	Manifest           *Manifest         `json:"manifest,omitempty"`
	Routes             []routeBinding    `json:"routes,omitempty"`
	Stats              *jsrt.PoolStats   `json:"stats,omitempty"`
	NativeStats        *NativeStats      `json:"nativeStats,omitempty"`
	Usage              *PluginUsage      `json:"usage,omitempty"`
	RecentLogs         []LogEntry        `json:"recentLogs,omitempty"`
	CircuitTripped     bool              `json:"circuitTripped,omitempty"`
	ConsecutiveErrors  int               `json:"consecutiveErrors,omitempty"`
}

// Manager coordinates plugin installation, two-phase atomic activation,
// hot enable/disable/uninstall, HTTP route dispatch, static assets, and circuit breaking.
type Manager struct {
	db                   *ent.Client
	bus                  *hook.Bus
	settings             *settings.Service
	accounts             *account.Service
	players              *player.Service
	textures             *texture.Service
	renders              *render.Service
	notify               *notify.Service
	mailer               *mail.Mailer
	captcha              *captcha.Service
	cdn                  *cdn.Service
	yggdrasil            *yggdrasil.Service
	store                storage.Store
	legacyStore          storage.Store
	storageHook          *storage.SwappableStore
	themes               *theme.Registry
	dbAllocator          *DBAllocator
	dataDir              string
	hostPlatformOverride string
	trustedPublicKeys    []string
	allowUnsignedNative  bool
	nativeDriverFactory  func(manifest *Manifest, dir string) Driver
	platformFetcher      PlatformPackageFetcher
	log                  *slog.Logger
	engine               jsrt.Engine
	ownsEngine           bool

	mu     sync.RWMutex
	active map[string]*pluginRuntime
	stored map[string]*storedBundle

	revSeq atomic.Uint64

	syncMu      sync.Mutex
	lifecycleMu sync.Mutex
	settingsMu  sync.Mutex
	cronMu      sync.Mutex
	cronStop    chan struct{}
	cronWg      *sync.WaitGroup
	registry    *RegistryClient
}

type storedBundle struct {
	manifest         *Manifest
	dir              string
	bundleCode       []byte
	staticFiles      map[string][]byte
	sha256           string
	platformPackages map[string]string
	registrySource   string
	rev              string
	source           string
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
	mgr := &Manager{
		db:                   deps.DB,
		bus:                  deps.Bus,
		settings:             deps.Settings,
		accounts:             deps.Accounts,
		players:              deps.Players,
		textures:             deps.Textures,
		renders:              deps.Renders,
		notify:               deps.Notify,
		mailer:               deps.Mailer,
		captcha:              deps.Captcha,
		cdn:                  deps.CDN,
		yggdrasil:            deps.Yggdrasil,
		store:                deps.Store,
		legacyStore:          deps.LegacyStore,
		storageHook:          deps.StorageHook,
		themes:               deps.Themes,
		dbAllocator:          deps.DBAllocator,
		dataDir:              deps.DataDir,
		hostPlatformOverride: deps.HostPlatform,
		trustedPublicKeys:    append([]string(nil), deps.TrustedPublicKeys...),
		allowUnsignedNative:  deps.AllowUnsignedNative,
		nativeDriverFactory:  deps.NativeDriverFactory,
		platformFetcher:      deps.PlatformPackageFetcher,
		log:                  log,
		engine:               eng,
		ownsEngine:           owns,
		active:               map[string]*pluginRuntime{},
		stored:               map[string]*storedBundle{},
	}
	mgr.registry = NewRegistryClient(mgr)
	if mgr.platformFetcher == nil {
		mgr.platformFetcher = mgr.registry.FetchPlatformPackage
	}
	return mgr, nil
}

// SetPlatformPackageFetcher installs or replaces the callback used by multi-arch
// SyncCluster to fetch missing architecture packages from a plugin's registry_source.
func (m *Manager) SetPlatformPackageFetcher(fn PlatformPackageFetcher) {
	m.mu.Lock()
	m.platformFetcher = fn
	m.mu.Unlock()
}

// AllowUnsignedNative reports whether dev-mode unsigned native plugins are permitted.
func (m *Manager) AllowUnsignedNative() bool {
	if m == nil {
		return false
	}
	return m.allowUnsignedNative
}

func (m *Manager) hostPlatform() string {
	if m != nil && m.hostPlatformOverride != "" {
		return m.hostPlatformOverride
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

// Start scans DB-registered plugins and themes, pulls missing packages from Store,
// and activates enabled items in dependency order.
func (m *Manager) Start(ctx context.Context) error {
	m.migrateLegacyPackageBlobs(ctx)
	if err := m.startWithSync(ctx); err != nil {
		return err
	}
	m.startCronLoop()
	m.Registry().StartPatrolLoop()
	return nil
}

// LoadFromDir loads an unpacked plugin directory from disk, persists its metadata, and
// optionally enables it atomically.
func (m *Manager) LoadFromDir(ctx context.Context, dir string, enable bool) (*PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	manifestRaw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("read plugin.json: %w", err)
	}
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if mf.EffectiveRuntime() == RuntimeNative && !m.allowUnsignedNative {
		return nil, ErrUnsignedNativeForbidden
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
		rev:        fmt.Sprintf("%s.%d", mf.Version, m.revSeq.Add(1)),
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
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if mf.EffectiveRuntime() == RuntimeNative {
		return nil, fmt.Errorf("%w: native plugins must be loaded via InstallPackage or LoadDevNative", ErrBadManifest)
	}
	if len(serverJS) > jsrt.MaxBundleBytes {
		return nil, jsrt.ErrBundleTooLarge
	}
	if len(staticFiles) > MaxPackageEntries {
		return nil, fmt.Errorf("%w: too many static files (%d > %d)", ErrBadPackage, len(staticFiles), MaxPackageEntries)
	}
	var totalStatic int64 = int64(len(manifestRaw) + len(serverJS))
	for name, data := range staticFiles {
		if int64(len(data)) > MaxPackageFileBytes {
			return nil, fmt.Errorf("%w: static file %q exceeds %d byte limit", ErrBadPackage, name, MaxPackageFileBytes)
		}
		totalStatic += int64(len(data))
		if totalStatic > MaxUnpackedPackageBytes {
			return nil, fmt.Errorf("%w: unpacked payload exceeds %d byte cap", ErrBadPackage, MaxUnpackedPackageBytes)
		}
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
		rev:         fmt.Sprintf("%s.%d", mf.Version, m.revSeq.Add(1)),
		source:      "package",
	}
	return m.installBundle(ctx, sb, grantedPerms, enable)
}

// LoadDevJS loads an unpacked Tier 1 JS plugin or theme during local development
// (skinloom-plugin dev), enforcing size bounds and theme validation and marking
// the bundle source as "local".
func (m *Manager) LoadDevJS(
	ctx context.Context,
	manifestRaw []byte,
	serverJS []byte,
	staticFiles map[string][]byte,
	grantedPerms []string,
	enable bool,
) (*PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if mf.EffectiveRuntime() == RuntimeNative {
		return nil, fmt.Errorf("%w: LoadDevJS requires runtime \"js\"", ErrBadManifest)
	}
	if len(serverJS) > jsrt.MaxBundleBytes {
		return nil, jsrt.ErrBundleTooLarge
	}
	if len(staticFiles) > MaxPackageEntries {
		return nil, fmt.Errorf("%w: too many dev files (%d > %d)", ErrBadPackage, len(staticFiles), MaxPackageEntries)
	}
	var total int64 = int64(len(manifestRaw) + len(serverJS))
	cleanFiles := make(map[string][]byte, len(staticFiles)+2)
	cleanFiles["plugin.json"] = append([]byte(nil), manifestRaw...)
	if mf.Server != "" && len(serverJS) > 0 {
		cleanFiles[path.Clean(mf.Server)] = append([]byte(nil), serverJS...)
	}
	for name, data := range staticFiles {
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if err := validateZipRawPath(clean); err != nil {
			return nil, err
		}
		if int64(len(data)) > MaxPackageFileBytes {
			return nil, fmt.Errorf("%w: dev file %q exceeds %d byte limit", ErrBadPackage, clean, MaxPackageFileBytes)
		}
		total += int64(len(data))
		if total > MaxUnpackedPackageBytes {
			return nil, fmt.Errorf("%w: dev payload exceeds %d byte cap", ErrBadPackage, MaxUnpackedPackageBytes)
		}
		cleanFiles[clean] = append([]byte(nil), data...)
	}
	if mf.Kind == KindTheme {
		tmpPkg := &ParsedPackage{Manifest: mf, Files: cleanFiles}
		if _, err := tmpPkg.ValidateTheme(m.themes); err != nil {
			return nil, err
		}
	}
	if grantedPerms == nil {
		grantedPerms = append([]string(nil), mf.EffectivePermissions()...)
	}
	sum := sha256.Sum256(append(append([]byte{}, manifestRaw...), serverJS...))
	sb := &storedBundle{
		manifest:    mf,
		bundleCode:  append([]byte(nil), serverJS...),
		staticFiles: staticFiles,
		sha256:      hex.EncodeToString(sum[:]),
		rev:         fmt.Sprintf("%s.dev.%d", mf.Version, m.revSeq.Add(1)),
		source:      "local",
	}
	return m.installBundle(ctx, sb, grantedPerms, enable)
}

// LoadDevNative writes an unsigned local-development native Go plugin build
// (binary + web/assets/locales) into a versioned staging directory under
// <dataDir>/plugins-dev/<id>/<rev>/ and atomically hot-reloads it.
// Requires Manager.AllowUnsignedNative() (dev mode).
func (m *Manager) LoadDevNative(
	ctx context.Context,
	manifestRaw []byte,
	files map[string][]byte,
	grantedPerms []string,
	enable bool,
) (*PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if !m.allowUnsignedNative {
		return nil, ErrUnsignedNativeForbidden
	}
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if mf.EffectiveRuntime() != RuntimeNative {
		return nil, fmt.Errorf("%w: LoadDevNative requires runtime \"native\"", ErrBadManifest)
	}
	if m.dataDir == "" {
		return nil, errors.New("dataDir is required for native dev reload")
	}

	rev := fmt.Sprintf("%s.dev.%d", mf.Version, m.revSeq.Add(1))
	devRoot := filepath.Join(m.dataDir, "plugins-dev", mf.ID)
	stageDir := filepath.Join(devRoot, rev)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dev staging dir: %w", err)
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageDir)
		}
	}()

	if err := os.WriteFile(filepath.Join(stageDir, "plugin.json"), manifestRaw, 0o644); err != nil {
		return nil, err
	}
	h := sha256.New()
	_, _ = h.Write(manifestRaw)

	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)

	var total int64
	for _, rawName := range names {
		clean := path.Clean(strings.ReplaceAll(rawName, "\\", "/"))
		if err := validateZipRawPath(clean); err != nil {
			return nil, err
		}
		if clean == "plugin.json" {
			continue
		}
		data := files[rawName]
		limit := int64(MaxPackageFileBytes)
		mode := os.FileMode(0o644)
		if isNativeExecutablePath(mf, clean) {
			limit = int64(MaxNativeBinaryBytes)
			mode = 0o755
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("%w: dev file %q exceeds %d byte limit", ErrBadPackage, clean, limit)
		}
		total += int64(len(data))
		if total > MaxNativeUnpackedSingleBytes {
			return nil, fmt.Errorf("%w: dev payload exceeds %d byte limit", ErrBadPackage, MaxNativeUnpackedSingleBytes)
		}
		target := filepath.Join(stageDir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return nil, err
		}
		_, _ = h.Write([]byte(clean))
		_, _ = h.Write(data)
	}

	if _, err := mf.ResolveNativeExecutable(stageDir, runtime.GOOS, runtime.GOARCH); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPackage, err)
	}

	if grantedPerms == nil {
		grantedPerms = append([]string(nil), mf.EffectivePermissions()...)
	}
	sb := &storedBundle{
		manifest: mf,
		dir:      stageDir,
		sha256:   hex.EncodeToString(h.Sum(nil)),
		rev:      rev,
		source:   "local",
	}
	m.mu.RLock()
	var prevDevDir string
	if prev := m.stored[mf.ID]; prev != nil && strings.HasPrefix(prev.dir, devRoot+string(filepath.Separator)) {
		prevDevDir = prev.dir
	}
	m.mu.RUnlock()

	info, err := m.installBundle(ctx, sb, grantedPerms, enable)
	if err != nil {
		return nil, err
	}
	cleanupStage = false

	// Best-effort prune ONLY the previously replaced dev staging revision (never
	// blindly deleting sibling revisions that may belong to a newer concurrent reload).
	if prevDevDir != "" && prevDevDir != stageDir {
		go func(oldDir string) {
			time.Sleep(2 * time.Second)
			m.mu.RLock()
			cur := m.active[mf.ID]
			st := m.stored[mf.ID]
			stillUsed := (cur != nil && cur.dir == oldDir) || (st != nil && st.dir == oldDir)
			m.mu.RUnlock()
			if !stillUsed {
				_ = os.RemoveAll(oldDir)
			}
		}(prevDevDir)
	}
	return info, nil
}

func (m *Manager) isBuiltinThemeID(id string) bool {
	return theme.IsBuiltinID(id) || (m.themes != nil && m.themes.IsBuiltin(id))
}

func (m *Manager) installBundle(ctx context.Context, sb *storedBundle, grantedPerms []string, enable bool) (*PluginInfo, error) {
	if sb.manifest.Kind == KindTheme && m.isBuiltinThemeID(sb.manifest.ID) {
		return nil, fmt.Errorf("%w: cannot override built-in theme %q", ErrBadPackage, sb.manifest.ID)
	}
	if grantedPerms == nil {
		grantedPerms = append([]string(nil), sb.manifest.EffectivePermissions()...)
	}

	var prevVer, prevSource, prevRegistrySource, prevSHA, prevErrMsg string
	var prevPerms []string
	var prevPlatformPkgs map[string]string
	var prevSettings map[string]any
	var prevStatus entplugin.Status
	var prevRuntime entplugin.Runtime
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
		prevPerms = append([]string(nil), existing.GrantedPermissions...)
		prevSettings = cloneMap(existing.Settings)
		prevSource = existing.Source
		prevRegistrySource = existing.RegistrySource
		prevSHA = existing.PackageSha256
		if len(existing.PlatformPackages) > 0 {
			prevPlatformPkgs = make(map[string]string, len(existing.PlatformPackages))
			for k, v := range existing.PlatformPackages {
				prevPlatformPkgs[k] = v
			}
		}
		prevStatus = existing.Status
		prevRuntime = existing.Runtime
		prevErrMsg = existing.Error
		existingSettings = existing.Settings
	}

	m.mu.RLock()
	oldStored := m.stored[sb.manifest.ID]
	m.mu.RUnlock()

	now := time.Now().UTC()
	rtEnum := entplugin.Runtime(sb.manifest.EffectiveRuntime())
	if isNew {
		create := m.db.Plugin.Create().
			SetID(sb.manifest.ID).
			SetKind(entplugin.Kind(sb.manifest.Kind)).
			SetRuntime(rtEnum).
			SetVersion(sb.manifest.Version).
			SetStatus(entplugin.StatusDisabled).
			SetGrantedPermissions(grantedPerms).
			SetSettings(mergeDefaultSettings(sb.manifest.Settings, nil)).
			SetError("").
			SetSource(sb.source).
			SetRegistrySource(sb.registrySource).
			SetPackageSha256(sb.sha256).
			SetInstalledAt(now).
			SetUpdatedAt(now)
		if len(sb.platformPackages) > 0 {
			create = create.SetPlatformPackages(sb.platformPackages)
		}
		_, err = create.Save(ctx)
		if err != nil {
			return nil, err
		}
		existingSettings = mergeDefaultSettings(sb.manifest.Settings, nil)
	} else {
		merged := mergeDefaultSettings(sb.manifest.Settings, existingSettings)
		upd := m.db.Plugin.UpdateOneID(sb.manifest.ID).
			SetRuntime(rtEnum).
			SetVersion(sb.manifest.Version).
			SetGrantedPermissions(grantedPerms).
			SetSettings(merged).
			SetError("").
			SetSource(sb.source).
			SetRegistrySource(sb.registrySource).
			SetPackageSha256(sb.sha256).
			SetUpdatedAt(now)
		if len(sb.platformPackages) > 0 {
			upd = upd.SetPlatformPackages(sb.platformPackages)
		} else {
			upd = upd.ClearPlatformPackages()
		}
		if !enable {
			upd = upd.SetStatus(entplugin.StatusDisabled)
		}
		_, err = upd.Save(ctx)
		if err != nil {
			return nil, err
		}
		existingSettings = merged
	}

	m.mu.Lock()
	m.stored[sb.manifest.ID] = sb
	m.mu.Unlock()

	if !enable && !isNew {
		// Upgrading/installing with Enable=false must stop any previously active runtime.
		if m.bus != nil {
			m.bus.Remove("plugin:" + sb.manifest.ID)
		}
		m.unregisterHostExtensions(sb.manifest.ID)
		m.mu.Lock()
		prevRT := m.active[sb.manifest.ID]
		delete(m.active, sb.manifest.ID)
		m.mu.Unlock()
		if prevRT != nil {
			_ = prevRT.invokeLifecycle(ctx, "onDisable", nil)
			go prevRT.close(defaultDrainTimeout)
		}
	}

	if enable {
		if err := m.activateBundle(ctx, sb, grantedPerms, existingSettings, isNew, prevVer, isNew); err != nil {
			if !isNew {
				m.mu.Lock()
				if oldStored != nil {
					m.stored[sb.manifest.ID] = oldStored
				} else {
					delete(m.stored, sb.manifest.ID)
				}
				m.mu.Unlock()
				rb := m.db.Plugin.UpdateOneID(sb.manifest.ID).
					SetRuntime(prevRuntime).
					SetVersion(prevVer).
					SetGrantedPermissions(prevPerms).
					SetSettings(prevSettings).
					SetSource(prevSource).
					SetRegistrySource(prevRegistrySource).
					SetPackageSha256(prevSHA).
					SetStatus(prevStatus).
					SetError(prevErrMsg).
					SetUpdatedAt(time.Now().UTC())
				if len(prevPlatformPkgs) > 0 {
					rb = rb.SetPlatformPackages(prevPlatformPkgs)
				} else {
					rb = rb.ClearPlatformPackages()
				}
				_ = rb.Exec(ctx)
			}
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
	markFailedOnErr bool,
) error {
	candidate, err := newPluginRuntime(sb.manifest, sb.dir, sb.bundleCode, sb.staticFiles, grantedPerms, settingsMap, m)
	if err != nil {
		if markFailedOnErr {
			m.markFailed(ctx, sb.manifest.ID, err)
		}
		return err
	}
	if sb.rev != "" {
		candidate.rev = sb.rev
	}

	// Phase 1: build runtime driver (JS WASM pool or Native Go subprocess) and run Init/setup.
	drv, dbCfg, err := m.prepareDriver(ctx, candidate, sb)
	if err != nil {
		candidate.close(2 * time.Second)
		if markFailedOnErr {
			m.markFailed(ctx, sb.manifest.ID, err)
		}
		return err
	}
	var pluginDataDir, runtimeDir string
	if m.dataDir != "" {
		pluginDataDir = filepath.Join(m.dataDir, "plugins-data", sb.manifest.ID)
		runtimeDir = filepath.Join(m.dataDir, "run")
	}
	if err := candidate.initDriver(ctx, drv, dbCfg, pluginDataDir, runtimeDir); err != nil {
		candidate.close(2 * time.Second)
		if markFailedOnErr {
			m.markFailed(ctx, sb.manifest.ID, err)
		}
		return err
	}
	regs, err := candidate.buildHookRegistrations()
	if err != nil {
		candidate.close(2 * time.Second)
		if markFailedOnErr {
			m.markFailed(ctx, sb.manifest.ID, err)
		}
		return err
	}
	if candidate.perms.Has(PermRoutesPublic) && len(sb.manifest.Routes.Public) > 0 {
		if err := m.checkPublicRouteConflicts(sb.manifest.ID, sb.manifest.Routes.Public); err != nil {
			candidate.close(2 * time.Second)
			if markFailedOnErr {
				m.markFailed(ctx, sb.manifest.ID, err)
			}
			return err
		}
	}

	// Phase 2: atomic swap into Bus and active route table.
	if m.bus != nil {
		if err := m.bus.Replace("plugin:"+sb.manifest.ID, regs...); err != nil {
			candidate.close(2 * time.Second)
			if markFailedOnErr {
				m.markFailed(ctx, sb.manifest.ID, err)
			}
			return err
		}
	}

	candidate.packageSHA256 = sb.sha256

	m.mu.Lock()
	old := m.active[sb.manifest.ID]
	m.active[sb.manifest.ID] = candidate
	m.mu.Unlock()

	m.registerHostExtensions(candidate)
	theme.RegisterPluginBlocks(candidate.id, candidate.blockTypes())
	if len(candidate.crons) > 0 {
		m.startCronLoop()
	}

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
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	row, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return err
	}
	m.mu.RLock()
	sb := m.stored[id]
	m.mu.RUnlock()
	if sb == nil || row.PackageSha256 != "" && sb.sha256 != row.PackageSha256 {
		sb, err = m.loadBundleFromRow(ctx, row, false)
		if err != nil {
			return err
		}
	}
	if err := m.checkDependenciesActive(ctx, sb.manifest); err != nil {
		return err
	}
	if err := m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, "", true); err != nil {
		return err
	}
	if row.Kind == entplugin.KindTheme {
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)
	return nil
}

// Disable atomically removes the plugin from the hook bus and route table, runs
// onDisable, and drains in-flight JS executions.
func (m *Manager) Disable(ctx context.Context, id string) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}
	m.unregisterHostExtensions(id)
	m.mu.Lock()
	rt := m.active[id]
	delete(m.active, id)
	m.mu.Unlock()

	row, err := m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusDisabled).
		SetError("").
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}

	if rt != nil {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		rt.close(defaultDrainTimeout)
	}
	if ent.IsNotFound(err) {
		return nil
	}
	if row.Kind == entplugin.KindTheme {
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)
	return nil
}

// Uninstall disables the plugin, invokes onUninstall, removes bundle metadata,
// and when purge is true wipes KV, documents, and private files.
func (m *Manager) Uninstall(ctx context.Context, id string, purge bool) error {
	if !pluginIDRe.MatchString(id) && !themeIDRe.MatchString(id) {
		return fmt.Errorf("%w: invalid plugin id %q", ErrBadManifest, id)
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	oldRow, getErr := m.db.Plugin.Get(ctx, id)

	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}
	m.unregisterHostExtensions(id)
	m.mu.Lock()
	rt := m.active[id]
	delete(m.active, id)
	delete(m.stored, id)
	m.mu.Unlock()

	if rt != nil {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		_ = rt.invokeLifecycle(ctx, "onUninstall", map[string]any{"purge": purge})
		rt.close(defaultDrainTimeout)
	}

	if err := m.db.Plugin.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	if purge {
		if _, err := m.db.PluginKV.Delete().Where(pluginkv.Plugin(id)).Exec(ctx); err != nil {
			return err
		}
		if _, err := m.db.PluginKV.Delete().Where(pluginkv.Plugin(cronKVPlugin), pluginkv.KeyHasPrefix(id+":")).Exec(ctx); err != nil {
			return err
		}
		if _, err := m.db.PluginDoc.Delete().Where(plugindoc.Plugin(id)).Exec(ctx); err != nil {
			return err
		}
		if m.dbAllocator != nil {
			if err := m.dbAllocator.Purge(ctx, id); err != nil {
				return err
			}
		}
		if m.dataDir != "" {
			_ = os.RemoveAll(filepath.Join(m.dataDir, "plugins-data", id))
		}
	}
	m.removeLocalDirs(id, getErr == nil && oldRow.Kind == entplugin.KindTheme)
	if m.dataDir != "" {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "plugins-dev", id))
	}
	if getErr == nil {
		cleaned := map[string]bool{}
		if oldRow.PackageSha256 != "" {
			cleaned[oldRow.PackageSha256] = true
			m.cleanupUnusedPackageBlob(ctx, oldRow.PackageSha256)
		}
		for _, sha := range oldRow.PlatformPackages {
			if sha != "" && !cleaned[sha] {
				cleaned[sha] = true
				m.cleanupUnusedPackageBlob(ctx, sha)
			}
		}
	}
	if getErr == nil && oldRow.Kind == entplugin.KindTheme {
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)
	return nil
}

// UpdateSettings updates a plugin's persisted settings (preserving omitted/redacted secrets),
// notifies ctx.settings.onChange, and broadcasts a cluster config bump.
func (m *Manager) UpdateSettings(ctx context.Context, id string, next map[string]any) error {
	m.settingsMu.Lock()

	mf := m.manifestForRow(ctx, id)
	if mf == nil {
		m.settingsMu.Unlock()
		return fmt.Errorf("%w: plugin manifest is unavailable; reload or reinstall %q before editing settings", ErrInvalidSettings, id)
	}

	var savedSettings map[string]any
	err := db.WithTx(ctx, m.db, func(tx *ent.Client) error {
		row, err := tx.Plugin.Get(ctx, id)
		if err != nil {
			return err
		}
		merged, err := validateAndMergeSettingsPatch(mf, row.Settings, next)
		if err != nil {
			return err
		}
		updated, err := tx.Plugin.UpdateOneID(id).
			SetSettings(merged).
			SetUpdatedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			return err
		}
		savedSettings = cloneMap(updated.Settings)
		return nil
	})
	m.settingsMu.Unlock()
	if err != nil {
		return err
	}
	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()
	if rt != nil && !rt.closed.Load() {
		rt.notifySettingsChanged(ctx, savedSettings)
	}
	m.bumpConfigVersion(ctx)
	return nil
}

// patchOwnSettings validates and persists a partial update to plugin id's own
// declared settings from inside a host RPC call without triggering a re-entrant
// gRPC callback back into the calling plugin.
func (m *Manager) patchOwnSettings(ctx context.Context, callerRT *pluginRuntime, id string, patch map[string]any) (map[string]any, error) {
	if callerRT == nil || callerRT.closed.Load() {
		return nil, fmt.Errorf("plugin %q is not active", id)
	}
	m.mu.RLock()
	activeRT := m.active[id]
	m.mu.RUnlock()
	if activeRT != callerRT {
		return nil, fmt.Errorf("plugin %q runtime has been superseded", id)
	}

	m.settingsMu.Lock()

	mf := m.manifestForRow(ctx, id)
	if mf == nil {
		m.settingsMu.Unlock()
		return nil, fmt.Errorf("%w: plugin manifest is unavailable for %q", ErrInvalidSettings, id)
	}

	var (
		savedSettings map[string]any
		changed       bool
		changedKeys   []string
	)
	err := db.WithTx(ctx, m.db, func(tx *ent.Client) error {
		row, err := tx.Plugin.Get(ctx, id)
		if err != nil {
			return err
		}
		merged, err := validateAndMergeSettingsPatch(mf, row.Settings, patch)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(row.Settings, merged) {
			savedSettings = cloneMap(row.Settings)
			return nil
		}
		changed = true
		for k := range patch {
			changedKeys = append(changedKeys, k)
		}
		sort.Strings(changedKeys)
		updated, err := tx.Plugin.UpdateOneID(id).
			SetSettings(merged).
			SetUpdatedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			return err
		}
		savedSettings = cloneMap(updated.Settings)
		if m.db != nil {
			rec := (&audit.Entry{
				Action: "plugin.settings.patched",
				Meta:   map[string]any{"plugin": id, "keys": changedKeys},
			}).Target("plugin", id)
			if aErr := audit.Write(ctx, tx, rec); aErr != nil && m.log != nil {
				m.log.Warn("plugin settings patch audit failed", "plugin", id, "err", aErr)
			}
		}
		return nil
	})
	m.settingsMu.Unlock()
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()
	if rt != nil && rt == callerRT && !rt.closed.Load() {
		mergedWithDefaults := mergeDefaultSettings(rt.manifest.Settings, savedSettings)
		rt.settingsMu.Lock()
		rt.rawSettings = cloneMap(savedSettings)
		rt.settings = mergedWithDefaults
		rt.settingsMu.Unlock()
		savedSettings = cloneMap(mergedWithDefaults)
	}
	if changed {
		m.bumpConfigVersion(ctx)
	}
	return savedSettings, nil
}

func (m *Manager) handleCircuitTrip(id string, trippedRT *pluginRuntime, tripErr error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.Lock()
	cur := m.active[id]
	if cur == nil || (trippedRT != nil && cur != trippedRT) {
		m.mu.Unlock()
		return
	}
	delete(m.active, id)
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if m.bus != nil {
		m.bus.Remove("plugin:" + id)
	}
	m.unregisterHostExtensions(id)

	go cur.close(5 * time.Second)

	errMsg := "circuit breaker tripped"
	if tripErr != nil {
		errMsg = tripErr.Error()
	}
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	m.log.Error("plugin circuit breaker tripped; plugin disabled", "plugin", id, "err", errMsg)
	m.markFailed(ctx, id, errors.New(errMsg))
	m.bumpConfigVersion(ctx)

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
	info := m.buildPluginInfo(ctx, row, true)
	return &info, true
}

func (m *Manager) buildPluginInfo(ctx context.Context, row *ent.Plugin, includeUsage bool) PluginInfo {
	m.mu.RLock()
	rt := m.active[row.ID]
	sb := m.stored[row.ID]
	m.mu.RUnlock()

	info := PluginInfo{
		ID:                 row.ID,
		Kind:               string(row.Kind),
		Runtime:            string(row.Runtime),
		Version:            row.Version,
		Status:             string(row.Status),
		Error:              row.Error,
		Source:             row.Source,
		RegistrySource:     row.RegistrySource,
		PackageSHA256:      row.PackageSha256,
		PlatformPackages:   row.PlatformPackages,
		GrantedPermissions: row.GrantedPermissions,
	}
	if includeUsage {
		info.Usage = m.pluginUsage(ctx, row.ID)
	}
	if sb != nil {
		info.Manifest = sb.manifest
	}
	if rt != nil {
		info.Manifest = rt.manifest
		info.Routes = append([]routeBinding(nil), rt.routes...)
		info.RecentLogs = rt.recentLogs()
		if rt.driver != nil {
			ds := rt.driver.Stats()
			info.Stats = ds.Pool
			info.NativeStats = ds.Native
		}
		consec, tripped, _, _ := rt.circuit.State()
		info.ConsecutiveErrors = consec
		info.CircuitTripped = tripped
	}
	info.Settings = redactSettingsMap(info.Manifest, row.Settings)
	return info
}

// List returns all installed plugins sorted by ID without N+1 DB lookups.
func (m *Manager) List(ctx context.Context) ([]PluginInfo, error) {
	rows, err := m.db.Plugin.Query().Order(ent.Asc(entplugin.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PluginInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, m.buildPluginInfo(ctx, row, false))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Manager) checkPublicRouteConflicts(pluginID string, patterns []string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for otherID, other := range m.active {
		if otherID == pluginID || other == nil || other.closed.Load() || !other.perms.Has(PermRoutesPublic) {
			continue
		}
		for _, want := range patterns {
			wantSegs := splitPathSegments(want)
			for _, have := range other.manifest.Routes.Public {
				if patternsOverlap(wantSegs, splitPathSegments(have)) {
					return fmt.Errorf("routes.public pattern %q conflicts with active plugin %q (%q)", want, otherID, have)
				}
			}
		}
	}
	return nil
}

func patternsOverlap(a, b []string) bool {
	i := 0
	for i < len(a) && i < len(b) {
		if a[i] == "*" || b[i] == "*" {
			return true
		}
		if !segmentsOverlap(a[i], b[i]) {
			return false
		}
		i++
	}
	return len(a) == len(b)
}

func segmentsOverlap(a, b string) bool {
	if a == b || isDynamicSegment(a) || isDynamicSegment(b) {
		return true
	}
	return false
}

func isDynamicSegment(seg string) bool {
	return seg == "*" || strings.HasPrefix(seg, ":") || (strings.Contains(seg, "{") && strings.Contains(seg, "}"))
}

// HasPublicRoute reports whether any active plugin with the routes.public permission
// claims the given root URL path in its manifest routes.public patterns.
func (m *Manager) HasPublicRoute(p string) bool {
	if m == nil {
		return false
	}
	return m.matchPublicRouteRuntime(p) != nil
}

func (m *Manager) matchPublicRouteRuntime(p string) *pluginRuntime {
	actualSegs := splitPathSegments(p)
	if len(actualSegs) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matched *pluginRuntime
	for id, rt := range m.active {
		if rt == nil || rt.closed.Load() || !rt.perms.Has(PermRoutesPublic) || len(rt.manifest.Routes.Public) == 0 {
			continue
		}
		for _, pat := range rt.manifest.Routes.Public {
			if _, ok := matchSegments(splitPathSegments(pat), actualSegs); ok {
				if matched == nil || id < matched.id {
					matched = rt
				}
				break
			}
		}
	}
	return matched
}

// IsWebhookNoCSRFRoute reports whether the incoming request matches an active plugin's
// public route explicitly opted into WebhookNoCSRF (used by csrfGuard to allow signed
// external machine webhooks while serveRuntimeRoute strips Viewer).
func (m *Manager) IsWebhookNoCSRFRoute(method, rawPath string) bool {
	if m == nil {
		return false
	}
	var rt *pluginRuntime
	var subpath string
	if rest, ok := strings.CutPrefix(rawPath, "/api/plugins/"); ok {
		id, tail, hasTail := strings.Cut(rest, "/")
		if id == "" {
			return false
		}
		m.mu.RLock()
		rt = m.active[id]
		m.mu.RUnlock()
		if rt == nil || rt.closed.Load() {
			return false
		}
		if hasTail {
			subpath = "/" + strings.TrimPrefix(tail, "/")
		} else {
			subpath = "/"
		}
	} else {
		rt = m.matchPublicRouteRuntime(rawPath)
		if rt == nil {
			return false
		}
		subpath = rawPath
	}
	if !strings.HasPrefix(subpath, "/") {
		subpath = "/" + subpath
	}
	if subpath != "/" {
		subpath = strings.TrimRight(subpath, "/")
	}
	route, _, _ := matchRoute(rt.routes, method, subpath)
	return route != nil && route.Auth == "public" && route.WebhookNoCSRF
}

// ServePublicRoute dispatches a root-level request claimed via manifest routes.public.
// Returns true if an active plugin owns the path (even if the plugin returns 404/405).
func (m *Manager) ServePublicRoute(w http.ResponseWriter, r *http.Request, viewer Viewer, locale string) bool {
	if m == nil {
		return false
	}
	rt := m.matchPublicRouteRuntime(r.URL.Path)
	if rt == nil {
		return false
	}
	m.serveRuntimeRoute(w, r, rt, r.URL.Path, viewer, locale)
	return true
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
	m.serveRuntimeRoute(w, r, rt, subpath, viewer, locale)
}

func (m *Manager) serveRuntimeRoute(w http.ResponseWriter, r *http.Request, rt *pluginRuntime, subpath string, viewer Viewer, locale string) {
	if !strings.HasPrefix(subpath, "/") {
		subpath = "/" + subpath
	}
	if subpath != "/" {
		subpath = strings.TrimRight(subpath, "/")
	}

	route, params, pathMatched := matchRoute(rt.routes, r.Method, subpath)
	if route == nil {
		if r.Method == http.MethodOptions && pathMatched && hasOpenCORSRoute(rt.routes, subpath) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", openCORSAllowHeaders(rt.routes, subpath))
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.Header().Set("Vary", "Origin")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if pathMatched {
			writePluginProblem(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writePluginProblem(w, http.StatusNotFound, "plugin route not found")
		return
	}

	// Capture whether the request carried host credentials (AdminToken or host
	// OAuth/PAT Bearer tokens) before WebhookNoCSRF strips Viewer, so Authorization
	// is never forwarded when it holds host credentials.
	isHostAuth := viewer.HasHostAuthHeader ||
		(viewer.Authenticated && viewer.UserID == 0 && viewer.Superadmin) ||
		isHostPrefixedBearer(r.Header.Get("Authorization"))
	if route.WebhookNoCSRF {
		// Defense-in-depth: CSRF-exempt webhook routes must never observe browser
		// cookies or host credentials.
		viewer = Viewer{}
	}
	if route.OpenCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Vary", "Origin")
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

	// Stream HTTP/WebSocket reverse proxy data plane for Tier 2 native plugins.
	if route.StreamProxy {
		if err := rt.perms.Check(PermHTTPStream); err != nil {
			writePluginProblem(w, http.StatusForbidden, "plugin lacks http.stream permission")
			return
		}
		proxy := rt.driver.ProxyHandler()
		if proxy == nil {
			writePluginProblem(w, http.StatusBadGateway, "plugin stream proxy is not initialized")
			return
		}
		servePluginStreamProxy(w, r, proxy, rt.id, subpath, viewer, locale)
		return
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
	for _, hk := range route.ForwardHeaders {
		lk := strings.ToLower(strings.TrimSpace(hk))
		if lk == "" || lk == "cookie" || lk == "proxy-authorization" || strings.HasPrefix(lk, "x-skinloom-") {
			continue
		}
		if lk == "authorization" && isHostAuth {
			continue
		}
		if v := r.Header.Get(hk); v != "" {
			hdrMap[lk] = v
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
	out, err := rt.invokeRoute(ctx, resolveHandlerID(route.HandlerID, route.FnID), reqObj)
	if err != nil {
		if errors.Is(err, jsrt.ErrTimeout) {
			writePluginProblem(w, http.StatusGatewayTimeout, "plugin route timed out")
			return
		}
		if rej, ok := hook.AsRejection(err); ok {
			writePluginProblem(w, http.StatusUnprocessableEntity, rej.Reason)
			return
		}
		if errors.Is(err, ErrPermissionDenied) || strings.Contains(err.Error(), ErrPermissionDenied.Error()) {
			writePluginProblem(w, http.StatusForbidden, "permission denied")
			return
		}
		rt.appendLog("error", fmt.Sprintf("plugin route %s %s failed: %v", r.Method, subpath, err))
		writePluginProblem(w, http.StatusInternalServerError, "plugin route error")
		return
	}

	writePluginRouteResponse(w, out)
}

func sanitizeStreamConnectionHeader(h http.Header) {
	conns := h.Values("Connection")
	if len(conns) == 0 {
		return
	}
	keepUpgrade := false
	for _, v := range conns {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
				keepUpgrade = true
				break
			}
		}
	}
	h.Del("Connection")
	if keepUpgrade && h.Get("Upgrade") != "" {
		h.Set("Connection", "Upgrade")
	}
}

func servePluginStreamProxy(
	w http.ResponseWriter,
	r *http.Request,
	proxy http.Handler,
	pluginID, subpath string,
	viewer Viewer,
	locale string,
) {
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = randomHexString(12)
	}
	meta := streamProxyMeta{
		PluginID:  pluginID,
		Locale:    locale,
		RequestID: reqID,
		Viewer:    viewer,
	}
	outReq := r.Clone(context.WithValue(r.Context(), streamProxyMetaKey{}, meta))
	outReq.URL.Path = subpath
	outReq.URL.RawPath = ""
	outReq.RequestURI = ""

	// Prevent client Connection: header from stripping host-injected headers in ReverseProxy.
	sanitizeStreamConnectionHeader(outReq.Header)

	// Strip sensitive host credentials and any caller-supplied X-SkinLoom-* spoofed headers.
	outReq.Header.Del("Cookie")
	outReq.Header.Del("Authorization")
	for k := range outReq.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-skinloom-") {
			outReq.Header.Del(k)
		}
	}

	outReq.Header.Set("X-SkinLoom-Plugin-ID", pluginID)
	outReq.Header.Set("X-SkinLoom-Locale", locale)
	outReq.Header.Set("X-Request-ID", reqID)
	if viewer.Authenticated {
		outReq.Header.Set("X-SkinLoom-Authenticated", "1")
		outReq.Header.Set("X-SkinLoom-User-ID", strconv.FormatInt(viewer.UserID, 10))
		outReq.Header.Set("X-SkinLoom-User-Role", viewer.Role)
		if viewer.Email != "" {
			outReq.Header.Set("X-SkinLoom-User-Email", viewer.Email)
		}
		if viewer.Nickname != "" {
			outReq.Header.Set("X-SkinLoom-User-Nickname", viewer.Nickname)
		}
	}

	proxy.ServeHTTP(w, outReq)
}

func isHostPrefixedBearer(authHeader string) bool {
	hdr := strings.TrimSpace(authHeader)
	if len(hdr) < 7 || !strings.EqualFold(hdr[:7], "Bearer ") {
		return false
	}
	tok := strings.TrimSpace(hdr[7:])
	return strings.HasPrefix(tok, "slo_") || strings.HasPrefix(tok, "slp_") || strings.HasPrefix(tok, "slr_")
}

func hasOpenCORSRoute(routes []routeBinding, actual string) bool {
	actualSegs := splitPathSegments(actual)
	for idx := range routes {
		if !routes[idx].OpenCORS {
			continue
		}
		if _, ok := matchSegments(splitPathSegments(routes[idx].Pattern), actualSegs); ok {
			return true
		}
	}
	return false
}

func openCORSAllowHeaders(routes []routeBinding, actual string) string {
	headers := []string{"Authorization", "Content-Type", "Accept"}
	seen := map[string]bool{"authorization": true, "content-type": true, "accept": true}
	actualSegs := splitPathSegments(actual)
	for idx := range routes {
		if !routes[idx].OpenCORS {
			continue
		}
		if _, ok := matchSegments(splitPathSegments(routes[idx].Pattern), actualSegs); !ok {
			continue
		}
		for _, hk := range routes[idx].ForwardHeaders {
			lk := strings.ToLower(strings.TrimSpace(hk))
			if lk == "" || lk == "cookie" || lk == "proxy-authorization" || strings.HasPrefix(lk, "x-skinloom-") || seen[lk] {
				continue
			}
			seen[lk] = true
			headers = append(headers, http.CanonicalHeaderKey(lk))
		}
	}
	return strings.Join(headers, ", ")
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
		if rb.Method == "*" || strings.EqualFold(rb.Method, "ANY") || strings.EqualFold(rb.Method, method) {
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
			if name, val, ok := matchInlineParamSegment(seg, actual[i]); ok {
				params[name] = val
			} else {
				return nil, false
			}
		}
	}
	return params, true
}

// matchInlineParamSegment matches segments containing a single "{param}" placeholder
// with optional literal prefix/suffix, e.g. "{name}.json" against "Steve.json".
func matchInlineParamSegment(pat, actual string) (string, string, bool) {
	open := strings.IndexByte(pat, '{')
	closeIdx := strings.IndexByte(pat, '}')
	if open < 0 || closeIdx <= open+1 {
		return "", "", false
	}
	prefix := pat[:open]
	name := pat[open+1 : closeIdx]
	suffix := pat[closeIdx+1:]
	if strings.ContainsAny(name, "{}") || strings.ContainsAny(suffix, "{}") {
		return "", "", false
	}
	if !strings.HasPrefix(actual, prefix) || !strings.HasSuffix(actual, suffix) {
		return "", "", false
	}
	if len(actual) <= len(prefix)+len(suffix) {
		return "", "", false
	}
	val := actual[len(prefix) : len(actual)-len(suffix)]
	return name, val, true
}

func isPluginResponseEnvelope(m map[string]any) bool {
	if _, hasBody := m["body"]; hasBody {
		return true
	}
	for k := range m {
		switch k {
		case "status", "headers", "body":
		default:
			return false
		}
	}
	return true
}

const pluginSandboxCSP = "default-src 'none'; frame-ancestors 'self'; sandbox"

func isBlockedPluginResponseHeader(hk string) bool {
	lower := strings.ToLower(strings.TrimSpace(hk))
	if strings.HasPrefix(lower, "access-control-") {
		return true
	}
	switch lower {
	case "set-cookie", "set-cookie2", "content-security-policy", "content-security-policy-report-only",
		"x-frame-options", "x-content-type-options", "strict-transport-security",
		"clear-site-data", "referrer-policy", "refresh", "cross-origin-opener-policy",
		"cross-origin-embedder-policy", "cross-origin-resource-policy":
		return true
	}
	return false
}

func stripPluginUnsafeHeaders(h http.Header) {
	for hk := range h {
		if isBlockedPluginResponseHeader(hk) {
			h.Del(hk)
		}
	}
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
		if s, hasStatus := m["status"]; hasStatus && isPluginResponseEnvelope(m) {
			if code := int(asFloat(s)); code >= 100 && code <= 599 {
				status = code
				if hdrs, ok := m["headers"].(map[string]any); ok {
					for hk, hv := range hdrs {
						if isBlockedPluginResponseHeader(hk) {
							continue
						}
						w.Header().Set(hk, fmt.Sprint(hv))
					}
				}
				body = m["body"]
			}
		}
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status == http.StatusNoContent || status == http.StatusNotModified || (status >= 300 && status < 400 && body == nil) {
		w.WriteHeader(status)
		return
	}
	ctype := w.Header().Get("Content-Type")
	if ctype == "" {
		ctype = "application/json; charset=utf-8"
		w.Header().Set("Content-Type", ctype)
	}
	lowerType := strings.ToLower(strings.TrimSpace(strings.SplitN(ctype, ";", 2)[0]))
	if lowerType != "application/json" && lowerType != "application/problem+json" && lowerType != "text/plain" {
		w.Header().Set("Content-Security-Policy", pluginSandboxCSP)
	}
	w.WriteHeader(status)
	switch b := body.(type) {
	case []byte:
		_, _ = w.Write(b)
		return
	case string:
		if !strings.HasPrefix(lowerType, "application/json") {
			_, _ = io.WriteString(w, b)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func writePluginProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
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

		m.mu.RLock()
		rt := m.active[id]
		m.mu.RUnlock()
		if rt == nil || rt.closed.Load() {
			http.NotFound(w, r)
			return
		}

		// Handle plugin-written files: /plugins-assets/{id}/_files/{key...}
		// Strictly isolated to <dataDir>/plugins-data/{id}/files/ (never exposing plugin.db
		// or private DataDir state), active plugins with storage.files permission only,
		// symlink-refused via os.OpenRoot, and streamed via http.ServeContent.
		if key, isFile := strings.CutPrefix(rest, "_files/"); isFile {
			if m.dataDir == "" || !rt.perms.Has(PermStorageFiles) {
				http.NotFound(w, r)
				return
			}
			rel, err := validateFileKey(key)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			filesRoot := filepath.Join(m.dataDir, "plugins-data", id, "files")
			rootFS, err := os.OpenRoot(filesRoot)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer rootFS.Close()

			nativeRel := filepath.FromSlash(rel)
			info, err := rootFS.Lstat(nativeRel)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			f, err := rootFS.Open(nativeRel)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()

			var head [512]byte
			n, _ := f.Read(head[:])
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				http.NotFound(w, r)
				return
			}
			ctype := safeAssetContentType(rel, head[:n], false)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", pluginSandboxCSP)
			w.Header().Set("Content-Type", ctype)
			if ctype == "application/octet-stream" {
				w.Header().Set("Content-Disposition", "attachment")
			}
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
			return
		}

		dir := rt.dir
		overlay := rt.staticFiles
		version := rt.manifest.Version
		rev := rt.rev
		cleanWeb := ""
		if rt.manifest.Web != "" {
			cleanWeb = strings.TrimPrefix(path.Clean(rt.manifest.Web), "/")
		}

		// Support both /plugins-assets/{id}/{rev-or-version}/{path} and /plugins-assets/{id}/{path}.
		assetRel := rest
		versioned := false
		if segs := strings.SplitN(rest, "/", 2); len(segs) == 2 && (segs[0] == rev || segs[0] == version) {
			if segRel, kErr := validateFileKey(segs[1]); kErr == nil {
				if _, ok := overlay[segRel]; ok {
					assetRel = segRel
					versioned = true
				} else if dir != "" {
					if rootFS, rErr := os.OpenRoot(dir); rErr == nil {
						info, sErr := rootFS.Lstat(filepath.FromSlash(segRel))
						_ = rootFS.Close()
						if sErr == nil && info.Mode().IsRegular() {
							assetRel = segRel
							versioned = true
						}
					}
				}
			}
		}

		rel, err := validateFileKey(assetRel)
		isScript := strings.HasPrefix(rel, "web/") || (cleanWeb != "" && rel == cleanWeb)
		if err != nil || (!isScript && !strings.HasPrefix(rel, "assets/")) {
			http.NotFound(w, r)
			return
		}

		if b, ok := overlay[rel]; ok {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Type", safeAssetContentType(rel, b, isScript))
			if !isScript {
				w.Header().Set("Content-Security-Policy", pluginSandboxCSP)
			}
			if versioned {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(b)
			}
			return
		}
		if dir == "" {
			http.NotFound(w, r)
			return
		}
		rootFS, err := os.OpenRoot(dir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer rootFS.Close()
		nativeRel := filepath.FromSlash(rel)
		info, err := rootFS.Lstat(nativeRel)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		f, err := rootFS.Open(nativeRel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		var head [512]byte
		n, _ := f.Read(head[:])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", safeAssetContentType(rel, head[:n], isScript))
		if !isScript {
			w.Header().Set("Content-Security-Policy", pluginSandboxCSP)
		}
		if versioned {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
	})
}

func safeAssetContentType(rel string, sample []byte, allowScript bool) string {
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
	case ".wasm":
		return "application/wasm"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	// Never allow unknown or active markup/script types (html/svg/xml/xht/xsl/etc.)
	// to render inline under our origin; only accept sniffed raster image formats.
	if len(sample) > 0 {
		detected := http.DetectContentType(sample)
		switch detected {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
			return detected
		}
	}
	return "application/octet-stream"
}

// Shutdown drains all active plugin pools and closes the owned jsrt engine.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.stopCronLoop()
	if m.registry != nil {
		m.registry.StopPatrolLoop()
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.Lock()
	active := make([]*pluginRuntime, 0, len(m.active))
	activeIDs := make([]string, 0, len(m.active))
	for id, rt := range m.active {
		activeIDs = append(activeIDs, id)
		active = append(active, rt)
		delete(m.active, id)
	}
	m.mu.Unlock()

	for _, id := range activeIDs {
		if m.bus != nil {
			m.bus.Remove("plugin:" + id)
		}
		m.unregisterHostExtensions(id)
	}

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

type serviceCallChainKey struct{}

const maxServiceCallDepth = 4

// CallService invokes an exported service method on another active plugin (ctx.services.use).
// Either the caller declares targetID in dependencies/optionalDependencies, OR targetID
// explicitly opts in by declaring callerID in its dependencies/optionalDependencies.
func (m *Manager) CallService(ctx context.Context, callerID, targetID, svcName string, args ...any) (any, error) {
	chain, _ := ctx.Value(serviceCallChainKey{}).([]string)
	if callerID != "" {
		if callerID == targetID {
			return nil, fmt.Errorf("plugin %q cannot call its own service %q", callerID, svcName)
		}
		if len(chain) == 0 {
			chain = []string{callerID}
		}
		if slices.Contains(chain, targetID) {
			return nil, fmt.Errorf("cyclic plugin service call detected: %s -> %s", strings.Join(chain, " -> "), targetID)
		}
		if len(chain) >= maxServiceCallDepth {
			return nil, fmt.Errorf("plugin service call depth exceeded (%d)", maxServiceCallDepth)
		}
	}
	var callerRT *pluginRuntime
	if callerID != "" {
		m.mu.RLock()
		callerRT = m.active[callerID]
		m.mu.RUnlock()
		if callerRT == nil || callerRT.closed.Load() {
			return nil, fmt.Errorf("calling plugin %q is not active", callerID)
		}
	}
	m.mu.RLock()
	rt := m.active[targetID]
	m.mu.RUnlock()
	if callerID != "" {
		targetOptedIn := rt != nil && !rt.closed.Load() && rt.manifest.HasDependency(callerID)
		if !callerRT.manifest.HasDependency(targetID) && !targetOptedIn {
			return nil, fmt.Errorf("%w: dependency between %q and %q must be declared in dependencies or optionalDependencies", ErrPermissionDenied, callerID, targetID)
		}
	}
	if rt == nil || rt.closed.Load() {
		return nil, fmt.Errorf("plugin %q is not active", targetID)
	}
	rt.setupMu.Lock()
	var foundSvc *serviceBinding
	for idx := range rt.services {
		if rt.services[idx].Name == svcName {
			cp := rt.services[idx]
			foundSvc = &cp
			break
		}
	}
	rt.setupMu.Unlock()
	if foundSvc == nil {
		return nil, fmt.Errorf("plugin %q does not expose service %q", targetID, svcName)
	}
	nextChain := append(append([]string(nil), chain...), targetID)
	callCtx := context.WithValue(ctx, serviceCallChainKey{}, nextChain)
	return rt.invokeService(callCtx, resolveHandlerID(foundSvc.HandlerID, foundSvc.FnID), svcName, callerID, args...)
}

// DiscoverServiceProviders returns sorted IDs of active plugins exposing svcName
// that share a declared dependency relationship with callerID (either caller
// depends on target, or target opted into caller via dependencies/optionalDependencies).
func (m *Manager) DiscoverServiceProviders(callerID, svcName string) ([]string, error) {
	svcName = strings.TrimSpace(svcName)
	if svcName == "" {
		return nil, errors.New("service name is required")
	}
	m.mu.RLock()
	callerRT := m.active[callerID]
	if callerID != "" && (callerRT == nil || callerRT.closed.Load()) {
		m.mu.RUnlock()
		return nil, fmt.Errorf("calling plugin %q is not active", callerID)
	}
	candidates := make([]*pluginRuntime, 0, len(m.active))
	for id, rt := range m.active {
		if id == callerID || rt == nil || rt.closed.Load() {
			continue
		}
		if callerID != "" && !callerRT.manifest.HasDependency(id) && !rt.manifest.HasDependency(callerID) {
			continue
		}
		candidates = append(candidates, rt)
	}
	m.mu.RUnlock()

	out := make([]string, 0, len(candidates))
	for _, rt := range candidates {
		rt.setupMu.Lock()
		exposes := false
		for idx := range rt.services {
			if rt.services[idx].Name == svcName {
				exposes = true
				break
			}
		}
		rt.setupMu.Unlock()
		if exposes {
			out = append(out, rt.id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Manager) prepareDriver(ctx context.Context, rt *pluginRuntime, sb *storedBundle) (Driver, *PluginDBConfig, error) {
	if sb.manifest.EffectiveRuntime() == RuntimeNative {
		if err := rt.perms.Check(PermNativeExec); err != nil {
			return nil, nil, err
		}
		var dbCfg *PluginDBConfig
		needsDB := sb.manifest.Native != nil && sb.manifest.Native.Database != nil && sb.manifest.Native.Database.Required
		if needsDB || rt.perms.Has(PermStorageSQL) {
			if err := rt.perms.Check(PermStorageSQL); err != nil {
				return nil, nil, err
			}
			if m.dbAllocator != nil {
				var err error
				dbCfg, err = m.dbAllocator.Allocate(ctx, sb.manifest.ID)
				if err != nil {
					return nil, nil, fmt.Errorf("allocate isolated plugin database: %w", err)
				}
			} else if needsDB {
				return nil, nil, errors.New("plugin requires isolated SQL database but DBAllocator is not configured")
			}
		}
		if m.nativeDriverFactory != nil {
			return m.nativeDriverFactory(sb.manifest, sb.dir), dbCfg, nil
		}
		return NewNativeDriver(NativeDriverOptions{}), dbCfg, nil
	}
	return NewJSDriver(m.engine, sb.bundleCode), nil, nil
}

func (m *Manager) registerHostExtensions(rt *pluginRuntime) {
	owner := "plugin:" + rt.id

	rt.setupMu.Lock()
	auths := append([]authProviderBinding(nil), rt.authProviders...)
	verifiers := append([]passwordVerifierBinding(nil), rt.verifiers...)
	captchas := append([]captchaBinding(nil), rt.captchas...)
	cdns := append([]cdnBinding(nil), rt.cdns...)
	yggs := append([]yggMetaBinding(nil), rt.yggMetas...)
	storProv := rt.storageProvider
	searchProv := rt.searchProvider
	renderProv := rt.renderProvider
	rt.setupMu.Unlock()

	// Unregister non-infrastructure extensions cleanly before re-registering.
	// Infrastructure providers (storage/search/render) are swapped atomically below
	// so hot-upgrades have zero nil-provider gap.
	theme.UnregisterPluginBlocks(rt.id)
	if m.accounts != nil {
		m.accounts.UnregisterAuthProviders(owner)
		m.accounts.UnregisterPasswordVerifiers(owner)
	}
	if m.captcha != nil {
		m.captcha.UnregisterProviders(owner)
	}
	if m.cdn != nil {
		m.cdn.UnregisterPurgers(owner)
	}
	if m.yggdrasil != nil {
		m.yggdrasil.UnregisterMetaContributors(owner)
	}

	if m.accounts != nil {
		for _, ap := range auths {
			def := ap.Def
			hasVerify := ap.VerifyFn >= 0 || ap.HandlerID != ""
			hid := resolveHandlerID(ap.HandlerID, ap.VerifyFn)
			if hasVerify {
				def.Verify = func(ctx context.Context, input map[string]any) (*account.ExternalUserInfo, error) {
					out, err := rt.invokeExtension(ctx, "auth", def.ID, hid, input)
					if err != nil {
						return nil, err
					}
					resMap, ok := asMap(out)
					if !ok || resMap == nil {
						return nil, errors.New("external auth provider returned empty profile")
					}
					subj, _ := resMap["subject"].(string)
					subj = strings.TrimSpace(subj)
					if subj == "" {
						return nil, errors.New("external auth provider profile missing subject")
					}
					email, _ := resMap["email"].(string)
					verified, _ := resMap["emailVerified"].(bool)
					nick, _ := resMap["nickname"].(string)
					next, _ := resMap["next"].(string)
					return &account.ExternalUserInfo{
						Subject:       subj,
						Email:         email,
						EmailVerified: verified,
						Nickname:      nick,
						Next:          next,
					}, nil
				}
			}
			if err := m.accounts.RegisterAuthProvider(owner, def); err != nil {
				m.log.Warn("failed to register auth provider", "plugin", rt.id, "provider", def.ID, "err", err)
			}
		}
		for _, pv := range verifiers {
			algo := pv.Algo
			hid := resolveHandlerID(pv.HandlerID, pv.FnID)
			if err := m.accounts.RegisterPasswordVerifier(owner, algo, func(ctx context.Context, storedHash, pw string) (bool, error) {
				out, err := rt.invokeExtension(ctx, "password_verifier", algo, hid, storedHash, pw)
				if err != nil {
					return false, err
				}
				matched, _ := out.(bool)
				return matched, nil
			}); err != nil {
				m.log.Warn("failed to register password verifier", "plugin", rt.id, "algo", algo, "err", err)
			}
		}
	}

	if m.captcha != nil {
		for _, cb := range captchas {
			id := cb.ID
			hid := resolveHandlerID(cb.HandlerID, cb.FnID)
			if err := m.captcha.RegisterProvider(owner, id, func(ctx context.Context, a captcha.Answer, remoteIP string) error {
				out, err := rt.invokeExtension(ctx, "captcha", id, hid, map[string]any{
					"id":       a.ID,
					"value":    a.Value,
					"remoteIp": remoteIP,
				})
				if err != nil {
					return fmt.Errorf("%w: %v", captcha.ErrUnavailable, err)
				}
				switch v := out.(type) {
				case bool:
					if v {
						return nil
					}
				case map[string]any:
					if ok, has := v["ok"].(bool); has && ok {
						return nil
					}
				}
				return captcha.ErrInvalid
			}); err != nil {
				m.log.Warn("failed to register captcha provider", "plugin", rt.id, "provider", id, "err", err)
			}
		}
	}

	if m.cdn != nil {
		for _, c := range cdns {
			hid := resolveHandlerID(c.HandlerID, c.FnID)
			if err := m.cdn.RegisterPurger(owner, c.ID, pluginCDNPurger{rt: rt, id: c.ID, handlerID: hid}); err != nil {
				m.log.Warn("failed to register CDN purger", "plugin", rt.id, "provider", c.ID, "err", err)
			}
		}
	}

	if m.yggdrasil != nil {
		for _, ym := range yggs {
			item := ym
			if item.Static != nil {
				staticCopy := *item.Static
				m.yggdrasil.RegisterMetaContributor(owner, func(context.Context) (yggdrasil.MetaExtension, error) {
					return staticCopy, nil
				})
				continue
			}
			hid := resolveHandlerID(item.HandlerID, item.FnID)
			m.yggdrasil.RegisterMetaContributor(owner, func(ctx context.Context) (yggdrasil.MetaExtension, error) {
				out, err := rt.invokeExtension(ctx, "yggdrasil_meta", rt.id, hid)
				if err != nil {
					return yggdrasil.MetaExtension{}, err
				}
				resMap, ok := asMap(out)
				if !ok || resMap == nil {
					return yggdrasil.MetaExtension{}, nil
				}
				ext := yggdrasil.MetaExtension{}
				if doms, ok := resMap["skinDomains"].([]any); ok {
					for _, d := range doms {
						if s, ok := d.(string); ok && s != "" {
							ext.SkinDomains = append(ext.SkinDomains, s)
						}
					}
				}
				if meta, ok := asMap(resMap["meta"]); ok {
					ext.Meta = meta
				}
				return ext, nil
			})
		}
	}

	// Native-only infrastructure providers (permission-guarded, atomic hot-swap).
	if m.storageHook != nil {
		if storProv != nil && rt.perms.Has(PermProviderStorage) {
			m.storageHook.SetProvider(owner, storProv)
		} else {
			m.storageHook.ClearProvider(owner)
		}
	}
	if m.textures != nil {
		if searchProv != nil && rt.perms.Has(PermProviderSearch) {
			m.textures.SetSearchProvider(owner, searchProv)
		} else {
			m.textures.ClearSearchProvider(owner)
		}
	}
	if m.renders != nil {
		if renderProv != nil && rt.perms.Has(PermProviderRender) {
			m.renders.SetProvider(owner, renderProv)
		} else {
			m.renders.ClearProvider(owner)
		}
	}
}

func (m *Manager) unregisterHostExtensions(id string) {
	theme.UnregisterPluginBlocks(id)
	owner := "plugin:" + id
	if m.accounts != nil {
		m.accounts.UnregisterAuthProviders(owner)
		m.accounts.UnregisterPasswordVerifiers(owner)
	}
	if m.captcha != nil {
		m.captcha.UnregisterProviders(owner)
	}
	if m.cdn != nil {
		m.cdn.UnregisterPurgers(owner)
	}
	if m.yggdrasil != nil {
		m.yggdrasil.UnregisterMetaContributors(owner)
	}
	if m.storageHook != nil {
		m.storageHook.ClearProvider(owner)
	}
	if m.textures != nil {
		m.textures.ClearSearchProvider(owner)
	}
	if m.renders != nil {
		m.renders.ClearProvider(owner)
	}
}
