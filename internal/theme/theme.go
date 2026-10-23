// Package theme loads theme packages and resolves the active theme for a request.
package theme

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// DefaultID is the built-in fallback theme.
const DefaultID = "default"

// Tokens are CSS custom property values, keyed by token name without the leading "--".
type Tokens struct {
	Light  map[string]string `json:"light"`
	Dark   map[string]string `json:"dark"`
	Fonts  map[string]string `json:"fonts,omitempty"`
	Viewer map[string]string `json:"viewer,omitempty"`
}

// Block is one entry of a page composition (home page in M0).
type Block struct {
	Type  string         `json:"type"`
	Props map[string]any `json:"props,omitempty"`
}

// Manifest is the subset of plugin.json a theme uses (docs/design/05 §6.2).
type Manifest struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Version     string            `json:"version"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description,omitempty"`
	APIVersion  int               `json:"apiVersion"`
	Theme       struct {
		Tokens Tokens            `json:"tokens"`
		CSS    string            `json:"css,omitempty"`
		Layout map[string]string `json:"layout,omitempty"`
		Home   []Block           `json:"home,omitempty"`
	} `json:"theme"`
}

// Theme is a loaded theme package.
type Theme struct {
	Manifest
	Source string // "builtin" or a package path
	files  fs.FS
}

// ResolvedTheme is what the frontend needs to render with the active theme.
type ResolvedTheme struct {
	ID      string            `json:"id"`
	Version string            `json:"version"`
	Tokens  Tokens            `json:"tokens"`
	CSS     []string          `json:"css"`
	Layout  map[string]string `json:"layout"`
	Home    []Block           `json:"home"`
}

// Registry holds every installed theme.
type Registry struct {
	mu     sync.RWMutex
	themes map[string]*Theme
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{themes: map[string]*Theme{}} }

// LoadDir loads every subdirectory of root that contains a theme plugin.json.
func (r *Registry) LoadDir(root fs.FS, source string) error {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := fs.Sub(root, e.Name())
		if err != nil {
			return err
		}
		t, err := load(sub, source)
		if err != nil {
			return fmt.Errorf("theme %s: %w", e.Name(), err)
		}
		r.Put(t)
	}
	return nil
}

func load(files fs.FS, source string) (*Theme, error) {
	raw, err := fs.ReadFile(files, "plugin.json")
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse plugin.json: %w", err)
	}
	if m.Kind != "theme" {
		return nil, fmt.Errorf("kind is %q, want theme", m.Kind)
	}
	if m.ID == "" || m.Version == "" {
		return nil, fmt.Errorf("id and version are required")
	}
	if m.Theme.CSS != "" {
		if _, err := fs.Stat(files, m.Theme.CSS); err != nil {
			return nil, fmt.Errorf("css file %q: %w", m.Theme.CSS, err)
		}
	}
	return &Theme{Manifest: m, Source: source, files: files}, nil
}

// Put adds or replaces a theme (hot: callers bump the config version afterwards).
func (r *Registry) Put(t *Theme) {
	r.mu.Lock()
	r.themes[t.ID] = t
	r.mu.Unlock()
}

// Get returns a theme by ID.
func (r *Registry) Get(id string) (*Theme, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.themes[id]
	return t, ok
}

// List returns all themes sorted by ID.
func (r *Registry) List() []*Theme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Theme, 0, len(r.themes))
	for _, t := range r.themes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ValidateActive is the validator for the theme.active setting.
func (r *Registry) ValidateActive(v any) error {
	id, _ := v.(string)
	if _, ok := r.Get(id); !ok {
		return fmt.Errorf("no installed theme %q", id)
	}
	return nil
}

// Resolve applies the active theme, admin token overrides and the home layout.
func (r *Registry) Resolve(s *settings.Snapshot) ResolvedTheme {
	t, ok := r.Get(s.String("theme.active"))
	if !ok {
		t, _ = r.Get(DefaultID)
	}
	if t == nil {
		return ResolvedTheme{ID: "none", Tokens: Tokens{Light: map[string]string{}, Dark: map[string]string{}}}
	}
	tokens := Tokens{
		Light:  maps.Clone(t.Theme.Tokens.Light),
		Dark:   maps.Clone(t.Theme.Tokens.Dark),
		Fonts:  maps.Clone(t.Theme.Tokens.Fonts),
		Viewer: maps.Clone(t.Theme.Tokens.Viewer),
	}
	var overrides Tokens
	_ = json.Unmarshal(s.Raw("theme.overrides"), &overrides)
	tokens.Light = merge(tokens.Light, overrides.Light)
	tokens.Dark = merge(tokens.Dark, overrides.Dark)
	tokens.Fonts = merge(tokens.Fonts, overrides.Fonts)
	tokens.Viewer = merge(tokens.Viewer, overrides.Viewer)

	home := t.Theme.Home
	if s.IsSet("home.blocks") {
		if blocks := settings.Decode[[]Block](s, "home.blocks"); blocks != nil {
			home = blocks
		}
	}
	var css []string
	if t.Theme.CSS != "" {
		css = append(css, AssetURL(t, t.Theme.CSS))
	}
	return ResolvedTheme{
		ID:      t.ID,
		Version: t.Version,
		Tokens:  tokens,
		CSS:     css,
		Layout:  maps.Clone(t.Theme.Layout),
		Home:    home,
	}
}

func merge(base, over map[string]string) map[string]string {
	if len(over) == 0 {
		return base
	}
	if base == nil {
		base = map[string]string{}
	}
	for k, v := range over {
		if safeTokenValue(v) {
			base[k] = v
		}
	}
	return base
}

// safeTokenValue keeps admin overrides from breaking out of the CSS declaration.
func safeTokenValue(v string) bool {
	return !strings.ContainsAny(v, ";{}<>") && !strings.Contains(v, "/*")
}

// AssetURL is the public URL of a theme file; the version in the path makes it cacheable forever.
func AssetURL(t *Theme, file string) string {
	return "/themes-assets/" + t.ID + "/" + t.Version + "/" + strings.TrimPrefix(file, "/")
}

// AssetHandler serves /themes-assets/{id}/{version}/{file...}.
func (r *Registry) AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rest := strings.TrimPrefix(req.URL.Path, "/themes-assets/")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) != 3 || parts[2] == "" {
			http.NotFound(w, req)
			return
		}
		t, ok := r.Get(parts[0])
		if !ok {
			http.NotFound(w, req)
			return
		}
		file := path.Clean(parts[2])
		if !fs.ValidPath(file) || file == "." || file == "plugin.json" {
			http.NotFound(w, req)
			return
		}
		// Only regular files: ServeFileFS would otherwise render directory listings.
		if info, err := fs.Stat(t.files, file); err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, req)
			return
		}
		if parts[1] == t.Version {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache") // stale URL after an upgrade
		}
		http.ServeFileFS(w, req, t.files, file)
	})
}
