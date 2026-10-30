package db

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
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
