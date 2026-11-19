package storage

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type storeOverride struct {
	owner string
	store Store
}

// SwappableStore wraps a fallback Store (Local or S3) and allows an authorized
// native plugin with the "provider.storage" permission to atomically install or
// relinquish a custom storage driver at runtime without restarting SkinLoom.
type SwappableStore struct {
	base     Store
	mu       sync.Mutex
	override atomic.Pointer[storeOverride]
}

// NewSwappable creates a hot-swappable Store backed by base when no plugin
// provider is registered.
func NewSwappable(base Store) *SwappableStore {
	return &SwappableStore{base: base}
}

// Base returns the underlying built-in store.
func (s *SwappableStore) Base() Store {
	return s.base
}

// SetProvider atomically activates provider under owner. Passing a nil provider
// reverts to the built-in base store.
func (s *SwappableStore) SetProvider(owner string, provider Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if provider == nil {
		s.override.Store(nil)
		return
	}
	s.override.Store(&storeOverride{owner: owner, store: provider})
}

// ClearProvider reverts to the built-in base store if and only if owner is the
// currently active provider owner (preventing a draining old plugin from
// clobbering a newly activated replacement).
func (s *SwappableStore) ClearProvider(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.override.Load()
	if cur != nil && cur.owner == owner {
		s.override.Store(nil)
	}
}

// CurrentOwner returns the owner identifier of the active storage provider, or
// "" when using the built-in store.
func (s *SwappableStore) CurrentOwner() string {
	if cur := s.override.Load(); cur != nil {
		return cur.owner
	}
	return ""
}

func (s *SwappableStore) active() Store {
	if cur := s.override.Load(); cur != nil && cur.store != nil {
		return cur.store
	}
	return s.base
}

func (s *SwappableStore) Put(ctx context.Context, key string, data []byte) error {
	return s.active().Put(ctx, key, data)
}

func (s *SwappableStore) Open(ctx context.Context, key string) (File, error) {
	return s.active().Open(ctx, key)
}

func (s *SwappableStore) Stat(ctx context.Context, key string) (time.Time, error) {
	return s.active().Stat(ctx, key)
}

func (s *SwappableStore) Walk(ctx context.Context, fn func(key string, modTime time.Time) error) error {
	return s.active().Walk(ctx, fn)
}

func (s *SwappableStore) Delete(ctx context.Context, key string) error {
	return s.active().Delete(ctx, key)
}

// URL delegates to the active store when it implements Linker.
func (s *SwappableStore) URL(key string) string {
	if l, ok := s.active().(Linker); ok {
		return l.URL(key)
	}
	return ""
}

// BytesFile wraps an in-memory byte slice as a storage.File (used by gRPC
// StorageProvider clients returning fetched object bytes).
type BytesFile struct {
	*bytes.Reader
	modTime time.Time
	size    int64
}

// NewBytesFile constructs a File from data and modTime.
func NewBytesFile(data []byte, modTime time.Time) File {
	return &BytesFile{
		Reader:  bytes.NewReader(data),
		modTime: modTime,
		size:    int64(len(data)),
	}
}

func (b *BytesFile) Close() error       { return nil }
func (b *BytesFile) ModTime() time.Time { return b.modTime }
func (b *BytesFile) Size() int64        { return b.size }

var (
	_ Store  = (*SwappableStore)(nil)
	_ Linker = (*SwappableStore)(nil)
)
