package bs

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Severity orders report entries by what the administrator has to do about them.
type Severity int

const (
	// Blocking stops the import: it needs a decision only a person can make (two
	// accounts with the same address). Fix it in BS and run the import again.
	Blocking Severity = iota
	// Warning: imported, but something does not work as it did (a missing file, a
	// password that cannot be verified).
	Warning
	// Fixed: imported after a change by the default policy (a renamed duplicate
	// player, a reference to a deleted texture cleared).
	Fixed
	// Dropped: not imported (a player whose account is gone, a duplicate closet row).
	Dropped
)

func (s Severity) String() string {
	if s < 0 || int(s) >= 4 {
		return "UNKNOWN"
	}
	return [...]string{"BLOCKING", "WARNING", "FIXED", "DROPPED"}[s]
}

// MarshalJSON encodes Severity as its uppercase label ("BLOCKING", "WARNING", ...).
func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON decodes Severity from its label or integer value.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		switch strings.ToUpper(str) {
		case "BLOCKING":
			*s = Blocking
		case "WARNING":
			*s = Warning
		case "FIXED":
			*s = Fixed
		case "DROPPED":
			*s = Dropped
		default:
			return fmt.Errorf("unknown severity %q", str)
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*s = Severity(n)
	return nil
}

// Entry is one line of the report.
type Entry struct {
	Severity Severity `json:"severity"`
	Table    string   `json:"table"`        // the BS table
	ID       string   `json:"id,omitempty"` // the BS row (uid, pid, tid...), empty for a whole table
	Message  string   `json:"message"`
	// LeftBehind: data that stays in BS (a plugin's, a table SkinLoom has no place
	// for). The report lists these first, and the command prints them in full.
	LeftBehind bool `json:"leftBehind,omitempty"`
}

// Report is the outcome of an import or a dry run.
type Report struct {
	DryRun  bool    `json:"dryRun"`
	Entries []Entry `json:"entries"`
	// Counts of rows written (or that would be) per SkinLoom table.
	Imported map[string]int `json:"imported"`
	// Files counts the texture files checked; FilesCopied those put into the store.
	Files       int `json:"files"`
	FilesCopied int `json:"filesCopied"`
	Superadmins int `json:"superadmins"`
	// Failed is the error that stopped an import after its plan was made (copying the
	// files or writing the database). The database is then unchanged.
	Failed error `json:"-"`
}

// MarshalJSON serializes Report for REST API responses, turning Failed into a string.
func (r Report) MarshalJSON() ([]byte, error) {
	type reportAlias struct {
		DryRun      bool           `json:"dryRun"`
		Entries     []Entry        `json:"entries"`
		Imported    map[string]int `json:"imported"`
		Files       int            `json:"files"`
		FilesCopied int            `json:"filesCopied"`
		Superadmins int            `json:"superadmins"`
		Failed      string         `json:"failed,omitempty"`
	}
	out := reportAlias{
		DryRun:      r.DryRun,
		Entries:     r.Entries,
		Imported:    r.Imported,
		Files:       r.Files,
		FilesCopied: r.FilesCopied,
		Superadmins: r.Superadmins,
	}
	if out.Entries == nil {
		out.Entries = []Entry{}
	}
	if out.Imported == nil {
		out.Imported = map[string]int{}
	}
	if r.Failed != nil {
		out.Failed = r.Failed.Error()
	}
	return json.Marshal(out)
}

func (r *Report) add(sev Severity, table string, id int64, format string, args ...any) {
	s := ""
	if id != 0 {
		s = strconv.FormatInt(id, 10)
	}
	r.Entries = append(r.Entries, Entry{Severity: sev, Table: table, ID: s, Message: fmt.Sprintf(format, args...)})
}

// leave records data that stays in BS (Entry.LeftBehind).
func (r *Report) leave(sev Severity, table string, format string, args ...any) {
	r.Entries = append(r.Entries, Entry{Severity: sev, Table: table, Message: fmt.Sprintf(format, args...), LeftBehind: true})
}

// LeftBehind returns the entries about data that stays in BS.
func (r *Report) LeftBehind() []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.LeftBehind {
			out = append(out, e)
		}
	}
	return out
}

// Count returns the number of entries of a severity.
func (r *Report) Count(sev Severity) int {
	n := 0
	for _, e := range r.Entries {
		if e.Severity == sev {
			n++
		}
	}
	return n
}

// Blocked reports whether the import was refused.
func (r *Report) Blocked() bool { return r.Count(Blocking) > 0 }

// tables lists the imported tables in the order they are written.
var tables = []string{"users", "blobs", "textures", "players", "closet_items", "reports", "notifications", "oauth_clients", "settings"}

// WriteText writes the report for people: a summary, then every entry grouped by
// severity, in the order found.
func (r *Report) WriteText(w io.Writer) error {
	var err error
	p := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, args...)
		}
	}
	switch {
	case r.Failed != nil && r.DryRun:
		p("Dry run failed: %v\n", r.Failed)
	case r.Failed != nil:
		p("Import failed, nothing was written to the database: %v\nFix the cause and run it again.\n", r.Failed)
	case r.Blocked():
		p("Import refused: fix the BLOCKING entries in Blessing Skin and run it again. Nothing was written.\n")
	case r.DryRun:
		p("Dry run: nothing was written. A real import gives the same result.\n")
	default:
		p("Imported.\n")
	}
	p("\n")
	for _, t := range tables {
		p("  %-14s %d\n", t, r.Imported[t])
	}
	p("  texture files  %d checked, %d copied\n", r.Files, r.FilesCopied)
	p("\n  blocking %d, warnings %d, fixed %d, dropped %d\n", r.Count(Blocking), r.Count(Warning), r.Count(Fixed), r.Count(Dropped))
	if left := r.LeftBehind(); len(left) > 0 {
		p("\nLEFT IN BLESSING SKIN (not imported: check before switching)\n")
		for _, e := range left {
			p("  %s: %s\n", e.Table, e.Message)
		}
	}
	entries := slices.DeleteFunc(slices.Clone(r.Entries), func(e Entry) bool { return e.LeftBehind })
	slices.SortStableFunc(entries, func(a, b Entry) int { return int(a.Severity) - int(b.Severity) })
	last := Severity(-1)
	for _, e := range entries {
		if e.Severity != last {
			p("\n%s\n", e.Severity)
			last = e.Severity
		}
		if e.ID != "" {
			p("  %s %s: %s\n", e.Table, e.ID, e.Message)
		} else {
			p("  %s: %s\n", e.Table, e.Message)
		}
	}
	return err
}
