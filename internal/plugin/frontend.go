package plugin

import (
	"encoding/json"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

// FrontendMenu is one localized navigation contribution declared by an active plugin.
type FrontendMenu struct {
	Area  string `json:"area"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order"`
	Role  string `json:"role,omitempty"`
}

// FrontendFormField is one localized form field appended by a plugin for a host form scope.
type FrontendFormField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// FrontendBlock describes one custom homepage block declared by a plugin.
type FrontendBlock struct {
	Type         string         `json:"type"`
	Label        string         `json:"label,omitempty"`
	DefaultProps map[string]any `json:"defaultProps,omitempty"`
}

// FrontendPlugin summarizes everything the frontend runtime needs for one active plugin.
type FrontendPlugin struct {
	ID        string                         `json:"id"`
	Kind      string                         `json:"kind"`
	Version   string                         `json:"version"`
	Rev       string                         `json:"rev"`
	Name      string                         `json:"name"`
	Entry     string                         `json:"entry,omitempty"`
	Style     string                         `json:"style,omitempty"`
	Slots     []string                       `json:"slots,omitempty"`
	Menus     []FrontendMenu                 `json:"menus,omitempty"`
	Forms     map[string][]FrontendFormField `json:"forms,omitempty"`
	Blocks    []FrontendBlock                `json:"blocks,omitempty"`
	Settings  map[string]any                 `json:"settings,omitempty"`
	Messages  map[string]string              `json:"messages,omitempty"`
	Overrides []string                       `json:"overrides,omitempty"`
}

// FrontendPlugins returns localized frontend metadata and asset URLs for all active
// plugins sorted deterministically by ID. Secret settings fields are omitted.
func (m *Manager) FrontendPlugins(locale string) []FrontendPlugin {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	rts := make([]*pluginRuntime, 0, len(m.active))
	for _, rt := range m.active {
		if rt != nil && !rt.closed.Load() {
			rts = append(rts, rt)
		}
	}
	m.mu.RUnlock()

	sort.Slice(rts, func(i, j int) bool { return rts[i].id < rts[j].id })
	out := make([]FrontendPlugin, 0, len(rts))
	for _, rt := range rts {
		out = append(out, rt.frontendView(locale))
	}
	return out
}

// HasTrustedThemeWeb reports whether themeID is currently enabled as a trusted
// plugin-managed theme bundle with a web entry.
func (m *Manager) HasTrustedThemeWeb(themeID string) bool {
	if m == nil || themeID == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt := m.active[themeID]; rt != nil && !rt.closed.Load() && rt.manifest.Kind == KindTheme && rt.manifest.Web != "" {
		return true
	}
	return false
}

func (r *pluginRuntime) frontendView(locale string) FrontendPlugin {
	rev := r.rev
	if rev == "" {
		rev = r.manifest.Version
	}
	fp := FrontendPlugin{
		ID:        r.id,
		Kind:      r.manifest.Kind,
		Version:   r.manifest.Version,
		Rev:       rev,
		Name:      r.manifest.Name.Resolve(locale),
		Slots:     slices.Clone(r.manifest.Slots),
		Settings:  r.publicSettingsCopy(),
		Messages:  r.mergedLocaleMessages(locale),
		Overrides: r.themeOverrides(),
	}
	if r.manifest.Web != "" {
		cleanWeb := strings.TrimPrefix(path.Clean(r.manifest.Web), "/")
		fp.Entry = "/plugins-assets/" + r.id + "/" + rev + "/" + cleanWeb
		cssCand := path.Clean(path.Dir(cleanWeb) + "/index.css")
		if r.hasStaticAsset(cssCand) {
			fp.Style = "/plugins-assets/" + r.id + "/" + rev + "/" + cssCand
		}
	}
	if len(r.manifest.Menus) > 0 {
		fp.Menus = make([]FrontendMenu, len(r.manifest.Menus))
		for i, item := range r.manifest.Menus {
			fp.Menus[i] = FrontendMenu{
				Area:  item.Area,
				Path:  item.Path,
				Title: item.Title.Resolve(locale),
				Icon:  item.Icon,
				Order: item.Order,
				Role:  item.Role,
			}
		}
	}
	if len(r.manifest.Forms) > 0 {
		fp.Forms = make(map[string][]FrontendFormField, len(r.manifest.Forms))
		for scope, defs := range r.manifest.Forms {
			items := make([]FrontendFormField, len(defs))
			for i, d := range defs {
				items[i] = FrontendFormField{
					Name:        d.Name,
					Type:        d.Type,
					Label:       d.Label.Resolve(locale),
					Description: d.Description.Resolve(locale),
					Placeholder: d.Placeholder.Resolve(locale),
					Required:    d.Required,
					MaxLength:   d.MaxLength,
					Options:     slices.Clone(d.Options),
				}
			}
			fp.Forms[scope] = items
		}
	}
	if len(r.manifest.Blocks) > 0 {
		keys := make([]string, 0, len(r.manifest.Blocks))
		for k := range r.manifest.Blocks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fb := FrontendBlock{Type: k}
			if raw, ok := r.manifest.Blocks[k].(map[string]any); ok {
				fb.Label = resolveAnyText(raw["name"], locale)
				if dp, ok := raw["defaultProps"].(map[string]any); ok {
					fb.DefaultProps = maps.Clone(dp)
				}
			}
			fp.Blocks = append(fp.Blocks, fb)
		}
	}
	return fp
}

func (r *pluginRuntime) blockTypes() []theme.BlockType {
	if len(r.manifest.Blocks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.manifest.Blocks))
	for k := range r.manifest.Blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]theme.BlockType, 0, len(keys))
	for _, k := range keys {
		out = append(out, theme.BlockType{Type: k, Props: []theme.Prop{}})
	}
	return out
}

func (r *pluginRuntime) hasStaticAsset(rel string) bool {
	if r.staticFiles != nil {
		if _, ok := r.staticFiles[rel]; ok {
			return true
		}
	}
	if r.dir != "" {
		if info, err := os.Stat(filepath.Join(r.dir, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (r *pluginRuntime) publicSettingsCopy() map[string]any {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	if len(r.settings) == 0 || len(r.manifest.Settings) == 0 {
		return nil
	}
	out := redactSettingsWithSchema(r.manifest.Settings, r.settings)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *pluginRuntime) mergedLocaleMessages(locale string) map[string]string {
	r.localesMu.RLock()
	defer r.localesMu.RUnlock()
	if len(r.locales) == 0 {
		return nil
	}
	merged := map[string]string{}
	apply := func(loc string) {
		for k, v := range r.locales[loc] {
			if v != "" {
				merged[k] = v
			}
		}
	}
	keys := make([]string, 0, len(r.locales))
	for k := range r.locales {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Apply fallback layers first, then visitor's active language last so SkinLoom's UI
	// locale always takes precedence when supported by the plugin.
	norm := normalizeLocaleTag(locale)
	if strings.EqualFold(norm, "en") || strings.HasPrefix(strings.ToLower(norm), "en-") {
		apply("zh-CN")
		apply("en")
	} else {
		apply("en")
		apply("zh-CN")
	}
	if norm != "" {
		base, _, _ := strings.Cut(strings.ToLower(norm), "-")
		// Apply same-family locales in reverse lexicographic order so earlier canonical tags
		// win over later variants, then re-apply canonical zh-CN for generic "zh", and exact match last.
		for i := len(keys) - 1; i >= 0; i-- {
			k := keys[i]
			kb, _, _ := strings.Cut(strings.ToLower(normalizeLocaleTag(k)), "-")
			if base != "" && kb == base {
				apply(k)
			}
		}
		if strings.EqualFold(norm, "zh") {
			apply("zh-CN")
		}
		for _, k := range keys {
			if strings.EqualFold(normalizeLocaleTag(k), norm) {
				apply(k)
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func (r *pluginRuntime) themeOverrides() []string {
	if len(r.manifest.Theme) == 0 {
		return nil
	}
	var spec struct {
		Overrides []string `json:"overrides"`
	}
	if err := json.Unmarshal(r.manifest.Theme, &spec); err != nil {
		return nil
	}
	var out []string
	for _, target := range spec.Overrides {
		if slices.Contains(theme.OverrideTargets, target) {
			out = append(out, target)
		}
	}
	return out
}

func resolveAnyText(v any, locale string) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		lt := make(LocalizedText, len(x))
		for k, val := range x {
			if s, ok := val.(string); ok {
				lt[k] = s
			}
		}
		return lt.Resolve(locale)
	default:
		return ""
	}
}
