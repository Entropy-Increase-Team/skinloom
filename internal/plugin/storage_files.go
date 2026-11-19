package plugin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrFileNotFound means a plugin private file key does not exist.
	ErrFileNotFound = errors.New("plugin file not found")
	// ErrInvalidFileKey means the file key is unsafe or traverses directories.
	ErrInvalidFileKey = errors.New("invalid plugin file key")
)

// FileStore isolates plugin-private files under <dataDir>/plugins-data/<id>/files/ with
// path traversal guards, symlink refusal, and total byte quota enforcement.
// Keeping public/private files inside a dedicated files/ subdirectory isolates them from
// native plugin SQLite databases (plugin.db) and internal DataDir state.
type FileStore struct {
	pluginID string
	root     string
	maxBytes int64
	perms    *PermissionSet
	mu       sync.Mutex
}

// NewFileStore constructs a plugin-scoped file store isolated under
// <dataDir>/plugins-data/<id>/files/.
func NewFileStore(pluginID, dataDir string, maxBytes int64, perms *PermissionSet) *FileStore {
	if maxBytes <= 0 {
		maxBytes = MaxPluginFileBytes
	}
	root := filepath.Join(dataDir, "plugins-data", pluginID, "files")
	return &FileStore{
		pluginID: pluginID,
		root:     root,
		maxBytes: maxBytes,
		perms:    perms,
	}
}

// ValidateFileKey verifies and normalizes a slash-separated relative asset/file key.
func ValidateFileKey(key string) (string, error) {
	return validateFileKey(key)
}

func validateFileKey(key string) (string, error) {
	if key == "" || len(key) > 256 || strings.HasPrefix(key, "/") || strings.ContainsRune(key, '\\') || strings.ContainsRune(key, ':') {
		return "", ErrInvalidFileKey
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] == 0x7f {
			return "", ErrInvalidFileKey
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrInvalidFileKey
		}
	}
	clean := path.Clean("/" + key)
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", ErrInvalidFileKey
	}
	return rel, nil
}

func (f *FileStore) diskPath(key string) (string, string, error) {
	rel, err := validateFileKey(key)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(f.root, filepath.FromSlash(rel))
	if absRoot, err := filepath.Abs(f.root); err == nil {
		if absTarget, err := filepath.Abs(target); err == nil {
			r, err := filepath.Rel(absRoot, absTarget)
			if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				return "", "", ErrInvalidFileKey
			}
		}
	}
	return rel, target, nil
}

func (f *FileStore) usageLocked() (int64, error) {
	var total int64
	err := filepath.WalkDir(f.root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// Put writes data to the plugin's private file space and returns its public asset path.
func (f *FileStore) Put(_ context.Context, key string, data []byte) (string, error) {
	if err := f.perms.Check(PermStorageFiles); err != nil {
		return "", err
	}
	rel, _, err := f.diskPath(key)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	curUsage, err := f.usageLocked()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		return "", err
	}
	rootFS, err := os.OpenRoot(f.root)
	if err != nil {
		return "", err
	}
	defer rootFS.Close()

	nativeRel := filepath.FromSlash(rel)
	var existingSize int64
	if info, err := rootFS.Lstat(nativeRel); err == nil {
		if !info.Mode().IsRegular() {
			return "", ErrInvalidFileKey
		}
		existingSize = info.Size()
	}
	if curUsage-existingSize+int64(len(data)) > f.maxBytes {
		return "", fmt.Errorf("%w: file quota (%d bytes) exceeded", ErrQuotaExceeded, f.maxBytes)
	}
	if dir := filepath.Dir(nativeRel); dir != "." && dir != "" {
		if err := rootFS.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := rootFS.WriteFile(nativeRel, data, 0o644); err != nil {
		return "", err
	}
	return f.urlForCleanKey(rel), nil
}

// Get reads data for key from the plugin's private file space.
func (f *FileStore) Get(_ context.Context, key string) ([]byte, error) {
	if err := f.perms.Check(PermStorageFiles); err != nil {
		return nil, err
	}
	rel, _, err := f.diskPath(key)
	if err != nil {
		return nil, err
	}
	rootFS, err := os.OpenRoot(f.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	defer rootFS.Close()

	nativeRel := filepath.FromSlash(rel)
	info, err := rootFS.Lstat(nativeRel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidFileKey
	}
	b, err := rootFS.ReadFile(nativeRel)
	if os.IsNotExist(err) {
		return nil, ErrFileNotFound
	}
	return b, err
}

// Delete removes a file by key.
func (f *FileStore) Delete(_ context.Context, key string) (bool, error) {
	if err := f.perms.Check(PermStorageFiles); err != nil {
		return false, err
	}
	rel, _, err := f.diskPath(key)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rootFS, err := os.OpenRoot(f.root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer rootFS.Close()

	err = rootFS.Remove(filepath.FromSlash(rel))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// URL returns the public HTTP asset path for a private file key.
func (f *FileStore) URL(key string) (string, error) {
	if err := f.perms.Check(PermStorageFiles); err != nil {
		return "", err
	}
	rel, _, err := f.diskPath(key)
	if err != nil {
		return "", err
	}
	return f.urlForCleanKey(rel), nil
}

func (f *FileStore) urlForCleanKey(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/plugins-assets/" + f.pluginID + "/_files/" + strings.Join(segs, "/")
}
