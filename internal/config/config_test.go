package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTemplateMatchesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if wrote, err := WriteTemplate(path); err != nil || !wrote {
		t.Fatalf("WriteTemplate: wrote=%v err=%v", wrote, err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Defaults()
	want.Server.TrustedProxies = []string{}
	if got.Server.TrustedProxies == nil {
		got.Server.TrustedProxies = []string{}
	}
	got.Web.DevDir = want.Web.DevDir // not in the template on purpose
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template drifted from defaults:\n got %+v\nwant %+v", got, want)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("SKINLOOM_DATABASE__DRIVER", "postgres")
	t.Setenv("SKINLOOM_SERVER__LISTEN", ":9000")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Driver != "postgres" || c.Server.Listen != ":9000" {
		t.Fatalf("env not applied: %+v", c)
	}
}

func TestDiffAndCold(t *testing.T) {
	a := Defaults()
	b := a
	b.Log.Level = "debug"
	b.Database.DSN = "other.db"
	changed := Diff(a, b)
	if !reflect.DeepEqual(changed, []string{"database.dsn", "log.level"}) {
		t.Fatalf("Diff = %v", changed)
	}
	if !IsCold("database.dsn") || IsCold("log.level") {
		t.Fatal("cold classification wrong")
	}
}

func TestInvalidRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(path, []byte("[server]\nrole = \"nope\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("want error for invalid role")
	}
}

// Regression: an absolute storage.path was joined under the data dir.
func TestStorageDir(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	abs := filepath.Join(t.TempDir(), "elsewhere")
	if got := (Storage{Path: "textures"}).Dir(data); got != filepath.Join(data, "textures") {
		t.Fatalf("relative: %s", got)
	}
	if got := (Storage{Path: abs}).Dir(data); got != abs {
		t.Fatalf("absolute: %s", got)
	}
}

func TestEnvOverridesAndWrite(t *testing.T) {
	t.Setenv("SKINLOOM_DATABASE__DRIVER", "mysql")
	t.Setenv("SKINLOOM_STORAGE__SECRET_KEY", "env-secret")
	t.Setenv("SKINLOOM_BS_DSN", "ignored-non-config-var")

	ov := EnvOverrides()
	if !ov["database.driver"] || !ov["storage.secret_key"] || ov["bs.dsn"] || ov["database.dsn"] {
		t.Fatalf("unexpected EnvOverrides: %v", ov)
	}

	c := Defaults()
	c.Server.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8"}
	c.Database.Driver = "mysql"
	c.Database.DSN = BuildMySQLDSN("db.internal", 3307, "skinloom", "app", "p@ss:w/ord#1\"'")
	c.Storage.Driver = "s3"
	c.Storage.Endpoint = "s3.example.com"
	c.Storage.Bucket = "skins"
	c.Storage.AccessKey = "ak"
	c.Storage.SecretKey = "env-secret"
	c.Storage.Prefix = "textures/"

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Write(path, c, ov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config.toml mode = %v, want 0600", info.Mode().Perm())
		}
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "env-secret") {
		t.Fatal("secret_key from env must not be written to config.toml")
	}
	if strings.Contains(string(raw), `driver = "mysql"`) {
		t.Fatal("env-overridden database.driver must not be written to config.toml")
	}
	if unk, err := UnknownKeys(path); err != nil || len(unk) != 0 {
		t.Fatalf("UnknownKeys = %v, err=%v", unk, err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Write: %v", err)
	}
	if loaded.Database.DSN != c.Database.DSN || loaded.Storage.Bucket != "skins" || len(loaded.Server.TrustedProxies) != 2 {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", loaded, c)
	}
}

func TestBuildDSNs(t *testing.T) {
	mysqlDSN := BuildMySQLDSN("127.0.0.1", 3306, "skin_db", "user", "p@ss#1")
	if !strings.Contains(mysqlDSN, "@tcp(127.0.0.1:3306)/skin_db") {
		t.Fatalf("unexpected mysql dsn: %s", mysqlDSN)
	}
	pgDSN := BuildPostgresDSN("pg.local", 5432, "skin_db", "user", "p@ss/1", "require")
	if !strings.HasPrefix(pgDSN, "postgres://user:p%40ss%2F1@pg.local:5432/skin_db?sslmode=require") {
		t.Fatalf("unexpected postgres dsn: %s", pgDSN)
	}
}
