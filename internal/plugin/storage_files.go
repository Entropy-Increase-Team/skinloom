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

// FileStore isolates plugin-private files under <dataDir>/plugins-data/<id>/ with
// path traversal guards and total byte quota enforcement.
type FileStore struct {
	pluginID string
	root     string
	maxBytes int64
	perms    *PermissionSet
	mu       sync.Mutex
}

// NewFileStore constructs a plugin-scoped private file store.
func NewFileStore(pluginID, dataDir string, maxBytes int64, perms *PermissionSet) *FileStore {
	if maxBytes <= 0 {
		maxBytes = MaxPluginFileBytes
	}
	root := filepath.Join(dataDir, "plugins-data", pluginID)
	return &FileStore{
		pluginID: pluginID,
		root:     root,
		maxBytes: maxBytes,
		perms:    perms,
	}
}

func validateFileKey(key string) (string, error) {
	if key == "" || len(key) > 256 || strings.HasPrefix(key, "/") || strings.ContainsRune(key, '\\') || strings.ContainsRune(key, 0) {
		return "", ErrInvalidFileKey
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrInvalidFileKey
		}
	}
	clean := path.Clean("/" + key)
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
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
	rel, target, err := f.diskPath(key)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	curUsage, err := f.usageLocked()
	if err != nil {
		return "", err
	}
	var existingSize int64
	if info, err := os.Stat(target); err == nil {
		existingSize = info.Size()
	}
	if curUsage-existingSize+int64(len(data)) > f.maxBytes {
		return "", fmt.Errorf("%w: file quota (%d bytes) exceeded", ErrQuotaExceeded, f.maxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return f.urlForCleanKey(rel), nil
}

// Get reads data for key from the plugin's private file space.
func (f *FileStore) Get(_ context.Context, key string) ([]byte, error) {
	if err := f.perms.Check(PermStorageFiles); err != nil {
		return nil, err
	}
	_, target, err := f.diskPath(key)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(target)
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
	_, target, err := f.diskPath(key)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	err = os.Remove(target)
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
