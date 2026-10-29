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

// listKeys are list-valued keys; their environment overrides are comma-separated.
var listKeys = map[string]bool{"server.trusted_proxies": true}

// Config is the whole static configuration.
type Config struct {
	Server   Server   `koanf:"server"`
	Database Database `koanf:"database"`
	Storage  Storage  `koanf:"storage"`
	Web      Web      `koanf:"web"`
	Log      Log      `koanf:"log"`
}

// Storage is where texture files live.
type Storage struct {
	// Driver: local | s3 (any S3-compatible service). Cold.
	Driver string `koanf:"driver"`
	// Path of the local store: absolute, or relative to the data dir. Cold.
	Path string `koanf:"path"`
	// The S3 store. Keep secret_key out of config.toml if you can: set
	// SKINLOOM_STORAGE__SECRET_KEY instead. Cold.
	Endpoint  string `koanf:"endpoint"` // host[:port], e.g. oss-cn-hangzhou.aliyuncs.com
	Bucket    string `koanf:"bucket"`
	Region    string `koanf:"region"`
	AccessKey string `koanf:"access_key"`
	SecretKey string `koanf:"secret_key"`
	Prefix    string `koanf:"prefix"`     // inside the bucket, e.g. "textures/"
	Insecure  bool   `koanf:"insecure"`   // plain HTTP, e.g. a MinIO on the same host
	PathStyle bool   `koanf:"path_style"` // bucket in the path (MinIO and most self-hosted stores)
	PublicURL string `koanf:"public_url"` // a CDN or public bucket URL: texture files redirect there
}

// Dir is the local store's directory for a data dir.
func (s Storage) Dir(dataDir string) string {
	if filepath.IsAbs(s.Path) {
		return s.Path
	}
	return filepath.Join(dataDir, s.Path)
}

type Server struct {
	// Listen is the single public address, e.g. ":17546". Cold.
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
	// Mode: managed (start bundled Next.js), dev (frontend dev server), external (proxy URL), off.
	Mode string `koanf:"mode"`
	// Dir holds the Next.js standalone build (managed mode). Empty = <exe dir>/web.
	Dir string `koanf:"dir"`
	// Port is the internal port of the managed/dev Next.js process; 0 or a busy port
	// means "pick a free one".
	Port int `koanf:"port"`
	// URL is the upstream in external mode, e.g. http://web:17547.
	URL string `koanf:"url"`
	// Node is an explicit node executable. Empty = bundled runtime, then PATH.
	Node string `koanf:"node"`
	// DevDir is the frontend workspace used in dev mode.
	DevDir string `koanf:"dev_dir"`
	// PackageManager runs the frontend in dev mode: auto (bun, then pnpm, then npm),
	// bun, pnpm or npm.
	PackageManager string `koanf:"package_manager"`
}

type Log struct {
	Level  string `koanf:"level"`  // debug | info | warn | error. Hot.
	Format string `koanf:"format"` // text | json. Hot.
}

// Defaults is what a fresh install runs with: zero external dependencies.
func Defaults() Config {
	return Config{
		Server:   Server{Listen: ":17546", Role: "all"},
		Database: Database{Driver: "sqlite", DSN: "skinloom.db"},
		Storage:  Storage{Driver: "local", Path: "textures"},
		Web:      Web{Mode: "managed", Port: 17547, DevDir: "web", PackageManager: "auto"},
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
	envKV := func(key, value string) (string, any) {
		key = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, EnvPrefix)), "__", ".")
		if listKeys[key] {
			// SKINLOOM_SERVER__TRUSTED_PROXIES=10.42.0.0/16,10.43.0.0/16
			var items []string
			for item := range strings.SplitSeq(value, ",") {
				if item = strings.TrimSpace(item); item != "" {
					items = append(items, item)
				}
			}
			return key, items
		}
		return key, value
	}
	if err := k.Load(env.ProviderWithValue(EnvPrefix, ".", envKV), nil); err != nil {
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
	switch c.Web.PackageManager {
	case "", "auto", "bun", "pnpm", "npm": // "" = auto, like the launcher
	default:
		return fmt.Errorf("web.package_manager must be auto, bun, pnpm or npm, got %q", c.Web.PackageManager)
	}
	switch c.Storage.Driver {
	case "local":
		if c.Storage.Path == "" {
			return errors.New("storage.path is required for the local driver")
		}
	case "s3":
		s := c.Storage
		if s.Endpoint == "" || s.Bucket == "" || s.AccessKey == "" || s.SecretKey == "" {
			return errors.New("storage.endpoint, storage.bucket, storage.access_key and storage.secret_key are required for the s3 driver")
		}
		if strings.Contains(s.Endpoint, "://") {
			return errors.New("storage.endpoint is a host name (and port), without http:// or https:// (set storage.insecure for plain HTTP)")
		}
		if s.PublicURL != "" && !strings.HasPrefix(s.PublicURL, "https://") && !strings.HasPrefix(s.PublicURL, "http://") {
			return errors.New("storage.public_url must start with https:// or http://")
		}
	default:
		return fmt.Errorf("storage.driver must be local or s3, got %q", c.Storage.Driver)
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
	"storage.driver":          true,
	"storage.path":            true,
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
listen = ":17546"         # [cold] the only port users need
role = "all"              # [cold] all | api
trusted_proxies = []      # [hot] proxies allowed to set X-Forwarded-*, e.g. ["127.0.0.1", "10.0.0.0/8"]
                          #       (the public site URL is the site.url setting, see "skinloom settings")

[database]
driver = "sqlite"         # [cold] sqlite | mysql | postgres
dsn = "skinloom.db"       # [cold] sqlite: path relative to the data dir
                          #        mysql:    user:pass@tcp(127.0.0.1:3306)/skinloom
                          #        postgres: postgres://user:pass@127.0.0.1:5432/skinloom

[storage]
driver = "local"          # [cold] local | s3 (Amazon S3, Aliyun OSS, Tencent COS, Qiniu, R2, MinIO...)
path = "textures"         # [cold] local: texture files, absolute or relative to the data dir
# s3: endpoint = "oss-cn-hangzhou.aliyuncs.com", bucket, region, access_key, prefix = "textures/",
#     path_style (MinIO), insecure (plain HTTP), public_url (a CDN: texture files redirect there).
#     Keep the secret out of this file: SKINLOOM_STORAGE__SECRET_KEY=...

[web]
mode = "managed"          # [hot] managed | dev | external | off
port = 17547              # [hot] internal port of the Next.js process (busy or 0 = pick a free one)
url = ""                  # [hot] upstream when mode = external
node = ""                 # [hot] node executable; empty = bundled runtime, then PATH
package_manager = "auto"  # [hot] dev mode only: auto (bun, then pnpm, then npm) | bun | pnpm | npm

[log]
level = "info"            # [hot] debug | info | warn | error
format = "text"           # [hot] text | json
`
