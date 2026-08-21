package events

import "context"

type transactionIDKey struct{}

// WithTransactionID 把 transaction_id 注入 ctx，供 EventPublisher.Publish
// 读取并写入信封（v2 设计 §3.2：Commit 的 uow.Run 先注入再调现有 CRUD；
// Bulk / 单条 CRUD 不注入该键）。
func WithTransactionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, transactionIDKey{}, id)
}

// TransactionIDFrom 返回 ctx 中的 transaction_id；无则返回空串。
func TransactionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(transactionIDKey{}).(string)
	return id
}
