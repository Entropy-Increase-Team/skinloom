package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
)

func writeUnsignedTestSLP(t *testing.T, dir string) string {
	t.Helper()
	files := map[string][]byte{
		"plugin.json": []byte(`{
			"id": "cli-demo",
			"kind": "plugin",
			"version": "1.0.0",
			"name": {"en": "CLI Demo"},
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv", "users.ban"]
		}`),
		"server.js": []byte(`export default { setup() {} };`),
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	var cs strings.Builder
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(files[name])
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&cs, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	cw, _ := zw.Create("checksums.txt")
	_, _ = cw.Write([]byte(cs.String()))
	_ = zw.Close()

	out := filepath.Join(dir, "cli-demo-1.0.0.slp")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPluginCLIWorkflow(t *testing.T) {
	dataDir := t.TempDir()
	slpPath := writeUnsignedTestSLP(t, t.TempDir())
	paths := &app.Paths{DataDir: dataDir, ConfigPath: filepath.Join(dataDir, "config.toml")}

	run := func(stdin string, args ...string) (string, error) {
		cmd := pluginCmd(paths)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if stdin != "" {
			cmd.SetIn(strings.NewReader(stdin))
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	// Unsigned install without --allow-unsigned must fail.
	if _, err := run("", "install", slpPath, "--yes"); err == nil {
		t.Fatal("expected install without --allow-unsigned to fail")
	}

	// Interactive confirmation matches plugin ID.
	if out, err := run("cli-demo\n", "install", slpPath, "--allow-unsigned"); err != nil {
		t.Fatalf("install failed: %v\nout=%s", err, out)
	} else if !strings.Contains(out, "installed cli-demo v1.0.0") {
		t.Fatalf("unexpected install output: %s", out)
	}

	if out, err := run("", "list"); err != nil || !strings.Contains(out, "cli-demo") || !strings.Contains(out, "enabled") {
		t.Fatalf("list failed: %v out=%s", err, out)
	}
	if out, err := run("", "disable", "cli-demo"); err != nil || !strings.Contains(out, "disabled cli-demo") {
		t.Fatalf("disable failed: %v out=%s", err, out)
	}
	if out, err := run("", "enable", "cli-demo"); err != nil || !strings.Contains(out, "enabled cli-demo") {
		t.Fatalf("enable failed: %v out=%s", err, out)
	}
	if out, err := run("", "reload", "cli-demo"); err != nil || !strings.Contains(out, "reloaded cli-demo") {
		t.Fatalf("reload failed: %v out=%s", err, out)
	}
	if out, err := run("", "reload"); err != nil || !strings.Contains(out, "config_version=") {
		t.Fatalf("reload all failed: %v out=%s", err, out)
	}
	if out, err := run("", "remove", "cli-demo", "--purge", "--yes"); err != nil || !strings.Contains(out, "removed cli-demo") {
		t.Fatalf("remove failed: %v out=%s", err, out)
	}
}
