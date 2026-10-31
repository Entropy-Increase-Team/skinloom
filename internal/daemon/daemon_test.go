package daemon

import (
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	o := Options{Exe: "/opt/sky loom/skinloom", DataDir: "/var/lib/skinloom", User: "skinloom"}
	unit := SystemdUnit(o)
	for _, want := range []string{
		`ExecStart="/opt/sky loom/skinloom" service run --data /var/lib/skinloom` + "\n",
		"User=skinloom\nGroup=skinloom\n",
		"WorkingDirectory=" + o.dir() + "\n",
		// A path with a space is quoted: systemd splits the list on spaces.
		"ReadWritePaths=" + systemdPath("-"+o.writable()[0]) + " " + systemdPath("-"+o.writable()[1]) + "\n",
		` "-`, // the frontend cache path has the space
		"ProtectSystem=strict\n",
		"Restart=on-failure\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	// The library runs the unit through text/template; it must come back unchanged.
	if strings.Contains(unit, "{{") {
		t.Fatal("template braces in the unit")
	}
	if u := SystemdUnit(Options{Exe: "/opt/skinloom/skinloom", DataDir: "/opt/skinloom/data"}); strings.Contains(u, "User=") {
		t.Fatal("no user: runs as root without a User= line")
	}
}

func TestArgs(t *testing.T) {
	o := Options{Exe: "/x/skinloom", DataDir: "/d", ConfigPath: "/etc/skinloom.toml"}
	if got := strings.Join(o.args(), " "); got != "service run --data /d --config /etc/skinloom.toml" {
		t.Fatalf("args %q", got)
	}
	if got := systemdQuote([]string{`a"b`, "100%", "$HOME"}); got != `"a\"b" "100%%" "$$HOME"` {
		t.Fatalf("quoted %s", got)
	}
}

func TestInstallRefusesGoRun(t *testing.T) {
	if err := Install(Options{Exe: "/tmp/go-build123/b001/exe/skinloom", DataDir: t.TempDir()}); err == nil {
		t.Fatal("installed a go run binary")
	}
}
