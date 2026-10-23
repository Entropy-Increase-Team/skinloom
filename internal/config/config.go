// Package config loads the static (file + environment) configuration.
//
// Runtime settings that admins edit live in the database (internal/settings); this file
// only holds what is needed before the database is reachable. Keys are either hot
// (applied while running) or cold (need a restart) — see IsCold. The canonical public
// URL is the site.url runtime setting, not a config key.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

// EnvPrefix prefixes every environment override. Nested keys use "__":
// SKINLOOM_DATABASE__DSN=... overrides [database] dsn.
const EnvPrefix = "SKINLOOM_"

// Config is the whole static configuration.
type Config struct {
	Server   Server   `koanf:"server"`
	Database Database `koanf:"database"`
	Web      Web      `koanf:"web"`
	Log      Log      `koanf:"log"`
}

type Server struct {
	// Listen is the single public address, e.g. ":8080". Cold.
	Listen string `koanf:"listen"`
	// Role selects the planes this process serves: all | api. Cold.
	Role string `koanf:"role"`
	// TrustedProxies lists IPs/CIDRs whose X-Forwarded-* headers are believed. Hot.
	TrustedProxies []string `koanf:"trusted_proxies"`
}

type Database struct {
	Driver       string `koanf:"driver"` // sqlite | mysql | postgres. Cold.
	DSN          string `koanf:"dsn"`    // Cold.
	MaxOpenConns int    `koanf:"max_open_conns"`
}

type Web struct {
	// Mode: managed (start bundled Next.js), dev (run `pnpm dev`), external (proxy URL), off.
	Mode string `koanf:"mode"`
	// Dir holds the Next.js standalone build (managed mode). Empty = <exe dir>/web.
	Dir string `koanf:"dir"`
	// Port is the internal port of the managed/dev Next.js process; 0 or a busy port
	// means "pick a free one".
	Port int `koanf:"port"`
	// URL is the upstream in external mode, e.g. http://web:3000.
	URL string `koanf:"url"`
	// Node is an explicit node executable. Empty = bundled runtime, then PATH.
	Node string `koanf:"node"`
	// DevDir is the pnpm workspace used in dev mode.
	DevDir string `koanf:"dev_dir"`
}

type Log struct {
	Level  string `koanf:"level"`  // debug | info | warn | error. Hot.
	Format string `koanf:"format"` // text | json. Hot.
}

// Defaults is what a fresh install runs with: zero external dependencies.
func Defaults() Config {
	return Config{
		Server:   Server{Listen: ":8080", Role: "all"},
		Database: Database{Driver: "sqlite", DSN: "skinloom.db"},
		Web:      Web{Mode: "managed", Port: 3000, DevDir: "web"},
		Log:      Log{Level: "info", Format: "text"},
	}
}

// Load reads defaults, then the TOML file (if it exists), then environment overrides.
func Load(path string) (Config, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(Defaults(), "koanf"), nil); err != nil {
		return Config{}, err
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
				return Config{}, fmt.Errorf("read %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}
	envKey := func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, EnvPrefix)), "__", ".")
	}
	if err := k.Load(env.Provider(EnvPrefix, ".", envKey), nil); err != nil {
		return Config{}, err
	}
	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return Config{}, err
	}
	return c, c.Validate()
}

// Validate rejects values the process cannot run with.
func (c Config) Validate() error {
	switch c.Server.Role {
	case "all", "api":
	case "game", "worker":
		return fmt.Errorf("server.role %q arrives in a later milestone; use all or api", c.Server.Role)
	default:
		return fmt.Errorf("server.role must be all or api, got %q", c.Server.Role)
	}
	switch c.Web.Mode {
	case "managed", "dev", "external", "off":
	default:
		return fmt.Errorf("web.mode must be managed, dev, external or off, got %q", c.Web.Mode)
	}
	if c.Web.Mode == "external" && c.Web.URL == "" {
		return errors.New("web.url is required when web.mode = external")
	}
	if c.Web.Port < 0 || c.Web.Port > 65535 {
		return fmt.Errorf("web.port must be 0-65535, got %d", c.Web.Port)
	}
	if _, err := realip.New(c.Server.TrustedProxies); err != nil {
		return fmt.Errorf("server.trusted_proxies: %w", err)
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		return fmt.Errorf("log.format must be text or json, got %q", c.Log.Format)
	}
	return nil
}

// UnknownKeys lists keys in the file at path that SkinLoom does not know, so typos
// ("lisen", "[databse]") are reported instead of silently ignored.
func UnknownKeys(path string) ([]string, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}
	known := flatten(Defaults())
	var out []string
	for _, key := range k.Keys() {
		if _, ok := known[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

// coldKeys need a restart to take effect.
var coldKeys = map[string]bool{
	"server.listen":           true,
	"server.role":             true,
	"database.driver":         true,
	"database.dsn":            true,
	"database.max_open_conns": true,
}

// Diff lists the dotted keys whose values differ between a and b.
func Diff(a, b Config) []string {
	ka, kb := flatten(a), flatten(b)
	var out []string
	for key, va := range ka {
		if !reflect.DeepEqual(va, kb[key]) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// IsCold reports whether a changed key needs a restart.
func IsCold(key string) bool { return coldKeys[key] }

func flatten(c Config) map[string]any {
	k := koanf.New(".")
	_ = k.Load(structs.Provider(c, "koanf"), nil)
	return k.All()
}

// WriteTemplate writes a commented default config if path does not exist yet.
func WriteTemplate(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(template), 0o644)
}

const template = `# SkinLoom configuration.
# Keys marked [hot] apply while running; [cold] keys need "skinloom restart".
# Every key can be overridden by an environment variable: SKINLOOM_<SECTION>__<KEY>,
# e.g. SKINLOOM_DATABASE__DSN. Site settings (name, theme, rules...) live in the admin panel.

[server]
listen = ":8080"          # [cold] the only port users need
role = "all"              # [cold] all | api
trusted_proxies = []      # [hot] proxies allowed to set X-Forwarded-*, e.g. ["127.0.0.1", "10.0.0.0/8"]
                          #       (the public site URL is the site.url setting, see "skinloom settings")

[database]
driver = "sqlite"         # [cold] sqlite | mysql | postgres
dsn = "skinloom.db"       # [cold] sqlite: path relative to the data dir
                          #        mysql:    user:pass@tcp(127.0.0.1:3306)/skinloom
                          #        postgres: postgres://user:pass@127.0.0.1:5432/skinloom

[web]
mode = "managed"          # [hot] managed | dev | external | off
port = 3000               # [hot] internal port of the Next.js process (busy or 0 = pick a free one)
url = ""                  # [hot] upstream when mode = external
node = ""                 # [hot] node executable; empty = bundled runtime, then PATH

[log]
level = "info"            # [hot] debug | info | warn | error
format = "text"           # [hot] text | json
`
