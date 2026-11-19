package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// ErrExists: the target already holds an install and Force was not given.
var ErrExists = errors.New("the data directory already holds an install")

// Target is where Restore writes.
type Target struct {
	DataDir    string
	ConfigPath string
	// Force overwrites an existing config, secrets and database.
	Force bool
	// OpenStore opens the texture store of the restored config.
	OpenStore func(ctx context.Context, c config.Config) (storage.Store, error)
}

// Result is what Restore wrote.
type Result struct {
	Manifest        Manifest
	Database        string // the SQLite file written, or "" when the archive has none
	Textures        int
	PluginDataFiles int
}

// Entry size caps: the object store holds both textures (<= 8 MiB) and cached .slp
// plugin archives (up to 150 MiB for multi-platform native fat packages). A bigger
// entry is refused, not truncated.
const (
	maxTexture         = 160 << 20
	maxSmall           = 1 << 20
	maxPluginData      = 256 << 20
	maxTotalPluginData = 2 << 30
)

// Restore unpacks an archive into an install that is not running. The texture files go
// through the store, so a local archive can be restored onto S3 and back.
//
// The archive lists the config and the secrets before the database and the textures.
// They are held back until the first of those (or the end), and every check that can
// refuse the restore runs then, before anything is written.
func Restore(ctx context.Context, r io.Reader, t Target) (Result, error) {
	var res Result
	if !t.Force {
		if err := refuseExisting(t.ConfigPath); err != nil {
			return res, err
		}
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return res, fmt.Errorf("not a SkinLoom backup: %w", err)
	}
	tr := tar.NewReader(gz)

	hdr, err := tr.Next()
	if err != nil || hdr.Name != manifestName {
		return res, errors.New("not a SkinLoom backup: no manifest first")
	}
	if err := json.NewDecoder(io.LimitReader(tr, maxSmall)).Decode(&res.Manifest); err != nil {
		return res, fmt.Errorf("manifest: %w", err)
	}
	if res.Manifest.Format != Format {
		return res, fmt.Errorf("backup format %d, this SkinLoom reads %d", res.Manifest.Format, Format)
	}

	var (
		configData []byte // the archive's config.toml; nil when it has none
		secrets    = map[string][]byte{}
		cfg        *config.Config // the restored config, set by commit
		store      storage.Store
	)
	staged := t.ConfigPath + ".restoring"
	defer os.Remove(staged)
	// commit checks the target against the restored config, then writes the config
	// and the secrets.
	commit := func() error {
		path := t.ConfigPath
		if configData != nil {
			if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
				return err
			}
			if err := os.WriteFile(staged, configData, 0o600); err != nil {
				return err
			}
			path = staged
		}
		c, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		cfg = &c
		if !t.Force {
			paths := []string{t.ConfigPath}
			for name := range secrets {
				paths = append(paths, filepath.Join(t.DataDir, "secrets", name))
			}
			if res.Manifest.HasDatabase && db.IsSQLite(c.Database.Driver) {
				paths = append(paths, db.SQLitePath(c.Database.DSN, t.DataDir))
			}
			if res.Manifest.PluginDataFiles > 0 {
				paths = append(paths, filepath.Join(t.DataDir, "plugins-data"))
			}
			if err := refuseExisting(paths...); err != nil {
				return err
			}
		}
		if configData != nil {
			if err := os.Rename(staged, t.ConfigPath); err != nil {
				return err
			}
		}
		if len(secrets) > 0 {
			dir := filepath.Join(t.DataDir, "secrets")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			if err := fsperm.Private(dir); err != nil {
				return err
			}
		}
		for name, data := range secrets {
			if err := writeFile(filepath.Join(t.DataDir, "secrets", name), bytes.NewReader(data), 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	small := func(hdr *tar.Header) ([]byte, error) {
		if hdr.Size > maxSmall {
			return nil, fmt.Errorf("entry %q is too large", hdr.Name)
		}
		return io.ReadAll(tr)
	}
	var totalPluginData int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return res, err
		}
		if hdr.Typeflag != tar.TypeReg {
			return res, fmt.Errorf("unexpected entry %q", hdr.Name)
		}
		name := hdr.Name
		if cfg == nil && (name == databaseName || textureName.MatchString(name) || isSafePluginDataEntry(name)) {
			if err := commit(); err != nil {
				return res, err
			}
		}
		switch {
		case name == configName:
			if cfg != nil || configData != nil {
				return res, fmt.Errorf("unexpected entry %q", name)
			}
			if configData, err = small(hdr); err != nil {
				return res, err
			}
		case secretName.MatchString(name):
			key := strings.TrimPrefix(name, "secrets/")
			if _, dup := secrets[key]; dup || cfg != nil {
				return res, fmt.Errorf("unexpected entry %q", name)
			}
			if secrets[key], err = small(hdr); err != nil {
				return res, err
			}
		case name == databaseName:
			if res.Database != "" {
				return res, fmt.Errorf("unexpected entry %q", name)
			}
			if !db.IsSQLite(cfg.Database.Driver) {
				return res, fmt.Errorf("the backup holds a SQLite database but the config uses %s", cfg.Database.Driver)
			}
			path := db.SQLitePath(cfg.Database.DSN, t.DataDir)
			for _, side := range []string{"-wal", "-shm"} { // they belong to the database being replaced
				if err := os.Remove(path + side); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return res, err
				}
			}
			if err := writeFile(path, tr, 0o600); err != nil {
				return res, err
			}
			res.Database = path
		case textureName.MatchString(name):
			if hdr.Size > maxTexture {
				return res, fmt.Errorf("texture %s is too large", name)
			}
			if store == nil {
				if store, err = t.OpenStore(ctx, *cfg); err != nil {
					return res, fmt.Errorf("open texture storage: %w", err)
				}
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return res, err
			}
			key := strings.TrimPrefix(name, "textures/")
			// The store serves a key as immutable: a corrupt file would be cached for good.
			if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != key {
				return res, fmt.Errorf("texture %s: the content does not match its name", name)
			}
			if err := store.Put(ctx, key, data); err != nil {
				return res, fmt.Errorf("texture %s: %w", name, err)
			}
			res.Textures++
		case isSafePluginDataEntry(name):
			if hdr.Size < 0 || hdr.Size > maxPluginData {
				return res, fmt.Errorf("plugin data %s is too large", name)
			}
			if totalPluginData+hdr.Size > maxTotalPluginData {
				return res, fmt.Errorf("plugin data exceeds %d byte total restore limit", maxTotalPluginData)
			}
			rel := strings.TrimPrefix(name, "plugins-data/")
			pluginDataRoot := filepath.Join(t.DataDir, "plugins-data")
			targetPath := filepath.Join(pluginDataRoot, filepath.FromSlash(rel))
			if err := checkNoSymlinkPath(pluginDataRoot, filepath.FromSlash(rel)); err != nil {
				return res, fmt.Errorf("plugin data %s: %w", name, err)
			}
			if !t.Force {
				if err := refuseExisting(targetPath); err != nil {
					return res, err
				}
			}
			if strings.HasSuffix(strings.ToLower(rel), ".db") || strings.HasSuffix(strings.ToLower(rel), ".sqlite") {
				for _, side := range []string{"-wal", "-shm"} {
					if err := os.Remove(targetPath + side); err != nil && !errors.Is(err, fs.ErrNotExist) {
						return res, err
					}
				}
			}
			if err := writeFile(targetPath, io.LimitReader(tr, maxPluginData), 0o600); err != nil {
				return res, fmt.Errorf("plugin data %s: %w", name, err)
			}
			totalPluginData += hdr.Size
			res.PluginDataFiles++
		default:
			return res, fmt.Errorf("unexpected entry %q", name)
		}
	}
	if cfg == nil {
		if err := commit(); err != nil {
			return res, err
		}
	}
	return res, nil
}

// refuseExisting fails when any of paths exists.
func refuseExisting(paths ...string) error {
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("%w (%s): restore into an empty one, or pass --force", ErrExists, p)
		}
	}
	return nil
}

// checkNoSymlinkPath rejects any existing symlink along root/<rel>.
func checkNoSymlinkPath(root, rel string) error {
	cur := root
	if info, err := os.Lstat(cur); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to write through symbolic link")
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		if info, err := os.Lstat(cur); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to write through symbolic link")
		}
	}
	return nil
}

// writeFile writes r to path through a temporary file, so a failure leaves no
// half-written file in its place.
func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".restoring"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
