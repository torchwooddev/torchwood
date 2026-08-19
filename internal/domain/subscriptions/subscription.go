// Package subscriptions 定义 v3 订阅子域的领域模型与端口：
// 双模统一状态机、计划、benefits 快照与 hosted 渠道端口（设计 §3）。
package subscriptions

import (
	"fmt"
	"time"
)

// Status 是订阅状态机（锁定，设计 §3.1）：
//
//	trialing → active ⇄ past_due → canceled | expired
type Status string

const (
	StatusTrialing Status = "trialing"
	StatusActive   Status = "active"
	StatusPastDue  Status = "past_due"
	StatusCanceled Status = "canceled"
	StatusExpired  Status = "expired"
)

// Mode 是订阅托管模式（D4）：hosted 以渠道 webhook 为事实源；
// platform 由 worker 周期扣款。
type Mode string

const (
	ModeHosted   Mode = "hosted"
	ModePlatform Mode = "platform"
)

// Interval 是计费周期。
type Interval string

const (
	IntervalMonth      Interval = "month"
	IntervalYear       Interval = "year"
	IntervalCustomDays Interval = "custom_days"
)

// PlanStatus 是计划生命周期。
type PlanStatus string

const (
	PlanStatusActive   PlanStatus = "active"
	PlanStatusArchived PlanStatus = "archived"
)

// 事件目录（设计 §5.1：复用 document_events_outbox 同表同管道）。
const (
	EventActivated = "subscriptions.activated"
	EventRenewed   = "subscriptions.renewed"
	EventPastDue   = "subscriptions.past_due"
	EventCanceled  = "subscriptions.canceled"
	EventExpired   = "subscriptions.expired"
)

// EventDomain 是经济事件信封的 domain 字段值（客户端按 domain 分流，D17）。
const EventDomain = "subscriptions"

// allowedTransitions 是状态机的合法迁移表（锁定，设计 §3.1）。
// canceled / expired 为终态。admin 强制 Cancel/Expire 允许从非终态直达。
var allowedTransitions = map[Status]map[Status]struct{}{
	StatusTrialing: {
		StatusActive:   {},
		StatusPastDue:  {},
		StatusCanceled: {},
		StatusExpired:  {},
	},
	StatusActive: {
		StatusPastDue:  {},
		StatusCanceled: {},
		StatusExpired:  {},
	},
	StatusPastDue: {
		StatusActive:   {}, // 宽限内重试成功
		StatusCanceled: {},
		StatusExpired:  {},
	},
}

// CanTransition 报告 from → to 是否合法。
func CanTransition(from, to Status) bool {
	if from == to {
		return true // 幂等：同态视为合法 no-op
	}
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = targets[to]
	return ok
}

// Transition 把订阅状态从当前值迁移到 to。非法迁移返回错误。
func (s *Subscription) Transition(to Status, now time.Time) error {
	if s.Status == to {
		s.UpdatedAt = now
		return nil
	}
	if !CanTransition(s.Status, to) {
		return fmt.Errorf("subscriptions: invalid transition %s -> %s (sub %s)", s.Status, to, s.ID)
	}
	s.Status = to
	s.UpdatedAt = now
	if to != StatusPastDue {
		s.GraceUntil = nil
	}
	return nil
}

// IsTerminal 报告状态是否为不可再迁移的终态。
func (s Status) IsTerminal() bool {
	return s == StatusCanceled || s == StatusExpired
}

// IsValid 校验 mode。
func (m Mode) IsValid() bool {
	return m == ModeHosted || m == ModePlatform
}

// IsValid 校验 interval。
func (i Interval) IsValid() bool {
	switch i {
	case IntervalMonth, IntervalYear, IntervalCustomDays:
		return true
	}
	return false
}

// Subscription 是订阅合同行（public.subscriptions）。
// 订阅不是资产，是产生资产的合同（D9）；履约只调资产系统 Grant/Mutate。
type Subscription struct {
	ID                 string
	ProjectID          string
	UserID             string
	PlanID             string
	Mode               Mode
	Provider           string
	ProviderSubID      string
	Status             Status
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	CancelAtPeriodEnd  bool
	GraceUntil         *time.Time
	BillingAssetCode   string
	Benefits           Benefits // 订阅时从 plan 快照
	IdempotencyKey     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PeriodDue 报告当前周期是否已到期（含等于 now）。
func (s *Subscription) PeriodDue(now time.Time) bool {
	if s == nil {
		return false
	}
	return !s.CurrentPeriodEnd.After(now)
}

// GraceElapsed 报告 past_due 宽限是否已过。
func (s *Subscription) GraceElapsed(now time.Time) bool {
	if s == nil || s.Status != StatusPastDue {
		return false
	}
	if s.GraceUntil == nil {
		return true
	}
	return !s.GraceUntil.After(now)
}

// AccountsChannel 返回订阅事件的 Realtime 频道（D17 单一 accounts.{userId}）。
func AccountsChannel(userID string) string {
	return "accounts." + userID
}

// NextPeriodEnd 按计划 interval 从 from 起算下一期结束时刻。
func NextPeriodEnd(from time.Time, interval Interval, intervalDays int64) (time.Time, error) {
	from = from.UTC()
	switch interval {
	case IntervalMonth:
		return from.AddDate(0, 1, 0), nil
	case IntervalYear:
		return from.AddDate(1, 0, 0), nil
	case IntervalCustomDays:
		if intervalDays <= 0 {
			return time.Time{}, fmt.Errorf("subscriptions: custom_days requires interval_days > 0")
		}
		return from.Add(time.Duration(intervalDays) * 24 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("subscriptions: invalid interval %q", interval)
	}
}

// ComputeGraceUntil 把 plan.grace_days 折算为宽限截止（D16）。
// grace_days=0 时 grace_until=from（下一轮扫描即可 expired）。
func ComputeGraceUntil(from time.Time, graceDays int32) time.Time {
	if graceDays <= 0 {
		return from.UTC()
	}
	return from.UTC().Add(time.Duration(graceDays) * 24 * time.Hour)
}
