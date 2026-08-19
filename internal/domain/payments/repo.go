package payments

import (
	"context"
	"time"
)

// OrderRepo 持久化 payment_orders（bun 静态表适配）。所有方法感知
// ctx 中的事务（clients.Conn(ctx)）：回调处理路径在 RunInTx 内调用，
// 与履约行 / outbox 事件同一段事务（设计 §1.3、总则 10）。
type OrderRepo interface {
	// Insert 落库新订单；(project_id, idempotency_key) 冲突时返回已存在的
	// 原单与 false（幂等锚点一：建单幂等键重复返回原单，不新建）。
	Insert(ctx context.Context, order *Order) (existing *Order, inserted bool, err error)
	// GetByID 按 id 取单（project 隔离过滤）。
	GetByID(ctx context.Context, projectID, orderID string) (*Order, error)
	// GetByIDForUpdate 在事务内 SELECT ... FOR UPDATE 锁单（回调 / 退款路径）。
	GetByIDForUpdate(ctx context.Context, projectID, orderID string) (*Order, error)
	// GetByProviderRef 按渠道会话 / 渠道支付单定位订单（回调路径）。
	GetByProviderRef(ctx context.Context, projectID, provider, providerSessionID, providerOrderID string) (*Order, error)
	// Update 写回订单（状态翻转、渠道引用回填）；带状态前置条件防并发覆写。
	Update(ctx context.Context, order *Order, expectStatus OrderStatus) error
	// ListByUser 返回本人订单（created_at DESC 分页）。
	ListByUser(ctx context.Context, projectID, userID string, limit int, before time.Time) ([]Order, error)
	// ListByProject 返回项目订单（created_at DESC 分页，Server/Console 面）。
	ListByProject(ctx context.Context, projectID string, limit int, before time.Time) ([]Order, error)
	// CloseExpired 把 created/paying 且超时的订单翻 closed，返回关单数
	// （worker 周期任务；状态机非法迁移自动跳过）。
	CloseExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

// CallbackEventRepo 登记渠道回调事件：幂等锚点二 (provider, provider_event_id)
// 唯一（设计 §1.3）。插入与订单翻转同一事务。
type CallbackEventRepo interface {
	// InsertIfAbsent 落一行回调事件；(provider, provider_event_id) 已存在时
	// 返回 false——调用方据此幂等短路（重放返回 200，不重入状态机）。
	// projectID / orderID 为定位结果（未命中订单时均可为空）。
	InsertIfAbsent(ctx context.Context, event *CallbackEvent, projectID, orderID string) (inserted bool, err error)
}

// FulfillmentStatus 是履约记录状态。
type FulfillmentStatus string

const (
	FulfillmentPending FulfillmentStatus = "pending"
	FulfillmentDone    FulfillmentStatus = "done"
	FulfillmentFailed  FulfillmentStatus = "failed"
)

// Fulfillment 是订单 paid 同事务内落下的履约记录（设计 §1.5）。
type Fulfillment struct {
	ID          string
	OrderID     string
	ProjectID   string
	PurposeKind PurposeKind
	Ref         string // 幂等锚点三：履约 ref（"order:{order_id}"；PR2 起指向 ledger entry）
	Status      FulfillmentStatus
	Detail      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FulfillmentRepo 持久化 payment_fulfillments。
type FulfillmentRepo interface {
	// InsertPending 落 pending 履约行；(order_id, purpose_kind) 已存在时
	// 返回已存在的行与 false（一单一类履约恰好一次）。
	InsertPending(ctx context.Context, f *Fulfillment) (existing *Fulfillment, inserted bool, err error)
	// MarkDone 把履约行置 done 并回填 ref。
	MarkDone(ctx context.Context, fulfillmentID, ref string, detail map[string]any) error
	// MarkFailed 把履约行置 failed（事务整体回滚路径一般用不到，
	// 留给人工排查标记）。
	MarkFailed(ctx context.Context, fulfillmentID, reason string) error
	// GetByOrder 按订单取履约行。
	GetByOrder(ctx context.Context, projectID, orderID string) (*Fulfillment, error)
}

// Fulfiller 是订单 paid 后的实际发放端口（设计 §1.5，hook）：
// topup / item_purchase 在 PR2 接入资产系统 Grant（与订单翻转同一事务），
// subscription 在 PR3 接入。PR1 的默认实现不发放、只返回占位 ref。
type Fulfiller interface {
	// Fulfill 在订单已翻 paid、履约行已插入后调用；返回履约 ref。
	// 返回错误则整段事务回滚（订单保持 paying，渠道重推后再试）。
	Fulfill(ctx context.Context, order *Order) (ref string, err error)
}
