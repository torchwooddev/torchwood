package auth

import (
	"context"
	"fmt"
	"time"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	loginFailWindow       = 15 * time.Minute
	loginEmailMaxFailures = 10
	loginIPMaxFailures    = 30
)

// RedisLoginThrottle throttles password sign-in attempts using sliding-window
// failure counters in Redis.
type RedisLoginThrottle struct {
	rdb *redis.Client
}

func NewRedisLoginThrottle(rdb *redis.Client) *RedisLoginThrottle {
	return &RedisLoginThrottle{rdb: rdb}
}

func (s *RedisLoginThrottle) Check(ctx context.Context, namespace, email, ip string) error {
	if limited, err := s.overLimit(ctx, loginThrottleKey(namespace, "email", email), loginEmailMaxFailures); err != nil {
		return err
	} else if limited {
		return status.Error(codes.ResourceExhausted, "too many failed sign-in attempts, try again later")
	}
	if limited, err := s.overLimit(ctx, loginThrottleKey(namespace, "ip", ip), loginIPMaxFailures); err != nil {
		return err
	} else if limited {
		return status.Error(codes.ResourceExhausted, "too many failed sign-in attempts, try again later")
	}
	return nil
}

func (s *RedisLoginThrottle) RecordFailure(ctx context.Context, namespace, email, ip string) error {
	if err := s.incr(ctx, loginThrottleKey(namespace, "email", email)); err != nil {
		return err
	}
	return s.incr(ctx, loginThrottleKey(namespace, "ip", ip))
}

func (s *RedisLoginThrottle) Reset(ctx context.Context, namespace, email, ip string) error {
	return s.rdb.Del(ctx, loginThrottleKey(namespace, "email", email), loginThrottleKey(namespace, "ip", ip)).Err()
}

func (s *RedisLoginThrottle) overLimit(ctx context.Context, key string, max int64) (bool, error) {
	if key == "" {
		return false, nil
	}
	count, err := s.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, status.Error(codes.Internal, "login throttle check failed")
	}
	return count >= max, nil
}

func (s *RedisLoginThrottle) incr(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	count, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return status.Error(codes.Internal, "login throttle update failed")
	}
	if count == 1 {
		if err := s.rdb.Expire(ctx, key, loginFailWindow).Err(); err != nil {
			return status.Error(codes.Internal, "login throttle update failed")
		}
	}
	return nil
}

func loginThrottleKey(namespace, dimension, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("Torchwood:login:fail:%s:%s:%s", namespace, dimension, value)
}

var _ domainauth.LoginThrottle = (*RedisLoginThrottle)(nil)
