package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
)

const (
	KindPlugin = "plugin"
	KindTheme  = "theme"

	RuntimeJS     = "js"
	RuntimeNative = "native"

	CurrentAPIVersion            = 1
	CurrentNativeProtocolVersion = 1

	MaxPluginKVKeys     = 1000
	MaxPluginKVValBytes = 64 * 1024
	MaxPluginDocs       = 50000
	MaxPluginFileBytes  = 50 * 1024 * 1024
)

var (
	pluginIDRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,39}$`)
	themeIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	versionRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	collNameRe     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
	indexFieldRe   = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
	platformRe     = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)
	ErrBadManifest = errors.New("plugin: invalid manifest")
)

// LocalizedText supports either a plain JSON string ("My Plugin") or a locale map
// ({"zh-CN": "...", "en": "..."}).
type LocalizedText map[string]string

func (l *LocalizedText) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*l = LocalizedText{"en": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*l = m
	return nil
}

func normalizeLocaleTag(locale string) string {
	return strings.ReplaceAll(strings.TrimSpace(locale), "_", "-")
}

// IsChineseLocale reports whether locale targets Chinese (e.g. "zh", "zh-CN", "zh-Hans").
func IsChineseLocale(locale string) bool {
	norm := strings.ToLower(normalizeLocaleTag(locale))
	return norm == "zh" || strings.HasPrefix(norm, "zh-")
}

// Resolve selects the best localized value for locale (exact -> case-insensitive ->
// same language family -> zh-CN -> en -> deterministic first non-empty key).
func (l LocalizedText) Resolve(locale string) string {
	if len(l) == 0 {
		return ""
	}
	norm := normalizeLocaleTag(locale)
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if norm != "" {
		if v, ok := l[norm]; ok && v != "" {
			return v
		}
		for _, k := range keys {
			if l[k] != "" && strings.EqualFold(normalizeLocaleTag(k), norm) {
				return l[k]
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(norm), "-")
		if base != "" {
			if v, ok := l[base]; ok && v != "" {
				return v
			}
			// Prefer canonical regional variant for base language (e.g. zh -> zh-CN).
			if base == "zh" {
				if v, ok := l["zh-CN"]; ok && v != "" {
					return v
				}
			}
			for _, k := range keys {
				if l[k] == "" {
					continue
				}
				kb, _, _ := strings.Cut(strings.ToLower(normalizeLocaleTag(k)), "-")
				if kb == base {
					return l[k]
				}
			}
		}
	}
	for _, k := range []string{"zh-CN", "en"} {
		if v, ok := l[k]; ok && v != "" {
			return v
		}
	}
	for _, k := range keys {
		if l[k] != "" {
			return l[k]
		}
	}
	return ""
}

// Manifest models plugin.json for both plugins and themes (docs/design/05 §3).
type Manifest struct {
	Schema      string        `json:"$schema,omitempty"`
	ID          string        `json:"id"`
	Kind        string        `json:"kind"`              // "plugin" | "theme"
	Runtime     string        `json:"runtime,omitempty"` // "js" (default) | "native"
	Version     string        `json:"version"`
	Name        LocalizedText `json:"name"`
	Description LocalizedText `json:"description,omitempty"`
	Authors     []string      `json:"authors,omitempty"`
	License     string        `json:"license,omitempty"`
	Homepage    string        `json:"homepage,omitempty"`
	Repository  string        `json:"repository,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Screenshot  string        `json:"screenshot,omitempty"`
	Extends     string        `json:"extends,omitempty"`
	Engines     EnginesSpec   `json:"engines,omitempty"`
	APIVersion  int           `json:"apiVersion"`

	Server               string               `json:"server,omitempty"`
	Native               *NativeSpec          `json:"native,omitempty"`
	Web                  string               `json:"web,omitempty"`
	Permissions          []string             `json:"permissions,omitempty"`
	HTTP                 HTTPManifest         `json:"http,omitempty"`
	Hooks                HooksManifest        `json:"hooks,omitempty"`
	Routes               RoutesManifest       `json:"routes,omitempty"`
	Storage              StorageManifest      `json:"storage,omitempty"`
	Settings             map[string]any       `json:"settings,omitempty"`
	Menus                []MenuDef            `json:"menus,omitempty"`
	Forms                map[string][]FormDef `json:"forms,omitempty"`
	Blocks               map[string]any       `json:"blocks,omitempty"`
	Slots                []string             `json:"slots,omitempty"`
	Dependencies         map[string]string    `json:"dependencies,omitempty"`
	OptionalDependencies map[string]string    `json:"optionalDependencies,omitempty"`
	Services             []string             `json:"services,omitempty"`
	Theme                json.RawMessage      `json:"theme,omitempty"`
}

type EnginesSpec struct {
	Skinloom string `json:"skinloom,omitempty"`
	Go       string `json:"go,omitempty"`
}

// NativeSpec declares native Go subprocess execution details for runtime == "native".
type NativeSpec struct {
	ProtocolVersion int                 `json:"protocolVersion"`
	Executable      string              `json:"executable"`
	Platforms       []string            `json:"platforms,omitempty"`
	HTTPProxy       bool                `json:"httpProxy,omitempty"`
	Database        *NativeDatabaseSpec `json:"database,omitempty"`
}

// NativeDatabaseSpec declares whether a native plugin requests host-isolated SQL storage.
type NativeDatabaseSpec struct {
	Required bool `json:"required,omitempty"`
}

type HTTPManifest struct {
	Allow        []string `json:"allow,omitempty"`
	MaxBodyBytes int64    `json:"maxBodyBytes,omitempty"`
}

type HooksManifest struct {
	Guard  []string `json:"guard,omitempty"`
	Filter []string `json:"filter,omitempty"`
	Event  []string `json:"event,omitempty"`
}

type RoutesManifest struct {
	Public []string `json:"public,omitempty"`
}

type StorageManifest struct {
	Collections map[string]CollectionSpec `json:"collections,omitempty"`
	Quota       QuotaSpec                 `json:"quota,omitempty"`
}

type CollectionSpec struct {
	Indexes map[string]string `json:"indexes,omitempty"`
}

type QuotaSpec struct {
	KVKeys     int   `json:"kvKeys,omitempty"`
	Docs       int   `json:"docs,omitempty"`
	FilesBytes int64 `json:"filesBytes,omitempty"`
}

type MenuDef struct {
	Area  string        `json:"area"` // user | admin | explore
	Path  string        `json:"path"`
	Title LocalizedText `json:"title"`
	Icon  string        `json:"icon,omitempty"`
	Order int           `json:"order,omitempty"`
	Role  string        `json:"role,omitempty"`
}

type FormDef struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Label       LocalizedText `json:"label"`
	Description LocalizedText `json:"description,omitempty"`
	Placeholder LocalizedText `json:"placeholder,omitempty"`
	Required    bool          `json:"required,omitempty"`
	MaxLength   int           `json:"maxLength,omitempty"`
	Options     []string      `json:"options,omitempty"`
}

// ParseManifest parses and validates plugin.json bytes strictly.
func ParseManifest(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks manifest invariants across identifiers, API version,
// entry paths, permissions, hooks, allowlists, and doc collection slot limits.
func (m *Manifest) Validate() error {
	switch m.Kind {
	case KindPlugin:
		if !pluginIDRe.MatchString(m.ID) {
			return fmt.Errorf("%w: id %q must be 3-40 lowercase kebab-case characters", ErrBadManifest, m.ID)
		}
	case KindTheme:
		if !themeIDRe.MatchString(m.ID) || m.ID == "_custom" {
			return fmt.Errorf("%w: theme id %q is invalid", ErrBadManifest, m.ID)
		}
	default:
		return fmt.Errorf("%w: kind must be 'plugin' or 'theme', got %q", ErrBadManifest, m.Kind)
	}
	if !versionRe.MatchString(m.Version) || strings.Contains(m.Version, "..") {
		return fmt.Errorf("%w: version %q must be 1-64 safe version characters (letters, digits, '.', '_', '+', '-') without '..'", ErrBadManifest, m.Version)
	}
	if m.Extends != "" && m.Extends == m.ID {
		return fmt.Errorf("%w: theme %q cannot extend itself", ErrBadManifest, m.ID)
	}
	if err := validateSafeRelPath(m.Screenshot, "screenshot"); err != nil {
		return err
	}
	if m.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("%w: unsupported apiVersion %d (expected %d)", ErrBadManifest, m.APIVersion, CurrentAPIVersion)
	}
	if len(m.Name) == 0 || m.Name.Resolve("en") == "" {
		return fmt.Errorf("%w: name is required", ErrBadManifest)
	}
	rt := m.EffectiveRuntime()
	switch m.Runtime {
	case "", RuntimeJS, RuntimeNative:
	default:
		return fmt.Errorf("%w: runtime must be %q or %q, got %q", ErrBadManifest, RuntimeJS, RuntimeNative, m.Runtime)
	}
	if m.Kind == KindTheme && rt == RuntimeNative {
		return fmt.Errorf("%w: themes cannot use native runtime", ErrBadManifest)
	}
	if m.Engines.Skinloom != "" {
		if _, err := ParseSemverRange(m.Engines.Skinloom); err != nil {
			return fmt.Errorf("%w: invalid engines.skinloom %q: %v", ErrBadManifest, m.Engines.Skinloom, err)
		}
	}
	if m.Engines.Go != "" {
		if _, err := ParseSemverRange(m.Engines.Go); err != nil {
			return fmt.Errorf("%w: invalid engines.go %q: %v", ErrBadManifest, m.Engines.Go, err)
		}
	}
	if err := validateSafeRelPath(m.Server, "server"); err != nil {
		return err
	}
	if err := validateSafeRelPath(m.Web, "web"); err != nil {
		return err
	}
	if rt == RuntimeNative {
		if m.Server != "" {
			return fmt.Errorf("%w: native plugins must not declare server JS entry %q", ErrBadManifest, m.Server)
		}
		if err := m.validateNativeSpec(); err != nil {
			return err
		}
	} else if m.Native != nil {
		return fmt.Errorf("%w: native configuration is only allowed when runtime is %q", ErrBadManifest, RuntimeNative)
	}
	for _, perm := range m.Permissions {
		def, ok := LookupPermission(perm)
		if !ok {
			return fmt.Errorf("%w: unknown permission %q", ErrBadManifest, perm)
		}
		if def.NativeOnly && rt != RuntimeNative {
			return fmt.Errorf("%w: permission %q requires runtime %q", ErrBadManifest, perm, RuntimeNative)
		}
	}
	if rt == RuntimeNative && m.Native != nil {
		if m.Native.Database != nil && m.Native.Database.Required && !slices.Contains(m.Permissions, PermStorageSQL) {
			return fmt.Errorf("%w: native.database.required requires %q permission", ErrBadManifest, PermStorageSQL)
		}
		if m.Native.HTTPProxy && !slices.Contains(m.Permissions, PermHTTPStream) {
			return fmt.Errorf("%w: native.httpProxy requires %q permission", ErrBadManifest, PermHTTPStream)
		}
	}
	for _, host := range m.HTTP.Allow {
		if err := validateAllowedHostPattern(host); err != nil {
			return err
		}
	}
	if err := m.validateHooks(); err != nil {
		return err
	}
	if err := m.validateCollections(); err != nil {
		return err
	}
	for _, s := range m.Slots {
		if _, ok := LookupSlot(s); !ok {
			return fmt.Errorf("%w: unknown frontend slot %q", ErrBadManifest, s)
		}
	}
	for scope := range m.Forms {
		if !slices.Contains(FormScopes, scope) {
			return fmt.Errorf("%w: unknown form scope %q", ErrBadManifest, scope)
		}
	}
	for _, menu := range m.Menus {
		switch menu.Area {
		case "explore", "user", "admin":
		default:
			return fmt.Errorf("%w: invalid menu area %q", ErrBadManifest, menu.Area)
		}
		if strings.TrimSpace(menu.Path) == "" {
			return fmt.Errorf("%w: menu path is required", ErrBadManifest)
		}
		if menu.Role != "" && menu.Role != "admin" && menu.Role != "superadmin" {
			return fmt.Errorf("%w: invalid menu role %q", ErrBadManifest, menu.Role)
		}
	}
	for _, svc := range m.Services {
		if strings.TrimSpace(svc) == "" || strings.ContainsAny(svc, " \t\r\n") {
			return fmt.Errorf("%w: invalid service name %q", ErrBadManifest, svc)
		}
	}
	for dep := range m.Dependencies {
		if dep == m.ID || !pluginIDRe.MatchString(dep) {
			return fmt.Errorf("%w: invalid dependency id %q", ErrBadManifest, dep)
		}
	}
	for dep := range m.OptionalDependencies {
		if dep == m.ID || !pluginIDRe.MatchString(dep) {
			return fmt.Errorf("%w: invalid optionalDependency id %q", ErrBadManifest, dep)
		}
	}
	return nil
}

func validateSafeRelPath(p, field string) error {
	if p == "" {
		return nil
	}
	if strings.ContainsRune(p, '\\') || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: %s path %q must be a relative path", ErrBadManifest, field, p)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w: %s path %q escapes plugin directory", ErrBadManifest, field, p)
	}
	return nil
}

func validateAllowedHostPattern(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || host == "*" || strings.ContainsAny(host, "/:@?# \t\n\r") {
		return fmt.Errorf("%w: invalid http.allow host %q", ErrBadManifest, host)
	}
	trimmed := strings.Trim(strings.ToLower(host), "[]")
	if net.ParseIP(trimmed) != nil {
		return fmt.Errorf("%w: IP literals are not allowed in http.allow (%q)", ErrBadManifest, host)
	}
	if trimmed == "localhost" || strings.HasSuffix(trimmed, ".localhost") ||
		strings.HasSuffix(trimmed, ".local") || strings.HasSuffix(trimmed, ".internal") {
		return fmt.Errorf("%w: internal hostnames are not allowed in http.allow (%q)", ErrBadManifest, host)
	}
	if strings.HasPrefix(host, "*.") && strings.Count(host, "*") == 1 && len(host) > 3 {
		return nil
	}
	if strings.Contains(host, "*") {
		return fmt.Errorf("%w: wildcard only allowed as leading '*.' in http.allow (%q)", ErrBadManifest, host)
	}
	return nil
}

func (m *Manifest) validateHooks() error {
	catalog := make(map[string]hook.Kind)
	for _, d := range hook.Catalog() {
		catalog[d.Name] = d.Kind
	}
	for _, g := range m.Hooks.Guard {
		if k, ok := catalog[g]; !ok || k != hook.KindGuard {
			return fmt.Errorf("%w: unknown guard hook %q", ErrBadManifest, g)
		}
	}
	for _, f := range m.Hooks.Filter {
		if k, ok := catalog[f]; !ok || k != hook.KindFilter {
			return fmt.Errorf("%w: unknown filter hook %q", ErrBadManifest, f)
		}
	}
	for _, e := range m.Hooks.Event {
		if strings.HasPrefix(e, "plugin:") {
			// Cross-plugin event names (plugin:<id>.<event>) are validated dynamically.
			if !strings.Contains(strings.TrimPrefix(e, "plugin:"), ".") {
				return fmt.Errorf("%w: invalid plugin event %q", ErrBadManifest, e)
			}
			continue
		}
		if k, ok := catalog[e]; !ok || k != hook.KindEvent {
			return fmt.Errorf("%w: unknown event hook %q", ErrBadManifest, e)
		}
	}
	return nil
}

func (m *Manifest) validateCollections() error {
	for coll, spec := range m.Storage.Collections {
		if !collNameRe.MatchString(coll) {
			return fmt.Errorf("%w: invalid collection name %q", ErrBadManifest, coll)
		}
		if _, err := compileCollectionSlots(coll, spec); err != nil {
			return err
		}
	}
	return nil
}

// EffectiveLimits returns resolved storage quotas bounded by system caps.
func (m *Manifest) EffectiveQuotas() QuotaSpec {
	q := m.Storage.Quota
	if q.KVKeys <= 0 || q.KVKeys > MaxPluginKVKeys*10 {
		q.KVKeys = MaxPluginKVKeys
	}
	if q.Docs <= 0 || q.Docs > MaxPluginDocs*10 {
		q.Docs = MaxPluginDocs
	}
	if q.FilesBytes <= 0 || q.FilesBytes > MaxPluginFileBytes*10 {
		q.FilesBytes = MaxPluginFileBytes
	}
	return q
}

// HasHook reports whether name was declared under the given hook kind.
func (m *Manifest) HasHook(kind hook.Kind, name string) bool {
	switch kind {
	case hook.KindGuard:
		return slices.Contains(m.Hooks.Guard, name)
	case hook.KindFilter:
		return slices.Contains(m.Hooks.Filter, name)
	case hook.KindEvent:
		return slices.Contains(m.Hooks.Event, name)
	default:
		return false
	}
}

// HasDependency reports whether id is listed in dependencies or optionalDependencies.
func (m *Manifest) HasDependency(id string) bool {
	if _, ok := m.Dependencies[id]; ok {
		return true
	}
	_, ok := m.OptionalDependencies[id]
	return ok
}

// EffectiveRuntime returns "native" or "js" (default when omitted).
func (m *Manifest) EffectiveRuntime() string {
	if m != nil && m.Runtime == RuntimeNative {
		return RuntimeNative
	}
	return RuntimeJS
}

// EffectivePermissions returns declared permissions, implicitly appending
// PermNativeExec ("native.exec") for native runtime plugins so every UI/policy
// check highlights native OS subprocess execution as high risk.
func (m *Manifest) EffectivePermissions() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.Permissions)+1)
	seen := make(map[string]bool, len(m.Permissions)+1)
	for _, p := range m.Permissions {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if m.EffectiveRuntime() == RuntimeNative && !seen[PermNativeExec] {
		out = append(out, PermNativeExec)
	}
	return out
}

func (m *Manifest) validateNativeSpec() error {
	if m.Native == nil {
		return fmt.Errorf("%w: native configuration is required when runtime is %q", ErrBadManifest, RuntimeNative)
	}
	if m.Native.ProtocolVersion != CurrentNativeProtocolVersion {
		return fmt.Errorf("%w: unsupported native.protocolVersion %d (expected %d)", ErrBadManifest, m.Native.ProtocolVersion, CurrentNativeProtocolVersion)
	}
	if strings.TrimSpace(m.Native.Executable) == "" {
		return fmt.Errorf("%w: native.executable is required", ErrBadManifest)
	}
	if err := validateSafeRelPath(m.Native.Executable, "native.executable"); err != nil {
		return err
	}
	seenPlat := make(map[string]bool, len(m.Native.Platforms))
	for _, p := range m.Native.Platforms {
		if !platformRe.MatchString(p) {
			return fmt.Errorf("%w: invalid native.platforms entry %q (expected os/arch)", ErrBadManifest, p)
		}
		if seenPlat[p] {
			return fmt.Errorf("%w: duplicate native.platforms entry %q", ErrBadManifest, p)
		}
		seenPlat[p] = true
	}
	return nil
}

// NativeExecutableCandidates returns ordered relative paths to search inside a
// native plugin bundle for the given target GOOS/GOARCH.
func (m *Manifest) NativeExecutableCandidates(goos, goarch string) []string {
	if m == nil || m.Native == nil || m.Native.Executable == "" {
		return nil
	}
	base := path.Clean(m.Native.Executable)
	ext := ""
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(base), ".exe") {
		ext = ".exe"
	}
	baseNoExt := strings.TrimSuffix(base, ".exe")
	cands := []string{
		fmt.Sprintf("%s_%s_%s%s", baseNoExt, goos, goarch, ext),
		fmt.Sprintf("%s-%s-%s%s", baseNoExt, goos, goarch, ext),
	}
	if ext != "" {
		cands = append(cands, baseNoExt+ext)
	}
	cands = append(cands, base)

	out := make([]string, 0, len(cands))
	seen := make(map[string]bool, len(cands))
	for _, c := range cands {
		clean := path.Clean(c)
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out
}

// ResolveNativeExecutable locates the native plugin binary on disk under dir for
// the given GOOS/GOARCH while enforcing directory containment.
func (m *Manifest) ResolveNativeExecutable(dir, goos, goarch string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("native plugin %q requires an extracted directory on disk", m.ID)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	cands := m.NativeExecutableCandidates(goos, goarch)
	for _, rel := range cands {
		full := filepath.Join(absDir, filepath.FromSlash(rel))
		relCheck, rErr := filepath.Rel(absDir, full)
		if rErr != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			continue
		}
		info, sErr := os.Stat(full)
		if sErr == nil && info.Mode().IsRegular() {
			return full, nil
		}
	}
	return "", fmt.Errorf("native executable for %s/%s not found in %s (tried %v)", goos, goarch, dir, cands)
}
