// Package payments 提供支付渠道适配器装配：PaymentProvider 注册表
// （PR1 仅 Stripe；PR4 补微信/支付宝/iOS）。
package payments

import (
	"fmt"

	"github.com/google/wire"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	domainsubs "github.com/torchwooddev/torchwood/internal/domain/subscriptions"
	"github.com/torchwooddev/torchwood/internal/infra/payments/stripe"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

// ProviderSet 装配渠道注册表。
var ProviderSet = wire.NewSet(
	NewStripeAdapter,
	NewRegistry,
	wire.Bind(new(domainpayments.ProviderRegistry), new(*Registry)),
	wire.Bind(new(domainsubs.HostedBilling), new(*stripe.Adapter)),
)

// NewStripeAdapter 从配置构造 Stripe 适配器（secret 走环境变量，
// 未配置时构造成功但操作 fail-closed）。
func NewStripeAdapter(cfg *config.AppConfig) *stripe.Adapter {
	stripeCfg := cfg.GetPayments().GetStripe()
	return stripe.New(stripe.Config{
		SecretKey:     stripeCfg.GetSecretKey(),
		WebhookSecret: stripeCfg.GetWebhookSecret(),
		APIBaseURL:    stripeCfg.GetApiBaseUrl(),
	})
}

// Registry 是渠道名 → PaymentProvider 的注册表（PR4 泛化回调路由时复用）。
type Registry struct {
	providers map[string]domainpayments.PaymentProvider
}

// NewRegistry 构造注册表（PR1 只登记 Stripe）。
func NewRegistry(stripeAdapter *stripe.Adapter) *Registry {
	return &Registry{providers: map[string]domainpayments.PaymentProvider{
		stripeAdapter.Name(): stripeAdapter,
	}}
}

// Get 按渠道名取 provider；未注册渠道返回错误。
func (r *Registry) Get(name string) (domainpayments.PaymentProvider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("payments: unknown provider %q", name)
	}
	return p, nil
}

// Names 返回已注册渠道名。
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for name := range r.providers {
		out = append(out, name)
	}
	return out
}
