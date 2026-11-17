// Package theme loads theme packages and resolves the active theme for a request.
package theme

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

// DefaultID is the built-in fallback theme.
const DefaultID = "default"

// Layouts are the layout variants a theme may choose (docs/design/05 §6.1 ③, ADR 0043),
// the default first. Keys not listed here are ignored.
var Layouts = map[string][]string{
	"auth":    {"card", "split"},
	"skinlib": {"comfortable", "compact"},
	"user":    {"sidebar", "tabs"},
	"texture": {"split", "stacked"},
}

// ColorSchemes are the color modes a theme may support (ADR 0043).
var ColorSchemes = []string{"both", "light", "dark"}

// DocumentedColorTokens are the color, shadow and radius tokens in web/design.md §2.2.
// Unlisted token names remain allowed for a theme's own CSS.
var DocumentedColorTokens = []string{
	"background", "foreground",
	"card", "card-foreground",
	"popover", "popover-foreground",
	"muted", "muted-foreground", "faint-foreground",
	"secondary", "secondary-foreground",
	"accent", "accent-foreground",
	"primary", "primary-foreground",
	"highlight", "highlight-foreground",
	"warning", "warning-foreground",
	"success", "success-foreground",
	"destructive", "destructive-foreground",
	"border", "input", "overlay", "ring",
	"chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
	"sidebar", "sidebar-foreground",
	"sidebar-primary", "sidebar-primary-foreground",
	"sidebar-accent", "sidebar-accent-foreground",
	"sidebar-border", "sidebar-ring",
	"pixel-a", "pixel-b",
	"shadow-popover", "shadow-dialog",
	"radius", "brand-gradient",
}

// DocumentedFontTokens are the font tokens in web/design.md §3.1.
var DocumentedFontTokens = []string{"sans", "mono", "display"}

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`) // "_custom" is taken by CustomCSSURL
	tokenName   = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	settingName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
	assetURLPat = regexp.MustCompile(`url\(\s*(['"]?)(\.?/?assets/[^)'"\s]+)(['"]?)\s*\)`)
)

// Allowed extensions served from /themes-assets/{id}/{rev}/{file...} (ADR 0043).
var (
	imageExts = []string{".png", ".webp", ".avif", ".jpg", ".jpeg", ".gif", ".svg"}
	assetExts = slices.Concat([]string{".css", ".woff2", ".woff"}, imageExts)
)

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

// SettingType is the kind of value a theme setting holds (ADR 0043).
type SettingType string

const (
	SettingColor   SettingType = "color"
	SettingImage   SettingType = "image"
	SettingChoice  SettingType = "choice"
	SettingText    SettingType = "text"
	SettingBoolean SettingType = "boolean"
)

// Setting is one setting declared by a theme in plugin.json.
type Setting struct {
	Type    SettingType       `json:"type" enum:"color,image,choice,text,boolean"`
	Label   map[string]string `json:"label,omitempty"`
	Default any               `json:"default,omitempty"`
	Options []string          `json:"options,omitempty"`
	Token   string            `json:"token,omitempty"`
}

// UnmarshalJSON accepts label as either a localized object {"zh-CN": "…", "en": "…"} or a plain string.
func (s *Setting) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type    SettingType     `json:"type"`
		Label   json.RawMessage `json:"label"`
		Default any             `json:"default"`
		Options []string        `json:"options"`
		Token   string          `json:"token"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Type = raw.Type
	s.Default = raw.Default
	s.Options = raw.Options
	s.Token = raw.Token
	if len(raw.Label) > 0 && string(raw.Label) != "null" {
		var str string
		if err := json.Unmarshal(raw.Label, &str); err == nil {
			s.Label = map[string]string{"zh-CN": str, "en": str}
			return nil
		}
		var m map[string]string
		if err := json.Unmarshal(raw.Label, &m); err != nil {
			return fmt.Errorf("label must be a string or localized object: %w", err)
		}
		s.Label = m
	}
	return nil
}

// Manifest is the subset of plugin.json a theme uses (docs/design/05 §6.2, ADR 0043).
type Manifest struct {
	ID          string             `json:"id"`
	Kind        string             `json:"kind"`
	Version     string             `json:"version"`
	Name        map[string]string  `json:"name"`
	Description map[string]string  `json:"description,omitempty"`
	APIVersion  int                `json:"apiVersion"`
	Extends     string             `json:"extends,omitempty"`
	Screenshot  string             `json:"screenshot,omitempty"`
	Settings    map[string]Setting `json:"settings,omitempty"`
	Theme       struct {
		ColorScheme string             `json:"colorScheme,omitempty"`
		Tokens      Tokens             `json:"tokens"`
		CSS         string             `json:"css,omitempty"`
		Layout      map[string]string  `json:"layout,omitempty"`
		Home        []Block            `json:"home,omitempty"`
		Settings    map[string]Setting `json:"settings,omitempty"`
	} `json:"theme"`
}

// Theme is a loaded theme package (or a failed local theme when Error != "").
type Theme struct {
	Manifest
	Source        string   // "builtin" or "local"
	Error         string   // non-empty when a local theme failed to load
	ScreenshotURL string   // resolved AssetURL of Screenshot
	CSSFiles      []string // resolved AssetURLs (parent first when Extends is set)
	rev           string   // Version for builtin, content hash for local
	files         fs.FS
}

// ResolvedTheme is what the frontend needs to render with the active theme. JS lists
// the scripts pages load with their CSP nonce (theme.custom_js).
type ResolvedTheme struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	ColorScheme string            `json:"colorScheme"`
	Tokens      Tokens            `json:"tokens"`
	CSS         []string          `json:"css"`
	JS          []string          `json:"js"`
	Layout      map[string]string `json:"layout"`
	Home        []Block           `json:"home"`
	Settings    map[string]string `json:"settings,omitempty"`
}

// Registry holds every installed theme.
type Registry struct {
	mu       sync.RWMutex
	themes   map[string]*Theme
	broken   []*Theme
	localDir string
	log      *slog.Logger
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{themes: map[string]*Theme{}} }

// LoadDir loads every subdirectory of root that contains a theme plugin.json.
func (r *Registry) LoadDir(root fs.FS, source string) error {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return err
	}
	var loaded []*Theme
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
		loaded = append(loaded, t)
	}
	// First register standalone themes, then resolve single-level inheritance.
	for _, t := range loaded {
		if t.Extends == "" {
			defaultWarning(t.Theme.Tokens.Light)
			defaultWarning(t.Theme.Tokens.Dark)
			r.Put(t)
		}
	}
	for _, t := range loaded {
		if t.Extends == "" {
			continue
		}
		if err := r.resolveExtends(t); err != nil {
			return fmt.Errorf("theme %s: %w", t.ID, err)
		}
		r.Put(t)
	}
	return nil
}

// SetLocalDir configures the local themes directory (<DataDir>/themes) and loads it.
// Broken local themes are recorded on the registry and logged without stopping startup.
func (r *Registry) SetLocalDir(dir string, log *slog.Logger) {
	r.mu.Lock()
	r.localDir = dir
	r.log = log
	r.mu.Unlock()
	r.Reload()
}

// Reload rescans the local themes directory, keeping built-in themes intact.
func (r *Registry) Reload() {
	r.mu.RLock()
	dir := r.localDir
	log := r.log
	next := map[string]*Theme{}
	for id, t := range r.themes {
		if t.Source == "builtin" {
			next[id] = t
		}
	}
	r.mu.RUnlock()

	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && log != nil {
			log.Warn("read local themes directory", "dir", dir, "err", err)
		}
		r.mu.Lock()
		r.themes = next
		r.broken = nil
		r.mu.Unlock()
		return
	}

	type pendingLocal struct {
		dir string
		t   *Theme
	}
	var pending []pendingLocal
	var broken []*Theme
	recordBroken := func(dirName string, m Manifest, err error) {
		id := cmp.Or(m.ID, dirName)
		name := m.Name
		if len(name) == 0 {
			name = map[string]string{"zh-CN": id, "en": id}
		}
		b := &Theme{
			Manifest: Manifest{ID: id, Kind: "theme", Version: m.Version, Name: name, Description: m.Description, Extends: m.Extends},
			Source:   "local",
			Error:    err.Error(),
		}
		b.Theme.ColorScheme = "both"
		b.Theme.Tokens = Tokens{Light: map[string]string{}, Dark: map[string]string{}}
		b.Theme.Layout = map[string]string{}
		b.Theme.Home = []Block{}
		broken = append(broken, b)
		if log != nil {
			log.Warn("skip invalid local theme", "dir", dirName, "id", id, "err", err)
		}
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := os.DirFS(filepath.Join(dir, e.Name()))
		t, partial, err := loadWithPartial(sub, "local")
		if err != nil {
			recordBroken(e.Name(), partial, err)
			continue
		}
		if existing, dup := next[t.ID]; dup {
			recordBroken(e.Name(), t.Manifest, fmt.Errorf("id %q is already used by a %s theme", t.ID, existing.Source))
			continue
		}
		if slices.ContainsFunc(pending, func(p pendingLocal) bool { return p.t.ID == t.ID }) {
			recordBroken(e.Name(), t.Manifest, fmt.Errorf("duplicate local theme id %q", t.ID))
			continue
		}
		pending = append(pending, pendingLocal{dir: e.Name(), t: t})
	}

	// Pass 1: themes without extends.
	for _, item := range pending {
		t := item.t
		if t.Extends == "" {
			defaultWarning(t.Theme.Tokens.Light)
			defaultWarning(t.Theme.Tokens.Dark)
			next[t.ID] = t
		}
	}
	// Pass 2: themes with single-level extends.
	for _, item := range pending {
		t := item.t
		if t.Extends == "" {
			continue
		}
		parent, ok := next[t.Extends]
		if !ok {
			if slices.ContainsFunc(pending, func(p pendingLocal) bool { return p.t.ID == t.Extends && p.t.Extends != "" }) {
				recordBroken(item.dir, t.Manifest, fmt.Errorf("cannot extend %q which itself extends another theme (only single-level inheritance is allowed)", t.Extends))
			} else {
				recordBroken(item.dir, t.Manifest, fmt.Errorf("extends unknown theme %q", t.Extends))
			}
			continue
		}
		if parent.Extends != "" {
			recordBroken(item.dir, t.Manifest, fmt.Errorf("cannot extend %q which itself extends another theme (only single-level inheritance is allowed)", t.Extends))
			continue
		}
		inherit(t, parent)
		defaultWarning(t.Theme.Tokens.Light)
		defaultWarning(t.Theme.Tokens.Dark)
		next[t.ID] = t
	}

	r.mu.Lock()
	r.themes = next
	r.broken = broken
	r.mu.Unlock()
}

// Watch monitors the local themes directory in dev mode and invokes onChange after reloading.
func (r *Registry) Watch(ctx context.Context, onChange func()) {
	r.mu.RLock()
	dir := r.localDir
	log := r.log
	r.mu.RUnlock()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		if log != nil {
			log.Warn("watch themes directory", "err", err)
		}
		return
	}
	defer watcher.Close()

	addDirs := func(root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && (!strings.HasPrefix(d.Name(), ".") || p == root) {
				_ = watcher.Add(p)
			}
			return nil
		})
	}
	addDirs(dir)

	var timer *time.Timer
	var timerCh <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					addDirs(ev.Name)
				}
			}
			if ev.Has(fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(100 * time.Millisecond)
				timerCh = timer.C
			}
		case <-timerCh:
			timerCh = nil
			r.Reload()
			if onChange != nil {
				onChange()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			if log != nil {
				log.Warn("themes watcher error", "err", err)
			}
		}
	}
}

func (r *Registry) resolveExtends(t *Theme) error {
	if t.Extends == t.ID {
		return fmt.Errorf("theme %q cannot extend itself", t.ID)
	}
	parent, ok := r.Get(t.Extends)
	if !ok {
		return fmt.Errorf("extends unknown theme %q", t.Extends)
	}
	if parent.Extends != "" {
		return fmt.Errorf("cannot extend %q which itself extends %q (only single-level inheritance is allowed)", parent.ID, parent.Extends)
	}
	inherit(t, parent)
	defaultWarning(t.Theme.Tokens.Light)
	defaultWarning(t.Theme.Tokens.Dark)
	return nil
}

func inherit(child, parent *Theme) {
	if child.Manifest.Theme.ColorScheme == "" {
		child.Theme.ColorScheme = cmp.Or(parent.Theme.ColorScheme, "both")
	}
	child.Theme.Tokens.Light = mergeClone(parent.Theme.Tokens.Light, child.Theme.Tokens.Light)
	child.Theme.Tokens.Dark = mergeClone(parent.Theme.Tokens.Dark, child.Theme.Tokens.Dark)
	child.Theme.Tokens.Fonts = mergeClone(parent.Theme.Tokens.Fonts, child.Theme.Tokens.Fonts)
	child.Theme.Tokens.Viewer = mergeClone(parent.Theme.Tokens.Viewer, child.Theme.Tokens.Viewer)
	child.CSSFiles = slices.Concat(parent.CSSFiles, child.CSSFiles)
	child.Theme.Layout = mergeClone(parent.Theme.Layout, child.Theme.Layout)
	if len(child.Theme.Home) == 0 && len(parent.Theme.Home) > 0 {
		child.Theme.Home = slices.Clone(parent.Theme.Home)
	}
	if len(parent.Theme.Settings) > 0 || len(child.Theme.Settings) > 0 {
		s := make(map[string]Setting, len(parent.Theme.Settings)+len(child.Theme.Settings))
		maps.Copy(s, parent.Theme.Settings)
		maps.Copy(s, child.Theme.Settings)
		child.Theme.Settings = s
	}
}

func mergeClone(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	maps.Copy(out, base)
	maps.Copy(out, over)
	return out
}

func defaultWarning(m map[string]string) {
	if m == nil {
		return
	}
	if m["warning"] == "" && m["highlight"] != "" {
		m["warning"] = m["highlight"]
	}
	if m["warning-foreground"] == "" && m["highlight-foreground"] != "" {
		m["warning-foreground"] = m["highlight-foreground"]
	}
}

func load(files fs.FS, source string) (*Theme, error) {
	t, _, err := loadWithPartial(files, source)
	return t, err
}

func loadWithPartial(files fs.FS, source string) (*Theme, Manifest, error) {
	var m Manifest
	raw, err := fs.ReadFile(files, "plugin.json")
	if err != nil {
		return nil, m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, m, fmt.Errorf("parse plugin.json: %w", err)
	}
	if m.Kind != "theme" {
		return nil, m, fmt.Errorf("kind is %q, want theme", m.Kind)
	}
	if !idPattern.MatchString(m.ID) || m.Version == "" {
		return nil, m, fmt.Errorf("id (1 to 32 lowercase letters, digits or dashes) and version are required")
	}
	if m.Extends == m.ID {
		return nil, m, fmt.Errorf("theme %q cannot extend itself", m.ID)
	}
	if m.Theme.ColorScheme != "" && !slices.Contains(ColorSchemes, m.Theme.ColorScheme) {
		return nil, m, fmt.Errorf("colorScheme: %q is not one of %v", m.Theme.ColorScheme, ColorSchemes)
	}
	if len(m.Settings) > 0 {
		if m.Theme.Settings == nil {
			m.Theme.Settings = map[string]Setting{}
		}
		for k, v := range m.Settings {
			if _, exists := m.Theme.Settings[k]; !exists {
				m.Theme.Settings[k] = v
			}
		}
	}

	rev := m.Version
	if source == "local" {
		rev = hashFS(files)
	}
	t := &Theme{Manifest: m, Source: source, rev: rev, files: files}

	if m.Screenshot != "" {
		clean, err := checkFileExt(files, m.Screenshot, imageExts)
		if err != nil {
			return nil, m, fmt.Errorf("screenshot %q: %w", m.Screenshot, err)
		}
		t.ScreenshotURL = AssetURL(t, clean)
	}
	if m.Theme.CSS != "" {
		clean, err := checkFileExt(files, m.Theme.CSS, []string{".css"})
		if err != nil {
			return nil, m, fmt.Errorf("css file %q: %w", m.Theme.CSS, err)
		}
		t.CSSFiles = []string{AssetURL(t, clean)}
	}
	for area, v := range m.Theme.Layout {
		if options, ok := Layouts[area]; ok && !slices.Contains(options, v) {
			return nil, m, fmt.Errorf("layout %s: %q is not one of %v", area, v, options)
		}
	}
	if err := rewriteBlockAssets(t); err != nil {
		return nil, m, fmt.Errorf("home: %w", err)
	}
	if err := ValidateBlocks(t.Theme.Home); err != nil {
		return nil, m, fmt.Errorf("home: %w", err)
	}
	for group, tokens := range map[string]map[string]string{
		"light":  t.Theme.Tokens.Light,
		"dark":   t.Theme.Tokens.Dark,
		"fonts":  t.Theme.Tokens.Fonts,
		"viewer": t.Theme.Tokens.Viewer,
	} {
		for name, v := range tokens {
			if !tokenName.MatchString(name) || !safeTokenValue(v) {
				return nil, m, fmt.Errorf("token %s.%s: want a CSS value without ; { } < > or comments", group, name)
			}
			rewritten, err := rewriteTokenAsset(t, group, v)
			if err != nil {
				return nil, m, fmt.Errorf("token %s.%s: %w", group, name, err)
			}
			tokens[name] = rewritten
		}
	}
	if err := validateDeclaredSettings(t); err != nil {
		return nil, m, err
	}
	return t, m, nil
}

func checkFileExt(files fs.FS, rel string, allowedExts []string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(rel, "./"))
	if !fs.ValidPath(clean) || clean == "." || clean == "plugin.json" {
		return "", errors.New("invalid file path")
	}
	ext := strings.ToLower(path.Ext(clean))
	if !slices.Contains(allowedExts, ext) {
		return "", fmt.Errorf("extension %q is not allowed (want one of %v)", ext, allowedExts)
	}
	info, err := fs.Stat(files, clean)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return clean, nil
}

func rewriteTokenAsset(t *Theme, group, v string) (string, error) {
	trimmed := strings.TrimPrefix(v, "./")
	if group == "viewer" && strings.HasPrefix(trimmed, "assets/") {
		clean, err := checkFileExt(t.files, trimmed, imageExts)
		if err != nil {
			return "", err
		}
		return AssetURL(t, clean), nil
	}
	var firstErr error
	out := assetURLPat.ReplaceAllStringFunc(v, func(match string) string {
		sub := assetURLPat.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		clean, err := checkFileExt(t.files, sub[2], assetExts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return match
		}
		return "url(" + sub[1] + AssetURL(t, clean) + sub[3] + ")"
	})
	return out, firstErr
}

func rewriteBlockAssets(t *Theme) error {
	for i := range t.Theme.Home {
		b := &t.Theme.Home[i]
		bt, ok := blockType(b.Type)
		if !ok || len(b.Props) == 0 {
			continue
		}
		for _, p := range bt.Props {
			if p.Type != PropImage {
				continue
			}
			s, ok := b.Props[p.Name].(string)
			if !ok {
				continue
			}
			trimmed := strings.TrimPrefix(s, "./")
			if strings.HasPrefix(trimmed, "assets/") {
				clean, err := checkFileExt(t.files, trimmed, imageExts)
				if err != nil {
					return fmt.Errorf("block %d (%s): %s: %w", i+1, b.Type, p.Name, err)
				}
				b.Props[p.Name] = AssetURL(t, clean)
			}
		}
	}
	return nil
}

func validateDeclaredSettings(t *Theme) error {
	for name, s := range t.Theme.Settings {
		if !settingName.MatchString(name) {
			return fmt.Errorf("setting %q: invalid setting name", name)
		}
		if s.Token != "" {
			if s.Type != SettingColor && s.Type != SettingText {
				return fmt.Errorf("setting %s: token binding is only supported for color or text settings", name)
			}
			if !tokenName.MatchString(s.Token) {
				return fmt.Errorf("setting %s: invalid token name %q", name, s.Token)
			}
		}
		switch s.Type {
		case SettingColor, SettingText:
			if s.Default != nil {
				str, ok := s.Default.(string)
				if !ok || utf8.RuneCountInString(str) > 200 || !safeTokenValue(str) {
					return fmt.Errorf("setting %s: default must be a safe CSS value", name)
				}
			}
		case SettingImage:
			if s.Default != nil {
				str, ok := s.Default.(string)
				if !ok {
					return fmt.Errorf("setting %s: default must be a string", name)
				}
				trimmed := strings.TrimPrefix(str, "./")
				if strings.HasPrefix(trimmed, "assets/") {
					clean, err := checkFileExt(t.files, trimmed, imageExts)
					if err != nil {
						return fmt.Errorf("setting %s: default image %q: %w", name, str, err)
					}
					s.Default = AssetURL(t, clean)
				} else if str != "" {
					if err := checkLink(str); err != nil {
						return fmt.Errorf("setting %s: default %w", name, err)
					}
					if !safeTokenValue(str) {
						return fmt.Errorf("setting %s: default %q contains forbidden characters", name, str)
					}
				}
			}
		case SettingChoice:
			if len(s.Options) == 0 {
				return fmt.Errorf("setting %s: choice requires non-empty options", name)
			}
			for _, opt := range s.Options {
				if opt == "" || utf8.RuneCountInString(opt) > 64 || !safeTokenValue(opt) {
					return fmt.Errorf("setting %s: invalid option %q", name, opt)
				}
			}
			if s.Default == nil {
				s.Default = s.Options[0]
			} else if str, ok := s.Default.(string); !ok || !slices.Contains(s.Options, str) {
				return fmt.Errorf("setting %s: default must be one of %v", name, s.Options)
			}
		case SettingBoolean:
			if s.Default == nil {
				s.Default = false
			} else if _, ok := s.Default.(bool); !ok {
				return fmt.Errorf("setting %s: default must be true or false", name)
			}
		default:
			return fmt.Errorf("setting %s: unknown type %q", name, s.Type)
		}
		if len(s.Label) == 0 {
			s.Label = map[string]string{"zh-CN": name, "en": name}
		}
		t.Theme.Settings[name] = s
	}
	return nil
}

// hashFS computes a short content hash of all regular non-hidden files in files.
func hashFS(files fs.FS) string {
	h := sha256.New()
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		f, err := files.Open(p)
		if err != nil {
			return nil
		}
		_, _ = io.WriteString(h, p)
		_, _ = h.Write([]byte{0})
		_, _ = io.Copy(h, f)
		_ = f.Close()
		_, _ = h.Write([]byte{0})
		return nil
	})
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8])
}

// Put adds or replaces a theme (hot: callers bump the config version afterwards).
func (r *Registry) Put(t *Theme) {
	if t.Theme.ColorScheme == "" {
		t.Theme.ColorScheme = "both"
	}
	if t.rev == "" {
		t.rev = t.Version
	}
	if len(t.CSSFiles) == 0 && t.Theme.CSS != "" {
		t.CSSFiles = []string{AssetURL(t, t.Theme.CSS)}
	}
	r.mu.Lock()
	r.themes[t.ID] = t
	r.mu.Unlock()
}

// Get returns a valid installed theme by ID.
func (r *Registry) Get(id string) (*Theme, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.themes[id]
	return t, ok
}

// List returns all themes (including broken local themes) sorted by ID.
func (r *Registry) List() []*Theme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Theme, 0, len(r.themes)+len(r.broken))
	for _, t := range r.themes {
		out = append(out, t)
	}
	out = append(out, r.broken...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return (out[i].Error != "") && (out[j].Error == "")
	})
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

// ValidateSettings is the validator for the theme.settings setting:
// {themeId: {settingName: value}}.
func (r *Registry) ValidateSettings(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return errors.New("want an object")
	}
	for themeID, rawVals := range m {
		vals, ok := rawVals.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object of setting values", themeID)
		}
		t, installed := r.Get(themeID)
		for name, val := range vals {
			if val == nil {
				continue
			}
			if !installed {
				if !settingName.MatchString(name) {
					return fmt.Errorf("%s.%s: invalid setting name", themeID, name)
				}
				switch x := val.(type) {
				case bool:
				case string:
					if utf8.RuneCountInString(x) > 500 || !safeTokenValue(x) {
						return fmt.Errorf("%s.%s: unsafe setting value", themeID, name)
					}
				default:
					return fmt.Errorf("%s.%s: want a string or boolean", themeID, name)
				}
				continue
			}
			def, exists := t.Theme.Settings[name]
			if !exists {
				return fmt.Errorf("%s: unknown setting %q", themeID, name)
			}
			if err := checkSettingValue(def, val); err != nil {
				return fmt.Errorf("%s.%s: %w", themeID, name, err)
			}
		}
	}
	return nil
}

func checkSettingValue(def Setting, val any) error {
	switch def.Type {
	case SettingColor, SettingText:
		s, ok := val.(string)
		if !ok || utf8.RuneCountInString(s) > 200 || !safeTokenValue(s) {
			return errors.New("want a CSS value without ; { } < > or comments")
		}
	case SettingImage:
		s, ok := val.(string)
		if !ok {
			return errors.New("want a string")
		}
		if s != "" {
			if err := checkLink(s); err != nil || !safeTokenValue(s) {
				return errors.New("want a path on this site (/…) or an http(s) URL")
			}
		}
	case SettingChoice:
		s, ok := val.(string)
		if !ok || (s != "" && !slices.Contains(def.Options, s)) {
			return fmt.Errorf("must be one of %v", def.Options)
		}
	case SettingBoolean:
		if _, ok := val.(bool); !ok {
			return errors.New("want true or false")
		}
	}
	return nil
}

var tokenGroups = []string{"light", "dark", "fonts", "viewer"}

// ValidateOverrides is the validator of theme.overrides: {"light" | "dark" | "fonts" |
// "viewer": {token: CSS value}}. Unsafe values were dropped on render before; refusing
// them on save tells the administrator.
func ValidateOverrides(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return errors.New("want an object")
	}
	for group, tokens := range m {
		if !slices.Contains(tokenGroups, group) {
			return fmt.Errorf("unknown group %q, want one of %v", group, tokenGroups)
		}
		tm, ok := tokens.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object of token values", group)
		}
		for name, value := range tm {
			s, ok := value.(string)
			if !ok || !tokenName.MatchString(name) || utf8.RuneCountInString(s) > 200 || !safeTokenValue(s) {
				return fmt.Errorf("%s.%s: want a CSS value without ; { } < > or comments", group, name)
			}
		}
	}
	return nil
}

// Resolve applies the active theme, theme settings, admin token overrides and the home layout.
func (r *Registry) Resolve(s *settings.Snapshot) ResolvedTheme {
	t, ok := r.Get(s.String("theme.active"))
	if !ok {
		t, _ = r.Get(DefaultID)
	}
	if t == nil {
		return ResolvedTheme{
			ID:          "none",
			ColorScheme: "both",
			Tokens:      Tokens{Light: map[string]string{}, Dark: map[string]string{}},
			JS:          []string{},
		}
	}
	tokens := Tokens{
		Light:  maps.Clone(t.Theme.Tokens.Light),
		Dark:   maps.Clone(t.Theme.Tokens.Dark),
		Fonts:  maps.Clone(t.Theme.Tokens.Fonts),
		Viewer: maps.Clone(t.Theme.Tokens.Viewer),
	}
	if tokens.Light == nil {
		tokens.Light = map[string]string{}
	}
	if tokens.Dark == nil {
		tokens.Dark = map[string]string{}
	}

	// Apply theme.settings before theme.overrides: settings with a token update that token
	// in both modes; others are exposed as --sl-setting-<name> and data-sl-setting-<name>.
	var allSettings map[string]map[string]any
	_ = json.Unmarshal(s.Raw("theme.settings"), &allSettings)
	savedSettings := allSettings[t.ID]
	var resolvedSettings map[string]string
	if len(t.Theme.Settings) > 0 {
		resolvedSettings = map[string]string{}
		for name, def := range t.Theme.Settings {
			rawVal := def.Default
			if v, ok := savedSettings[name]; ok && v != nil {
				if str, isStr := v.(string); !isStr || str != "" {
					rawVal = v
				}
			}
			var strVal string
			switch x := rawVal.(type) {
			case bool:
				strVal = strconv.FormatBool(x)
			case string:
				if safeTokenValue(x) {
					strVal = x
				}
			}
			if strVal == "" {
				continue
			}
			if def.Token != "" {
				tokens.Light[def.Token] = strVal
				tokens.Dark[def.Token] = strVal
			} else {
				resolvedSettings[name] = strVal
			}
		}
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
	css := slices.Clone(t.CSSFiles)
	if len(css) == 0 && t.Theme.CSS != "" {
		css = append(css, AssetURL(t, t.Theme.CSS))
	}
	if custom := s.String("theme.custom_css"); custom != "" {
		css = append(css, CustomCSSURL(custom)) // after the theme's, so it wins
	}
	js := []string{}
	if custom := s.String("theme.custom_js"); custom != "" {
		js = append(js, CustomJSURL(custom))
	}
	layout := make(map[string]string, len(Layouts))
	for area, options := range Layouts {
		layout[area] = cmp.Or(t.Theme.Layout[area], options[0])
	}
	return ResolvedTheme{
		ID:          t.ID,
		Version:     t.Version,
		ColorScheme: cmp.Or(t.Theme.ColorScheme, "both"),
		Tokens:      tokens,
		CSS:         css,
		JS:          js,
		Layout:      layout,
		Home:        home,
		Settings:    resolvedSettings,
	}
}

// CustomCSSURL is where theme.custom_css is served: named by its content, so it can be
// cached forever and a change reaches every page at once.
func CustomCSSURL(css string) string { return customURL(css, ".css") }

// CustomJSURL is where theme.custom_js is served, named like CustomCSSURL.
func CustomJSURL(js string) string { return customURL(js, ".js") }

func customURL(content, ext string) string {
	sum := sha256.Sum256([]byte(content))
	return "/themes-assets/_custom/" + hex.EncodeToString(sum[:8]) + ext
}

// CustomHandler serves theme.custom_css and theme.custom_js (css and js return the
// current values) at CustomCSSURL and CustomJSURL. Files of their own, not a <style> or
// <script> in the page: nothing in them can end the element and inject HTML, and the
// script runs only where a page loads it with its nonce.
func CustomHandler(css, js func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var content, url, typ string
		switch path.Ext(req.URL.Path) {
		case ".css":
			content = css()
			url, typ = CustomCSSURL(content), "text/css; charset=utf-8"
		case ".js":
			content = js()
			url, typ = CustomJSURL(content), "text/javascript; charset=utf-8"
		}
		if content == "" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if req.URL.Path == url {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache") // a page from before the last change
		}
		_, _ = io.WriteString(w, content)
	})
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

// safeTokenValue keeps a token value inside its CSS declaration. All tokens are written
// on one line, so a value must neither end the declaration (; { } < >, a comment) nor
// run past it into the next ones (an unterminated string, an unclosed bracket, a
// trailing escape, a line break).
func safeTokenValue(v string) bool {
	if strings.ContainsAny(v, ";{}<>\n\r\f") || strings.Contains(v, "/*") {
		return false
	}
	var quote rune
	depth, escaped := 0, false
	for _, c := range v {
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	return !escaped && quote == 0 && depth == 0
}

// AssetURL is the public URL of a theme file; the version or content hash in the path
// makes it cacheable forever.
func AssetURL(t *Theme, file string) string {
	rev := cmp.Or(t.rev, t.Version)
	return "/themes-assets/" + t.ID + "/" + rev + "/" + strings.TrimPrefix(file, "/")
}

// AssetHandler serves /themes-assets/{id}/{version-or-hash}/{file...}.
func (r *Registry) AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rest := strings.TrimPrefix(req.URL.Path, "/themes-assets/")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) != 3 || parts[2] == "" {
			http.NotFound(w, req)
			return
		}
		t, ok := r.Get(parts[0])
		if !ok || t.files == nil {
			http.NotFound(w, req)
			return
		}
		file := path.Clean(parts[2])
		ext := strings.ToLower(path.Ext(file))
		if !fs.ValidPath(file) || file == "." || file == "plugin.json" || !slices.Contains(assetExts, ext) {
			http.NotFound(w, req)
			return
		}
		// Only regular files: ServeFileFS would otherwise render directory listings.
		if info, err := fs.Stat(t.files, file); err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if ext == ".svg" {
			w.Header().Set("Content-Security-Policy", "sandbox")
		}
		if parts[1] == cmp.Or(t.rev, t.Version) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache") // stale URL after an upgrade or edit
		}
		http.ServeFileFS(w, req, t.files, file)
	})
}
