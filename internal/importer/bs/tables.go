package bs

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// srcTable is a table of the BS database that the import does not read.
type srcTable struct {
	Name string // without the prefix
	Rows int64  // up to tableRowCap+1; -1 when it could not be counted
}

// tableRowCap bounds the row count of a table left behind: a large log table would
// otherwise take a while to count, and "more than 100000" says enough.
const tableRowCap = 100000

// readTables are the tables the import reads.
var readTables = []string{"users", "players", "textures", "user_closet", "reports", "notifications", "options", "uuid", "language_lines", "oauth_clients"}

var plainTableName = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)

// otherTables lists the tables with the site's prefix that the import does not read,
// with their row counts, so the report can say what stays behind (plugins' tables,
// another program sharing the database). It runs after the read transaction: on
// PostgreSQL a failed statement would have spoiled that.
func (s Source) otherTables(ctx context.Context, pool *sql.DB) ([]srcTable, error) {
	var q string
	switch s.Driver {
	case "mysql":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"
	case "postgres":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
	default:
		q = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	}
	rows, err := pool.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		if rest, ok := strings.CutPrefix(n, s.Prefix); ok && rest != "" && !slices.Contains(readTables, rest) {
			names = append(names, rest)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Sort(names)
	out := make([]srcTable, 0, len(names))
	for _, n := range names {
		t := srcTable{Name: n, Rows: -1}
		if plainTableName.MatchString(s.Prefix + n) {
			q := fmt.Sprintf("SELECT COUNT(*) FROM (SELECT 1 FROM %s LIMIT %d) t", s.table(n), tableRowCap+1)
			if err := pool.QueryRowContext(ctx, q).Scan(&t.Rows); err != nil {
				t.Rows = -1
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// tableFates says what becomes of a known table's rows; any other table with rows is
// reported as left behind. A table missing here and empty is not mentioned.
var tableFates = map[string]pluginFate{
	// Laravel's and BS's own bookkeeping: nothing a site would miss.
	"migrations": {}, "jobs": {}, "failed_jobs": {}, "job_batches": {}, "password_resets": {}, "password_reset_tokens": {},
	"sessions": {}, "cache": {}, "cache_locks": {}, "scopes": {}, "oauth_personal_access_clients": {},
	"oauth_access_tokens":  {Dropped, "issued OAuth tokens are never imported: users of third-party apps authorize them once more"},
	"oauth_refresh_tokens": {Dropped, "issued OAuth tokens are never imported: users of third-party apps authorize them once more"},
	"oauth_auth_codes":     {Dropped, "short-lived OAuth codes: never imported"},
	"ygg_log":              {Dropped, "the Yggdrasil plugin's activity log: not imported"},
	"code_id_to_uuid":      {Dropped, "yggdrasil-connect's own table: not imported"},
	"textures_description": {Warning, "texture descriptions (texture-description plugin): not imported"},
	"mojang_verifications": {Warning, "verified Minecraft accounts (mojang-verification plugin): not imported"},
	"invitation_codes":     {Warning, "invitation codes (invitation-codes plugin): not imported"},
}

// planTables reports the tables left behind that hold rows.
func planTables(d *sourceData, r *Report) {
	if d.TablesErr != nil {
		r.leave(Warning, "tables", "could not list the database's other tables (%v): check by hand what plugins stored", d.TablesErr)
		return
	}
	for _, t := range d.OtherTables {
		f, known := tableFates[t.Name]
		if known && f.msg == "" {
			continue
		}
		if t.Rows == 0 {
			continue
		}
		if !known {
			f = pluginFate{Warning, "SkinLoom does not know this table (a plugin's, or another program sharing the database): not imported"}
		}
		rows := fmt.Sprintf("%d rows", t.Rows)
		switch {
		case t.Rows == 1:
			rows = "1 row"
		case t.Rows < 0:
			rows = "rows not counted"
		case t.Rows > tableRowCap:
			rows = fmt.Sprintf("more than %d rows", tableRowCap)
		}
		if f.sev == Warning {
			r.leave(f.sev, d.Prefix+t.Name, "%s: %s", rows, f.msg)
		} else {
			r.add(f.sev, d.Prefix+t.Name, 0, "%s: %s", rows, f.msg)
		}
	}
}
