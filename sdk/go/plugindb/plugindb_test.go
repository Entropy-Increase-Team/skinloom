package plugindb

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSafePrefixAndTableValidation(t *testing.T) {
	prefix, err := SafePrefix("daily-checkin")
	if err != nil {
		t.Fatalf("SafePrefix failed: %v", err)
	}
	if prefix != "plg_daily_checkin_" {
		t.Fatalf("got prefix %q, want plg_daily_checkin_", prefix)
	}
	if _, err := SafePrefix("../evil"); err == nil {
		t.Fatal("expected invalid plugin id error")
	}

	db, _, err := OpenMemory("daily-checkin")
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer db.Close()

	if got := db.Table("records"); got != "plg_daily_checkin_records" {
		t.Fatalf("got table %q, want plg_daily_checkin_records", got)
	}
	if _, err := db.SafeTable("bad;DROP TABLE users"); err == nil {
		t.Fatal("expected invalid table name error")
	}
	if _, err := db.SafeTable("UpperCase"); err == nil {
		t.Fatal("expected uppercase table name rejection")
	}
}

func TestOpenRejectsUnsafeConfig(t *testing.T) {
	if _, err := Open(Config{Driver: "sqlite", DSN: ":memory:", Prefix: "users_"}); err == nil {
		t.Fatal("expected unsafe prefix error")
	}
	if _, err := Open(Config{Driver: "oracle", DSN: "x", Prefix: "plg_demo_"}); err == nil {
		t.Fatal("expected unsupported driver error")
	}
	if _, err := Open(Config{Driver: "postgres", DSN: "postgres://x", Prefix: "plg_demo_", Schema: "public"}); err == nil {
		t.Fatal("expected unsafe schema error")
	}
}

func TestRebindPostgresPlaceholders(t *testing.T) {
	pg := &DB{dialect: "postgres", prefix: "plg_demo_"}
	in := `SELECT * FROM {{prefix}}items WHERE note = 'what? ''really?''' AND id = ? AND status = ?`
	got := pg.PrepareQuery(in)
	want := `SELECT * FROM plg_demo_items WHERE note = 'what? ''really?''' AND id = $1 AND status = $2`
	if got != want {
		t.Fatalf("PrepareQuery mismatch:\n got: %s\nwant: %s", got, want)
	}

	// Verify comments, double-quoted identifiers, dollar-quoted strings, and JSONB ?? / ?| / ?& operators are handled properly.
	complexSQL := `SELECT "col?name", $tag$dollar?body$tag$ FROM {{prefix}}items -- line comment?
/* block comment? */ WHERE meta ?? 'key' AND meta ?| array['a'] AND id = ?`
	gotComplex := pg.PrepareQuery(complexSQL)
	wantComplex := `SELECT "col?name", $tag$dollar?body$tag$ FROM plg_demo_items -- line comment?
/* block comment? */ WHERE meta ? 'key' AND meta ?| array['a'] AND id = $1`
	if gotComplex != wantComplex {
		t.Fatalf("complex PrepareQuery mismatch:\n got: %s\nwant: %s", gotComplex, wantComplex)
	}

	sq := &DB{dialect: "sqlite", prefix: "plg_demo_"}
	gotSQ := sq.PrepareQuery(`SELECT * FROM {{prefix}}items WHERE id = ?`)
	if gotSQ != `SELECT * FROM plg_demo_items WHERE id = ?` {
		t.Fatalf("unexpected sqlite query: %s", gotSQ)
	}
}

func TestMigrateAndCRUDWithPrefixIsolation(t *testing.T) {
	ctx := context.Background()
	db, _, err := OpenMemory("audit-helper")
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer db.Close()

	migFS := fstest.MapFS{
		"migrations/001_init.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
CREATE TABLE {{prefix}}events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	note TEXT NOT NULL
);

-- +goose Down
DROP TABLE {{prefix}}events;
`),
		},
		"migrations/002_add_tag.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
ALTER TABLE {{prefix}}events ADD COLUMN tag TEXT NOT NULL DEFAULT '';

-- +goose Down
SELECT 1;
`),
		},
	}

	if err := db.Migrate(ctx, migFS, "migrations"); err != nil {
		t.Fatalf("Migrate first run: %v", err)
	}
	// Running a second time must be idempotent.
	if err := db.Migrate(ctx, migFS, "migrations"); err != nil {
		t.Fatalf("Migrate second run: %v", err)
	}

	// Verify plugin-scoped goose_db_version table exists and core goose_db_version does NOT.
	var tables []string
	rows, err := db.SQL().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()

	joined := strings.Join(tables, ",")
	if !strings.Contains(joined, "plg_audit_helper_events") {
		t.Fatalf("missing plugin table in %v", tables)
	}
	if !strings.Contains(joined, "plg_audit_helper_goose_db_version") {
		t.Fatalf("missing prefixed goose version table in %v", tables)
	}
	for _, tbl := range tables {
		if tbl == "goose_db_version" {
			t.Fatalf("must not create unprefixed goose_db_version table: %v", tables)
		}
	}

	// Exercise Exec, QueryRow, and Tx.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO {{prefix}}events (note, tag) VALUES (?, ?)`, "hello", "boot"); err != nil {
		t.Fatalf("tx.Exec: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}

	var note, tag string
	if err := db.QueryRow(ctx, `SELECT note, tag FROM `+db.Table("events")+` WHERE id = ?`, 1).Scan(&note, &tag); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if note != "hello" || tag != "boot" {
		t.Fatalf("got (%q, %q), want (hello, boot)", note, tag)
	}
}

func TestMigrateDialectSubdirOverride(t *testing.T) {
	ctx := context.Background()
	db, _, err := OpenMemory("dialect-demo")
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer db.Close()

	migFS := fstest.MapFS{
		"migrations/sqlite/001_sqlite.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
CREATE TABLE {{prefix}}sqlite_only (id INTEGER PRIMARY KEY);
`),
		},
		"migrations/postgres/001_pg.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
CREATE TABLE {{prefix}}pg_only (id BIGSERIAL PRIMARY KEY);
`),
		},
	}

	if err := db.Migrate(ctx, migFS, "migrations"); err != nil {
		t.Fatalf("Migrate dialect subdir: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO {{prefix}}sqlite_only (id) VALUES (?)`, 42); err != nil {
		t.Fatalf("insert into sqlite_only failed: %v", err)
	}
}
