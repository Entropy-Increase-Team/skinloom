package config

import (
	"os"
	"path/filepath"
	"reflect"
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
