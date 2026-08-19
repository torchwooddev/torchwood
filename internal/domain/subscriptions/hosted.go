package subscriptions

import "context"

// HostedCheckoutInput 是渠道托管（Stripe Billing Checkout mode=subscription）入参。
type HostedCheckoutInput struct {
	SubscriptionID string
	ProjectID      string
	PriceID        string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

// HostedCheckout 是托管收银台结果。
type HostedCheckout struct {
	SessionID  string
	PaymentURL string
}

// HostedBilling 是渠道托管订阅端口（Stripe Billing）：平台不主动扣款，
// 只建 Checkout / 通知渠道取消；状态以 webhook 为事实源（设计 §3.1）。
type HostedBilling interface {
	CreateCheckout(ctx context.Context, in HostedCheckoutInput) (*HostedCheckout, error)
	CancelAtPeriodEnd(ctx context.Context, providerSubID string) error
	CancelNow(ctx context.Context, providerSubID string) error
}
