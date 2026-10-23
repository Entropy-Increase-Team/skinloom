package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidatePorts(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		c := Defaults()
		c.Web.Port = port
		if err := c.Validate(); err == nil {
			t.Errorf("web.port %d: want error", port)
		}
	}
	c := Defaults()
	c.Web.Port = 0 // 0 = pick a free port
	if err := c.Validate(); err != nil {
		t.Errorf("web.port 0 must be accepted: %v", err)
	}
}

func TestValidateTrustedProxies(t *testing.T) {
	c := Defaults()
	c.Server.TrustedProxies = []string{"10.0.0.0/8", "bogus"}
	if err := c.Validate(); err == nil {
		t.Fatal("invalid trusted proxy CIDR must be rejected")
	}
}

// A typo in config.toml used to be silently ignored.
func TestUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[server]\nlisten = \":8080\"\nlisen = \":9\"\n\n[databse]\ndsn = \"x\"\n\n[web]\nport = 3000\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := UnknownKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"databse.dsn", "server.lisen"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UnknownKeys = %v, want %v", got, want)
	}

	if _, err := WriteTemplate(filepath.Join(t.TempDir(), "c.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateHasNoUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	got, err := UnknownKeys(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("template has unknown keys %v (err %v)", got, err)
	}
}
