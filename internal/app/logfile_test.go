package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// OpenCore opened log.file before the database, and its later error returns closed
// the database but not the log file; on Windows the leaked handle kept the file
// locked for the life of the process.
func TestOpenCoreClosesLogFileOnError(t *testing.T) {
	dir := t.TempDir()
	cfg := "[database]\ndriver = \"nosuchdb\"\n\n[log]\nfile = \"logs/x.log\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCore(context.Background(), Paths{DataDir: dir, Serve: true}); err == nil {
		t.Fatal("OpenCore with an unknown database driver succeeded")
	}
	logPath := filepath.Join(dir, "logs", "x.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("the serving process did not open its log file: %v", err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Errorf("log file still held open after OpenCore failed: %v", err)
	}
}

// Only the serving process writes the log file: a CLI command run next to it
// (skinloom settings, backup ...) opening it would rotate the file the server holds
// open, which fails on Windows and loses the server's lines elsewhere.
func TestOnlyTheServerOpensTheLogFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[log]\nfile = \"logs/x.log\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenCore(context.Background(), Paths{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	c.Close(context.Background())
	if _, err := os.Stat(filepath.Join(dir, "logs", "x.log")); err == nil {
		t.Error("a CLI command opened the log file")
	}
}
