package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func TestWithTx(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		put := func(key string) func(tx *ent.Client) error {
			return func(tx *ent.Client) error { return tx.Setting.Create().SetID(key).SetValue("1").Exec(ctx) }
		}
		exists := func(key string) bool {
			_, err := d.Ent.Setting.Get(ctx, key)
			if err != nil && !ent.IsNotFound(err) {
				t.Fatal(err)
			}
			return err == nil
		}

		if err := db.WithTx(ctx, d.Ent, put("committed")); err != nil {
			t.Fatal(err)
		}
		if !exists("committed") {
			t.Error("a transaction whose function succeeds is committed")
		}

		boom := errors.New("boom")
		err := db.WithTx(ctx, d.Ent, func(tx *ent.Client) error {
			if err := put("rolled-back")(tx); err != nil {
				return err
			}
			return boom
		})
		// The very error, not a wrapper: callers compare sentinel errors with ==.
		if err != boom {
			t.Fatalf("the function's error comes back as is: %#v", err)
		}
		if exists("rolled-back") {
			t.Error("a transaction whose function fails is rolled back")
		}

		func() {
			defer func() {
				if recover() == nil {
					t.Error("a panic in the function reaches the caller")
				}
			}()
			_ = db.WithTx(ctx, d.Ent, func(tx *ent.Client) error {
				if err := put("panicked")(tx); err != nil {
					return err
				}
				panic("bug")
			})
		}()
		if exists("panicked") {
			t.Error("a transaction whose function panics is rolled back")
		}
		// The panicking transaction let its locks go: on SQLite (one writer) this write
		// would wait for the abandoned transaction and fail with SQLITE_BUSY.
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := db.WithTx(wctx, d.Ent, put("after-panic")); err != nil {
			t.Fatalf("write after a panicked transaction: %v", err)
		}
	})
}
