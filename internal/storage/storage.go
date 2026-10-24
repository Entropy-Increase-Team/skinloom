// Package storage keeps texture files. Files are content addressed (the key is the
// SHA-256 of the bytes), so a key is written once and never changes: writes are
// idempotent and readers can cache forever. The local driver keeps one flat
// directory like Blessing Skin's storage/textures/{hash}; S3-compatible stores
// arrive later in M2.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// ErrNotFound means no file is stored under the key.
var ErrNotFound = errors.New("not found in storage")

// File is an open stored file.
type File interface {
	io.ReadSeekCloser
	ModTime() time.Time
	Size() int64
}

// Store keeps files by key.
type Store interface {
	// Put stores data under key. Storing an existing key is a no-op.
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (File, error)
	Delete(ctx context.Context, key string) error
}

var validKey = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidKey reports whether key is a lowercase hex SHA-256, the only keys stored.
func ValidKey(key string) bool { return validKey.MatchString(key) }

// Local stores files in a directory.
type Local struct {
	dir string
}

// NewLocal returns a store in dir, creating it.
func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("storage directory: %w", err)
	}
	return &Local{dir: dir}, nil
}

func (l *Local) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return filepath.Join(l.dir, key), nil
}

// Put writes to a temporary file and renames it into place, so a reader never sees
// a partial file and a crash leaves at most a stray temporary file.
func (l *Local) Put(_ context.Context, key string, data []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(l.dir, ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		// A concurrent Put of the same key may have won (on Windows, replacing a file
		// that is being read fails); the content is the same by definition.
		if _, serr := os.Stat(p); serr == nil {
			return nil
		}
		return err
	}
	return nil
}

type localFile struct {
	*os.File
	info fs.FileInfo
}

func (f localFile) ModTime() time.Time { return f.info.ModTime() }
func (f localFile) Size() int64        { return f.info.Size() }

func (l *Local) Open(_ context.Context, key string) (File, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, ErrNotFound
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return localFile{File: f, info: info}, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
