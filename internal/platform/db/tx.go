package db

import (
	"context"
	"errors"

	"github.com/Entropy-Increase-Team/skinloom/ent"
)

// WithTx runs fn in a transaction: committed when fn returns nil, rolled back when it
// returns an error or panics. Without the rollback on panic (recovered further up by
// the HTTP server) the transaction would keep its connection and locks, and on SQLite,
// which has one writer, every later write would wait for it and fail.
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx.Client()); err != nil {
		// fn's error as is (callers compare sentinel errors with ==), joined only
		// with a failed rollback.
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}
