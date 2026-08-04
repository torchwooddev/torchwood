package auth

import (
	"context"
	"time"
)

// RateLimiter 通用滑动窗口限流端口：对任意维度（如 IP）的调用频次做窗口计数。
type RateLimiter interface {
	// Allow 为 key 记录一次调用；当 key 在 window 内超过 limit 次时返回
	// codes.ResourceExhausted 错误。key 为空时不做限制。
	Allow(ctx context.Context, key string, limit int, window time.Duration) error
}
