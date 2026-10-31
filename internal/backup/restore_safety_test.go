package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

func sqliteManifest() string {
	m, _ := json.Marshal(Manifest{Format: Format, HasDatabase: true, Database: "sqlite"})
	return string(m)
}

// "secrets/.." used to match secretName: the joined path is the data directory itself,
// and writeFile created <DataDir>.restoring next to it, outside the data directory.
func TestRestoreRefusesDotDotSecret(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "data")
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o750); err != nil {
		t.Fatal(err)
	}
	buf := archive(t, map[string]string{manifestName: sqliteManifest(), "secrets/..": "attacker"}, manifestName, "secrets/..")
	_, err := Restore(context.Background(), buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")})
	if err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Errorf("secrets/.. was not refused as an unexpected entry: %v", err)
	}
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.Name() != "data" {
			t.Errorf("restore wrote %s outside the data directory", filepath.Join(parent, e.Name()))
		}
	}
}

// ADR 0032/0034: a target that already has a database is refused without --force,
// before anything is written. The check used to run only when the database entry was
// reached, after config.toml and the live install's secrets had been overwritten.
func TestRefusedRestoreWritesNothing(t *testing.T) {
	dir := t.TempDir()
	// An install whose database exists (its config lives elsewhere or was lost).
	if err := os.WriteFile(filepath.Join(dir, "skinloom.db"), []byte("live db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets", "admin-token"), []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "[database]\ndriver = \"sqlite\"\ndsn = \"skinloom.db\"\n"
	order := []string{manifestName, configName, "secrets/admin-token", databaseName}
	buf := archive(t, map[string]string{manifestName: sqliteManifest(), configName: cfg,
		"secrets/admin-token": "from-archive", databaseName: "archived db"}, order...)
	_, err := Restore(context.Background(), buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "secrets", "admin-token")); string(b) != "live" {
		t.Errorf("a refused restore replaced the live admin token with %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err == nil {
		t.Errorf("a refused restore wrote config.toml")
	}
}

// db.Open takes "sqlite3" as SQLite, and so does Restore: an install configured with
// driver = "sqlite3" can be restored.
func TestRestoreSqlite3Driver(t *testing.T) {
	dir := t.TempDir()
	cfg := "[database]\ndriver = \"sqlite3\"\ndsn = \"skinloom.db\"\n"
	if c, err := config.Load(writeTemp(t, cfg)); err != nil || c.Database.Driver != "sqlite3" {
		t.Fatalf("config with sqlite3: %v %q", err, c.Database.Driver)
	}
	buf := archive(t, map[string]string{manifestName: sqliteManifest(), configName: cfg, databaseName: "db"},
		manifestName, configName, databaseName)
	if _, err := Restore(context.Background(), buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml")}); err != nil {
		t.Errorf("restore with driver sqlite3: %v", err)
	}
}

func writeTemp(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memStore) Put(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
	return nil
}
func (m *memStore) Open(context.Context, string) (storage.File, error) {
	return nil, storage.ErrNotFound
}
func (m *memStore) Stat(context.Context, string) (time.Time, error) { return time.Time{}, nil }
func (m *memStore) Walk(context.Context, func(string, time.Time) error) error {
	return nil
}
func (m *memStore) Delete(context.Context, string) error { return nil }

// A texture entry bigger than maxTexture is refused, not cut to 64 MB and stored
// under a hash it no longer has.
func TestRestoreRefusesOversizedTexture(t *testing.T) {
	big := make([]byte, maxTexture+10)
	sum := sha256.Sum256(big)
	key := hex.EncodeToString(sum[:])
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	put := func(name string, body []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(body)
	}
	put(manifestName, []byte(sqliteManifest()))
	put("textures/"+key, big)
	tw.Close()
	gz.Close()

	dir := t.TempDir()
	store := &memStore{data: map[string][]byte{}}
	_, err := Restore(context.Background(), &buf, Target{DataDir: dir, ConfigPath: filepath.Join(dir, "config.toml"),
		OpenStore: func(context.Context, config.Config) (storage.Store, error) { return store, nil }})
	if got := len(store.data[key]); err == nil && got != len(big) {
		t.Errorf("texture of %d bytes restored as %d bytes without an error", len(big), got)
	}
}

// walkThenUpload lists the textures, then a texture is uploaded and recorded in the
// database (as an upload racing the backup does) before Write takes the snapshot.
type walkThenUpload struct {
	*storage.Local
	d   *db.DB
	key string
}

func (w walkThenUpload) Walk(ctx context.Context, fn func(string, time.Time) error) error {
	if err := w.Local.Walk(ctx, fn); err != nil {
		return err
	}
	if err := w.Local.Put(ctx, w.key, []byte("new upload")); err != nil {
		return err
	}
	_, err := w.d.SQL.ExecContext(ctx, `INSERT INTO racing_uploads(hash) VALUES (?)`, w.key)
	return err
}

// Write snapshots the database before it lists the textures: listed first, a texture
// uploaded in between was referenced by the archived database but missing from the
// archive. Extra files are harmless (the orphan sweep drops them).
func TestBackupHoldsEveryReferencedTexture(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "skinloom.db", DataDir: dir, MaxOpenConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := migrate.Up(ctx, d, dir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `CREATE TABLE racing_uploads(hash TEXT)`); err != nil {
		t.Fatal(err)
	}
	local, err := storage.NewLocal(filepath.Join(dir, "textures"))
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("cd", 32)
	var out bytes.Buffer
	if _, err := Write(ctx, &out, Sources{DB: d, Store: walkThenUpload{Local: local, d: d, key: key}, DataDir: dir,
		ConfigPath: filepath.Join(dir, "config.toml"), Now: time.Now()}); err != nil {
		t.Fatal(err)
	}

	gz, err := gzip.NewReader(&out)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hasTexture := false
	snap := filepath.Join(t.TempDir(), "snap.db")
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch hdr.Name {
		case "textures/" + key:
			hasTexture = true
		case databaseName:
			b, _ := io.ReadAll(tr)
			if err := os.WriteFile(snap, b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	sd, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: snap, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	var n int
	if err := sd.SQL.QueryRowContext(ctx, `SELECT count(*) FROM racing_uploads WHERE hash = ?`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 1 && !hasTexture {
		t.Errorf("the archived database references texture %s, which the archive does not hold", key[:8])
	}
}
