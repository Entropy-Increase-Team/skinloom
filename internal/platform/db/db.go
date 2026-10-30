// Package db opens the database connection shared by ent and raw SQL users.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
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

// sqliteDSN turns a plain path into a DSN with the pragmas SkinLoom relies on.
// A value that already contains "?" is used as-is.
func sqliteDSN(dsn, dataDir string) (string, error) {
	if dsn == "" {
		dsn = "skinloom.db"
	}
	if strings.Contains(dsn, "?") {
		return dsn, nil
	}
	path := strings.TrimPrefix(dsn, "file:")
	if !filepath.IsAbs(path) {
		path = filepath.Join(dataDir, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create sqlite directory: %w", err)
	}
	q := url.Values{}
	for _, p := range []string{"busy_timeout(10000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"} {
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
