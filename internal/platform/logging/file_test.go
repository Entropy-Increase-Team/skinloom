package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFileRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "skinloom.log")
	f, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("first\n")
	f.Close()
	if f, err = OpenFile(path); err != nil { // small: appended to
		t.Fatal(err)
	}
	f.WriteString("second\n")
	f.Close()
	if b, _ := os.ReadFile(path); string(b) != "first\nsecond\n" {
		t.Fatalf("appended %q", b)
	}

	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), RotateAt+1), 0o640); err != nil {
		t.Fatal(err)
	}
	if f, err = OpenFile(path); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if info, _ := os.Stat(path); info.Size() != 0 {
		t.Fatalf("new file has %d bytes", info.Size())
	}
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != RotateAt+1 {
		t.Fatalf("rotated: %v", err)
	}
}

type broken struct{}

func (broken) Write([]byte) (int, error) { return 0, errors.New("no console") }

// A service has no console: the file must still get every line, in either console form.
func TestFileSurvivesNoConsole(t *testing.T) {
	for _, pretty := range []bool{false, true} {
		var file bytes.Buffer
		_, log := New(Options{Console: broken{}, File: &file, Level: "info", Format: "text", Pretty: pretty})
		log.Info("hello")
		if !strings.Contains(file.String(), "msg=hello") {
			t.Fatalf("pretty=%v: %q", pretty, file.String())
		}
	}
}
