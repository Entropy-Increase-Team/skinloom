package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

// Atlas changes a SQLite table it cannot ALTER by rebuilding it: PRAGMA foreign_keys =
// off, create new_<table>, copy, DROP TABLE <table>, rename. goose runs every file in
// a transaction, where that PRAGMA does nothing, and SkinLoom opens SQLite with foreign
// keys on: the DROP TABLE then deletes the old rows first, cascading into every table
// that references them (dropping users would empty sessions, closets, players, the
// score ledger). Such a migration must be rewritten by hand (ADD COLUMN, a new table)
// before it is committed.
func TestSQLiteMigrationsDoNotRebuildTables(t *testing.T) {
	dir, err := FS("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(dir, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations: %v", err)
	}
	for _, name := range files {
		data, err := fs.ReadFile(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(data), "-- +goose Down")
		for _, bad := range []string{"PRAGMA foreign_keys", "CREATE TABLE `new_"} {
			if strings.Contains(up, bad) {
				t.Errorf("%s rebuilds a table (%q): rewrite it without DROP TABLE of a referenced table", name, bad)
			}
		}
	}
}
