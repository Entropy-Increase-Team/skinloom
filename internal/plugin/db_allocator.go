package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
)

var (
	safePrefixRe = regexp.MustCompile(`^plg_[a-z0-9_]{3,40}_$`)
	safeSchemaRe = regexp.MustCompile(`^plg_[a-z0-9_]{3,40}$`)
)

// PluginDBConfig holds the isolated relational database connection details and
// mandatory table prefix handed to a Tier 2 native plugin during Init.
type PluginDBConfig struct {
	Driver string `json:"driver"` // "sqlite" | "mysql" | "postgres"
	DSN    string `json:"dsn"`
	Prefix string `json:"prefix"` // "plg_<safe_id>_"
	Schema string `json:"schema,omitempty"`
}

// SafeSQLPrefix derives the unforgeable SQL table prefix "plg_<safe_id>_" from
// a validated kebab-case plugin identifier.
func SafeSQLPrefix(pluginID string) (string, error) {
	if !pluginIDRe.MatchString(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q for SQL prefix", pluginID)
	}
	prefix := "plg_" + strings.ReplaceAll(pluginID, "-", "_") + "_"
	if !safePrefixRe.MatchString(prefix) {
		return "", fmt.Errorf("derived unsafe SQL prefix %q", prefix)
	}
	return prefix, nil
}

// SafeSQLSchema derives the optional PostgreSQL schema name "plg_<safe_id>".
func SafeSQLSchema(pluginID string) (string, error) {
	if !pluginIDRe.MatchString(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q for SQL schema", pluginID)
	}
	schema := "plg_" + strings.ReplaceAll(pluginID, "-", "_")
	if !safeSchemaRe.MatchString(schema) {
		return "", fmt.Errorf("derived unsafe SQL schema %q", schema)
	}
	return schema, nil
}

// DBAllocatorOptions configures the three-dialect native plugin database allocator.
type DBAllocatorOptions struct {
	MainDriver string
	MainDSN    string
	MainSQL    *sql.DB
	Override   config.PluginDatabase
	DataDir    string
}

// DBAllocator provisions isolated SQL storage spaces for native plugins across
// SQLite (dedicated <DataDir>/plugins-data/<id>/plugin.db in WAL mode),
// MySQL/MariaDB (shared or dedicated DSN + plg_<safe_id>_ prefix), and
// PostgreSQL (shared or dedicated DSN + plg_<safe_id>_ prefix or optional schema).
type DBAllocator struct {
	mainDriver string
	mainDSN    string
	mainSQL    *sql.DB
	override   config.PluginDatabase
	dataDir    string
}

// NewDBAllocator creates a PluginDBAllocator.
func NewDBAllocator(opts DBAllocatorOptions) *DBAllocator {
	return &DBAllocator{
		mainDriver: opts.MainDriver,
		mainDSN:    opts.MainDSN,
		mainSQL:    opts.MainSQL,
		override:   opts.Override,
		dataDir:    opts.DataDir,
	}
}

func (a *DBAllocator) effectiveDialect() (db.Dialect, error) {
	raw := strings.TrimSpace(a.override.Driver)
	if raw == "" {
		raw = strings.TrimSpace(a.mainDriver)
	}
	if raw == "" {
		raw = "sqlite"
	}
	switch strings.ToLower(raw) {
	case "sqlite", "sqlite3":
		return db.SQLite, nil
	case "mysql", "mariadb":
		return db.MySQL, nil
	case "postgres", "postgresql", "pg":
		return db.Postgres, nil
	default:
		return "", fmt.Errorf("unsupported plugin database driver %q", raw)
	}
}

func (a *DBAllocator) effectiveServerDSN() string {
	if strings.TrimSpace(a.override.DSN) != "" {
		return strings.TrimSpace(a.override.DSN)
	}
	return strings.TrimSpace(a.mainDSN)
}

// SQLiteFilePath returns the isolated SQLite database file path for pluginID.
func (a *DBAllocator) SQLiteFilePath(pluginID string) (string, error) {
	if !pluginIDRe.MatchString(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q", pluginID)
	}
	if a.dataDir == "" {
		return "", errors.New("dataDir is required for isolated plugin SQLite storage")
	}
	return filepath.Join(a.dataDir, "plugins-data", pluginID, "plugin.db"), nil
}

// Allocate prepares and returns the isolated database configuration for pluginID.
func (a *DBAllocator) Allocate(ctx context.Context, pluginID string) (*PluginDBConfig, error) {
	if a == nil {
		return nil, errors.New("plugin database allocator not configured")
	}
	prefix, err := SafeSQLPrefix(pluginID)
	if err != nil {
		return nil, err
	}
	dialect, err := a.effectiveDialect()
	if err != nil {
		return nil, err
	}

	switch dialect {
	case db.SQLite:
		return a.allocateSQLite(ctx, pluginID, prefix)
	case db.MySQL:
		return a.allocateMySQL(prefix)
	case db.Postgres:
		return a.allocatePostgres(ctx, pluginID, prefix)
	default:
		return nil, fmt.Errorf("unsupported dialect %q", dialect)
	}
}

func (a *DBAllocator) allocateSQLite(ctx context.Context, pluginID, prefix string) (*PluginDBConfig, error) {
	dbPath, err := a.SQLiteFilePath(pluginID)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(dbPath)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, fmt.Errorf("create plugin db dir: %w", err)
	}
	_ = fsperm.Private(parent)

	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?" + q.Encode()

	// Initialize the SQLite database file and verify WAL mode immediately so backups
	// and subprocess startup see a clean, ready database.
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open plugin sqlite: %w", err)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping plugin sqlite: %w", err)
	}
	var mode string
	if err := pool.QueryRowContext(pingCtx, `PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		return nil, fmt.Errorf("enable WAL on plugin sqlite: %w", err)
	}
	if _, err := pool.ExecContext(pingCtx, `PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, fmt.Errorf("configure pragmas on plugin sqlite: %w", err)
	}
	return &PluginDBConfig{
		Driver: string(db.SQLite),
		DSN:    dsn,
		Prefix: prefix,
	}, nil
}

func (a *DBAllocator) allocateMySQL(prefix string) (*PluginDBConfig, error) {
	rawDSN := a.effectiveServerDSN()
	if rawDSN == "" {
		return nil, errors.New("mysql DSN is required for native plugin SQL allocation")
	}
	cfg, err := mysql.ParseDSN(rawDSN)
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	return &PluginDBConfig{
		Driver: string(db.MySQL),
		DSN:    cfg.FormatDSN(),
		Prefix: prefix,
	}, nil
}

func (a *DBAllocator) allocatePostgres(ctx context.Context, pluginID, prefix string) (*PluginDBConfig, error) {
	rawDSN := a.effectiveServerDSN()
	if rawDSN == "" {
		return nil, errors.New("postgres DSN is required for native plugin SQL allocation")
	}
	out := &PluginDBConfig{
		Driver: string(db.Postgres),
		DSN:    rawDSN,
		Prefix: prefix,
	}
	if a.override.PostgresSchema {
		schemaName, err := SafeSQLSchema(pluginID)
		if err != nil {
			return nil, err
		}
		pool, closePool, err := a.openServerPool(db.Postgres)
		if err != nil {
			return nil, err
		}
		defer closePool()
		stmt := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)
		if _, err := pool.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("create postgres plugin schema %q: %w", schemaName, err)
		}
		dsnWithSearch, err := appendPostgresSearchPath(rawDSN, schemaName)
		if err != nil {
			return nil, err
		}
		out.DSN = dsnWithSearch
		out.Schema = schemaName
	}
	return out, nil
}

var pgSearchPathKVRe = regexp.MustCompile(`(?i)\bsearch_path=('(?:\\.|[^'])*'|\S+)`)

func appendPostgresSearchPath(rawDSN, schemaName string) (string, error) {
	u, err := url.Parse(rawDSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		// Key=value DSN form: replace any existing search_path= parameter instead of keeping it.
		if pgSearchPathKVRe.MatchString(rawDSN) {
			return pgSearchPathKVRe.ReplaceAllString(rawDSN, "search_path="+schemaName), nil
		}
		return strings.TrimSpace(rawDSN) + " search_path=" + schemaName, nil
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Purge cleanly drops all isolated SQL objects belonging strictly to pluginID
// (SQLite plugin.db files, or MySQL/PostgreSQL tables matching plg_<safe_id>_*
// and optional PostgreSQL schema plg_<safe_id>), never touching core tables.
func (a *DBAllocator) Purge(ctx context.Context, pluginID string) error {
	if a == nil {
		return nil
	}
	prefix, err := SafeSQLPrefix(pluginID)
	if err != nil {
		return err
	}
	dialect, err := a.effectiveDialect()
	if err != nil {
		return err
	}

	switch dialect {
	case db.SQLite:
		return a.purgeSQLite(pluginID)
	case db.MySQL:
		return a.purgeMySQL(ctx, prefix)
	case db.Postgres:
		return a.purgePostgres(ctx, pluginID, prefix)
	default:
		return fmt.Errorf("unsupported dialect %q", dialect)
	}
}

func (a *DBAllocator) purgeSQLite(pluginID string) error {
	if a.dataDir == "" {
		return nil
	}
	dbPath, err := a.SQLiteFilePath(pluginID)
	if err != nil {
		return err
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (a *DBAllocator) openServerPool(dialect db.Dialect) (*sql.DB, func(), error) {
	if strings.TrimSpace(a.override.DSN) == "" && a.mainSQL != nil {
		return a.mainSQL, func() {}, nil
	}
	dsn := a.effectiveServerDSN()
	if dsn == "" {
		return nil, func() {}, fmt.Errorf("missing %s DSN for plugin database purge", dialect)
	}
	driverName := "mysql"
	if dialect == db.Postgres {
		driverName = "pgx"
	} else {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, func() {}, err
		}
		cfg.ParseTime = true
		dsn = cfg.FormatDSN()
	}
	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, func() {}, err
	}
	return pool, func() { _ = pool.Close() }, nil
}

func escapeSQLLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func escapeSQLLikePipe(s string) string {
	r := strings.NewReplacer(`|`, `||`, `%`, `|%`, `_`, `|_`)
	return r.Replace(s)
}

func (a *DBAllocator) purgeMySQL(ctx context.Context, prefix string) error {
	pool, closePool, err := a.openServerPool(db.MySQL)
	if err != nil {
		return err
	}
	defer closePool()

	pattern := escapeSQLLikePipe(prefix) + "%"
	rows, err := pool.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE' AND table_name LIKE ? ESCAPE '|'`,
		pattern,
	)
	if err != nil {
		return fmt.Errorf("list mysql plugin tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.HasPrefix(name, prefix) {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	if len(tables) == 0 {
		return nil
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SET FOREIGN_KEY_CHECKS = 1`)
	}()

	for _, tbl := range tables {
		quoted := "`" + strings.ReplaceAll(tbl, "`", "``") + "`"
		if _, err := conn.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoted); err != nil {
			return fmt.Errorf("drop mysql plugin table %q: %w", tbl, err)
		}
	}
	return nil
}

func (a *DBAllocator) purgePostgres(ctx context.Context, pluginID, prefix string) error {
	pool, closePool, err := a.openServerPool(db.Postgres)
	if err != nil {
		return err
	}
	defer closePool()

	if a.override.PostgresSchema {
		schemaName, err := SafeSQLSchema(pluginID)
		if err != nil {
			return err
		}
		if _, err := pool.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)); err != nil {
			return fmt.Errorf("drop postgres plugin schema %q: %w", schemaName, err)
		}
	}

	pattern := escapeSQLLike(prefix) + "%"
	rows, err := pool.QueryContext(ctx,
		`SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = current_schema() AND tablename LIKE $1 ESCAPE '\'`,
		pattern,
	)
	if err != nil {
		return fmt.Errorf("list postgres plugin tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.HasPrefix(name, prefix) {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, tbl := range tables {
		quoted := `"` + strings.ReplaceAll(tbl, `"`, `""`) + `"`
		if _, err := pool.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoted+` CASCADE`); err != nil {
			return fmt.Errorf("drop postgres plugin table %q: %w", tbl, err)
		}
	}
	return nil
}
