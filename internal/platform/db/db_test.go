package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// The MySQL options the schema and the code assume, whatever the operator's DSN says.
func TestMySQLDSN(t *testing.T) {
	out, err := mysqlDSN("root:pass@tcp(127.0.0.1:3306)/skinloom?parseTime=false&loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatalf("the DSN we build must parse again: %v (%s)", err, out)
	}
	if !cfg.ParseTime || cfg.Loc.String() != "UTC" || !strings.Contains(out, "charset=utf8mb4") {
		t.Fatalf("options: %+v", cfg)
	}
	// MySQL rounds sub-microsecond digits into datetime(6); truncating first makes
	// stored deadlines match the ones computed (and PostgreSQL's).
	if !strings.Contains(out, "timeTruncate=1µs") {
		t.Fatalf("times are not truncated to microseconds: %s", out)
	}
}

// A DSN with parameters keeps them, gets every pragma it does not set, and always has
// foreign keys on: it used to be taken as-is, which turned foreign keys (every
// cascade and SET NULL in the schema) off.
func TestSQLiteDSNKeepsRequiredPragmas(t *testing.T) {
	out, err := sqliteDSN("file:/srv/skinloom.db?_pragma=busy_timeout(500)&_pragma=foreign_keys(0)&mode=rwc", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base, query, _ := strings.Cut(out, "?")
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	pragmas := strings.Join(q["_pragma"], " ")
	if base != "file:/srv/skinloom.db" || q.Get("mode") != "rwc" || q.Get("_txlock") != "immediate" ||
		!strings.Contains(pragmas, "busy_timeout(500)") || strings.Contains(pragmas, "busy_timeout(10000)") ||
		!strings.Contains(pragmas, "foreign_keys(1)") || strings.Contains(pragmas, "foreign_keys(0)") ||
		!strings.Contains(pragmas, "journal_mode(WAL)") {
		t.Fatalf("dsn %s", out)
	}
}

func TestProbeSQLiteStatesAndNoSideEffects(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.db")

	// Missing SQLite file must probe as empty without creating the file.
	res, err := Probe(ctx, Options{Driver: "sqlite", DSN: "new.db", DataDir: dir})
	if err != nil || !res.OK || res.State != ProbeEmpty || res.Version == "" {
		t.Fatalf("missing sqlite probe: res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("Probe must not create %s", dbPath)
	}

	// Suspected BS schema.
	bsPath := filepath.Join(dir, "bs.db")
	bsPool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(bsPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE bs_options (option_name TEXT, option_value TEXT)`,
		`CREATE TABLE bs_users (uid INTEGER PRIMARY KEY, email TEXT)`,
		`CREATE TABLE bs_players (pid INTEGER PRIMARY KEY, uid INTEGER, name TEXT)`,
	} {
		if _, err := bsPool.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	bsPool.Close()

	res, err = Probe(ctx, Options{Driver: "sqlite", DSN: bsPath, DataDir: dir})
	if err != nil || !res.OK || res.State != ProbeSuspectedBS {
		t.Fatalf("bs sqlite probe: res=%+v err=%v", res, err)
	}

	// Installed SkinLoom schema.
	slPath := filepath.Join(dir, "sl.db")
	slPool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(slPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE system_state ("key" TEXT PRIMARY KEY, int_value INTEGER)`,
		`INSERT INTO system_state ("key", int_value) VALUES ('setup.completed', 1)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY)`,
		`INSERT INTO users (id) VALUES (1), (2)`,
		`CREATE TABLE textures (id INTEGER PRIMARY KEY)`,
		`INSERT INTO textures (id) VALUES (1)`,
		`CREATE TABLE players (id INTEGER PRIMARY KEY)`,
	} {
		if _, err := slPool.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	slPool.Close()

	res, err = Probe(ctx, Options{Driver: "sqlite", DSN: slPath, DataDir: dir})
	if err != nil || !res.OK || res.State != ProbeSkinLoomInstalled || res.Users != 2 || res.Textures != 1 {
		t.Fatalf("installed sqlite probe: res=%+v err=%v", res, err)
	}
}

func TestMissingDatabaseHints(t *testing.T) {
	name, ok := missingDatabaseName(MySQL, "user:pass@tcp(127.0.0.1:3306)/skinloom_prod", &mysql.MySQLError{Number: 1049})
	if !ok || name != "skinloom_prod" {
		t.Fatalf("mysql missing db: ok=%v name=%q", ok, name)
	}
	if sqlHint := createDatabaseSQL(MySQL, name); !strings.Contains(sqlHint, "`skinloom_prod`") {
		t.Fatalf("mysql hint: %s", sqlHint)
	}
	name, ok = missingDatabaseName(Postgres, "postgres://user:pass@127.0.0.1:5432/skinloom_pg?sslmode=disable", &pgconn.PgError{Code: "3D000"})
	if !ok || name != "skinloom_pg" {
		t.Fatalf("pg missing db: ok=%v name=%q", ok, name)
	}
	if sqlHint := createDatabaseSQL(Postgres, name); !strings.Contains(sqlHint, `"skinloom_pg"`) {
		t.Fatalf("pg hint: %s", sqlHint)
	}
}
