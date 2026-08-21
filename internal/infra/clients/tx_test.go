package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// TestInTx (A4): InTx 仅在 ctx 携带 WithTx 注入的 bun.Tx 时返回 true；
// 普通 ctx 与零值 Tx 边界均覆盖（零值 Tx 仅用于类型断言，不调用其方法）。
func TestInTx(t *testing.T) {
	ctx := context.Background()
	require.False(t, InTx(ctx))

	txCtx := WithTx(ctx, bun.Tx{})
	require.True(t, InTx(txCtx))

	require.False(t, InTx(context.WithValue(ctx, txContextKey{}, "not-a-tx")))
}

// TestRun_JoinsExistingTx：ctx 已带事务时 Run 与 RunInTx 一样加入外层，
// 不碰根连接（nil DB 也不会被调用）。
func TestRun_JoinsExistingTx(t *testing.T) {
	d := &Database{}
	outer := WithTx(context.Background(), bun.Tx{})
	var got context.Context
	err := d.Run(outer, func(txCtx context.Context) error {
		got = txCtx
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, outer, got)
	require.True(t, InTx(got))
}
