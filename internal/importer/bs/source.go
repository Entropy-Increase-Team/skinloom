package bs

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
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
	// Mail is BS's mail configuration, which lives in .env; nil without --env.
	Mail *Mail
}

// Mail is the MAIL_* part of BS's .env, as Laravel reads it.
type Mail struct {
	Mailer, Host, Port, Username, Password, Encryption string
	FromAddress, FromName                              string
	// HostSet: MAIL_HOST is in .env (Laravel's default host is a placeholder).
	HostSet bool
}

// Method is a BS password method (PWD_METHOD's values) with its salt.
type Method struct{ Name, Salt string }

var validPrefix = regexp.MustCompile(`^[A-Za-z0-9_]*$`)

// listSep splits PASSWORD_METHODS and the plugin's SALT list as the plugin does.
var listSep = regexp.MustCompile(`,\s*`)

// EnvInfo holds non-secret metadata parsed from a Blessing Skin .env file for display
// and pre-filling the setup wizard.
type EnvInfo struct {
	Driver           string   `json:"driver"`
	Host             string   `json:"host,omitempty"`
	Port             int      `json:"port,omitempty"`
	Database         string   `json:"database,omitempty"`
	Username         string   `json:"username,omitempty"`
	Socket           string   `json:"socket,omitempty"`
	Prefix           string   `json:"prefix"`
	PasswordMethod   string   `json:"passwordMethod"`
	Transition       []string `json:"transition,omitempty"`
	HasSalt          bool     `json:"hasSalt"`
	TexturesDir      string   `json:"texturesDir,omitempty"`
	FSDriver         string   `json:"fsDriver,omitempty"`
	HasMail          bool     `json:"hasMail"`
	LocalhostWarning bool     `json:"localhostWarning"`
}

// FromEnv reads a Blessing Skin .env file; the BS root is the directory it is in.
func FromEnv(path string) (Source, error) {
	s, _, err := FromEnvWithInfo(path)
	return s, err
}

// FromEnvWithInfo reads a Blessing Skin .env file and returns both the Source and
// non-secret summary metadata.
func FromEnvWithInfo(path string) (Source, EnvInfo, error) {
	raw, err := ReadEnv(path)
	if err != nil {
		return Source{}, EnvInfo{}, err
	}
	return fromEnvMap(raw, filepath.Dir(path))
}

// FromEnvBytes parses uploaded Blessing Skin .env content in memory.
func FromEnvBytes(data []byte) (Source, EnvInfo, error) {
	raw, err := ReadEnvBytes(data)
	if err != nil {
		return Source{}, EnvInfo{}, err
	}
	return fromEnvMap(raw, "")
}

func fromEnvMap(raw map[string]string, root string) (Source, EnvInfo, error) {
	env := make(map[string]string, len(raw))
	for k, v := range raw {
		env[k] = laravelValue(v)
	}
	s := Source{
		Prefix:         env["DB_PREFIX"],
		PasswordMethod: env["PWD_METHOD"],
		Salt:           env["SALT"],
		TexturesDir:    env["TEXTURES_DIR"],
	}
	info := EnvInfo{
		Prefix:         s.Prefix,
		PasswordMethod: cmp.Or(s.PasswordMethod, "BCRYPT"),
		HasSalt:        s.Salt != "",
		FSDriver:       cmp.Or(env["FS_DRIVER"], "local"),
	}
	if methods := strings.TrimSpace(env["PASSWORD_METHODS"]); methods != "" {
		salts := listSep.Split(env["SALT"], -1)
		for i, m := range listSep.Split(methods, -1) {
			salt := ""
			if i < len(salts) {
				salt = salts[i]
			}
			name := strings.TrimSpace(m)
			s.Transition = append(s.Transition, Method{Name: name, Salt: salt})
			if name != "" {
				info.Transition = append(info.Transition, name)
			}
		}
	}
	_, hostSet := env["MAIL_HOST"]
	s.Mail = &Mail{
		Mailer: strings.ToLower(cmp.Or(env["MAIL_MAILER"], env["MAIL_DRIVER"], "smtp")), Host: env["MAIL_HOST"],
		Port: cmp.Or(env["MAIL_PORT"], "587"), Username: env["MAIL_USERNAME"], Password: env["MAIL_PASSWORD"],
		Encryption: strings.ToLower(env["MAIL_ENCRYPTION"]), FromAddress: env["MAIL_FROM_ADDRESS"],
		FromName: env["MAIL_FROM_NAME"], HostSet: hostSet && env["MAIL_HOST"] != "",
	}
	info.HasMail = s.Mail.HostSet
	if s.TexturesDir == "" && root != "" {
		s.TexturesDir = filepath.Join(root, "storage", "textures")
	} else if s.TexturesDir != "" && !filepath.IsAbs(s.TexturesDir) && root != "" {
		s.TexturesDir = filepath.Join(root, s.TexturesDir)
	}
	if d := env["FS_DRIVER"]; d != "" && d != "local" {
		// The files live in object storage (an OSS/COS/Qiniu plugin): they have to be
		// downloaded into a directory first and given with --textures.
		s.TexturesDir = ""
	}
	info.TexturesDir = s.TexturesDir

	host := cmp.Or(env["DB_HOST"], "127.0.0.1")
	switch conn := strings.ToLower(cmp.Or(env["DB_CONNECTION"], "mysql")); conn {
	case "mysql", "mariadb":
		s.Driver = "mysql"
		portStr := cmp.Or(env["DB_PORT"], "3306")
		port, _ := strconv.Atoi(portStr)
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.DBName = env["DB_USERNAME"], env["DB_PASSWORD"], env["DB_DATABASE"]
		if sock := env["DB_SOCKET"]; sock != "" {
			cfg.Net, cfg.Addr = "unix", sock
			info.Socket = sock
		} else {
			cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(host, portStr)
			info.Host, info.Port = host, port
		}
		s.DSN = cfg.FormatDSN()
		info.Driver, info.Database, info.Username = "mysql", env["DB_DATABASE"], env["DB_USERNAME"]
		info.LocalhostWarning = isLocalhost(host) && runningInContainer()
	case "pgsql", "postgres", "postgresql":
		s.Driver = "postgres"
		portStr := cmp.Or(env["DB_PORT"], "5432")
		port, _ := strconv.Atoi(portStr)
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, portStr), Path: "/" + env["DB_DATABASE"]}
		if env["DB_USERNAME"] != "" {
			u.User = url.UserPassword(env["DB_USERNAME"], env["DB_PASSWORD"])
		}
		s.DSN = u.String()
		info.Driver, info.Host, info.Port, info.Database, info.Username = "postgres", host, port, env["DB_DATABASE"], env["DB_USERNAME"]
		info.LocalhostWarning = isLocalhost(host) && runningInContainer()
	case "sqlite":
		s.Driver = "sqlite"
		defPath := "database/database.sqlite"
		if root != "" {
			defPath = filepath.Join(root, "database", "database.sqlite")
		}
		s.DSN = cmp.Or(env["DB_DATABASE"], defPath)
		if !filepath.IsAbs(s.DSN) && root != "" {
			s.DSN = filepath.Join(root, s.DSN)
		}
		info.Driver, info.Database = "sqlite", s.DSN
	default:
		return Source{}, EnvInfo{}, fmt.Errorf("DB_CONNECTION %q is not supported (want mysql, pgsql or sqlite)", conn)
	}
	return s, info, nil
}

func isLocalhost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// SourceSummary describes a read-only check of a Blessing Skin source database and texture directory.
type SourceSummary struct {
	Users           int            `json:"users"`
	Players         int            `json:"players"`
	Textures        int            `json:"textures"`
	Closet          int            `json:"closet"`
	HasYggdrasilKey bool           `json:"hasYggdrasilKey"`
	TextureDirStats TextureDirInfo `json:"textureDirStats"`
}

// TextureDirInfo summarizes the local BS textures directory if given.
type TextureDirInfo struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// TestSource runs a lightweight read-only check against s without loading all rows.
func TestSource(ctx context.Context, s Source) (SourceSummary, error) {
	var sum SourceSummary
	pool, err := s.open(ctx)
	if err != nil {
		return sum, err
	}
	defer pool.Close()

	var cols string
	if rows, err := pool.QueryContext(ctx, "SELECT pid, uid, name, tid_skin, tid_cape FROM "+s.table("players")+" WHERE 1=0"); err != nil {
		return sum, fmt.Errorf("read %s: %w", s.table("players"), err)
	} else {
		rows.Close()
	}
	_ = cols

	tx, err := s.begin(ctx, pool)
	if err != nil {
		return sum, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("users")).Scan(&sum.Users); err != nil {
		return sum, fmt.Errorf("count users: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("players")).Scan(&sum.Players); err != nil {
		return sum, fmt.Errorf("count players: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("textures")).Scan(&sum.Textures); err != nil {
		return sum, fmt.Errorf("count textures: %w", err)
	}
	_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("user_closet")).Scan(&sum.Closet)
	var keyVal string
	if err := tx.QueryRowContext(ctx, "SELECT option_value FROM "+s.table("options")+" WHERE option_name = 'ygg_private_key'").Scan(&keyVal); err == nil {
		sum.HasYggdrasilKey = strings.TrimSpace(keyVal) != ""
	}
	_ = tx.Commit()

	if s.TexturesDir != "" {
		sum.TextureDirStats = statTexturesDir(s.TexturesDir)
	}
	return sum, nil
}

func statTexturesDir(dir string) TextureDirInfo {
	info := TextureDirInfo{Path: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return info
	}
	info.Exists = true
	for _, e := range entries {
		if e.IsDir() || !storage.ValidKey(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() {
			info.Files++
			info.Bytes += fi.Size()
		}
	}
	return info
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

// col quotes a column name that is a reserved word (language_lines.group, .key).
func (s Source) col(name string) string {
	if s.Driver == "mysql" {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}

// table is the quoted name of a BS table.
func (s Source) table(name string) string {
	if s.Driver == "mysql" {
		return "`" + s.Prefix + name + "`"
	}
	return `"` + s.Prefix + name + `"`
}
