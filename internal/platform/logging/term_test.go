package logging

import (
	"os"
	"path/filepath"
	"testing"
)

// A log file or a pipe is not a terminal: it gets key=value lines and no colors.
func TestConsoleStyleOfAFile(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if pretty, color := ConsoleStyle(f); pretty || color {
		t.Errorf("file: pretty=%v color=%v", pretty, color)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if pretty, color := ConsoleStyle(w); pretty || color {
		t.Errorf("pipe: pretty=%v color=%v", pretty, color)
	}
	if pretty, _ := ConsoleStyle(nil); pretty {
		t.Error("nil file")
	}
}

// FORCE_COLOR (https://force-color.org) asks for the readable, colored form where it
// would not be chosen (docker compose logs, CI); NO_COLOR still turns colors off.
func TestForceColor(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	if pretty, color := ConsoleStyle(f); !pretty || !color {
		t.Errorf("FORCE_COLOR: pretty=%v color=%v", pretty, color)
	}
	t.Setenv("NO_COLOR", "1")
	if pretty, color := ConsoleStyle(f); !pretty || color {
		t.Errorf("FORCE_COLOR with NO_COLOR: pretty=%v color=%v", pretty, color)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "0")
	if pretty, _ := ConsoleStyle(f); pretty {
		t.Error("FORCE_COLOR=0 forced the readable form")
	}
}
