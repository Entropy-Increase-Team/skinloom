// Package backup writes and restores a whole SkinLoom install as one .tar.gz
// (docs/design/02 F78): the SQLite database (an online snapshot, the site may keep
// running), the config file, the secrets and the texture files. MySQL and PostgreSQL
// are left to their own dump tools; the archive then carries everything else.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// Format is the archive layout version.
const Format = 1

// Entry names. Restore takes nothing else, so an archive writes only the config file,
// the secrets, the database file its config names and the texture store.
const (
	manifestName = "manifest.json"
	configName   = "config.toml"
	databaseName = "database.sqlite"
)

var (
	secretName   = regexp.MustCompile(`^secrets/[A-Za-z0-9_-][A-Za-z0-9._-]{0,99}$`) // not "." or ".."
	textureName  = regexp.MustCompile(`^textures/[0-9a-f]{64}$`)
	pluginIDSeg  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	sqliteHeader = []byte("SQLite format 3\x00")
)

// isSafePluginDataEntry checks that name is a clean relative path under plugins-data/<id>/...
// with no path traversal, backslashes, NUL bytes, or transient SQLite WAL/SHM sidecars.
func isSafePluginDataEntry(name string) bool {
	rest, ok := strings.CutPrefix(name, "plugins-data/")
	if !ok || rest == "" || strings.ContainsRune(rest, '\\') || strings.ContainsRune(rest, ':') {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < 0x20 || rest[i] == 0x7f {
			return false
		}
	}
	if strings.HasSuffix(rest, "-wal") || strings.HasSuffix(rest, "-shm") {
		return false
	}
	segs := strings.Split(rest, "/")
	if len(segs) < 2 || !pluginIDSeg.MatchString(segs[0]) {
		return false
	}
	for _, seg := range segs[1:] {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	clean := path.Clean(name)
	return clean == name && filepath.IsLocal(filepath.FromSlash(rest))
}

// Manifest describes an archive; it is its first entry.
type Manifest struct {
	Format    int       `json:"format"`
	Version   string    `json:"version"` // of the SkinLoom that wrote it
	CreatedAt time.Time `json:"createdAt"`
	Database  string    `json:"database"` // dialect
	Migration int64     `json:"migration"`
	// HasDatabase is false for MySQL and PostgreSQL: dump those with their own tools.
	HasDatabase     bool  `json:"hasDatabase"`
	Textures        int   `json:"textures"`
	TextureBytes    int64 `json:"textureBytes"`
	PluginDataFiles int   `json:"pluginDataFiles,omitempty"`
}

// Sources is what Write reads.
type Sources struct {
	DB         *db.DB
	Store      storage.Store // nil leaves the textures out
	DataDir    string
	ConfigPath string
	Version    string
	Now        time.Time
}

// Write streams the archive to w.
func Write(ctx context.Context, w io.Writer, src Sources) (Manifest, error) {
	m := Manifest{Format: Format, Version: src.Version, CreatedAt: src.Now.UTC(), Database: string(src.DB.Dialect),
		HasDatabase: src.DB.Dialect == db.SQLite}
	var err error
	if m.Migration, _, err = migrate.Versions(ctx, src.DB); err != nil {
		return m, err
	}
	// The database is copied before the textures are listed: a texture uploaded in
	// between is then in the archive without a reference (the orphan sweep drops it),
	// never referenced by the archived database but missing from the archive.
	var dbCopy string
	if m.HasDatabase {
		path, err := snapshot(ctx, src.DB, src.DataDir)
		if err != nil {
			return m, fmt.Errorf("database: %w", err)
		}
		defer os.Remove(path)
		dbCopy = path
	}
	var keys []string
	if src.Store != nil {
		if err := src.Store.Walk(ctx, func(key string, _ time.Time) error {
			if storage.ValidKey(key) {
				keys = append(keys, key)
			}
			return nil
		}); err != nil {
			return m, fmt.Errorf("list textures: %w", err)
		}
	}
	m.Textures = len(keys)

	type pluginDataItem struct {
		archiveName string
		diskPath    string
		cleanup     bool
	}
	var pluginItems []pluginDataItem
	defer func() {
		for _, it := range pluginItems {
			if it.cleanup {
				_ = os.Remove(it.diskPath)
			}
		}
	}()
	pluginDataRoot := filepath.Join(src.DataDir, "plugins-data")
	if _, statErr := os.Stat(pluginDataRoot); statErr == nil {
		var rels []string
		err := filepath.WalkDir(pluginDataRoot, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, rErr := filepath.Rel(pluginDataRoot, p)
			if rErr != nil {
				return rErr
			}
			archiveName := "plugins-data/" + filepath.ToSlash(rel)
			if !isSafePluginDataEntry(archiveName) {
				return nil
			}
			rels = append(rels, archiveName)
			return nil
		})
		if err != nil {
			return m, fmt.Errorf("plugins-data: %w", err)
		}
		sort.Strings(rels)
		for _, archiveName := range rels {
			rel := strings.TrimPrefix(archiveName, "plugins-data/")
			full := filepath.Join(pluginDataRoot, filepath.FromSlash(rel))
			if isSQLiteFile(full) {
				snap, snapErr := snapshotSQLiteFile(ctx, full, src.DataDir)
				if snapErr != nil {
					return m, fmt.Errorf("snapshot %s: %w", archiveName, snapErr)
				}
				pluginItems = append(pluginItems, pluginDataItem{archiveName: archiveName, diskPath: snap, cleanup: true})
			} else {
				pluginItems = append(pluginItems, pluginDataItem{archiveName: archiveName, diskPath: full})
			}
		}
	}
	m.PluginDataFiles = len(pluginItems)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	put := func(name string, size int64, r io.Reader) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: src.Now, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := io.Copy(tw, r)
		return err
	}
	putFile := func(name, path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		return put(name, info.Size(), f)
	}

	manifest, _ := json.MarshalIndent(m, "", "  ")
	if err := put(manifestName, int64(len(manifest)), bytes.NewReader(manifest)); err != nil {
		return m, err
	}
	if err := putFile(configName, src.ConfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, fmt.Errorf("config: %w", err)
	}
	secrets, err := os.ReadDir(filepath.Join(src.DataDir, "secrets"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, fmt.Errorf("secrets: %w", err)
	}
	for _, e := range secrets {
		if name := "secrets/" + e.Name(); e.Type().IsRegular() && secretName.MatchString(name) {
			if err := putFile(name, filepath.Join(src.DataDir, "secrets", e.Name())); err != nil {
				return m, fmt.Errorf("secrets: %w", err)
			}
		}
	}
	if dbCopy != "" {
		if err := putFile(databaseName, dbCopy); err != nil {
			return m, fmt.Errorf("database: %w", err)
		}
	}
	for _, item := range pluginItems {
		if err := putFile(item.archiveName, item.diskPath); err != nil {
			return m, fmt.Errorf("%s: %w", item.archiveName, err)
		}
	}
	for _, key := range keys {
		f, err := src.Store.Open(ctx, key)
		if errors.Is(err, storage.ErrNotFound) {
			continue // deleted since the listing
		}
		if err != nil {
			return m, fmt.Errorf("texture %s: %w", key, err)
		}
		err = put("textures/"+key, f.Size(), f)
		m.TextureBytes += f.Size()
		f.Close()
		if err != nil {
			return m, fmt.Errorf("texture %s: %w", key, err)
		}
	}
	if err := tw.Close(); err != nil {
		return m, err
	}
	return m, gz.Close()
}

// snapshot copies the live SQLite database with VACUUM INTO (a consistent copy while
// the site keeps writing) into a temporary file next to the database; the caller
// removes it.
func snapshot(ctx context.Context, d *db.DB, dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	// The copy holds every account; on Windows it would take the folder's permissions.
	if err := fsperm.Private(dir); err != nil {
		return "", err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	path := filepath.Join(dir, ".snapshot-"+hex.EncodeToString(b)+".db")
	if _, err := d.SQL.ExecContext(ctx, `VACUUM INTO ?`, filepath.ToSlash(path)); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func isSQLiteFile(p string) bool {
	if !strings.HasSuffix(strings.ToLower(p), ".db") && !strings.HasSuffix(strings.ToLower(p), ".sqlite") {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(sqliteHeader))
	n, err := io.ReadFull(f, buf)
	return err == nil && n == len(sqliteHeader) && bytes.Equal(buf, sqliteHeader)
}

func snapshotSQLiteFile(ctx context.Context, srcPath, dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if err := fsperm.Private(dir); err != nil {
		return "", err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	dst := filepath.Join(dir, ".plugin-snapshot-"+hex.EncodeToString(b)+".db")

	q := url.Values{"_pragma": {"busy_timeout(5000)"}}
	pool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(srcPath)+"?"+q.Encode())
	if err != nil {
		return "", err
	}
	defer pool.Close()
	if _, err := pool.ExecContext(ctx, `VACUUM INTO ?`, filepath.ToSlash(dst)); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}
