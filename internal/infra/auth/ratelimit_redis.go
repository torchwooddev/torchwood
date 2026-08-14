package auth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RedisRateLimiter 基于 Redis 固定窗口计数（INCR + EXPIRE）的通用限流器，
// 与 OTP/login throttle 的窗口实现保持一致。
type RedisRateLimiter struct {
	rdb *redis.Client
}

func NewRedisRateLimiter(rdb *redis.Client) *RedisRateLimiter {
	return &RedisRateLimiter{rdb: rdb}
}

func (l *RedisRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) error {
	if key == "" {
		return nil
	}
	redisKey := "Torchwood:ratelimit:" + key
	// Round3 H6-4：INCR + 首次 EXPIRE 原子化，崩溃不留下无 TTL 计数键。
	count, err := incrWithTTL(ctx, l.rdb, redisKey, window)
	if err != nil {
		return status.Error(codes.Internal, "rate limit check failed")
	}
	if count > int64(limit) {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return nil
}

var _ domainauth.RateLimiter = (*RedisRateLimiter)(nil)
