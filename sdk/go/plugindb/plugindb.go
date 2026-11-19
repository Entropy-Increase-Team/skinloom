// Package plugindb provides three-dialect isolated SQL database access and
// goose-powered schema migrations for Tier 2 SkinLoom native Go plugins.
//
// Every native plugin with the "storage.sql" permission receives a DatabaseConfig
// containing its dialect ("sqlite", "mysql", or "postgres"), DSN, mandatory table
// prefix ("plg_<safe_id>_"), and optional PostgreSQL schema. All queries and
// migrations executed through *DB automatically scope table names and migration
// metadata to that prefix so plugin data never collides with core SkinLoom tables
// and can be cleanly purged on uninstall.
package plugindb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
	"testing/fstest"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

var (
	safePrefixRe = regexp.MustCompile(`^plg_[a-z0-9_]{3,40}_$`)
	safeSchemaRe = regexp.MustCompile(`^plg_[a-z0-9_]{3,40}$`)
	safeTableRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	pluginIDRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

	memDBCounter atomic.Uint64
)

// Config holds the isolated database connection details handed by the SkinLoom host.
type Config struct {
	Driver string // "sqlite" | "mysql" | "postgres"
	DSN    string
	Prefix string // "plg_<safe_id>_"
	Schema string // optional PostgreSQL schema ("plg_<safe_id>")
}

// DB wraps a *sql.DB connection pool scoped to one native plugin's isolated prefix.
type DB struct {
	sqlDB   *sql.DB
	dialect string
	prefix  string
	schema  string
}

// SafePrefix derives the canonical "plg_<safe_id>_" SQL table prefix for pluginID.
func SafePrefix(pluginID string) (string, error) {
	if !pluginIDRe.MatchString(pluginID) {
		return "", fmt.Errorf("plugindb: invalid plugin id %q", pluginID)
	}
	prefix := "plg_" + strings.ReplaceAll(pluginID, "-", "_") + "_"
	if !safePrefixRe.MatchString(prefix) {
		return "", fmt.Errorf("plugindb: derived unsafe prefix %q", prefix)
	}
	return prefix, nil
}

// Open connects to the host-allocated plugin database and validates the safety
// of its dialect, prefix, and optional PostgreSQL schema.
func Open(cfg Config) (*DB, error) {
	dialect, sqlDriver, err := normalizeDriver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("plugindb: DSN is required")
	}
	if !safePrefixRe.MatchString(cfg.Prefix) {
		return nil, fmt.Errorf("plugindb: unsafe table prefix %q (must match plg_<safe_id>_)", cfg.Prefix)
	}
	if cfg.Schema != "" && !safeSchemaRe.MatchString(cfg.Schema) {
		return nil, fmt.Errorf("plugindb: unsafe schema name %q", cfg.Schema)
	}

	pool, err := sql.Open(sqlDriver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("plugindb: open %s: %w", dialect, err)
	}

	switch dialect {
	case "sqlite":
		// SQLite allows one writer at a time; capping open connections to 1
		// prevents SQLITE_BUSY errors inside the plugin process.
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		pool.SetConnMaxLifetime(0)
	default:
		pool.SetMaxOpenConns(10)
		pool.SetMaxIdleConns(5)
		pool.SetConnMaxLifetime(30 * time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("plugindb: ping %s: %w", dialect, err)
	}
	if dialect == "sqlite" {
		_, _ = pool.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`)
	}

	return &DB{
		sqlDB:   pool,
		dialect: dialect,
		prefix:  cfg.Prefix,
		schema:  cfg.Schema,
	}, nil
}

// OpenMemory opens a unique shared in-memory SQLite database scoped to pluginID,
// intended for unit tests and sdk/go/plugintest.
func OpenMemory(pluginID string) (*DB, Config, error) {
	prefix, err := SafePrefix(pluginID)
	if err != nil {
		return nil, Config{}, err
	}
	n := memDBCounter.Add(1)
	q := url.Values{}
	q.Set("mode", "memory")
	q.Set("cache", "shared")
	q.Add("_pragma", "foreign_keys(1)")
	dsn := fmt.Sprintf("file:skinloom_plg_mem_%d?%s", n, q.Encode())
	cfg := Config{
		Driver: "sqlite",
		DSN:    dsn,
		Prefix: prefix,
	}
	db, err := Open(cfg)
	if err != nil {
		return nil, Config{}, err
	}
	return db, cfg, nil
}

func normalizeDriver(raw string) (dialect string, sqlDriver string, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sqlite", "sqlite3":
		return "sqlite", "sqlite", nil
	case "mysql", "mariadb":
		return "mysql", "mysql", nil
	case "postgres", "postgresql", "pg":
		return "postgres", "pgx", nil
	default:
		return "", "", fmt.Errorf("plugindb: unsupported driver %q", raw)
	}
}

// SQL returns the underlying *sql.DB connection pool.
func (d *DB) SQL() *sql.DB { return d.sqlDB }

// Dialect returns the normalized SQL dialect ("sqlite", "mysql", or "postgres").
func (d *DB) Dialect() string { return d.dialect }

// Prefix returns the mandatory table prefix ("plg_<safe_id>_").
func (d *DB) Prefix() string { return d.prefix }

// Schema returns the optional PostgreSQL schema name, or "" when unused.
func (d *DB) Schema() string { return d.schema }

// Close closes the underlying *sql.DB connection pool.
func (d *DB) Close() error {
	if d == nil || d.sqlDB == nil {
		return nil
	}
	return d.sqlDB.Close()
}

// SafeTable validates name as a lowercase SQL identifier and returns "<prefix><name>".
func (d *DB) SafeTable(name string) (string, error) {
	if !safeTableRe.MatchString(name) {
		return "", fmt.Errorf("plugindb: invalid table name %q (must match ^[a-z][a-z0-9_]{0,62}$)", name)
	}
	return d.prefix + name, nil
}

// Table validates name and returns "<prefix><name>", panicking on invalid static names.
func (d *DB) Table(name string) string {
	tbl, err := d.SafeTable(name)
	if err != nil {
		panic(err)
	}
	return tbl
}

// ExpandPrefix replaces "{{prefix}}" with the plugin's table prefix (and "{{schema}}"
// with its PostgreSQL schema when set).
func (d *DB) ExpandPrefix(query string) string {
	out := strings.ReplaceAll(query, "{{prefix}}", d.prefix)
	if d.schema != "" {
		out = strings.ReplaceAll(out, "{{schema}}", d.schema)
	}
	return out
}

// Rebind rewrites positional "?" bind placeholders into "$1, $2, ..." on PostgreSQL
// while skipping single-quoted literals, double-quoted identifiers, SQL comments
// (-- and /* ... */), PostgreSQL dollar-quoted strings ($tag$...$tag$), and
// JSONB operators (??, ?|, ?&). On SQLite and MySQL it returns query unchanged.
func (d *DB) Rebind(query string) string {
	if d.dialect != "postgres" || !strings.Contains(query, "?") {
		return query
	}
	var sb strings.Builder
	sb.Grow(len(query) + 16)
	paramIdx := 1
	n := len(query)
	for i := 0; i < n; {
		ch := query[i]
		// Single-quoted string literal (handles '' escapes).
		if ch == '\'' {
			sb.WriteByte(ch)
			i++
			for i < n {
				sb.WriteByte(query[i])
				if query[i] == '\'' {
					i++
					if i < n && query[i] == '\'' {
						sb.WriteByte('\'')
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		// Double-quoted identifier (handles "" escapes).
		if ch == '"' {
			sb.WriteByte(ch)
			i++
			for i < n {
				sb.WriteByte(query[i])
				if query[i] == '"' {
					i++
					if i < n && query[i] == '"' {
						sb.WriteByte('"')
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		// Line comment -- ... \n
		if ch == '-' && i+1 < n && query[i+1] == '-' {
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				sb.WriteString(query[i:])
				break
			}
			sb.WriteString(query[i : i+end+1])
			i += end + 1
			continue
		}
		// Block comment /* ... */
		if ch == '/' && i+1 < n && query[i+1] == '*' {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				sb.WriteString(query[i:])
				break
			}
			sb.WriteString(query[i : i+2+end+2])
			i += 2 + end + 2
			continue
		}
		// Dollar-quoted string $tag$...$tag$
		if ch == '$' {
			if tag, ok := matchDollarTag(query[i:]); ok {
				sb.WriteString(tag)
				i += len(tag)
				end := strings.Index(query[i:], tag)
				if end < 0 {
					sb.WriteString(query[i:])
					break
				}
				sb.WriteString(query[i : i+end+len(tag)])
				i += end + len(tag)
				continue
			}
		}
		if ch == '?' {
			// Escape ?? -> literal ? (for PostgreSQL JSONB ? operator) and preserve ?| / ?&.
			if i+1 < n {
				next := query[i+1]
				if next == '?' {
					sb.WriteByte('?')
					i += 2
					continue
				}
				if next == '|' || next == '&' {
					sb.WriteByte('?')
					sb.WriteByte(next)
					i += 2
					continue
				}
			}
			fmt.Fprintf(&sb, "$%d", paramIdx)
			paramIdx++
			i++
			continue
		}
		sb.WriteByte(ch)
		i++
	}
	return sb.String()
}

func matchDollarTag(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	if s[1] == '$' {
		return "$$", true
	}
	if !((s[1] >= 'a' && s[1] <= 'z') || (s[1] >= 'A' && s[1] <= 'Z') || s[1] == '_') {
		return "", false
	}
	for j := 2; j < len(s); j++ {
		c := s[j]
		if c == '$' {
			return s[:j+1], true
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", false
		}
	}
	return "", false
}

// PrepareQuery applies ExpandPrefix followed by Rebind.
func (d *DB) PrepareQuery(query string) string {
	return d.Rebind(d.ExpandPrefix(query))
}

// Exec expands "{{prefix}}", rebinds placeholders for the active dialect, and executes query.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sqlDB.ExecContext(ctx, d.PrepareQuery(query), args...)
}

// Query expands "{{prefix}}", rebinds placeholders for the active dialect, and queries rows.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sqlDB.QueryContext(ctx, d.PrepareQuery(query), args...)
}

// QueryRow expands "{{prefix}}", rebinds placeholders for the active dialect, and returns a single row.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sqlDB.QueryRowContext(ctx, d.PrepareQuery(query), args...)
}

// BeginTx starts a transaction wrapped in *Tx with the same prefix/rebind helpers.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.sqlDB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, db: d}, nil
}

// Tx wraps *sql.Tx with ExpandPrefix and Rebind helpers.
type Tx struct {
	tx *sql.Tx
	db *DB
}

func (t *Tx) SQL() *sql.Tx { return t.tx }

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.db.PrepareQuery(query), args...)
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.db.PrepareQuery(query), args...)
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.db.PrepareQuery(query), args...)
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// Migrate runs goose SQL migrations from fsys under dir using a plugin-isolated
// version table ("<prefix>goose_db_version").
//
// Resolution order:
//  1. If "<dir>/<dialect>" exists in fsys (e.g. "migrations/sqlite"), files from
//     that dialect-specific directory are used.
//  2. Otherwise files directly under dir are used.
//
// Every ".sql" file has "{{prefix}}" (and "{{schema}}") automatically expanded to
// the plugin's isolated table prefix before goose executes it.
func (d *DB) Migrate(ctx context.Context, fsys fs.FS, dir string) error {
	if fsys == nil {
		return errors.New("plugindb: migration fs is nil")
	}
	cleanDir := strings.Trim(path.Clean(strings.ReplaceAll(dir, "\\", "/")), "/")
	if cleanDir == "." {
		cleanDir = ""
	}

	targetDir := cleanDir
	dialectDir := d.dialect
	if cleanDir != "" {
		dialectDir = cleanDir + "/" + d.dialect
	}
	if info, err := fs.Stat(fsys, dialectDir); err == nil && info.IsDir() {
		targetDir = dialectDir
	}

	subFS := fsys
	if targetDir != "" {
		var err error
		subFS, err = fs.Sub(fsys, targetDir)
		if err != nil {
			return fmt.Errorf("plugindb: open migrations dir %q: %w", targetDir, err)
		}
	}

	rendered, err := d.renderMigrationFS(subFS)
	if err != nil {
		return err
	}

	var gd goose.Dialect
	switch d.dialect {
	case "sqlite":
		gd = goose.DialectSQLite3
	case "mysql":
		gd = goose.DialectMySQL
	case "postgres":
		gd = goose.DialectPostgres
	default:
		return fmt.Errorf("plugindb: unsupported goose dialect %q", d.dialect)
	}

	provider, err := goose.NewProvider(
		gd,
		d.sqlDB,
		rendered,
		goose.WithTableName(d.prefix+"goose_db_version"),
	)
	if err != nil {
		return fmt.Errorf("plugindb: create goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("plugindb: apply migrations: %w", err)
	}
	return nil
}

func (d *DB) renderMigrationFS(src fs.FS) (fs.FS, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, fmt.Errorf("plugindb: read migrations dir: %w", err)
	}
	mem := fstest.MapFS{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
			continue
		}
		raw, err := fs.ReadFile(src, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("plugindb: read migration %q: %w", entry.Name(), err)
		}
		expanded := d.ExpandPrefix(string(raw))
		mem[entry.Name()] = &fstest.MapFile{
			Data: []byte(expanded),
			Mode: 0o444,
		}
	}
	return mem, nil
}
