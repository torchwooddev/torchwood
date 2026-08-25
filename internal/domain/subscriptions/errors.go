package subscriptions

import "errors"

var (
	ErrInvalidTransition   = errors.New("subscriptions: invalid status transition")
	ErrPlanNotFound        = errors.New("subscriptions: plan not found")
	ErrPlanArchived        = errors.New("subscriptions: plan is archived")
	ErrDuplicateCode       = errors.New("subscriptions: plan code already exists")
	ErrNotFound            = errors.New("subscriptions: subscription not found")
	ErrAlreadySubscribed   = errors.New("subscriptions: already subscribed to this plan")
	ErrInvalidMode         = errors.New("subscriptions: invalid mode")
	ErrNotConfigured       = errors.New("subscriptions: hosted billing is not configured")
	ErrConcurrent          = errors.New("subscriptions: concurrently modified")
	ErrIdempotencyRequired = errors.New("subscriptions: idempotency_key is required")
	// ErrReplayTerminalSubscription：Subscribe 幂等键命中已终态（canceled/expired）
	// 的历史订阅——原合同已死，不得作为成功重放返回；客户端应换新幂等键重新订阅。
	ErrReplayTerminalSubscription = errors.New("subscriptions: idempotent replay matched a terminated subscription; retry with a new idempotency key")
)
