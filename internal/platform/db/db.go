// Package db opens the database connection shared by ent and raw SQL users.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	_ "modernc.org/sqlite"             // registers the "sqlite" driver

	"github.com/Entropy-Increase-Team/skinloom/ent"
)

// Dialect is one of the supported database engines.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	MySQL    Dialect = "mysql"
	Postgres Dialect = "postgres"
)

// Options describe how to reach the database.
type Options struct {
	Driver       string
	DSN          string
	DataDir      string // relative SQLite paths resolve against this
	MaxOpenConns int
}

// DB bundles the raw pool and the ent client built on top of it.
type DB struct {
	Dialect Dialect
	SQL     *sql.DB
	Ent     *ent.Client
}

// ServerVersion asks the database for its version, for the system status page.
func (d *DB) ServerVersion(ctx context.Context) (string, error) {
	q := map[Dialect]string{SQLite: `SELECT sqlite_version()`, MySQL: `SELECT version()`, Postgres: `SHOW server_version`}[d.Dialect]
	var v string
	err := d.SQL.QueryRowContext(ctx, q).Scan(&v)
	return v, err
}

// IsSQLite reports whether driver names SQLite the way Open accepts it.
func IsSQLite(driver string) bool {
	d := Dialect(strings.ToLower(driver))
	return d == SQLite || d == "sqlite3"
}

// Open connects and pings the database.
func Open(ctx context.Context, o Options) (*DB, error) {
	var (
		d          = Dialect(strings.ToLower(o.Driver))
		driverName string
		entDialect string
		dsn        = o.DSN
		err        error
	)
	switch d {
	case SQLite, "sqlite3":
		d, driverName, entDialect = SQLite, "sqlite", dialect.SQLite
		dsn, err = sqliteDSN(dsn, o.DataDir)
	case MySQL, "mariadb":
		d, driverName, entDialect = MySQL, "mysql", dialect.MySQL
		dsn, err = mysqlDSN(dsn)
	case Postgres, "postgresql", "pg":
		d, driverName, entDialect = Postgres, "pgx", dialect.Postgres
	default:
		return nil, fmt.Errorf("unsupported database driver %q (want sqlite, mysql or postgres)", o.Driver)
	}
	if err != nil {
		return nil, err
	}

	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", d, err)
	}
	maxOpen := o.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 20
		if d == SQLite {
			maxOpen = 4 // one writer at a time anyway; WAL lets readers proceed
		}
	}
	pool.SetMaxOpenConns(maxOpen)
	pool.SetMaxIdleConns(maxOpen)
	pool.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to %s: %w", d, err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(entDialect, pool)))
	return &DB{Dialect: d, SQL: pool, Ent: client}, nil
}

// Close releases the pool.
func (db *DB) Close() error { return db.Ent.Close() }

// ProbeState classifies a probed database target during first-run setup.
type ProbeState string

const (
	ProbeEmpty             ProbeState = "empty"
	ProbeSkinLoomInstalled ProbeState = "skinloom_installed"
	ProbeSuspectedBS       ProbeState = "suspected_bs"
	ProbeMissingDB         ProbeState = "missing_db"
	ProbeError             ProbeState = "error"
)

// ProbeResult describes the connectivity and existing schema state of a database target.
type ProbeResult struct {
	OK          bool
	Dialect     Dialect
	Version     string
	State       ProbeState
	Summary     string
	Users       int
	Textures    int
	Players     int
	HintSQL     string
	ErrorDetail string
}

// Probe tests connectivity to o with a short timeout and classifies the database state
// without creating SQLite files on disk or applying migrations.
func Probe(ctx context.Context, o Options) (ProbeResult, error) {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	d := Dialect(strings.ToLower(o.Driver))
	switch d {
	case SQLite, "sqlite3":
		return probeSQLite(pctx, o)
	case MySQL, "mariadb":
		d = MySQL
		dsn, err := mysqlDSN(o.DSN)
		if err != nil {
			res := ProbeResult{Dialect: d, State: ProbeError, ErrorDetail: err.Error()}
			return res, err
		}
		return probeServer(pctx, d, "mysql", dsn, o.DSN)
	case Postgres, "postgresql", "pg":
		return probeServer(pctx, Postgres, "pgx", o.DSN, o.DSN)
	default:
		err := fmt.Errorf("unsupported database driver %q (want sqlite, mysql or postgres)", o.Driver)
		return ProbeResult{State: ProbeError, ErrorDetail: err.Error()}, err
	}
}

func probeSQLite(ctx context.Context, o Options) (ProbeResult, error) {
	path := SQLitePath(o.DSN, o.DataDir)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		ver := sqliteVersion(ctx)
		return ProbeResult{
			OK:      true,
			Dialect: SQLite,
			Version: ver,
			State:   ProbeEmpty,
			Summary: "SQLite database file will be created at " + path,
		}, nil
	}
	if err != nil {
		return ProbeResult{Dialect: SQLite, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	if info.IsDir() {
		err := fmt.Errorf("%s is a directory", path)
		return ProbeResult{Dialect: SQLite, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	if info.Size() == 0 {
		return ProbeResult{
			OK:      true,
			Dialect: SQLite,
			Version: sqliteVersion(ctx),
			State:   ProbeEmpty,
			Summary: "SQLite database file is empty",
		}, nil
	}
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(3000)"}}
	pool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		return ProbeResult{Dialect: SQLite, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	defer pool.Close()
	if err := pool.PingContext(ctx); err != nil {
		return ProbeResult{Dialect: SQLite, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	return inspectTables(ctx, pool, SQLite)
}

func sqliteVersion(ctx context.Context) string {
	pool, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return ""
	}
	defer pool.Close()
	var ver string
	_ = pool.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&ver)
	return ver
}

func probeServer(ctx context.Context, d Dialect, driverName, driverDSN, rawDSN string) (ProbeResult, error) {
	pool, err := sql.Open(driverName, driverDSN)
	if err != nil {
		return ProbeResult{Dialect: d, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	defer pool.Close()
	if err := pool.PingContext(ctx); err != nil {
		if dbName, ok := missingDatabaseName(d, rawDSN, err); ok {
			return ProbeResult{
				OK:          false,
				Dialect:     d,
				State:       ProbeMissingDB,
				Summary:     fmt.Sprintf("database %q does not exist", dbName),
				HintSQL:     createDatabaseSQL(d, dbName),
				ErrorDetail: err.Error(),
			}, err
		}
		return ProbeResult{Dialect: d, State: ProbeError, ErrorDetail: err.Error()}, err
	}
	return inspectTables(ctx, pool, d)
}

func missingDatabaseName(d Dialect, rawDSN string, err error) (string, bool) {
	switch d {
	case MySQL:
		var my *mysql.MySQLError
		if errors.As(err, &my) && my.Number == 1049 {
			if cfg, perr := mysql.ParseDSN(rawDSN); perr == nil && cfg.DBName != "" {
				return cfg.DBName, true
			}
			return "skinloom", true
		}
	case Postgres:
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "3D000" {
			if u, perr := url.Parse(rawDSN); perr == nil {
				if name := strings.TrimPrefix(u.Path, "/"); name != "" {
					return name, true
				}
			}
			return "skinloom", true
		}
	}
	return "", false
}

func createDatabaseSQL(d Dialect, dbName string) string {
	switch d {
	case MySQL:
		return fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;",
			strings.ReplaceAll(dbName, "`", "``"))
	case Postgres:
		return fmt.Sprintf(`CREATE DATABASE "%s" ENCODING 'UTF8';`,
			strings.ReplaceAll(dbName, `"`, `""`))
	default:
		return ""
	}
}

func inspectTables(ctx context.Context, pool *sql.DB, d Dialect) (ProbeResult, error) {
	res := ProbeResult{OK: true, Dialect: d, State: ProbeEmpty}
	verQuery := map[Dialect]string{SQLite: `SELECT sqlite_version()`, MySQL: `SELECT version()`, Postgres: `SHOW server_version`}[d]
	if err := pool.QueryRowContext(ctx, verQuery).Scan(&res.Version); err != nil {
		res.OK = false
		res.State = ProbeError
		res.ErrorDetail = err.Error()
		return res, err
	}

	tables, err := listTables(ctx, pool, d)
	if err != nil {
		res.OK = false
		res.State = ProbeError
		res.ErrorDetail = err.Error()
		return res, err
	}
	if len(tables) == 0 {
		res.Summary = "Database is empty"
		return res, nil
	}

	has := map[string]bool{}
	for _, t := range tables {
		has[strings.ToLower(t)] = true
	}

	if has["system_state"] {
		var done int
		q := `SELECT COUNT(*) FROM system_state WHERE "key" = 'setup.completed'`
		if d == MySQL {
			q = "SELECT COUNT(*) FROM system_state WHERE `key` = 'setup.completed'"
		}
		if err := pool.QueryRowContext(ctx, q).Scan(&done); err == nil && done > 0 {
			res.State = ProbeSkinLoomInstalled
			if has["users"] {
				_ = pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&res.Users)
			}
			if has["textures"] {
				_ = pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM textures`).Scan(&res.Textures)
			}
			if has["players"] {
				_ = pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM players`).Scan(&res.Players)
			}
			res.Summary = fmt.Sprintf("Detected existing SkinLoom data (%d users, %d textures, %d players)", res.Users, res.Textures, res.Players)
			return res, nil
		}
		// Unfinished SkinLoom schema (e.g. migrations ran without setup.completed).
		var users, textures int
		if has["users"] {
			_ = pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)
		}
		if has["textures"] {
			_ = pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM textures`).Scan(&textures)
		}
		if users == 0 && textures == 0 {
			res.Summary = "SkinLoom tables exist but setup has not been completed"
			return res, nil
		}
		res.Users, res.Textures = users, textures
		res.Summary = fmt.Sprintf("Database already contains %d users and %d textures without setup.completed", users, textures)
		return res, nil
	}

	if looksLikeBS(tables) {
		res.State = ProbeSuspectedBS
		res.Summary = "Suspected Blessing Skin database detected; please create a new empty database for SkinLoom and use the Blessing Skin migration wizard"
		return res, nil
	}

	// Only goose_db_version or empty schema left from an earlier aborted migration.
	if len(tables) == 1 && has["goose_db_version"] {
		res.Summary = "Database is ready for initialization"
		return res, nil
	}

	res.Summary = fmt.Sprintf("Database contains %d existing tables", len(tables))
	return res, nil
}

func looksLikeBS(tables []string) bool {
	var hasOptions, hasUsers, hasPlayers bool
	for _, t := range tables {
		low := strings.ToLower(t)
		if low == "options" || strings.HasSuffix(low, "_options") {
			hasOptions = true
		}
		if low == "users" || strings.HasSuffix(low, "_users") {
			hasUsers = true
		}
		if low == "players" || strings.HasSuffix(low, "_players") {
			hasPlayers = true
		}
	}
	return hasOptions && hasUsers && hasPlayers
}

func listTables(ctx context.Context, pool *sql.DB, d Dialect) ([]string, error) {
	var q string
	switch d {
	case SQLite:
		q = `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`
	case MySQL:
		q = `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'`
	case Postgres:
		q = `SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = current_schema()`
	}
	rows, err := pool.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// sqlitePragmas are the connection settings SkinLoom relies on. foreign_keys above
// all: without it every ON DELETE CASCADE / SET NULL in the schema silently does
// nothing.
var sqlitePragmas = []string{"busy_timeout(10000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"}

// SQLitePath is the database file a SQLite DSN names, found the way Open finds it: a
// plain relative path is in dataDir, a DSN with parameters is used as written.
func SQLitePath(dsn, dataDir string) string {
	if dsn == "" {
		dsn = "skinloom.db"
	}
	base, _, hasQuery := strings.Cut(dsn, "?")
	path := strings.TrimPrefix(base, "file:")
	if !hasQuery && !filepath.IsAbs(path) {
		path = filepath.Join(dataDir, path)
	}
	return filepath.Clean(path)
}

// sqliteDSN turns a plain path into a DSN with the pragmas SkinLoom relies on. A value
// that already has a query keeps its path and its own parameters, and gets every
// pragma it does not set itself; foreign_keys is always on (it used to be used as-is,
// so a DSN with any parameter turned foreign keys off).
func sqliteDSN(dsn, dataDir string) (string, error) {
	if dsn == "" {
		dsn = "skinloom.db"
	}
	if base, raw, ok := strings.Cut(dsn, "?"); ok {
		q, err := url.ParseQuery(raw)
		if err != nil {
			return "", fmt.Errorf("sqlite dsn: %w", err)
		}
		set := map[string]bool{}
		var keep []string
		for _, p := range q["_pragma"] {
			name, _, _ := strings.Cut(p, "(")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "foreign_keys" {
				continue
			}
			set[name] = true
			keep = append(keep, p)
		}
		for _, p := range sqlitePragmas {
			if name, _, _ := strings.Cut(p, "("); !set[name] {
				keep = append(keep, p)
			}
		}
		q["_pragma"] = keep
		if q.Get("_txlock") == "" {
			q.Set("_txlock", "immediate")
		}
		return base + "?" + q.Encode(), nil
	}
	path := strings.TrimPrefix(dsn, "file:")
	if !filepath.IsAbs(path) {
		path = filepath.Join(dataDir, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create sqlite directory: %w", err)
	}
	q := url.Values{}
	for _, p := range sqlitePragmas {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	return "file:" + filepath.ToSlash(path) + "?" + q.Encode(), nil
}

// mysqlDSN forces the connection options the schema assumes.
func mysqlDSN(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	// Send times truncated to the microseconds datetime(6) keeps. MySQL otherwise
	// rounds the nanoseconds it gets (MariaDB truncates), so a stored deadline could
	// end up later than the one computed and a token outlive its expiry by a fraction
	// of a microsecond. PostgreSQL's driver truncates too: now every database stores
	// the same instant.
	if err := cfg.Apply(mysql.TimeTruncate(time.Microsecond)); err != nil {
		return "", err
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	return cfg.FormatDSN(), nil
}
