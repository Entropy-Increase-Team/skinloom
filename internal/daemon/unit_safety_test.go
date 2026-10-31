package daemon

import (
	"strings"
	"testing"
)

// A line break in a path or user name would add directives to the unit file of a
// service run by root: New (behind install and every other command) refuses it.
func TestNewRefusesControlCharacters(t *testing.T) {
	for name, o := range map[string]Options{
		"data":      {Exe: "/opt/skinloom/skinloom", DataDir: "/srv/x\nExecStartPre=/bin/sh -c id"},
		"exe":       {Exe: "/opt/a\nExecStartPre=/bin/sh -c id/skinloom", DataDir: "/srv/x"},
		"user":      {Exe: "/opt/skinloom/skinloom", DataDir: "/srv/x", User: "skinloom\nExecStartPre=/bin/sh -c id"},
		"user name": {Exe: "/opt/skinloom/skinloom", DataDir: "/srv/x", User: "a b"},
	} {
		if _, err := New(o, nil); err == nil {
			t.Errorf("%s: accepted %+v", name, o)
		}
	}
}

// "$$" is how ExecStart= escapes "$", but ReadWritePaths= does no variable expansion:
// a doubled "$" there names another directory, which ProtectSystem=strict then keeps
// the service from writing.
func TestSystemdReadWritePathsKeepDollar(t *testing.T) {
	unit := SystemdUnit(Options{Exe: "/opt/skinloom/skinloom", DataDir: "/srv/a$b%c"})
	if !strings.Contains(unit, "ReadWritePaths=-/srv/a$b%%c ") {
		t.Errorf("ReadWritePaths does not name the data directory:\n%s", unit)
	}
}

// ProtectHome=true hides /home, /root and /run/user from the service, which could
// then not even start a bundle unpacked there: read-only instead.
func TestSystemdUnitBundleInHome(t *testing.T) {
	for exe, want := range map[string]string{
		"/opt/skinloom/skinloom":        "ProtectHome=true\n",
		"/home/steve/skinloom/skinloom": "ProtectHome=read-only\n",
		"/root/skinloom/skinloom":       "ProtectHome=read-only\n",
	} {
		dir := exe[:strings.LastIndex(exe, "/")]
		if unit := SystemdUnit(Options{Exe: exe, DataDir: dir + "/data"}); !strings.Contains(unit, want) {
			t.Errorf("%s: want %q in\n%s", exe, want, unit)
		}
	}
}
