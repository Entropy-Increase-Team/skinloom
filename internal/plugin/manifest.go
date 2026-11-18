package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
)

const (
	KindPlugin = "plugin"
	KindTheme  = "theme"

	MaxPluginKVKeys     = 1000
	MaxPluginKVValBytes = 64 * 1024
	MaxPluginDocs       = 50000
	MaxPluginFileBytes  = 50 * 1024 * 1024
)

var (
	pluginIDRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,39}$`)
	themeIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	collNameRe     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
	indexFieldRe   = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
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

// Resolve selects the best localized value with en -> zh-CN -> any fallback.
func (l LocalizedText) Resolve(locale string) string {
	if len(l) == 0 {
		return ""
	}
	if locale != "" {
		if v, ok := l[locale]; ok && v != "" {
			return v
		}
		if base, _, ok := strings.Cut(locale, "-"); ok {
			if v, ok := l[base]; ok && v != "" {
				return v
			}
		}
	}
	for _, k := range []string{"en", "zh-CN"} {
		if v, ok := l[k]; ok && v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return l[keys[0]]
}

// Manifest models plugin.json for both plugins and themes (docs/design/05 §3).
type Manifest struct {
	Schema      string        `json:"$schema,omitempty"`
	ID          string        `json:"id"`
	Kind        string        `json:"kind"` // "plugin" | "theme"
	Version     string        `json:"version"`
	Name        LocalizedText `json:"name"`
	Description LocalizedText `json:"description,omitempty"`
	Authors     []string      `json:"authors,omitempty"`
	License     string        `json:"license,omitempty"`
	Homepage    string        `json:"homepage,omitempty"`
	Repository  string        `json:"repository,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Engines     EnginesSpec   `json:"engines,omitempty"`
	APIVersion  int           `json:"apiVersion"`

	Server               string               `json:"server,omitempty"`
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
	Area  string `json:"area"` // user | admin | explore
	Path  string `json:"path"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order,omitempty"`
	Role  string `json:"role,omitempty"`
}

type FormDef struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Label     string   `json:"label"`
	Required  bool     `json:"required,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Options   []string `json:"options,omitempty"`
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
		if !themeIDRe.MatchString(m.ID) {
			return fmt.Errorf("%w: theme id %q is invalid", ErrBadManifest, m.ID)
		}
	default:
		return fmt.Errorf("%w: kind must be 'plugin' or 'theme', got %q", ErrBadManifest, m.Kind)
	}
	if strings.TrimSpace(m.Version) == "" || len(m.Version) > 64 {
		return fmt.Errorf("%w: version must be 1-64 characters", ErrBadManifest)
	}
	if m.APIVersion != 1 {
		return fmt.Errorf("%w: unsupported apiVersion %d (expected 1)", ErrBadManifest, m.APIVersion)
	}
	if len(m.Name) == 0 || m.Name.Resolve("en") == "" {
		return fmt.Errorf("%w: name is required", ErrBadManifest)
	}
	if err := validateSafeRelPath(m.Server, "server"); err != nil {
		return err
	}
	if err := validateSafeRelPath(m.Web, "web"); err != nil {
		return err
	}
	for _, perm := range m.Permissions {
		if _, ok := LookupPermission(perm); !ok {
			return fmt.Errorf("%w: unknown permission %q", ErrBadManifest, perm)
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
