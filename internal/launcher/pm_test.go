package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeLookPath(installed ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range installed {
			if n == name {
				return "/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestFindPackageManagerPrefersBun(t *testing.T) {
	cases := []struct {
		want      string
		installed []string
		got       string
		err       bool
	}{
		{"auto", []string{"npm", "pnpm", "bun"}, "bun", false},
		{"auto", []string{"npm", "pnpm"}, "pnpm", false},
		{"auto", []string{"npm"}, "npm", false},
		{"", []string{"npm"}, "npm", false},
		{"auto", nil, "", true},
		{"pnpm", []string{"bun", "pnpm"}, "pnpm", false},
		{"bun", []string{"npm"}, "", true},
		{"yarn", []string{"yarn"}, "", true},
	}
	for _, c := range cases {
		name, path, err := FindPackageManager(c.want, fakeLookPath(c.installed...))
		if c.err != (err != nil) || name != c.got {
			t.Errorf("FindPackageManager(%q, %v) = %q, %v; want %q (err %v)", c.want, c.installed, name, err, c.got, c.err)
		}
		if err == nil && path != "/bin/"+name {
			t.Errorf("path %q", path)
		}
	}
}

func TestDevArgs(t *testing.T) {
	cases := map[string][]string{
		"bun":  {"run", "--silent", "dev", "--port", "3100", "--hostname", "127.0.0.1"},
		"pnpm": {"run", "dev", "--port", "3100", "--hostname", "127.0.0.1"},
		"npm":  {"run", "dev", "--", "--port", "3100", "--hostname", "127.0.0.1"},
	}
	for pm, want := range cases {
		if got := DevArgs(pm, 3100); !reflect.DeepEqual(got, want) {
			t.Errorf("DevArgs(%s) = %v, want %v", pm, got, want)
		}
	}
}

func TestDevCommandNeedsInstalledDependencies(t *testing.T) {
	dir := t.TempDir()
	_, err := devCommand(Spec{Mode: "dev", DevDir: dir, PackageManager: "auto", Port: 3000})
	if err == nil {
		t.Skip("no package manager on this machine")
	}
	if !strings.Contains(err.Error(), "install") {
		t.Fatalf("want an install hint, got %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, err := devCommand(Spec{Mode: "dev", DevDir: dir, PackageManager: "auto", Port: 3000})
	if err != nil {
		t.Skipf("no package manager on this machine: %v", err)
	}
	if cmd.Dir != filepath.Join(dir, "apps", "web") {
		t.Fatalf("dev command runs in %s, want web/apps/web", cmd.Dir)
	}
}
