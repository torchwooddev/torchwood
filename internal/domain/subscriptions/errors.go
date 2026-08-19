package subscriptions

import "errors"

var (
	ErrInvalidTransition = errors.New("subscriptions: invalid status transition")
	ErrPlanNotFound      = errors.New("subscriptions: plan not found")
	ErrPlanArchived      = errors.New("subscriptions: plan is archived")
	ErrDuplicateCode     = errors.New("subscriptions: plan code already exists")
	ErrNotFound          = errors.New("subscriptions: subscription not found")
	ErrAlreadySubscribed = errors.New("subscriptions: already subscribed to this plan")
	ErrInvalidMode       = errors.New("subscriptions: invalid mode")
	ErrNotConfigured     = errors.New("subscriptions: hosted billing is not configured")
	ErrConcurrent        = errors.New("subscriptions: concurrently modified")
	ErrIdempotencyRequired = errors.New("subscriptions: idempotency_key is required")
)
