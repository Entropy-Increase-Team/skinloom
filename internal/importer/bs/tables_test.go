package bs

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// Tables the import does not read and that hold rows are reported: plugins' data and
// unknown tables as left in BS (listed first, printed whole), tokens and logs as
// dropped, Laravel's bookkeeping and empty tables not at all.
func TestTablesLeftBehind(t *testing.T) {
	f := newFixture(t,
		`CREATE TABLE school_identities (id integer primary key, uid integer)`,
		`INSERT INTO school_identities (uid) VALUES (1), (2), (3)`,
		`CREATE TABLE textures_description (id integer primary key, tid integer, description varchar(255))`,
		`INSERT INTO textures_description (tid, description) VALUES (1, 'a'), (2, 'b')`,
		`CREATE TABLE migrations (id integer primary key, migration varchar(255))`,
		`INSERT INTO migrations (migration) VALUES ('2016_create_users')`,
		`CREATE TABLE oauth_access_tokens (id varchar(100) primary key)`,
		`INSERT INTO oauth_access_tokens (id) VALUES ('t1')`,
		`CREATE TABLE ygg_log (id integer primary key)`,
		`CREATE TABLE big_log (id integer primary key)`,
		`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 100005) INSERT INTO big_log SELECT x FROM c`,
	)
	ctx := context.Background()
	pool, err := f.src.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	d, err := f.src.read(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	r := &Report{}
	planTables(d, r)

	left := map[string]string{}
	for _, e := range r.LeftBehind() {
		left[e.Table] = e.Message
	}
	if len(left) != 3 || !strings.HasPrefix(left["school_identities"], "3 rows: SkinLoom does not know this table") ||
		!strings.HasPrefix(left["textures_description"], "2 rows: texture descriptions") ||
		!strings.HasPrefix(left["big_log"], "more than 100000 rows") {
		t.Errorf("left behind: %v", left)
	}
	if got := entries(r, Dropped); !strings.Contains(got, "oauth_access_tokens : 1 row: issued OAuth tokens") || strings.Contains(got, "ygg_log") || strings.Contains(got, "migrations") {
		t.Errorf("dropped:\n%s", got)
	}
	for _, e := range r.Entries {
		for _, read := range readTables {
			if e.Table == read {
				t.Errorf("a table the import reads is reported: %+v", e)
			}
		}
	}

	// The report lists what stays behind right after the summary, and only there.
	var text bytes.Buffer
	if err := r.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	out := text.String()
	head, rest, ok := strings.Cut(out, "LEFT IN BLESSING SKIN")
	if !ok || strings.Contains(head, "school_identities") || strings.Count(rest, "school_identities") != 1 {
		t.Errorf("report:\n%s", out)
	}
	if i, j := strings.Index(out, "LEFT IN BLESSING SKIN"), strings.Index(out, "\nDROPPED\n"); i < 0 || j < 0 || i > j {
		t.Errorf("the left-behind section is not first:\n%s", out)
	}
}

// Only tables with the site's prefix are the site's: another program's tables in the
// same database are not listed.
func TestOtherTablesFollowThePrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bs.sqlite")
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, s := range []string{
		`CREATE TABLE bs_players (pid integer primary key)`,
		`CREATE TABLE bs_extra (id integer primary key)`, `INSERT INTO bs_extra VALUES (1), (2)`,
		`CREATE TABLE "bs_odd name" (id integer primary key)`,
		`CREATE TABLE unrelated (id integer primary key)`, `INSERT INTO unrelated VALUES (1)`,
	} {
		if _, err := pool.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Source{Driver: "sqlite", Prefix: "bs_"}.otherTables(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (srcTable{Name: "extra", Rows: 2}) || got[1] != (srcTable{Name: "odd name", Rows: -1}) {
		t.Errorf("tables: %+v", got)
	}
}
