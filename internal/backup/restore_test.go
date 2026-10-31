package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, entries map[string]string, order ...string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range order {
		body := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return &buf
}

// Only the known entry names are written: an archive cannot reach outside the data
// directory, whoever made it.
func TestRestoreRefusesUnknownEntries(t *testing.T) {
	manifest, _ := json.Marshal(Manifest{Format: Format})
	for _, bad := range []string{"../escape", "secrets/../../escape", "textures/../x", "database.sqlite.bak", "/abs"} {
		dir := t.TempDir()
		buf := archive(t, map[string]string{manifestName: string(manifest), bad: "x"}, manifestName, bad)
		_, err := Restore(context.Background(), buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")})
		if err == nil || !strings.Contains(err.Error(), "unexpected entry") {
			t.Errorf("%s: %v", bad, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
			t.Fatalf("%s escaped the data directory", bad)
		}
	}
}

func TestRestoreChecksTheArchive(t *testing.T) {
	dir := t.TempDir()
	target := Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")}
	if _, err := Restore(context.Background(), strings.NewReader("not gzip"), target); err == nil {
		t.Fatal("not an archive")
	}
	if _, err := Restore(context.Background(), archive(t, map[string]string{"config.toml": ""}, "config.toml"), target); err == nil {
		t.Fatal("no manifest")
	}
	future, _ := json.Marshal(Manifest{Format: Format + 1})
	if _, err := Restore(context.Background(), archive(t, map[string]string{manifestName: string(future)}, manifestName), target); err == nil {
		t.Fatal("newer format")
	}
}
