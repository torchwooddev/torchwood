package clients

import (
	"context"

	"github.com/uptrace/bun"
)

type txContextKey struct{}

// WithTx stores a bun transaction in context for repository adapters.
func WithTx(ctx context.Context, tx bun.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// Conn returns the active transaction when present, otherwise the root database.
func (d *Database) Conn(ctx context.Context) bun.IDB {
	if tx, ok := ctx.Value(txContextKey{}).(bun.Tx); ok {
		return tx
	}
	return d.DB
}

// InTx reports whether ctx carries an active transaction (see WithTx).
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txContextKey{}).(bun.Tx)
	return ok
}

// RunInTx runs fn inside a database transaction. When ctx already carries an
// active transaction (see WithTx), fn runs on that transaction instead of
// opening a nested one — nested transactions on separate connections would
// deadlock on DDL locks held by the outer transaction.
func (d *Database) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if InTx(ctx) {
		return fn(ctx)
	}
	return d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(WithTx(ctx, tx))
	})
}
