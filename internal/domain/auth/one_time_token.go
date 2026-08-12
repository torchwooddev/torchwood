package auth

import (
	"context"
	"time"
)

// OneTimeJWTKeyPrefix 是 CreateJWT 一次性消费记录的 Redis key 前缀
// （签发方 Register 与验证方 Consume 必须使用同一 key 约定）。
const OneTimeJWTKeyPrefix = "Torchwood:jwt:one-time:"

// OneTimeTokenStore records one-time tokens (e.g. the CreateJWT single-use JWT)
// so that presenting the same token twice can be detected and rejected.
type OneTimeTokenStore interface {
	// Register records value under key for ttl unless the key already exists
	// (SETNX semantics); returns false when the key is already present.
	Register(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// Consume atomically fetches and deletes the key (GETDEL semantics); an
	// empty result with nil error means the key did not exist (already
	// consumed or expired).
	Consume(ctx context.Context, key string) (string, error)
}
