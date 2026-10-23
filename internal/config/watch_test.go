package config

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Editors that save atomically replace the file; the watch must survive that.
func TestWatchSurvivesAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if _, err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	initial, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan []string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Watch(ctx, path, initial, slog.New(slog.NewTextHandler(io.Discard, nil)), func(_, _ Config, changed []string) {
		got <- changed
	})

	replace := func(from, to string) {
		t.Helper()
		b, _ := os.ReadFile(path)
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(strings.Replace(string(b), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(want string) {
		t.Helper()
		select {
		case changed := <-got:
			if len(changed) != 1 || changed[0] != want {
				t.Fatalf("changed = %v, want [%s]", changed, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no reload for %s", want)
		}
	}

	replace(`level = "info"`, `level = "debug"`)
	wait("log.level")
	replace(`format = "text"`, `format = "json"`) // second save after a replace
	wait("log.format")
}
