// Package migrate applies the embedded versioned migrations with goose.
package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/migrations"
)

func provider(d *db.DB) (*goose.Provider, error) {
	fsys, err := migrations.FS(string(d.Dialect))
	if err != nil {
		return nil, err
	}
	var (
		dialect goose.Dialect
		opts    []goose.ProviderOption
	)
	switch d.Dialect {
	case db.SQLite:
		dialect = goose.DialectSQLite3 // single node: no lock needed
	case db.MySQL:
		dialect = goose.DialectMySQL
		l, err := lock.NewMySQLTableLocker()
		if err != nil {
			return nil, err
		}
		opts = append(opts, goose.WithLocker(l))
	case db.Postgres:
		dialect = goose.DialectPostgres
		l, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return nil, err
		}
		opts = append(opts, goose.WithSessionLocker(l))
	}
	return goose.NewProvider(dialect, d.SQL, fsys, opts...)
}

// Up applies pending migrations. For SQLite it first snapshots the database file
// into <dataDir>/backups so a failed upgrade can be rolled back by copying it back.
func Up(ctx context.Context, d *db.DB, dataDir string, log *slog.Logger) error {
	p, err := provider(d)
	if err != nil {
		return err
	}
	pending, err := p.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check pending migrations: %w", err)
	}
	if !pending {
		return nil
	}
	if d.Dialect == db.SQLite {
		if err := snapshotSQLite(ctx, d, dataDir, log); err != nil {
			return err
		}
	}
	results, err := p.Up(ctx)
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "file", filepath.Base(r.Source.Path), "took", r.Duration.Round(time.Millisecond))
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Status lists every migration and whether it has been applied.
func Status(ctx context.Context, d *db.DB) ([]*goose.MigrationStatus, error) {
	p, err := provider(d)
	if err != nil {
		return nil, err
	}
	return p.Status(ctx)
}

func snapshotSQLite(ctx context.Context, d *db.DB, dataDir string, log *slog.Logger) error {
	var tables int
	// goose creates its own version table while checking for pending migrations, so only
	// application tables count as "there is data worth protecting".
	const q = `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version'`
	if err := d.SQL.QueryRowContext(ctx, q).Scan(&tables); err != nil || tables == 0 {
		return nil // fresh database, nothing to protect
	}
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	target := filepath.Join(dir, "pre-migrate-"+time.Now().UTC().Format("20060102-150405")+".db")
	if _, err := d.SQL.ExecContext(ctx, `VACUUM INTO ?`, filepath.ToSlash(target)); err != nil {
		return fmt.Errorf("snapshot sqlite before migrating: %w", err)
	}
	log.Info("sqlite snapshot written before migrating", "path", target)
	return nil
}
