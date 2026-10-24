// Package dbtest opens migrated databases for tests: SQLite always, plus MySQL and
// PostgreSQL when SKINLOOM_TEST_MYSQL / SKINLOOM_TEST_POSTGRES name a test database
// (CI's integration job sets them). Server databases are shared between tests, so
// they are emptied first.
package dbtest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

// Tables in an order that deletes children before parents.
var tables = []string{
	"closet_items", "textures", "blobs", "score_ledger",
	"jobs", "sessions", "auth_tokens", "audit_logs", "users",
}

// ForEach runs fn once per configured database, each in its own subtest.
func ForEach(t *testing.T, fn func(t *testing.T, d *db.DB)) {
	t.Helper()
	targets := []struct{ driver, dsn string }{{"sqlite", "t.db"}}
	if dsn := os.Getenv("SKINLOOM_TEST_MYSQL"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"mysql", dsn})
	}
	if dsn := os.Getenv("SKINLOOM_TEST_POSTGRES"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"postgres", dsn})
	}
	for _, tg := range targets {
		t.Run(tg.driver, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			d, err := db.Open(ctx, db.Options{Driver: tg.driver, DSN: tg.dsn, DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.Close() })
			if err := migrate.Up(ctx, d, dir, log); err != nil {
				t.Fatal(err)
			}
			if tg.driver != "sqlite" {
				for _, table := range tables {
					if _, err := d.SQL.Exec("DELETE FROM " + table); err != nil {
						t.Fatal(err)
					}
				}
			}
			fn(t, d)
		})
	}
}
