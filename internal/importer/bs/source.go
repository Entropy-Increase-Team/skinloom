package bs

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Source is the Blessing Skin site to import from. FromEnv fills it from the site's
// .env file; command-line flags override single fields.
type Source struct {
	// Driver is mysql, postgres or sqlite.
	Driver string
	// DSN in the driver's own format (go-sql-driver/mysql, a postgres:// URL, or the
	// SQLite file path).
	DSN string
	// Prefix is DB_PREFIX, prepended to every table name.
	Prefix string
	// PasswordMethod is PWD_METHOD (BCRYPT when unset) and Salt is SALT.
	PasswordMethod string
	Salt           string
	// Transition lists PASSWORD_METHODS with their salts, for the
	// password-algorithm-transition plugin: the method new passwords use first, then the
	// older ones still accepted. It is used only when the plugin is enabled in BS.
	Transition []Method
	// TexturesDir holds the texture files named by their hash (storage/textures).
	TexturesDir string
	// Location is the time zone BS wrote its datetimes in (config/app.php, Asia/Shanghai
	// unless the site changed it).
	Location *time.Location
}

// Method is a BS password method (PWD_METHOD's values) with its salt.
type Method struct{ Name, Salt string }

var validPrefix = regexp.MustCompile(`^[A-Za-z0-9_]*$`)

// listSep splits PASSWORD_METHODS and the plugin's SALT list as the plugin does.
var listSep = regexp.MustCompile(`,\s*`)

// FromEnv reads a Blessing Skin .env file; the BS root is the directory it is in.
func FromEnv(path string) (Source, error) {
	raw, err := ReadEnv(path)
	if err != nil {
		return Source{}, err
	}
	env := make(map[string]string, len(raw))
	for k, v := range raw {
		env[k] = laravelValue(v)
	}
	root := filepath.Dir(path)
	s := Source{
		Prefix:         env["DB_PREFIX"],
		PasswordMethod: env["PWD_METHOD"],
		Salt:           env["SALT"],
		TexturesDir:    env["TEXTURES_DIR"],
	}
	if methods := strings.TrimSpace(env["PASSWORD_METHODS"]); methods != "" {
		salts := listSep.Split(env["SALT"], -1)
		for i, m := range listSep.Split(methods, -1) {
			salt := ""
			if i < len(salts) {
				salt = salts[i]
			}
			s.Transition = append(s.Transition, Method{Name: strings.TrimSpace(m), Salt: salt})
		}
	}
	if s.TexturesDir == "" {
		s.TexturesDir = filepath.Join(root, "storage", "textures")
	} else if !filepath.IsAbs(s.TexturesDir) {
		s.TexturesDir = filepath.Join(root, s.TexturesDir)
	}
	if d := env["FS_DRIVER"]; d != "" && d != "local" {
		// The files live in object storage (an OSS/COS/Qiniu plugin): they have to be
		// downloaded into a directory first and given with --textures.
		s.TexturesDir = ""
	}
	host := cmp.Or(env["DB_HOST"], "127.0.0.1")
	switch conn := strings.ToLower(cmp.Or(env["DB_CONNECTION"], "mysql")); conn {
	case "mysql", "mariadb":
		s.Driver = "mysql"
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.DBName = env["DB_USERNAME"], env["DB_PASSWORD"], env["DB_DATABASE"]
		if sock := env["DB_SOCKET"]; sock != "" {
			cfg.Net, cfg.Addr = "unix", sock
		} else {
			cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(host, cmp.Or(env["DB_PORT"], "3306"))
		}
		s.DSN = cfg.FormatDSN()
	case "pgsql", "postgres", "postgresql":
		s.Driver = "postgres"
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, cmp.Or(env["DB_PORT"], "5432")), Path: "/" + env["DB_DATABASE"]}
		if env["DB_USERNAME"] != "" {
			u.User = url.UserPassword(env["DB_USERNAME"], env["DB_PASSWORD"])
		}
		s.DSN = u.String()
	case "sqlite":
		s.Driver = "sqlite"
		s.DSN = cmp.Or(env["DB_DATABASE"], filepath.Join(root, "database", "database.sqlite"))
		if !filepath.IsAbs(s.DSN) {
			s.DSN = filepath.Join(root, s.DSN)
		}
	default:
		return Source{}, fmt.Errorf("DB_CONNECTION %q is not supported (want mysql, pgsql or sqlite)", conn)
	}
	return s, nil
}

// laravelValue is what Laravel's env() makes of a value: null, (null), empty and
// (empty) are null or "" (config() then gives ""), the rest is as written.
func laravelValue(v string) string {
	switch strings.ToLower(v) {
	case "null", "(null)", "empty", "(empty)":
		return ""
	}
	return v
}

// open connects to the source database. Nothing is ever written to it: SQLite is opened
// read-only, and the server databases are only read inside a read-only transaction.
func (s Source) open(ctx context.Context) (*sql.DB, error) {
	if !validPrefix.MatchString(s.Prefix) {
		return nil, fmt.Errorf("table prefix %q: only letters, digits and _", s.Prefix)
	}
	var (
		pool *sql.DB
		err  error
	)
	switch s.Driver {
	case "mysql":
		cfg, perr := mysql.ParseDSN(s.DSN)
		if perr != nil {
			return nil, fmt.Errorf("source dsn: %w", perr)
		}
		cfg.ParseTime = false // datetimes are read as text and placed in Location
		conn, cerr := mysql.NewConnector(cfg)
		if cerr != nil {
			return nil, cerr
		}
		pool = sql.OpenDB(conn)
	case "postgres":
		pool, err = sql.Open("pgx", s.DSN)
	case "sqlite":
		path := strings.TrimPrefix(s.DSN, "file:")
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		q := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(5000)"}}
		pool, err = sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	default:
		return nil, fmt.Errorf("source driver %q: want mysql, postgres or sqlite", s.Driver)
	}
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(2)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to the Blessing Skin database: %w", err)
	}
	return pool, nil
}

// begin starts the read-only transaction everything is read in, so the import sees one
// consistent snapshot even if the BS site is still running.
func (s Source) begin(ctx context.Context, pool *sql.DB) (*sql.Tx, error) {
	if s.Driver == "sqlite" {
		return pool.BeginTx(ctx, nil) // the connection itself is read-only
	}
	return pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
}

// table is the quoted name of a BS table.
func (s Source) table(name string) string {
	if s.Driver == "mysql" {
		return "`" + s.Prefix + name + "`"
	}
	return `"` + s.Prefix + name + `"`
}

var errNoTable = errors.New("table not found")
