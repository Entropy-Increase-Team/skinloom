// Package storage keeps texture files. Files are content addressed (the key is the
// SHA-256 of the bytes), so a key is written once and never changes: writes are
// idempotent and readers can cache forever. The local driver keeps one flat
// directory like Blessing Skin's storage/textures/{hash}; S3 keeps the same keys in a
// bucket (s3.go).
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
	// Put stores data under key. Storing an existing key only marks it as just
	// written (its ModTime becomes now), which keeps a file that a new upload is about
	// to reference away from the orphan sweep.
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (File, error)
	// Stat returns when key was last written (ErrNotFound if it is not stored).
	Stat(ctx context.Context, key string) (time.Time, error)
	// Walk calls fn for every stored key.
	Walk(ctx context.Context, fn func(key string, modTime time.Time) error) error
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
		now := time.Now()
		return os.Chtimes(p, now, now)
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

func (l *Local) Stat(_ context.Context, key string) (time.Time, error) {
	p, err := l.path(key)
	if err != nil {
		return time.Time{}, ErrNotFound
	}
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// Walk lists the directory; temporary files of interrupted writes are skipped.
func (l *Local) Walk(ctx context.Context, fn func(key string, modTime time.Time) error) error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.IsDir() || !ValidKey(e.Name()) {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted meanwhile
		}
		if err != nil {
			return err
		}
		if err := fn(e.Name(), info.ModTime()); err != nil {
			return err
		}
	}
	return nil
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
