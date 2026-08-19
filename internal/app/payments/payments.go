// Package payments 是 v3 支付子域的 use-case 聚合（设计 §1）：
// 建单 / 查单（Client 面）、订单与回调管理（Server 面）、回调处理
// （serverhttp 面）、退款与人工履约、超时关单（worker 面）。
package payments

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

const (
	// defaultOrderTTL 是建单默认有效期（Stripe Checkout Session 上限 24h）。
	defaultOrderTTL = 24 * time.Hour
	// maxOrderTTL 是允许的最大有效期。
	maxOrderTTL = 24 * time.Hour
	// minOrderTTL 是允许的最小有效期。
	minOrderTTL = time.Minute
	// defaultListLimit / maxListLimit 是订单列表分页约束。
	defaultListLimit = 25
	maxListLimit     = 100
	// closeExpiredBatch 是关单 worker 单轮处理上限。
	closeExpiredBatch = 500
)

// 指标（前缀 torchwood_，设计 §Observability；履约 lag 直方图待 PR2
// 真实发放落地后补充——本期履约与翻转同事务同步完成）。
var (
	paymentOrdersTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_payment_orders_total",
		Help: "Total payment order status transitions.",
	}, []string{"provider", "status"})
	paymentCallbackVerifyFailTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_payment_callback_verify_fail_total",
		Help: "Total payment callback signature verification failures.",
	}, []string{"provider"})
)

func init() {
	prometheus.MustRegister(paymentOrdersTotal, paymentCallbackVerifyFailTotal)
}

// txRunner 是 RunInTx 端口：生产走 *clients.Database，-short 单测注入
// 内存实现以验证「订单翻转 + fulfillments + outbox 同一事务 / 失败回滚」。
type txRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Payments 是支付子域 use-case 聚合。
type Payments struct {
	cfg          *config.AppConfig
	db           txRunner // 仅为 RunInTx 注入（同 app/shared 先例）
	orders       domainpayments.OrderRepo
	callbacks    domainpayments.CallbackEventRepo
	fulfillments domainpayments.FulfillmentRepo
	fulfiller    domainpayments.Fulfiller
	providers    domainpayments.ProviderRegistry
	events       shared.EventPublisher
	subs         domainpayments.SubscriptionCallbackHandler // hosted 订阅 webhook（PR3，可空）
	logger       *slog.Logger
}

// NewPayments 构造 use-case 聚合（Wire：*clients.Database 满足 txRunner）。
func NewPayments(
	cfg *config.AppConfig,
	db *clients.Database,
	orders domainpayments.OrderRepo,
	callbacks domainpayments.CallbackEventRepo,
	fulfillments domainpayments.FulfillmentRepo,
	fulfiller domainpayments.Fulfiller,
	providers domainpayments.ProviderRegistry,
	events shared.EventPublisher,
	logger *slog.Logger,
	subs domainpayments.SubscriptionCallbackHandler,
) *Payments {
	return newPayments(cfg, db, orders, callbacks, fulfillments, fulfiller, providers, events, logger, subs)
}

// newPayments 允许单测注入内存 txRunner。
func newPayments(
	cfg *config.AppConfig,
	db txRunner,
	orders domainpayments.OrderRepo,
	callbacks domainpayments.CallbackEventRepo,
	fulfillments domainpayments.FulfillmentRepo,
	fulfiller domainpayments.Fulfiller,
	providers domainpayments.ProviderRegistry,
	events shared.EventPublisher,
	logger *slog.Logger,
	subs domainpayments.SubscriptionCallbackHandler,
) *Payments {
	if logger == nil {
		logger = slog.Default()
	}
	return &Payments{
		cfg:          cfg,
		db:           db,
		orders:       orders,
		callbacks:    callbacks,
		fulfillments: fulfillments,
		fulfiller:    fulfiller,
		providers:    providers,
		events:       events,
		subs:         subs,
		logger:       logger,
	}
}

// newOrderID 生成订单 id（ULID，字典序即时间序，便于游标分页）。
func newOrderID() string {
	return idgen.ULID().String()
}
