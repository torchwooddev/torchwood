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
