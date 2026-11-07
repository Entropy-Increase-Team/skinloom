package datalock

import (
	"errors"
	"testing"
)

func TestSharedAndExclusive(t *testing.T) {
	dir := t.TempDir()
	a, err := Shared(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Shared(dir) // a second role on the same directory
	if err != nil {
		t.Fatalf("second shared: %v", err)
	}
	if _, err := Exclusive(dir); !errors.Is(err, ErrInUse) {
		t.Fatalf("exclusive while shared: %v", err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := Exclusive(dir); !errors.Is(err, ErrInUse) {
		t.Fatalf("exclusive while one shared is left: %v", err)
	}
	if err := b.Release(); err != nil {
		t.Fatal(err)
	}

	x, err := Exclusive(dir)
	if err != nil {
		t.Fatalf("exclusive when free: %v", err)
	}
	if _, err := Shared(dir); !errors.Is(err, ErrInUse) {
		t.Fatalf("shared during a restore: %v", err)
	}
	if err := x.Release(); err != nil {
		t.Fatal(err)
	}
	if err := x.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	s, err := Shared(dir)
	if err != nil {
		t.Fatalf("shared after release: %v", err)
	}
	_ = s.Release()
}
