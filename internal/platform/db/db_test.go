package db

import (
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
