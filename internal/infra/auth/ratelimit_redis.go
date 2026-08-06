package auth

import (
	"context"
	"time"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/redis/go-redis/v9"
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
	count, err := l.rdb.Incr(ctx, redisKey).Result()
	if err != nil {
		return status.Error(codes.Internal, "rate limit check failed")
	}
	if count == 1 {
		if err := l.rdb.Expire(ctx, redisKey, window).Err(); err != nil {
			return status.Error(codes.Internal, "rate limit check failed")
		}
	}
	if count > int64(limit) {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return nil
}

var _ domainauth.RateLimiter = (*RedisRateLimiter)(nil)
