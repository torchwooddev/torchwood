package auth

import (
	"context"
	"time"
)

// NonceStore 是带 TTL 的一次性 KV。调用方用 key 前缀隔离。
type NonceStore interface {
	// Put 覆盖写入并设置 TTL。
	Put(ctx context.Context, key, value string, ttl time.Duration) error
	// PutNX 仅在 key 不存在时写入；已存在返回 false。
	PutNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// Get 读取；缺失时返回空字符串和 nil error。
	Get(ctx context.Context, key string) (string, error)
	// Consume 原子读取并删除；缺失时返回空字符串和 nil error。
	Consume(ctx context.Context, key string) (string, error)
}
