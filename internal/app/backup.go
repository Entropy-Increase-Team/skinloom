package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/backup"
	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// BackupOptions control Backup.
type BackupOptions struct {
	OutDir   string // empty = <data>/backups
	Textures bool   // include the texture files
	Keep     int    // keep this many archives in OutDir, deleting older ones; 0 = all
}

var archiveName = regexp.MustCompile(`^skinloom-\d{8}-\d{6}\.tar\.gz$`)

// Backup writes <OutDir>/skinloom-<time>.tar.gz of the install at p. The site may keep
// running: SQLite is copied with VACUUM INTO. The database is opened without migrating,
// so a backup taken before an upgrade holds the schema as it was.
func Backup(ctx context.Context, p Paths, o BackupOptions) (string, backup.Manifest, error) {
	var m backup.Manifest
	p, err := p.Resolve()
	if err != nil {
		return "", m, err
	}
	cfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return "", m, err
	}
	database, err := db.Open(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: p.DataDir, MaxOpenConns: 2})
	if err != nil {
		return "", m, err
	}
	defer database.Close()
	var store storage.Store
	if o.Textures {
		if store, err = openStore(ctx, cfg.Storage, p.DataDir); err != nil {
			return "", m, err
		}
	}
	dir := o.OutDir
	if dir == "" {
		dir = filepath.Join(p.DataDir, "backups")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", m, err
	}
	now := time.Now()
	path := filepath.Join(dir, "skinloom-"+now.Format("20060102-150405")+".tar.gz")
	tmp := path + ".partial"
	// 0600: the archive holds the secrets and, with SQLite, every account.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", m, err
	}
	m, err = backup.Write(ctx, f, backup.Sources{DB: database, Store: store, DataDir: p.DataDir, ConfigPath: p.ConfigPath,
		Version: buildinfo.Version, Now: now})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", m, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", m, err
	}
	if o.Keep > 0 {
		if err := prune(dir, o.Keep); err != nil {
			return path, m, fmt.Errorf("backup written, but removing old ones failed: %w", err)
		}
	}
	return path, m, nil
}

// prune keeps the newest keep archives in dir (names sort by time).
func prune(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && archiveName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// Restore unpacks an archive into the install at p, which must not be running.
func Restore(ctx context.Context, p Paths, file string, force bool) (backup.Result, error) {
	p, err := p.Resolve()
	if err != nil {
		return backup.Result{}, err
	}
	f, err := os.Open(file)
	if err != nil {
		return backup.Result{}, err
	}
	defer f.Close()
	if err := os.MkdirAll(p.DataDir, 0o750); err != nil {
		return backup.Result{}, err
	}
	return backup.Restore(ctx, f, backup.Target{DataDir: p.DataDir, ConfigPath: p.ConfigPath, Force: force,
		OpenStore: func(ctx context.Context, c config.Config) (storage.Store, error) {
			return openStore(ctx, c.Storage, p.DataDir)
		}})
}
