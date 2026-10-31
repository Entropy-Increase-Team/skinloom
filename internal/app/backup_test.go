package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/backup"
	"github.com/Entropy-Increase-Team/skinloom/internal/secrets"
)

// A backup taken from a running install restores into an empty data directory with
// the same settings, textures and secrets.
func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	src := Paths{DataDir: t.TempDir()}
	core, err := OpenCore(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := secrets.Ensure(core.Paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Settings.Set(ctx, nil, map[string]json.RawMessage{"site.name": json.RawMessage(`{"en":"Backed up"}`)}, nil); err != nil {
		t.Fatal(err)
	}
	png := []byte("png bytes")
	sum := sha256.Sum256(png) // the store names a file by its content's hash
	key := hex.EncodeToString(sum[:])
	store, err := openStore(ctx, core.Config.Storage, core.Paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, key, png); err != nil {
		t.Fatal(err)
	}

	// The site keeps running (the core stays open) while the backup is taken.
	out := t.TempDir()
	path, m, err := Backup(ctx, src, BackupOptions{OutDir: out, Textures: true})
	if err != nil {
		t.Fatal(err)
	}
	core.Close(ctx)
	if !m.HasDatabase || m.Textures != 1 || m.Migration == 0 {
		t.Fatalf("manifest %+v", m)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 { // it holds the secrets
		t.Fatalf("archive mode %v", info.Mode())
	}

	dst := Paths{DataDir: t.TempDir()}
	res, err := Restore(ctx, dst, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Textures != 1 || res.Database == "" {
		t.Fatalf("restored %+v", res)
	}
	if _, err := Restore(ctx, dst, path, false); !errors.Is(err, backup.ErrExists) {
		t.Fatalf("restore over an install: %v", err)
	}
	if _, err := Restore(ctx, dst, path, true); err != nil {
		t.Fatalf("--force: %v", err)
	}

	restored, err := OpenCore(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	if got := restored.Settings.Current().Localized("site.name", "en"); got != "Backed up" {
		t.Fatalf("site.name %q", got)
	}
	rs, _ := openStore(ctx, restored.Config.Storage, restored.Paths.DataDir)
	if _, err := rs.Stat(ctx, key); err != nil {
		t.Fatalf("texture: %v", err)
	}
	token, _ := os.ReadFile(secrets.AdminTokenFile(restored.Paths.DataDir))
	if strings.TrimSpace(string(token)) != sec.AdminToken {
		t.Fatal("admin token not restored")
	}
}

func TestBackupKeep(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"skinloom-20261029-100000.tar.gz", "skinloom-20261030-100000.tar.gz", "skinloom-20261031-100000.tar.gz", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := prune(dir, 2); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, ",") != "notes.txt,skinloom-20261030-100000.tar.gz,skinloom-20261031-100000.tar.gz" {
		t.Fatalf("left %v", left)
	}
}
