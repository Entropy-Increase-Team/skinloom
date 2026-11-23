package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

func TestBackupRestoreIncludesPluginDataAndSQLiteSnapshot(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	d, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "skinloom.db", DataDir: srcDir})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := migrate.Up(ctx, d, srcDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}

	// Create an active WAL-mode native plugin SQLite DB + private file under plugins-data/<id>/.
	plugDir := filepath.Join(srcDir, "plugins-data", "bs-importer")
	if err := os.MkdirAll(filepath.Join(plugDir, "exports"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugDir, "exports", "report.txt"), []byte("imported 42 rows"), 0o600); err != nil {
		t.Fatal(err)
	}
	plugDBPath := filepath.Join(plugDir, "plugin.db")
	plugPool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(plugDBPath)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer plugPool.Close()
	if _, err := plugPool.ExecContext(ctx, `CREATE TABLE plg_bs_importer_tasks (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := plugPool.ExecContext(ctx, `INSERT INTO plg_bs_importer_tasks (id, name) VALUES (1, 'migration-run')`); err != nil {
		t.Fatal(err)
	}

	// Write backup while plugPool is still open (verifies online VACUUM INTO snapshot).
	store, err := storage.NewLocal(filepath.Join(srcDir, "textures"))
	if err != nil {
		t.Fatal(err)
	}
	pkgStore, err := storage.NewLocal(filepath.Join(srcDir, "plugin-packages"))
	if err != nil {
		t.Fatal(err)
	}
	pkgBytes := []byte("PK\x03\x04fake-slp-archive-bytes")
	pkgSum := sha256.Sum256(pkgBytes)
	pkgSHA := hex.EncodeToString(pkgSum[:])
	if err := pkgStore.Put(ctx, pkgSHA, pkgBytes); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m, err := Write(ctx, &buf, Sources{
		DB:                 d,
		Store:              store,
		PluginPackageStore: pkgStore,
		DataDir:            srcDir,
		ConfigPath:         filepath.Join(srcDir, "config.toml"),
		Version:            "1.0.0",
		Now:                time.Now(),
	})
	if err != nil {
		t.Fatalf("backup.Write: %v", err)
	}
	if m.PluginDataFiles != 2 {
		t.Fatalf("PluginDataFiles = %d, want 2 (plugin.db + exports/report.txt)", m.PluginDataFiles)
	}
	if m.PluginPackages != 1 {
		t.Fatalf("PluginPackages = %d, want 1", m.PluginPackages)
	}

	// Restore into a brand new data directory and verify plugin.db, report.txt, and plugin-packages/<sha>.
	dstDir := t.TempDir()
	res, err := Restore(ctx, &buf, Target{
		DataDir:    dstDir,
		ConfigPath: filepath.Join(dstDir, "config.toml"),
		OpenStore: func(_ context.Context, c config.Config) (storage.Store, error) {
			return storage.NewLocal(c.Storage.Dir(dstDir))
		},
		OpenPluginPackageStore: func(_ context.Context, _ config.Config) (storage.Store, error) {
			return storage.NewLocal(filepath.Join(dstDir, "plugin-packages"))
		},
	})
	if err != nil {
		t.Fatalf("backup.Restore: %v", err)
	}
	if res.PluginDataFiles != 2 {
		t.Fatalf("restored PluginDataFiles = %d, want 2", res.PluginDataFiles)
	}
	if res.PluginPackages != 1 {
		t.Fatalf("restored PluginPackages = %d, want 1", res.PluginPackages)
	}
	restoredPkgStore, err := storage.NewLocal(filepath.Join(dstDir, "plugin-packages"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := restoredPkgStore.Open(ctx, pkgSHA)
	if err != nil {
		t.Fatalf("open restored plugin package: %v", err)
	}
	gotPkg, _ := io.ReadAll(pf)
	_ = pf.Close()
	if !bytes.Equal(gotPkg, pkgBytes) {
		t.Fatalf("restored plugin package mismatch: got %q", gotPkg)
	}

	gotReport, err := os.ReadFile(filepath.Join(dstDir, "plugins-data", "bs-importer", "exports", "report.txt"))
	if err != nil || string(gotReport) != "imported 42 rows" {
		t.Fatalf("restored report.txt = (%q, %v)", gotReport, err)
	}

	restoredDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dstDir, "plugins-data", "bs-importer", "plugin.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var taskName string
	if err := restoredDB.QueryRowContext(ctx, `SELECT name FROM plg_bs_importer_tasks WHERE id = 1`).Scan(&taskName); err != nil {
		t.Fatalf("query restored plugin.db: %v", err)
	}
	if taskName != "migration-run" {
		t.Fatalf("restored task name = %q, want migration-run", taskName)
	}
}

func TestRestoreRefusesUnsafePluginDataPath(t *testing.T) {
	for _, bad := range []string{
		"plugins-data/../evil.txt",
		"plugins-data/bs-importer/../../escape.txt",
		"plugins-data/BadID/plugin.db",
		"plugins-data/bs-importer/plugin.db-wal",
	} {
		dir := t.TempDir()
		buf := archive(t, map[string]string{manifestName: sqliteManifest(), bad: "pwn"}, manifestName, bad)
		_, err := Restore(context.Background(), buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")})
		if err == nil || !strings.Contains(err.Error(), "unexpected entry") {
			t.Errorf("expected %q to be refused as unexpected entry, got %v", bad, err)
		}
	}
}
