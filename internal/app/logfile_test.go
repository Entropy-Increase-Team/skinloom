package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A step after the database opened (here the texture store) failing must close the
// database too: on Windows an open SQLite file cannot be deleted or replaced.
func TestOpenCoreClosesDatabaseOnLateError(t *testing.T) {
	dir := t.TempDir()
	cfg := "[storage]\ndriver = \"s3\"\nendpoint = \"not/an/endpoint\"\nbucket = \"x\"\naccess_key = \"k\"\nsecret_key = \"s\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenCore(context.Background(), Paths{DataDir: dir})
	if err == nil || !strings.Contains(err.Error(), "s3") {
		t.Fatalf("OpenCore with a bad S3 endpoint: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.db"))
	if len(matches) == 0 {
		t.Fatal("the database was not created before the failing step")
	}
	for _, f := range matches {
		if err := os.Remove(f); err != nil {
			t.Errorf("database still open after OpenCore failed: %v", err)
		}
	}
}
