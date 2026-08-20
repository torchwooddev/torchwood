package payments

import "context"

// provider_resource_index.kind 取值（K16）。
const (
	IndexKindPaymentSession = "payment_session"
	IndexKindPaymentOrder   = "payment_order"
	IndexKindSubscription   = "subscription"
	IndexKindIOSTransaction = "ios_transaction"
)

// ProviderIndexRepo 定位无项目头的渠道引用 → project_id（public 表）。
type ProviderIndexRepo interface {
	// Lookup 等值点查；未命中返回 ""。
	Lookup(ctx context.Context, provider, kind, providerRef string) (projectID string, err error)
	// Upsert 写入 (provider, kind, ref) → projectID。
	// 已存在且 project_id 不同 → PermissionDenied（他项占用）。
	Upsert(ctx context.Context, provider, kind, providerRef, projectID string) error
}
