package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// A PostgreSQL server installed on a Chinese system reports constraint violations in
// Chinese, which ent.IsConstraintError does not recognise; the SQLSTATE code does.
func TestIsConstraintReadsPostgresCodes(t *testing.T) {
	for code, want := range map[string]bool{"23505": true, "23503": true, "23514": true, "23502": false, "40P01": false} {
		err := fmt.Errorf("create user: %w", &pgconn.PgError{Code: code, Message: "重复键违反唯一约束\"users_email_normalized_key\""})
		if got := IsConstraint(err); got != want {
			t.Errorf("SQLSTATE %s: IsConstraint = %v, want %v", code, got, want)
		}
	}
	if IsConstraint(nil) || IsConstraint(errors.New("violates nothing")) {
		t.Error("an ordinary error counts as a constraint violation")
	}
}

// New code must not go back to ent.IsConstraintError: on a Chinese PostgreSQL it
// misses every violation.
func TestNoEnglishOnlyConstraintChecks(t *testing.T) {
	root := filepath.Join("..", "..") // internal/
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasPrefix(filepath.ToSlash(path), "../../platform/db/") {
			return err
		}
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), "ent.IsConstraintError(") {
			t.Errorf("%s: use db.IsConstraint, ent.IsConstraintError only reads English messages", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
