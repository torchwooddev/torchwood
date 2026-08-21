package clients

import (
	"context"

	"github.com/torchwooddev/torchwood/pkg/uow"
	"github.com/uptrace/bun"
)

var _ uow.Runner = (*Database)(nil)

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

// Run 实现 uow.Runner：已在工作单元内则加入，否则开启新事务。
// 委托 RunInTx，嵌套行为不变。
func (d *Database) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.RunInTx(ctx, fn)
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

// RunInNewTx runs fn in a fresh transaction even when ctx already carries one.
// 用于「子操作必须先于外层事务提交」的路径（如订单 + provider index 在渠道
// 下单之前 COMMIT，见设计 §9.2）：外层失败回滚自己的部分，子事务保持已提交。
// 调用方自行确保两张事务不触碰会互相加锁的行。
func (d *Database) RunInNewTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(WithTx(ctx, tx))
	})
}
