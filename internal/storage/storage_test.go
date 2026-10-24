package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"
)

func key(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestLocal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("png bytes")
	k := key(data)
	if _, err := s.Open(ctx, k); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	for range 2 { // writing a key twice is a no-op
		if err := s.Put(ctx, k, data); err != nil {
			t.Fatal(err)
		}
	}
	f, err := s.Open(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != string(data) || f.Size() != int64(len(data)) {
		t.Fatalf("read back %q (size %d)", got, f.Size())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("want only the stored file in %s, got %d entries (temporary files left?)", dir, len(entries))
	}
	if err := s.Delete(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, k); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
	// Concurrent writes of one key (two uploads of the same image) all succeed.
	other := []byte("another png")
	errs := make(chan error, 8)
	for range 8 {
		go func() { errs <- s.Put(ctx, key(other), other) }()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Put: %v", err)
		}
	}
	// Keys are hashes; anything else (path traversal included) is refused.
	for _, bad := range []string{"../config.toml", "ABC", k[:63], k + "0"} {
		if err := s.Put(ctx, bad, data); err == nil {
			t.Fatalf("Put(%q) must fail", bad)
		}
		if _, err := s.Open(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Open(%q): %v", bad, err)
		}
	}
}
