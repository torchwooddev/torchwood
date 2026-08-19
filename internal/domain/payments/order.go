package payments

import (
	"encoding/json"
	"fmt"
	"time"
)

// OrderStatus 是订单状态机（锁定，设计 §1.3）：
//
//	created → paying → paid | failed | closed
//	paid → refunding → refunded
type OrderStatus string

const (
	OrderStatusCreated   OrderStatus = "created"
	OrderStatusPaying    OrderStatus = "paying"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusClosed    OrderStatus = "closed"
	OrderStatusRefunding OrderStatus = "refunding"
	OrderStatusRefunded  OrderStatus = "refunded"
)

// PurposeKind 是订单用途（决定 paid 后的履约分发，设计 §1.5）。
type PurposeKind string

const (
	PurposeTopup        PurposeKind = "topup"
	PurposeItemPurchase PurposeKind = "item_purchase"
	PurposeSubscription PurposeKind = "subscription"
)

// IsValid 校验 purpose_kind 取值。
func (k PurposeKind) IsValid() bool {
	switch k {
	case PurposeTopup, PurposeItemPurchase, PurposeSubscription:
		return true
	}
	return false
}

// 事件目录（设计 §5.1：复用 document_events_outbox 同表同管道）。
const (
	EventOrderPaid     = "payments.orders.paid"
	EventOrderFailed   = "payments.orders.failed"
	EventOrderRefunded = "payments.orders.refunded"
)

// EventDomain 是经济事件信封的 domain 字段值（客户端按 domain 分流，D17）。
const EventDomain = "payments"

// Order 是支付订单实体（public.payment_orders 行）。
type Order struct {
	ID                string
	ProjectID         string
	UserID            string
	Provider          string
	IdempotencyKey    string
	ProviderSessionID string
	ProviderOrderID   string
	Amount            int64 // 最小货币单位，>0（bigint，禁止 float）
	Currency          string
	PurposeKind       PurposeKind
	Purpose           json.RawMessage
	Status            OrderStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PaidAt            *time.Time
	ExpiresAt         time.Time
}

// allowedTransitions 是状态机的合法迁移表（锁定，设计 §1.3）。
var allowedTransitions = map[OrderStatus]map[OrderStatus]struct{}{
	OrderStatusCreated: {
		OrderStatusPaying: struct{}{}, // 渠道 session 已建
		OrderStatusPaid:   struct{}{}, // iOS 单 created → 直接等验票翻 paid
		OrderStatusFailed: struct{}{}, // 下单失败 / 回调 failed
		OrderStatusClosed: struct{}{}, // 超时未付关单
	},
	OrderStatusPaying: {
		OrderStatusPaid:   struct{}{}, // 回调 paid
		OrderStatusFailed: struct{}{}, // 回调 failed
		OrderStatusClosed: struct{}{}, // 超时未付关单
	},
	OrderStatusPaid: {
		OrderStatusRefunding: struct{}{}, // 发起退款（渠道确认中）
		OrderStatusRefunded:  struct{}{}, // 退款同步成功 / 回调 refunded
	},
	OrderStatusRefunding: {
		OrderStatusRefunded: struct{}{}, // 渠道退款确认
	},
	// 终态：paid（履约后稳定）、failed / closed / refunded 无出边。
}

// CanTransition 报告 from → to 是否合法。
func CanTransition(from, to OrderStatus) bool {
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = targets[to]
	return ok
}

// Transition 把订单状态从当前值迁移到 to（状态机校验 + paid_at 维护）。
// 非法迁移返回错误——回调重放、并发翻转、乱序事件全部由此挡住。
func (o *Order) Transition(to OrderStatus, now time.Time) error {
	if !CanTransition(o.Status, to) {
		return fmt.Errorf("payments: invalid order transition %s -> %s (order %s)", o.Status, to, o.ID)
	}
	o.Status = to
	o.UpdatedAt = now
	if to == OrderStatusPaid && o.PaidAt == nil {
		t := now
		o.PaidAt = &t
	}
	return nil
}

// IsTerminal 报告状态是否为不可再迁移的终态。
func (s OrderStatus) IsTerminal() bool {
	switch s {
	case OrderStatusFailed, OrderStatusClosed, OrderStatusRefunded:
		return true
	}
	return false
}

// Expired 报告订单是否已超时未付（created/paying 且超过 expires_at）。
func (o *Order) Expired(now time.Time) bool {
	switch o.Status {
	case OrderStatusCreated, OrderStatusPaying:
		return now.After(o.ExpiresAt)
	}
	return false
}

// AccountsChannel 返回订单事件的 Realtime 频道（D17 单一 accounts.{userId}）。
func AccountsChannel(userID string) string {
	return "accounts." + userID
}
