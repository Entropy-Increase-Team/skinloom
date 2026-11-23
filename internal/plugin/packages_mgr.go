package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/blob"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/plugindoc"
	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

var (
	// ErrInvalidSettings indicates a plugin settings patch failed manifest schema validation.
	ErrInvalidSettings = errors.New("invalid plugin settings")
	// ErrMissingDependency indicates a required plugin dependency is missing or inactive.
	ErrMissingDependency = errors.New("missing plugin dependency")
)

// PluginUsage summarizes persistent storage currently used by a plugin.
type PluginUsage struct {
	KVKeys     int   `json:"kvKeys"`
	Docs       int   `json:"docs"`
	FilesBytes int64 `json:"filesBytes"`
}

// PluginSettingFieldView describes one declared plugin setting field and its redacted value.
type PluginSettingFieldView struct {
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Value       any            `json:"value"`
	Default     any            `json:"default,omitempty"`
	IsSet       bool           `json:"isSet"`
	Secret      bool           `json:"secret"`
	Options     []string       `json:"options,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Min         *float64       `json:"min,omitempty"`
	Max         *float64       `json:"max,omitempty"`
	Step        *float64       `json:"step,omitempty"`
	Order       int            `json:"order,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
	Item        map[string]any `json:"item,omitempty"`
}

// PluginSettingsResponse returns ordered setting schema definitions alongside redacted stored values.
type PluginSettingsResponse struct {
	PluginID string                   `json:"pluginId"`
	Fields   []PluginSettingFieldView `json:"fields"`
	Values   map[string]any           `json:"values"`
}

// InstallOptions controls `.slp` package verification and permission approval during installation.
type InstallOptions struct {
	GrantedPermissions []string
	GrantAllDeclared   bool
	AllowUnsigned      bool
	Enable             bool
	Source             string            // "package" | "market" (defaults to "package")
	RegistrySource     string            // optional market registry URL for multi-arch fallback
	PlatformPackages   map[string]string // optional extra platform -> sha256 entries
	ExtraTrustedKeys   []string          // optional registry-scoped public keys
}

// InspectPackage validates an uploaded .slp package and builds a pre-install inspection report.
func (m *Manager) InspectPackage(ctx context.Context, pkgBytes []byte, locale string) (*PackageInspectResult, error) {
	return m.inspectPackageWithKeys(ctx, pkgBytes, locale, nil)
}

func (m *Manager) inspectPackageWithKeys(ctx context.Context, pkgBytes []byte, locale string, extraKeys []string) (*PackageInspectResult, error) {
	hostPlat := m.hostPlatform()
	trusted := append(m.allTrustedKeys(), extraKeys...)
	pkg, err := ParsePackageForPlatform(pkgBytes, trusted, hostPlat)
	if err != nil {
		return nil, err
	}

	verification := pkg.Verification
	verification.Warnings = localizePackageWarnings(pkg.Verification.Warnings, locale)
	warnings := localizePackageWarnings(pkg.Warnings, locale)
	if pkg.Manifest.Kind == KindTheme {
		themeWarns, tErr := pkg.ValidateTheme(m.themes)
		if tErr != nil {
			return nil, tErr
		}
		warnings = append(warnings, themeWarns...)
	}

	isNative := pkg.Manifest.EffectiveRuntime() == RuntimeNative
	isZh := IsChineseLocale(locale)
	nativeBlocked := false
	nativeBlockReason := ""
	if isNative {
		if !pkg.Verification.Trusted && !m.allowUnsignedNative {
			nativeBlocked = true
			if isZh {
				nativeBlockReason = "生产模式禁止安装未签名或不受信任的 Tier 2 原生插件。仅允许安装通过官方签名或超级管理员受信任 Ed25519 公钥验签的原生扩展包（仅限本地开发开启 plugins.allow_unsigned_native = true）。"
			} else {
				nativeBlockReason = "Production mode forbids installing unsigned or untrusted Tier 2 native plugins. Only officially signed or administrator-trusted Ed25519 packages are permitted (set plugins.allow_unsigned_native = true only in local development)."
			}
			warnings = append(warnings, nativeBlockReason)
		}
		if !pkg.HostCompatible {
			if isZh {
				warnings = append(warnings, fmt.Sprintf("该原生插件包不包含适用于当前宿主机平台 %s 的可执行程序。", hostPlat))
			} else {
				warnings = append(warnings, fmt.Sprintf("This native plugin package does not include an executable for host platform %s.", hostPlat))
			}
		}
	}

	var isUpgrade bool
	var curVer string
	var prevDeclared, prevGranted []string
	if m.db != nil {
		if row, gErr := m.db.Plugin.Get(ctx, pkg.Manifest.ID); gErr == nil {
			isUpgrade = true
			curVer = row.Version
			prevGranted = append([]string(nil), row.GrantedPermissions...)
			if sb, lErr := m.loadBundleFromRow(ctx, row, false); lErr == nil && sb != nil && sb.manifest != nil {
				prevDeclared = append([]string(nil), sb.manifest.EffectivePermissions()...)
			}
		}
	}

	permGroups, needsHighRiskConfirm := ClassifyPermissionsForLocale(pkg.Manifest.EffectivePermissions(), prevDeclared, prevGranted, isUpgrade, locale)
	if len(pkg.Manifest.Dependencies) > 0 {
		for _, depID := range sortedMapKeys(pkg.Manifest.Dependencies) {
			if !m.isPluginEnabled(depID) {
				if isZh {
					warnings = append(warnings, fmt.Sprintf("需要先安装并启用依赖插件 %q。", depID))
				} else {
					warnings = append(warnings, fmt.Sprintf("Requires dependency plugin %q to be installed and enabled.", depID))
				}
			}
		}
	}

	hooksCount := len(pkg.Manifest.Hooks.Guard) + len(pkg.Manifest.Hooks.Filter) + len(pkg.Manifest.Hooks.Event)
	return &PackageInspectResult{
		ID:                      pkg.Manifest.ID,
		Kind:                    pkg.Manifest.Kind,
		Runtime:                 pkg.Manifest.EffectiveRuntime(),
		Version:                 pkg.Manifest.Version,
		Name:                    pkg.Manifest.Name.Resolve(locale),
		Description:             pkg.Manifest.Description.Resolve(locale),
		Authors:                 append([]string(nil), pkg.Manifest.Authors...),
		Homepage:                pkg.Manifest.Homepage,
		IsUpgrade:               isUpgrade,
		CurrentVersion:          curVer,
		HasServer:               pkg.Manifest.Server != "" || isNative,
		HasWeb:                  pkg.Manifest.Web != "",
		RoutesCount:             len(pkg.Manifest.Routes.Public),
		HooksCount:              hooksCount,
		Slots:                   append([]string(nil), pkg.Manifest.Slots...),
		SettingsCount:           len(pkg.Manifest.Settings),
		Dependencies:            maps.Clone(pkg.Manifest.Dependencies),
		HostPlatform:            hostPlat,
		SupportedPlatforms:      append([]string(nil), pkg.SupportedPlatforms...),
		PlatformCompatible:      pkg.HostCompatible,
		AllowUnsignedNative:     m.allowUnsignedNative,
		NativeInstallBlocked:    nativeBlocked,
		NativeBlockReason:       nativeBlockReason,
		ArchiveSHA256:           pkg.ArchiveSHA256,
		ArchiveBytes:            len(pkg.ArchiveBytes),
		Verification:            verification,
		Permissions:             permGroups,
		RequiresHighRiskConfirm: needsHighRiskConfirm,
		Warnings:                warnings,
	}, nil
}

// InstallPackage verifies an .slp archive, extracts it cleanly, persists the archive blob in
// storage.Store for multi-replica sync, and atomically installs/upgrades the plugin or theme.
func (m *Manager) InstallPackage(ctx context.Context, pkgBytes []byte, opts InstallOptions) (*PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	hostPlat := m.hostPlatform()
	trusted := append(m.allTrustedKeys(), opts.ExtraTrustedKeys...)
	pkg, err := ParsePackageForPlatform(pkgBytes, trusted, hostPlat)
	if err != nil {
		return nil, err
	}
	isNative := pkg.Manifest.EffectiveRuntime() == RuntimeNative
	if isNative {
		if !pkg.Verification.Trusted {
			if !m.allowUnsignedNative {
				return nil, ErrUnsignedNativeForbidden
			}
			if !opts.AllowUnsigned {
				return nil, ErrUntrustedPackage
			}
		}
		if !pkg.HostCompatible {
			return nil, fmt.Errorf("%w: native plugin %q does not contain an executable for host platform %s", ErrBadPackage, pkg.Manifest.ID, hostPlat)
		}
	} else if !pkg.Verification.Trusted && !opts.AllowUnsigned {
		return nil, ErrUntrustedPackage
	}
	if pkg.Manifest.Kind == KindTheme {
		if _, err := pkg.ValidateTheme(m.themes); err != nil {
			return nil, err
		}
	}

	granted := resolveGrantedPermissions(pkg.Manifest.EffectivePermissions(), opts)
	if opts.Enable {
		if err := m.checkDependenciesActive(ctx, pkg.Manifest); err != nil {
			return nil, err
		}
	}

	source := opts.Source
	if source == "" {
		source = "package"
	}

	var oldSHA, prevDir string
	var oldPlatformSHAs map[string]string
	if m.db != nil {
		if oldRow, err := m.db.Plugin.Get(ctx, pkg.Manifest.ID); err == nil {
			oldSHA = oldRow.PackageSha256
			oldPlatformSHAs = maps.Clone(oldRow.PlatformPackages)
			if m.dataDir != "" {
				prevDir = filepath.Join(m.dataDir, "plugins", pkg.Manifest.ID, oldRow.Version)
			}
		}
	}

	var platformPkgs map[string]string
	if isNative {
		platformPkgs = make(map[string]string, len(opts.PlatformPackages)+len(pkg.ContainedPlatforms)+1)
		for k, v := range opts.PlatformPackages {
			if k != "" && v != "" {
				platformPkgs[k] = v
			}
		}
		for _, plat := range pkg.ContainedPlatforms {
			platformPkgs[plat] = pkg.ArchiveSHA256
		}
		platformPkgs[hostPlat] = pkg.ArchiveSHA256
	}

	if m.store != nil {
		if err := m.store.Put(ctx, pkg.ArchiveSHA256, pkg.ArchiveBytes); err != nil {
			return nil, fmt.Errorf("persist package blob: %w", err)
		}
	}

	var pluginDir, prevBackupDir string
	var themeDir, themeBackupDir string
	rollbackDirs := func() {
		if themeDir != "" {
			if themeBackupDir != "" {
				_ = os.RemoveAll(themeDir)
				_ = renameDirWithRetry(themeBackupDir, themeDir)
			} else {
				_ = os.RemoveAll(themeDir)
			}
		}
		if prevBackupDir != "" {
			_ = os.RemoveAll(pluginDir)
			_ = renameDirWithRetry(prevBackupDir, prevDir)
		} else if pluginDir != "" && pluginDir != prevDir {
			_ = os.RemoveAll(pluginDir)
		}
		if oldSHA != pkg.ArchiveSHA256 {
			m.cleanupUnusedPackageBlob(ctx, pkg.ArchiveSHA256)
		}
	}

	if m.dataDir != "" {
		pluginDir = filepath.Join(m.dataDir, "plugins", pkg.Manifest.ID, pkg.Manifest.Version)
		// Preserve existing same-version directory so rollback on activation failure restores it intact.
		if pluginDir == prevDir {
			if _, statErr := os.Stat(prevDir); statErr == nil {
				if tmpBak, mkErr := os.MkdirTemp(filepath.Dir(prevDir), ".prev-"+pkg.Manifest.ID+"-*"); mkErr == nil {
					_ = os.Remove(tmpBak)
					if rnErr := renameDirWithRetry(prevDir, tmpBak); rnErr == nil {
						prevBackupDir = tmpBak
					}
				}
			}
		}
		if err := ExtractPackageFiles(pkg, pluginDir); err != nil {
			rollbackDirs()
			return nil, fmt.Errorf("extract plugin files: %w", err)
		}
		if pkg.Manifest.Kind == KindTheme {
			themeDir = filepath.Join(m.dataDir, "themes", pkg.Manifest.ID)
			if _, statErr := os.Stat(themeDir); statErr == nil {
				if tmpBak, mkErr := os.MkdirTemp(filepath.Dir(themeDir), ".prev-theme-"+pkg.Manifest.ID+"-*"); mkErr == nil {
					_ = os.Remove(tmpBak)
					if rnErr := renameDirWithRetry(themeDir, tmpBak); rnErr == nil {
						themeBackupDir = tmpBak
					}
				}
			}
			if err := ExtractPackageFiles(pkg, themeDir); err != nil {
				rollbackDirs()
				return nil, fmt.Errorf("extract theme files: %w", err)
			}
		}
	}

	var staticOverlay map[string][]byte
	if pluginDir == "" {
		staticOverlay = pkg.Files
	}
	sb := &storedBundle{
		manifest:         pkg.Manifest,
		dir:              pluginDir,
		bundleCode:       pkg.ServerJS,
		staticFiles:      staticOverlay,
		sha256:           pkg.ArchiveSHA256,
		platformPackages: platformPkgs,
		registrySource:   strings.TrimSpace(opts.RegistrySource),
		source:           source,
	}

	info, err := m.installBundle(ctx, sb, granted, opts.Enable)
	if err != nil {
		rollbackDirs()
		return nil, err
	}
	if prevBackupDir != "" {
		_ = os.RemoveAll(prevBackupDir)
	}
	if themeBackupDir != "" {
		_ = os.RemoveAll(themeBackupDir)
	}
	cleaned := map[string]bool{pkg.ArchiveSHA256: true}
	if oldSHA != "" && !cleaned[oldSHA] {
		cleaned[oldSHA] = true
		m.cleanupUnusedPackageBlob(ctx, oldSHA)
	}
	for _, sha := range oldPlatformSHAs {
		if sha != "" && !cleaned[sha] {
			cleaned[sha] = true
			m.cleanupUnusedPackageBlob(ctx, sha)
		}
	}
	if pkg.Manifest.Kind == KindTheme {
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)
	return m.enrichInfo(ctx, info), nil
}

// ReloadPlugin reloads a plugin or theme cleanly from disk or shared storage.
func (m *Manager) ReloadPlugin(ctx context.Context, id string) (*PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.db == nil {
		return nil, errors.New("database unavailable")
	}
	row, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sb, err := m.loadBundleFromRow(ctx, row, true)
	if err != nil {
		return nil, err
	}
	if row.Status == entplugin.StatusEnabled {
		if err := m.checkDependenciesActive(ctx, sb.manifest); err != nil {
			return nil, err
		}
		if err := m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, "", true); err != nil {
			return nil, err
		}
	}
	if row.Kind == entplugin.KindTheme {
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)
	info, ok := m.Get(ctx, id)
	if !ok {
		return nil, fmt.Errorf("plugin %q not found after reload", id)
	}
	return info, nil
}

// SyncCluster reconciles this node's in-memory runtimes and local extracted bundles
// with the shared DB `plugins` table and shared package `storage.Store`.
func (m *Manager) SyncCluster(ctx context.Context) error {
	if m == nil || m.db == nil {
		return nil
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	rows, err := m.db.Plugin.Query().Order(ent.Asc(entplugin.FieldID)).All(ctx)
	if err != nil {
		return err
	}

	rowByID := make(map[string]*ent.Plugin, len(rows))
	for _, row := range rows {
		rowByID[row.ID] = row
	}

	type activeSnap struct {
		id string
		rt *pluginRuntime
	}
	m.mu.RLock()
	snaps := make([]activeSnap, 0, len(m.active))
	for id, rt := range m.active {
		snaps = append(snaps, activeSnap{id: id, rt: rt})
	}
	var orphanedStored []struct {
		id      string
		isTheme bool
	}
	for id, sb := range m.stored {
		if _, exists := rowByID[id]; !exists {
			orphanedStored = append(orphanedStored, struct {
				id      string
				isTheme bool
			}{id: id, isTheme: sb != nil && sb.manifest != nil && sb.manifest.Kind == KindTheme})
		}
	}
	m.mu.RUnlock()

	themeChanged := false
	for _, s := range snaps {
		row, exists := rowByID[s.id]
		if !exists || row.Status != entplugin.StatusEnabled {
			if m.bus != nil {
				m.bus.Remove("plugin:" + s.id)
			}
			m.unregisterHostExtensions(s.id)
			m.mu.Lock()
			if cur := m.active[s.id]; cur == s.rt {
				delete(m.active, s.id)
			}
			if !exists {
				delete(m.stored, s.id)
			}
			m.mu.Unlock()
			if s.rt != nil {
				_ = s.rt.invokeLifecycle(ctx, "onDisable", nil)
				isTheme := s.rt.manifest.Kind == KindTheme
				go s.rt.close(defaultDrainTimeout)
				if isTheme {
					themeChanged = true
				}
			}
			if !exists {
				m.removeLocalDirs(s.id, s.rt != nil && s.rt.manifest.Kind == KindTheme)
			}
		}
	}
	for _, orph := range orphanedStored {
		m.unregisterHostExtensions(orph.id)
		m.mu.Lock()
		delete(m.stored, orph.id)
		m.mu.Unlock()
		m.removeLocalDirs(orph.id, orph.isTheme)
		if orph.isTheme {
			themeChanged = true
		}
	}
	if m.dataDir != "" {
		if entries, readErr := os.ReadDir(filepath.Join(m.dataDir, "plugins")); readErr == nil {
			for _, entDir := range entries {
				if !entDir.IsDir() || strings.HasPrefix(entDir.Name(), ".") {
					continue
				}
				if _, exists := rowByID[entDir.Name()]; !exists {
					_ = os.RemoveAll(filepath.Join(m.dataDir, "plugins", entDir.Name()))
				}
			}
		}
		if entries, readErr := os.ReadDir(filepath.Join(m.dataDir, "themes")); readErr == nil {
			for _, entDir := range entries {
				if !entDir.IsDir() || strings.HasPrefix(entDir.Name(), ".") {
					continue
				}
				if _, exists := rowByID[entDir.Name()]; exists {
					continue
				}
				// Only prune theme dirs managed by an uninstalled .slp package (.package.sha256 marker).
				markerPath := filepath.Join(m.dataDir, "themes", entDir.Name(), ".package.sha256")
				if _, statErr := os.Stat(markerPath); statErr == nil {
					_ = os.RemoveAll(filepath.Join(m.dataDir, "themes", entDir.Name()))
					themeChanged = true
				}
			}
		}
	}

	ordered := m.sortPluginRowsByDeps(ctx, rows)
	for _, row := range ordered {
		if row.Status != entplugin.StatusEnabled {
			if row.Kind == entplugin.KindTheme && m.dataDir != "" && row.PackageSha256 != "" {
				marker, err := os.ReadFile(filepath.Join(m.dataDir, "themes", row.ID, ".package.sha256"))
				if err != nil || strings.TrimSpace(string(marker)) != row.PackageSha256 {
					if _, loadErr := m.loadBundleFromRow(ctx, row, true); loadErr == nil {
						themeChanged = true
					}
				}
			}
			continue
		}
		m.mu.RLock()
		rt := m.active[row.ID]
		m.mu.RUnlock()

		expectedSHA := m.resolveRowPackageSHA(row)
		needsActivate := rt == nil ||
			rt.closed.Load() ||
			rt.manifest.Version != row.Version ||
			(expectedSHA != "" && rt.packageSHA256 != expectedSHA) ||
			!stringSlicesEqualUnordered(rt.grantedPerms, row.GrantedPermissions)

		if needsActivate {
			sb, err := m.loadBundleFromRow(ctx, row, true)
			if err != nil {
				// Never write StatusFailed into the shared DB during SyncCluster: a single
				// node with missing local files or a different OS/Arch must not poison
				// healthy sibling replicas in the cluster.
				m.log.Warn("cluster sync failed to load plugin on this node", "plugin", row.ID, "platform", m.hostPlatform(), "err", err)
				continue
			}
			if err := m.checkDependenciesActive(ctx, sb.manifest); err != nil {
				m.log.Warn("cluster sync skipped plugin with inactive dependency on this node", "plugin", row.ID, "err", err)
				continue
			}
			prevVer := ""
			if rt != nil && rt.manifest.Version != row.Version {
				prevVer = rt.manifest.Version
			}
			if err := m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, prevVer, false); err != nil {
				m.log.Warn("cluster sync failed to activate plugin on this node", "plugin", row.ID, "err", err)
				continue
			}
			if row.Kind == entplugin.KindTheme {
				themeChanged = true
			}
			continue
		}

		if rt != nil {
			rt.settingsMu.RLock()
			curRaw := cloneMap(rt.rawSettings)
			rt.settingsMu.RUnlock()
			if !reflect.DeepEqual(normalizeNilMap(curRaw), normalizeNilMap(row.Settings)) {
				rt.notifySettingsChanged(ctx, row.Settings)
				if row.Kind == entplugin.KindTheme {
					themeChanged = true
				}
			}
		}
	}

	if themeChanged {
		m.reloadThemes()
	}
	return nil
}

// PluginSettingsView returns the plugin's declared setting fields and redacted stored values.
func (m *Manager) PluginSettingsView(ctx context.Context, id, locale string) (*PluginSettingsResponse, error) {
	if m.db == nil {
		return nil, errors.New("database unavailable")
	}
	row, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	mf := m.manifestForRow(ctx, id)

	schemaMap := map[string]any{}
	if mf != nil && mf.Settings != nil {
		schemaMap = mf.Settings
	}
	keys := make([]string, 0, len(schemaMap))
	for k := range schemaMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fields := make([]PluginSettingFieldView, 0, len(keys))
	redactedValues := redactSettingsMap(mf, row.Settings)
	for _, k := range keys {
		rawDef, ok := schemaMap[k].(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(strVal(rawDef["type"])))
		if typ == "" {
			typ = "string"
		}
		isSecret := typ == "secret"
		rawStored, hasStored := row.Settings[k]
		isSet := hasStored && rawStored != nil
		if isSecret {
			if s, ok := rawStored.(string); ok && s == "" {
				isSet = false
			}
		}
		val := rawDef["default"]
		if typ == "group" || typ == "list" {
			if typ == "group" {
				val = redactedValues[k]
			} else if _, hasRedacted := redactedValues[k]; hasRedacted {
				val = redactedValues[k]
			} else if itemDef, ok := rawDef["item"].(map[string]any); ok && strings.EqualFold(strVal(itemDef["type"]), "secret") {
				val = nil
			}
		} else if hasStored && rawStored != nil {
			val = rawStored
		}
		if isSecret {
			val = nil
		}
		field := PluginSettingFieldView{
			Key:         k,
			Type:        typ,
			Label:       cmpOrString(resolveAnyText(rawDef["label"], locale), k),
			Description: resolveAnyText(rawDef["description"], locale),
			Value:       val,
			Default:     maskSecretDefault(isSecret, rawDef["default"]),
			IsSet:       isSet,
			Secret:      isSecret,
			Required:    boolVal(rawDef["required"]),
			Order:       int(asFloat(rawDef["order"])),
		}
		switch optsRaw := rawDef["options"].(type) {
		case []any:
			for _, item := range optsRaw {
				field.Options = append(field.Options, fmt.Sprint(item))
			}
		case []string:
			field.Options = append(field.Options, optsRaw...)
		}
		if minV, ok := numPtr(rawDef["min"]); ok {
			field.Min = minV
		}
		if maxV, ok := numPtr(rawDef["max"]); ok {
			field.Max = maxV
		}
		if stepV, ok := numPtr(rawDef["step"]); ok {
			field.Step = stepV
		}
		if subFields, ok := rawDef["fields"].(map[string]any); ok {
			field.Fields = sanitizeGroupFieldSchemaForView(subFields, rawStored)
		}
		if itemSchema, ok := rawDef["item"].(map[string]any); ok {
			field.Item = cloneMap(itemSchema)
		}
		fields = append(fields, field)
	}

	sort.SliceStable(fields, func(i, j int) bool {
		if fields[i].Order != fields[j].Order {
			return fields[i].Order < fields[j].Order
		}
		return fields[i].Key < fields[j].Key
	})

	return &PluginSettingsResponse{
		PluginID: row.ID,
		Fields:   fields,
		Values:   redactedValues,
	}, nil
}

func (m *Manager) startWithSync(ctx context.Context) error {
	if m.db == nil {
		return nil
	}
	rows, err := m.db.Plugin.Query().Order(ent.Asc(entplugin.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	ordered := m.sortPluginRowsByDeps(ctx, rows)
	themeExtracted := false
	for _, row := range ordered {
		sb, err := m.loadBundleFromRow(ctx, row, false)
		if err != nil {
			if row.Status == entplugin.StatusEnabled {
				m.markFailed(ctx, row.ID, err)
			}
			continue
		}
		if row.Kind == entplugin.KindTheme {
			themeExtracted = true
		}
		if row.Status != entplugin.StatusEnabled {
			continue
		}
		if err := m.checkDependenciesActive(ctx, sb.manifest); err != nil {
			m.markFailed(ctx, row.ID, err)
			continue
		}
		_ = m.activateBundle(ctx, sb, row.GrantedPermissions, row.Settings, false, "", true)
	}
	if themeExtracted {
		m.reloadThemes()
	}
	return nil
}

func (m *Manager) resolveRowPackageSHA(row *ent.Plugin) string {
	if row == nil {
		return ""
	}
	if row.Runtime == entplugin.RuntimeNative && len(row.PlatformPackages) > 0 {
		if sha := strings.TrimSpace(row.PlatformPackages[m.hostPlatform()]); sha != "" {
			return sha
		}
	}
	return row.PackageSha256
}

func (m *Manager) loadBundleFromRow(ctx context.Context, row *ent.Plugin, forceReload bool) (*storedBundle, error) {
	if row == nil {
		return nil, errors.New("nil plugin row")
	}
	hostPlat := m.hostPlatform()
	targetSHA := m.resolveRowPackageSHA(row)

	if !forceReload {
		m.mu.RLock()
		cached := m.stored[row.ID]
		m.mu.RUnlock()
		if cached != nil && cached.manifest.Version == row.Version && (targetSHA == "" || cached.sha256 == targetSHA) {
			return cached, nil
		}
	}

	if row.Runtime == entplugin.RuntimeNative && row.Source == "local" && !m.allowUnsignedNative {
		return nil, ErrUnsignedNativeForbidden
	}

	var dir string
	resolvedSHA := targetSHA
	if m.dataDir != "" {
		cand := filepath.Join(m.dataDir, "plugins", row.ID, row.Version)
		if _, err := os.Stat(filepath.Join(cand, "plugin.json")); err == nil {
			dir = cand
		} else if row.Source == "local" {
			legacy := filepath.Join(m.dataDir, "plugins", row.ID)
			if _, err := os.Stat(filepath.Join(legacy, "plugin.json")); err == nil {
				dir = legacy
			}
		} else if row.Kind == entplugin.KindTheme {
			themeDir := filepath.Join(m.dataDir, "themes", row.ID)
			if _, err := os.Stat(filepath.Join(themeDir, "plugin.json")); err == nil {
				dir = themeDir
			}
		}

		// Verify extracted package marker matches targetSHA when Store is available.
		if dir != "" && targetSHA != "" && row.Source != "local" && m.store != nil {
			marker, err := os.ReadFile(filepath.Join(dir, ".package.sha256"))
			if err != nil || strings.TrimSpace(string(marker)) != targetSHA {
				dir = ""
			}
		}

		// In production mode (!allowUnsignedNative), never blindly trust pre-extracted
		// native plugin binaries on disk without re-verifying the Ed25519 signature
		// against currently trusted keys from storage.Store (prevents revoked-key and
		// disk-tamper bypasses across restarts and reloads).
		if row.Runtime == entplugin.RuntimeNative && row.Source != "local" && !m.allowUnsignedNative {
			if m.store == nil || targetSHA == "" {
				return nil, ErrUnsignedNativeForbidden
			}
			dir = ""
		}

		// Also verify a locally extracted native dir actually contains a binary for hostPlat.
		if dir != "" && row.Runtime == entplugin.RuntimeNative && row.Source != "local" {
			if rawMf, rErr := os.ReadFile(filepath.Join(dir, "plugin.json")); rErr == nil {
				if parsedMf, pErr := ParseManifest(rawMf); pErr == nil {
					goos, goarch, _ := strings.Cut(hostPlat, "/")
					if _, exErr := parsedMf.ResolveNativeExecutable(dir, goos, goarch); exErr != nil {
						dir = ""
					}
				}
			}
		}

		extractAndVerify := func(slpBytes []byte, wantSHA string) (string, string, error) {
			pkg, parseErr := ParsePackageForPlatform(slpBytes, m.allTrustedKeys(), hostPlat)
			if parseErr != nil {
				return "", "", parseErr
			}
			if pkg.Manifest.ID != row.ID {
				return "", "", fmt.Errorf("%w: package manifest id %q does not match plugin %q", ErrBadPackage, pkg.Manifest.ID, row.ID)
			}
			if row.Version != "" && pkg.Manifest.Version != row.Version {
				return "", "", fmt.Errorf("%w: package manifest version %q does not match expected %q", ErrBadPackage, pkg.Manifest.Version, row.Version)
			}
			if wantSHA != "" && pkg.ArchiveSHA256 != wantSHA {
				return "", "", fmt.Errorf("%w: package archive sha256 %q does not match expected %q", ErrBadPackage, pkg.ArchiveSHA256, wantSHA)
			}
			if pkg.Manifest.EffectiveRuntime() == RuntimeNative {
				if !pkg.Verification.Trusted && !m.allowUnsignedNative {
					return "", "", ErrUnsignedNativeForbidden
				}
				if !pkg.HostCompatible {
					return "", "", fmt.Errorf("%w: native package %q does not contain executable for %s", ErrBadPackage, row.ID, hostPlat)
				}
			}
			targetDir := filepath.Join(m.dataDir, "plugins", row.ID, row.Version)
			if err := ExtractPackageFiles(pkg, targetDir); err != nil {
				return "", "", err
			}
			if pkg.Manifest.Kind == KindTheme {
				if err := ExtractPackageFiles(pkg, filepath.Join(m.dataDir, "themes", row.ID)); err != nil {
					return "", "", fmt.Errorf("extract theme files: %w", err)
				}
			}
			return targetDir, pkg.ArchiveSHA256, nil
		}

		if dir == "" && targetSHA != "" && (m.store != nil || m.legacyStore != nil) {
			slpBytes, readErr := m.readPackageBlob(ctx, targetSHA)
			if readErr == nil && len(slpBytes) <= MaxNativeFatPackageBytes {
				extractedDir, sha, exErr := extractAndVerify(slpBytes, targetSHA)
				if exErr == nil {
					dir = extractedDir
					resolvedSHA = sha
				} else if errors.Is(exErr, ErrUnsignedNativeForbidden) || row.Runtime != entplugin.RuntimeNative || row.RegistrySource == "" {
					return nil, exErr
				}
				// If native package from shared store was for another arch and
				// registry_source is configured, fall through to registry fallback below.
			}
		}

		// Multi-arch cluster fallback: when this node's GOOS/GOARCH binary is not
		// yet present in storage.Store, pull the platform-specific package from
		// registry_source, verify its signature, cache it in storage.Store, and
		// record its SHA in plugins.platform_packages for sibling nodes of the same arch.
		if dir == "" && row.Runtime == entplugin.RuntimeNative && row.RegistrySource != "" {
			m.mu.RLock()
			fetcher := m.platformFetcher
			m.mu.RUnlock()
			if fetcher != nil {
				slpBytes, fErr := fetcher(ctx, row.RegistrySource, row.ID, row.Version, hostPlat)
				if fErr != nil {
					return nil, fmt.Errorf("fetch native platform package %s/%s (%s): %w", row.ID, row.Version, hostPlat, fErr)
				}
				extractedDir, sha, exErr := extractAndVerify(slpBytes, "")
				if exErr != nil {
					return nil, exErr
				}
				if m.store != nil {
					if putErr := m.store.Put(ctx, sha, slpBytes); putErr != nil {
						return nil, fmt.Errorf("cache fetched platform package: %w", putErr)
					}
				}
				if m.db != nil {
					_ = db.WithTx(ctx, m.db, func(tx *ent.Client) error {
						cur, gErr := tx.Plugin.Get(ctx, row.ID)
						if gErr != nil {
							return gErr
						}
						nextPlats := maps.Clone(cur.PlatformPackages)
						if nextPlats == nil {
							nextPlats = map[string]string{}
						}
						nextPlats[hostPlat] = sha
						return tx.Plugin.UpdateOneID(row.ID).SetPlatformPackages(nextPlats).Exec(ctx)
					})
				}
				dir = extractedDir
				resolvedSHA = sha
			}
		}
	}

	if dir == "" {
		m.mu.RLock()
		cached := m.stored[row.ID]
		m.mu.RUnlock()
		if cached != nil && cached.manifest.Version == row.Version && (resolvedSHA == "" || cached.sha256 == resolvedSHA) {
			return cached, nil
		}
		return nil, fmt.Errorf("missing manifest for plugin %q version %q (platform %s)", row.ID, row.Version, hostPlat)
	}

	manifestRaw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest in %s: %w", dir, err)
	}
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if mf.ID != row.ID {
		return nil, fmt.Errorf("%w: manifest id %q does not match plugin %q", ErrBadManifest, mf.ID, row.ID)
	}
	var serverJS []byte
	if mf.Server != "" {
		serverJS, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(mf.Server)))
		if err != nil {
			return nil, fmt.Errorf("read server bundle %q: %w", mf.Server, err)
		}
		if len(serverJS) > MaxServerBundleBytes {
			return nil, fmt.Errorf("server bundle %q exceeds %d byte limit", mf.Server, MaxServerBundleBytes)
		}
	}
	sb := &storedBundle{
		manifest:         mf,
		dir:              dir,
		bundleCode:       serverJS,
		sha256:           resolvedSHA,
		platformPackages: maps.Clone(row.PlatformPackages),
		registrySource:   row.RegistrySource,
		source:           row.Source,
	}
	m.mu.Lock()
	m.stored[mf.ID] = sb
	m.mu.Unlock()
	return sb, nil
}

func (m *Manager) manifestForRow(ctx context.Context, id string) *Manifest {
	m.mu.RLock()
	rt := m.active[id]
	sb := m.stored[id]
	m.mu.RUnlock()
	if rt != nil && rt.manifest != nil {
		return rt.manifest
	}
	if sb != nil && sb.manifest != nil {
		return sb.manifest
	}
	if m.db != nil {
		if row, err := m.db.Plugin.Get(ctx, id); err == nil {
			if loaded, lErr := m.loadBundleFromRow(ctx, row, false); lErr == nil && loaded != nil {
				return loaded.manifest
			}
		}
	}
	return nil
}

func (m *Manager) checkDependenciesActive(_ context.Context, mf *Manifest) error {
	if mf == nil || len(mf.Dependencies) == 0 {
		return nil
	}
	deps := make([]string, 0, len(mf.Dependencies))
	for depID := range mf.Dependencies {
		deps = append(deps, depID)
	}
	sort.Strings(deps)
	for _, depID := range deps {
		if !m.isPluginEnabled(depID) {
			return fmt.Errorf("%w: %q requires active plugin %q", ErrMissingDependency, mf.ID, depID)
		}
	}
	return nil
}

func (m *Manager) isPluginEnabled(id string) bool {
	m.mu.RLock()
	rt := m.active[id]
	m.mu.RUnlock()
	return rt != nil && !rt.closed.Load()
}

func (m *Manager) sortPluginRowsByDeps(ctx context.Context, rows []*ent.Plugin) []*ent.Plugin {
	if len(rows) <= 1 {
		return rows
	}
	byID := make(map[string]*ent.Plugin, len(rows))
	depsOf := make(map[string][]string, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
		if sb, err := m.loadBundleFromRow(ctx, r, false); err == nil && sb != nil && sb.manifest != nil {
			for dep := range sb.manifest.Dependencies {
				depsOf[r.ID] = append(depsOf[r.ID], dep)
			}
			sort.Strings(depsOf[r.ID])
		}
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)

	visited := make(map[string]int, len(rows)) // 0=unvisited, 1=visiting, 2=done
	ordered := make([]*ent.Plugin, 0, len(rows))
	var visit func(id string)
	visit = func(id string) {
		if visited[id] != 0 {
			return
		}
		visited[id] = 1
		for _, dep := range depsOf[id] {
			if _, ok := byID[dep]; ok {
				visit(dep)
			}
		}
		visited[id] = 2
		ordered = append(ordered, byID[id])
	}
	for _, id := range ids {
		visit(id)
	}
	return ordered
}

func (m *Manager) allTrustedKeys() []string {
	m.mu.RLock()
	keys := append([]string(nil), m.trustedPublicKeys...)
	reg := m.registry
	m.mu.RUnlock()
	if reg != nil {
		if testOff := reg.testOfficialPublicKeyOverride(); testOff != "" {
			keys = append(keys, testOff)
		}
	}
	if m.settings != nil {
		if extra := settings.Decode[[]string](m.settings.Current(), "plugins.trusted_keys"); len(extra) > 0 {
			keys = append(keys, extra...)
		}
	}
	return keys
}

func (m *Manager) reloadThemes() {
	if m.themes != nil {
		m.themes.Reload()
	}
}

func (m *Manager) bumpConfigVersion(ctx context.Context) {
	if m.settings != nil {
		_, _ = m.settings.Touch(ctx)
	}
}

func (m *Manager) removeLocalDirs(id string, removeThemeDir bool) {
	if m.dataDir == "" || id == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(m.dataDir, "plugins", id))
	if removeThemeDir {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "themes", id))
	}
}

func (m *Manager) readPackageBlob(ctx context.Context, pkgSHA string) ([]byte, error) {
	if pkgSHA == "" {
		return nil, storage.ErrNotFound
	}
	if m.store != nil {
		rc, err := m.store.Open(ctx, pkgSHA)
		if err == nil {
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, MaxNativeFatPackageBytes+1))
		}
		if !errors.Is(err, storage.ErrNotFound) || m.legacyStore == nil {
			return nil, err
		}
	}
	if m.legacyStore == nil {
		return nil, storage.ErrNotFound
	}
	rc, err := m.legacyStore.Open(ctx, pkgSHA)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(rc, MaxNativeFatPackageBytes+1))
	_ = rc.Close()
	if readErr != nil {
		return nil, readErr
	}
	if m.store != nil && len(data) <= MaxNativeFatPackageBytes {
		if putErr := m.store.Put(ctx, pkgSHA, data); putErr == nil {
			m.deleteLegacyPackageBlobIfUnreferencedTexture(ctx, pkgSHA)
		}
	}
	return data, nil
}

// migrateLegacyPackageBlobs moves any v1.1 .slp archives that were stored in the
// public texture store into the dedicated private plugin package store on boot,
// removing the legacy public copy once safely persisted.
func (m *Manager) migrateLegacyPackageBlobs(ctx context.Context) {
	if m.store == nil || m.legacyStore == nil || m.db == nil {
		return
	}
	rows, err := m.db.Plugin.Query().
		Select(entplugin.FieldPackageSha256, entplugin.FieldPlatformPackages).
		All(ctx)
	if err != nil {
		return
	}
	seen := map[string]struct{}{}
	migrateOne := func(sha string) {
		if sha == "" {
			return
		}
		if _, dup := seen[sha]; dup {
			return
		}
		seen[sha] = struct{}{}
		if _, err := m.store.Stat(ctx, sha); err == nil {
			// Already present in the private package store; remove any leftover public copy.
			m.deleteLegacyPackageBlobIfUnreferencedTexture(ctx, sha)
			return
		}
		rc, err := m.legacyStore.Open(ctx, sha)
		if err != nil {
			return
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, MaxNativeFatPackageBytes+1))
		_ = rc.Close()
		if readErr != nil || len(data) > MaxNativeFatPackageBytes {
			return
		}
		if putErr := m.store.Put(ctx, sha, data); putErr != nil {
			m.log.Warn("migrate legacy plugin package blob failed", "sha256", sha, "err", putErr)
			return
		}
		m.deleteLegacyPackageBlobIfUnreferencedTexture(ctx, sha)
		m.log.Info("migrated plugin package archive to isolated private store", "sha256", sha)
	}
	for _, r := range rows {
		migrateOne(r.PackageSha256)
		for _, sha := range r.PlatformPackages {
			migrateOne(sha)
		}
	}
}

func (m *Manager) deleteLegacyPackageBlobIfUnreferencedTexture(ctx context.Context, pkgSHA string) {
	if m.legacyStore == nil || pkgSHA == "" {
		return
	}
	if m.db != nil {
		isTexture, err := m.db.Blob.Query().Where(blob.ID(pkgSHA)).Exist(ctx)
		if err != nil || isTexture {
			return
		}
	}
	_ = m.legacyStore.Delete(ctx, pkgSHA)
}

func (m *Manager) cleanupUnusedPackageBlob(ctx context.Context, pkgSHA string) {
	if (m.store == nil && m.legacyStore == nil) || m.db == nil || pkgSHA == "" {
		return
	}
	rows, err := m.db.Plugin.Query().
		Select(entplugin.FieldPackageSha256, entplugin.FieldPlatformPackages).
		All(ctx)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.PackageSha256 == pkgSHA {
			return
		}
		for _, sha := range r.PlatformPackages {
			if sha == pkgSHA {
				return
			}
		}
	}
	if m.store != nil {
		_ = m.store.Delete(ctx, pkgSHA)
	}
	m.deleteLegacyPackageBlobIfUnreferencedTexture(ctx, pkgSHA)
}

func (m *Manager) pluginUsage(ctx context.Context, id string) *PluginUsage {
	if m.db == nil {
		return nil
	}
	usage := &PluginUsage{}
	if n, err := m.db.PluginKV.Query().Where(pluginkv.Plugin(id)).Count(ctx); err == nil {
		usage.KVKeys = n
	}
	if n, err := m.db.PluginDoc.Query().Where(plugindoc.Plugin(id)).Count(ctx); err == nil {
		usage.Docs = n
	}
	if m.dataDir != "" {
		root := filepath.Join(m.dataDir, "plugins-data", id, "files")
		_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, statErr := d.Info(); statErr == nil {
				usage.FilesBytes += info.Size()
			}
			return nil
		})
	}
	return usage
}

func (m *Manager) enrichInfo(ctx context.Context, info *PluginInfo) *PluginInfo {
	if info == nil {
		return nil
	}
	if full, ok := m.Get(ctx, info.ID); ok {
		return full
	}
	return info
}

func resolveGrantedPermissions(declared []string, opts InstallOptions) []string {
	declSet := make(map[string]bool, len(declared))
	for _, p := range declared {
		declSet[p] = true
	}
	out := make([]string, 0, len(declared))
	seen := map[string]bool{}
	add := func(p string) {
		if declSet[p] && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if opts.GrantAllDeclared {
		for _, p := range declared {
			add(p)
		}
		return out
	}
	if opts.GrantedPermissions != nil {
		for _, p := range opts.GrantedPermissions {
			add(p)
		}
		return out
	}
	for _, p := range declared {
		if def, ok := LookupPermission(p); ok && def.Tier == RiskNormal {
			add(p)
		}
	}
	return out
}

func cloneJSONValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []string:
		return append([]string(nil), x...)
	default:
		return v
	}
}

func sanitizeGroupFieldSchemaForView(subFields map[string]any, rawStored any) map[string]any {
	if len(subFields) == 0 {
		return nil
	}
	storedMap, _ := rawStored.(map[string]any)
	out := make(map[string]any, len(subFields))
	for sk, rawSub := range subFields {
		subDef, ok := rawSub.(map[string]any)
		if !ok {
			continue
		}
		cp := cloneMap(subDef)
		if strings.EqualFold(strVal(cp["type"]), "secret") {
			delete(cp, "default")
			if s, ok := storedMap[sk].(string); ok && s != "" {
				cp["isSet"] = true
			}
		}
		out[sk] = cp
	}
	return out
}

func redactSettingsWithSchema(schemaMap map[string]any, stored map[string]any) map[string]any {
	if len(schemaMap) == 0 || len(stored) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(stored))
	for k, v := range stored {
		rawDef, known := schemaMap[k]
		if !known {
			continue
		}
		def, ok := rawDef.(map[string]any)
		if !ok {
			out[k] = cloneJSONValue(v)
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(strVal(def["type"])))
		if typ == "secret" {
			continue
		}
		if typ == "group" {
			subFields, _ := def["fields"].(map[string]any)
			if subMap, ok := v.(map[string]any); ok {
				out[k] = redactSettingsWithSchema(subFields, subMap)
			}
			continue
		}
		if typ == "list" {
			if itemDef, ok := def["item"].(map[string]any); ok {
				itemType := strings.ToLower(strings.TrimSpace(strVal(itemDef["type"])))
				if itemType == "secret" {
					continue
				}
				if itemType == "group" {
					subFields, _ := itemDef["fields"].(map[string]any)
					if items, ok := v.([]any); ok {
						redactedList := make([]any, 0, len(items))
						for _, it := range items {
							if itMap, ok := it.(map[string]any); ok {
								redactedList = append(redactedList, redactSettingsWithSchema(subFields, itMap))
							}
						}
						out[k] = redactedList
						continue
					}
				}
			}
		}
		out[k] = cloneJSONValue(v)
	}
	return out
}

func redactSettingsMap(mf *Manifest, stored map[string]any) map[string]any {
	if mf == nil || len(mf.Settings) == 0 || len(stored) == 0 {
		return map[string]any{}
	}
	return redactSettingsWithSchema(mf.Settings, stored)
}

func validateAndMergeSettingsPatch(mf *Manifest, existing, patch map[string]any) (map[string]any, error) {
	if mf == nil {
		return nil, fmt.Errorf("%w: manifest unavailable", ErrInvalidSettings)
	}
	return mergeSettingsPatchLevel(mf.Settings, existing, patch, "")
}

func isEmptySettingValue(typ string, val any) bool {
	if val == nil {
		return true
	}
	switch typ {
	case "", "string", "text", "markdown", "color", "image", "select", "choice", "secret":
		s, ok := val.(string)
		return ok && strings.TrimSpace(s) == ""
	case "multiselect", "list":
		switch x := val.(type) {
		case []string:
			return len(x) == 0
		case []any:
			return len(x) == 0
		}
	}
	return false
}

func mergeSettingsPatchLevel(schemaMap, existing, patch map[string]any, prefix string) (map[string]any, error) {
	merged := cloneMap(existing)
	for k, val := range patch {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		if len(schemaMap) == 0 {
			return nil, fmt.Errorf("%w: unknown setting key %q", ErrInvalidSettings, fullKey)
		}
		rawDef, known := schemaMap[k]
		if !known {
			return nil, fmt.Errorf("%w: unknown setting key %q", ErrInvalidSettings, fullKey)
		}
		def, _ := rawDef.(map[string]any)
		typ := strings.ToLower(strings.TrimSpace(strVal(def["type"])))
		required := boolVal(def["required"])

		if typ == "secret" {
			if val == nil {
				if required {
					return nil, fmt.Errorf("%w: setting %q is required", ErrInvalidSettings, fullKey)
				}
				delete(merged, k)
				continue
			}
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("%w: setting %q must be a string", ErrInvalidSettings, fullKey)
			}
			if s == settings.Redacted {
				if required && strVal(merged[k]) == "" {
					return nil, fmt.Errorf("%w: setting %q is required", ErrInvalidSettings, fullKey)
				}
				continue
			}
			if s == "" {
				if required {
					return nil, fmt.Errorf("%w: setting %q is required", ErrInvalidSettings, fullKey)
				}
				delete(merged, k)
			} else {
				merged[k] = s
			}
			continue
		}
		if val == nil {
			if required {
				return nil, fmt.Errorf("%w: setting %q is required", ErrInvalidSettings, fullKey)
			}
			delete(merged, k)
			continue
		}
		if typ == "group" {
			patchMap, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: setting %q must be an object", ErrInvalidSettings, fullKey)
			}
			subSchema, _ := def["fields"].(map[string]any)
			subExisting, _ := merged[k].(map[string]any)
			subMerged, err := mergeSettingsPatchLevel(subSchema, subExisting, patchMap, fullKey)
			if err != nil {
				return nil, err
			}
			merged[k] = subMerged
			continue
		}
		if err := validateSettingFieldValue(fullKey, typ, def, val); err != nil {
			return nil, err
		}
		if required && isEmptySettingValue(typ, val) {
			return nil, fmt.Errorf("%w: setting %q is required", ErrInvalidSettings, fullKey)
		}
		merged[k] = cloneJSONValue(val)
	}
	return merged, nil
}

func validateSettingFieldValue(key, typ string, def map[string]any, val any) error {
	switch typ {
	case "", "string", "text", "markdown", "color", "image":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%w: setting %q must be a string", ErrInvalidSettings, key)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%w: setting %q must be a boolean", ErrInvalidSettings, key)
		}
	case "number":
		switch v := val.(type) {
		case float64, float32, int, int64:
			n := asFloat(v)
			if minV, ok := numPtr(def["min"]); ok && n < *minV {
				return fmt.Errorf("%w: setting %q must be >= %v", ErrInvalidSettings, key, *minV)
			}
			if maxV, ok := numPtr(def["max"]); ok && n > *maxV {
				return fmt.Errorf("%w: setting %q must be <= %v", ErrInvalidSettings, key, *maxV)
			}
		default:
			return fmt.Errorf("%w: setting %q must be a number", ErrInvalidSettings, key)
		}
	case "select", "choice":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("%w: setting %q must be a string", ErrInvalidSettings, key)
		}
		if allowed, hasOpts := optionSet(def["options"]); hasOpts && !allowed[s] {
			return fmt.Errorf("%w: setting %q has invalid choice %q", ErrInvalidSettings, key, s)
		}
	case "multiselect":
		items, ok := toStringSlice(val)
		if !ok {
			return fmt.Errorf("%w: setting %q must be an array of strings", ErrInvalidSettings, key)
		}
		if allowed, hasOpts := optionSet(def["options"]); hasOpts {
			for _, item := range items {
				if !allowed[item] {
					return fmt.Errorf("%w: setting %q contains invalid option %q", ErrInvalidSettings, key, item)
				}
			}
		}
	case "list":
		switch val.(type) {
		case []any, []string:
		default:
			return fmt.Errorf("%w: setting %q must be an array", ErrInvalidSettings, key)
		}
	case "group":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("%w: setting %q must be an object", ErrInvalidSettings, key)
		}
	case "json":
		// Any JSON value is allowed.
	}
	return nil
}

func optionSet(raw any) (map[string]bool, bool) {
	switch list := raw.(type) {
	case []string:
		if len(list) == 0 {
			return nil, false
		}
		out := make(map[string]bool, len(list))
		for _, item := range list {
			out[item] = true
		}
		return out, true
	case []any:
		if len(list) == 0 {
			return nil, false
		}
		out := make(map[string]bool, len(list))
		for _, item := range list {
			out[fmt.Sprint(item)] = true
		}
		return out, true
	default:
		return nil, false
	}
}

func toStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func maskSecretDefault(isSecret bool, def any) any {
	if isSecret {
		return nil
	}
	return def
}

func strVal(v any) string {
	s, _ := v.(string)
	return s
}

func boolVal(v any) bool {
	b, _ := v.(bool)
	return b
}

func cmpOrString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func numPtr(v any) (*float64, bool) {
	switch x := v.(type) {
	case float64:
		return &x, true
	case float32:
		f := float64(x)
		return &f, true
	case int:
		f := float64(x)
		return &f, true
	case int64:
		f := float64(x)
		return &f, true
	default:
		return nil, false
	}
}

func stringSlicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	return slices.Equal(sa, sb)
}

func normalizeNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
