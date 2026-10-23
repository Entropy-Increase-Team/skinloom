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

func TestValidatePackageManager(t *testing.T) {
	for _, pm := range []string{"", "auto", "bun", "pnpm", "npm"} {
		c := Defaults()
		c.Web.PackageManager = pm
		if err := c.Validate(); err != nil {
			t.Errorf("web.package_manager %q must be accepted: %v", pm, err)
		}
	}
	for _, pm := range []string{"yarn", "Bun"} {
		c := Defaults()
		c.Web.PackageManager = pm
		if err := c.Validate(); err == nil {
			t.Errorf("web.package_manager %q: want error", pm)
		}
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

// The defaults avoid the four-digit ports other software on the same host very often
// already uses (8080, 3000, ...), and stay below the ephemeral (Linux 32768+, Windows
// 49152+) and Kubernetes NodePort (30000-32767) ranges: 1 + "SKIN" on a phone keypad,
// with the frontend next to it.
func TestDefaultPorts(t *testing.T) {
	c := Defaults()
	if c.Server.Listen != ":17546" || c.Web.Port != 17547 {
		t.Fatalf("defaults = %q / %d, want :17546 / 17547", c.Server.Listen, c.Web.Port)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Server.Listen != ":17546" || got.Web.Port != 17547 {
		t.Fatalf("template = %q / %d (%v)", got.Server.Listen, got.Web.Port, err)
	}
}

// Container and Kubernetes deployments set list keys through the environment, e.g.
// the pod network as trusted proxies behind an ingress controller.
func TestListFromEnvironment(t *testing.T) {
	t.Setenv("SKINLOOM_SERVER__TRUSTED_PROXIES", "10.42.0.0/16, 10.43.0.0/16")
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"10.42.0.0/16", "10.43.0.0/16"}; !reflect.DeepEqual(c.Server.TrustedProxies, want) {
		t.Fatalf("trusted_proxies = %q, want %q", c.Server.TrustedProxies, want)
	}
}
