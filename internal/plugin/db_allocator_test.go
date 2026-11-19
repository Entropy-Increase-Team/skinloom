package plugin

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func TestSafeSQLPrefixAndSchema(t *testing.T) {
	pref, err := SafeSQLPrefix("bs-importer")
	if err != nil || pref != "plg_bs_importer_" {
		t.Fatalf("SafeSQLPrefix = (%q, %v), want plg_bs_importer_", pref, err)
	}
	sch, err := SafeSQLSchema("bs-importer")
	if err != nil || sch != "plg_bs_importer" {
		t.Fatalf("SafeSQLSchema = (%q, %v), want plg_bs_importer", sch, err)
	}
	for _, bad := range []string{"", "ab", "Bad-Upper", "a;drop table users;--", "../escape", "foo_bar"} {
		if _, err := SafeSQLPrefix(bad); err == nil {
			t.Errorf("expected SafeSQLPrefix(%q) to reject invalid plugin ID", bad)
		}
	}
}

func TestDBAllocatorSQLiteIsolationAndPurge(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	alloc := NewDBAllocator(DBAllocatorOptions{
		MainDriver: "sqlite",
		MainDSN:    "skinloom.db",
		DataDir:    dataDir,
	})

	cfg, err := alloc.Allocate(ctx, "bs-importer")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if cfg.Driver != "sqlite" || cfg.Prefix != "plg_bs_importer_" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}

	dbPath := filepath.Join(dataDir, "plugins-data", "bs-importer", "plugin.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected isolated plugin.db at %s: %v", dbPath, err)
	}

	// Open with returned DSN, verify WAL mode and prefixed table CRUD.
	pool, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	var journal string
	if err := pool.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	if _, err := pool.ExecContext(ctx, `CREATE TABLE `+cfg.Prefix+`tasks (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.ExecContext(ctx, `INSERT INTO `+cfg.Prefix+`tasks (id, status) VALUES (1, 'ok')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_ = pool.Close()

	// Purge removes plugin.db and WAL/SHM sidecars.
	if err := alloc.Purge(ctx, "bs-importer"); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected plugin.db removed after Purge, stat err = %v", err)
	}
}

func TestDBAllocatorDialectPurgePreservesCoreTables(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		dataDir := t.TempDir()

		var mainDSN string
		switch d.Dialect {
		case db.SQLite:
			mainDSN = "skinloom.db"
		case db.MySQL:
			mainDSN = os.Getenv("SKINLOOM_TEST_MYSQL")
		case db.Postgres:
			mainDSN = os.Getenv("SKINLOOM_TEST_POSTGRES")
		}

		alloc := NewDBAllocator(DBAllocatorOptions{
			MainDriver: string(d.Dialect),
			MainDSN:    mainDSN,
			MainSQL:    d.SQL,
			DataDir:    dataDir,
		})

		cfg, err := alloc.Allocate(ctx, "demo-alloc")
		if err != nil {
			t.Fatalf("Allocate(%s): %v", d.Dialect, err)
		}
		if cfg.Prefix != "plg_demo_alloc_" {
			t.Fatalf("prefix = %q", cfg.Prefix)
		}

		// For MySQL/Postgres, create plugin tables + a non-matching sibling table in main DB.
		if d.Dialect != db.SQLite {
			_, err := d.SQL.ExecContext(ctx, `CREATE TABLE `+cfg.Prefix+`jobs (id INT PRIMARY KEY)`)
			if err != nil {
				t.Fatalf("create prefixed table: %v", err)
			}
			// Underscore-wildcard trap: "plgXdemoXallocXtrap" would match unescaped LIKE 'plg_demo_alloc_%'!
			_, err = d.SQL.ExecContext(ctx, `CREATE TABLE plgxdemoxallocxtrap (id INT PRIMARY KEY)`)
			if err != nil {
				t.Fatalf("create trap table: %v", err)
			}
			defer func() {
				_, _ = d.SQL.ExecContext(ctx, `DROP TABLE IF EXISTS plgxdemoxallocxtrap`)
			}()

			if err := alloc.Purge(ctx, "demo-alloc"); err != nil {
				t.Fatalf("Purge(%s): %v", d.Dialect, err)
			}

			// Prefixed table must be gone.
			if _, err := d.SQL.ExecContext(ctx, `SELECT 1 FROM `+cfg.Prefix+`jobs`); err == nil {
				t.Fatalf("expected %sjobs to be dropped after Purge", cfg.Prefix)
			}
			// Trap table & core `plugins` table must remain intact!
			if _, err := d.SQL.ExecContext(ctx, `SELECT COUNT(*) FROM plgxdemoxallocxtrap`); err != nil {
				t.Fatalf("wildcard trap table was accidentally dropped: %v", err)
			}
			if _, err := d.SQL.ExecContext(ctx, `SELECT COUNT(*) FROM plugins`); err != nil {
				t.Fatalf("core plugins table was damaged: %v", err)
			}
		} else {
			if err := alloc.Purge(ctx, "demo-alloc"); err != nil {
				t.Fatalf("Purge(sqlite): %v", err)
			}
		}
	})
}

func TestDBAllocatorMySQLAndPostgresConfigFormatting(t *testing.T) {
	ctx := context.Background()
	myAlloc := NewDBAllocator(DBAllocatorOptions{
		MainDriver: "mysql",
		MainDSN:    "user:pass@tcp(127.0.0.1:3306)/skinloom",
		Override: config.PluginDatabase{
			DSN: "plguser:secret@tcp(127.0.0.1:3306)/skinloom_plugins",
		},
	})
	myCfg, err := myAlloc.Allocate(ctx, "my-plugin")
	if err != nil {
		t.Fatalf("Allocate mysql: %v", err)
	}
	if myCfg.Driver != "mysql" || myCfg.Prefix != "plg_my_plugin_" || !strings.Contains(myCfg.DSN, "skinloom_plugins") || !strings.Contains(myCfg.DSN, "parseTime=true") {
		t.Fatalf("unexpected mysql cfg: %+v", myCfg)
	}

	pgAlloc := NewDBAllocator(DBAllocatorOptions{
		MainDriver: "postgres",
		MainDSN:    "postgres://u:p@127.0.0.1:5432/skinloom?sslmode=disable",
	})
	pgCfg, err := pgAlloc.Allocate(ctx, "pg-plugin")
	if err != nil {
		t.Fatalf("Allocate postgres: %v", err)
	}
	if pgCfg.Driver != "postgres" || pgCfg.Prefix != "plg_pg_plugin_" {
		t.Fatalf("unexpected postgres cfg: %+v", pgCfg)
	}

	dsnWithSP, err := appendPostgresSearchPath("postgres://u:p@127.0.0.1:5432/skinloom?sslmode=disable", "plg_pg_plugin")
	if err != nil || !strings.Contains(dsnWithSP, "search_path=plg_pg_plugin") {
		t.Fatalf("appendPostgresSearchPath = (%q, %v)", dsnWithSP, err)
	}
}
