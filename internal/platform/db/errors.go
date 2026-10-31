package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Entropy-Increase-Team/skinloom/ent"
)

// IsConstraint reports whether err is a unique, foreign key or check constraint
// violation. Use it instead of ent.IsConstraintError, which recognises them by the
// English text of the message ("violates unique constraint"): PostgreSQL writes its
// messages in the server's language (lc_messages, Chinese on a server installed on a
// Chinese system), and ent then passes the error on unrecognised, so a name already
// taken became a 500. The SQLSTATE code is the same in every language. MySQL messages
// always carry the error number ent matches, and SQLite's are not translated.
func IsConstraint(err error) bool {
	if ent.IsConstraintError(err) {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "23503", "23514": // unique, foreign key, check
			return true
		}
	}
	return false
}
