// Command migdiff writes a new versioned migration (goose format) for one dialect by
// diffing ent/schema against the replayed migration directory on a dev database.
//
//	go run ./internal/tools/migdiff -dialect sqlite -name add_users
//	go run ./internal/tools/migdiff -dialect mysql -dev "mysql://root:pass@localhost:3306/dev" -name add_users
//	go run ./internal/tools/migdiff -dialect postgres -dev "postgres://u:p@localhost:5432/dev?sslmode=disable" -name add_users
//
// SQLite uses an in-memory dev database; MySQL and PostgreSQL need a disposable, empty
// database (CI provides them as service containers). Commit the generated files.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"path/filepath"

	atlasmigrate "ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/sqltool"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	sqlite "modernc.org/sqlite"

	"github.com/Entropy-Increase-Team/skinloom/ent/migrate"
)

func init() {
	// Atlas' SQLite driver opens "sqlite3"; serve it with the pure-Go driver.
	sql.Register("sqlite3", &sqlite.Driver{})
}

func main() {
	var (
		name    = flag.String("name", "", "migration name, e.g. add_users")
		which   = flag.String("dialect", "sqlite", "sqlite | mysql | postgres")
		devURL  = flag.String("dev", "", "dev database URL (required for mysql/postgres)")
		rootDir = flag.String("dir", "migrations", "migrations root directory")
		rehash  = flag.Bool("rehash", false, "only recompute atlas.sum for -dialect (after adding a file by hand)")
	)
	flag.Parse()
	if *rehash {
		dir, err := sqltool.NewGooseDir(filepath.Join(*rootDir, *which))
		if err != nil {
			log.Fatalf("open migration dir: %v", err)
		}
		sum, err := dir.Checksum()
		if err != nil {
			log.Fatalf("checksum: %v", err)
		}
		if err := atlasmigrate.WriteSumFile(dir, sum); err != nil {
			log.Fatalf("write atlas.sum: %v", err)
		}
		return
	}
	if *name == "" {
		log.Fatal("-name is required")
	}
	var d string
	switch *which {
	case "sqlite":
		d = dialect.SQLite
		if *devURL == "" {
			*devURL = "sqlite://dev?mode=memory&_pragma=foreign_keys(1)"
		}
	case "mysql":
		d = dialect.MySQL
	case "postgres":
		d = dialect.Postgres
	default:
		log.Fatalf("unknown dialect %q", *which)
	}
	if *devURL == "" {
		log.Fatalf("-dev is required for %s", *which)
	}
	dir, err := sqltool.NewGooseDir(filepath.Join(*rootDir, *which))
	if err != nil {
		log.Fatalf("open migration dir: %v", err)
	}
	err = migrate.NamedDiff(context.Background(), *devURL, *name,
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(d),
		schema.WithFormatter(sqltool.GooseFormatter),
		schema.WithDropColumn(true),
		schema.WithDropIndex(true),
	)
	if err != nil {
		log.Fatalf("generate migration: %v", err)
	}
}
