package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu      sync.RWMutex
	files   map[string][]byte
	times   map[string]time.Time
	baseURL string
}

func newMemStore(baseURL string) *memStore {
	return &memStore{
		files:   map[string][]byte{},
		times:   map[string]time.Time{},
		baseURL: baseURL,
	}
}

func (m *memStore) Put(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = append([]byte(nil), data...)
	m.times[key] = time.Now().UTC()
	return nil
}

func (m *memStore) Open(_ context.Context, key string) (File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.files[key]
	if !ok {
		return nil, ErrNotFound
	}
	return NewBytesFile(b, m.times[key]), nil
}

func (m *memStore) Stat(_ context.Context, key string) (time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.times[key]
	if !ok {
		return time.Time{}, ErrNotFound
	}
	return t, nil
}

func (m *memStore) Walk(_ context.Context, fn func(string, time.Time) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, t := range m.times {
		if err := fn(k, t); err != nil {
			return err
		}
	}
	return nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	delete(m.times, key)
	return nil
}

func (m *memStore) URL(key string) string {
	if m.baseURL == "" {
		return ""
	}
	return m.baseURL + "/" + key
}

func TestSwappableStoreHotSwapAndOwnerGuard(t *testing.T) {
	ctx := context.Background()
	base, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	sw := NewSwappable(base)

	sum := sha256.Sum256([]byte("base-content"))
	key := hex.EncodeToString(sum[:])

	if err := sw.Put(ctx, key, []byte("base-content")); err != nil {
		t.Fatalf("base Put: %v", err)
	}
	if sw.CurrentOwner() != "" || sw.URL(key) != "" {
		t.Fatalf("unexpected initial owner/url: %q %q", sw.CurrentOwner(), sw.URL(key))
	}

	p1 := newMemStore("https://cdn1.example.com")
	sw.SetProvider("plugin:stor-a", p1)
	if sw.CurrentOwner() != "plugin:stor-a" {
		t.Fatalf("CurrentOwner = %q, want plugin:stor-a", sw.CurrentOwner())
	}
	if err := sw.Put(ctx, key, []byte("p1-content")); err != nil {
		t.Fatalf("p1 Put: %v", err)
	}
	if got := sw.URL(key); got != "https://cdn1.example.com/"+key {
		t.Fatalf("URL = %q", got)
	}
	f, err := sw.Open(ctx, key)
	if err != nil {
		t.Fatalf("p1 Open: %v", err)
	}
	gotBytes, _ := io.ReadAll(f)
	_ = f.Close()
	if string(gotBytes) != "p1-content" {
		t.Fatalf("Open content = %q, want p1-content", gotBytes)
	}

	// Replacing with p2, then clearing stale owner p1 must NOT clear p2.
	p2 := newMemStore("https://cdn2.example.com")
	sw.SetProvider("plugin:stor-b", p2)
	sw.ClearProvider("plugin:stor-a")
	if sw.CurrentOwner() != "plugin:stor-b" {
		t.Fatalf("stale ClearProvider clobbered active provider: owner=%q", sw.CurrentOwner())
	}

	// Clearing active owner restores base store.
	sw.ClearProvider("plugin:stor-b")
	if sw.CurrentOwner() != "" {
		t.Fatalf("expected base fallback, got owner=%q", sw.CurrentOwner())
	}
	f, err = sw.Open(ctx, key)
	if err != nil {
		t.Fatalf("base Open after clear: %v", err)
	}
	gotBytes, _ = io.ReadAll(f)
	_ = f.Close()
	if string(gotBytes) != "base-content" {
		t.Fatalf("base content = %q, want base-content", gotBytes)
	}
}
