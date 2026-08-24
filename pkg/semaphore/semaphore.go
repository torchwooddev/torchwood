package semaphore

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Semaphore 是计数信号量的抽象：TryAcquire 非阻塞尝试获取一个许可，
// 成功时返回 true 与释放函数（必须调用），失败时返回 false。
// 与进程内 chan 语义对齐，但可由 Redis 实现提供跨进程全局配额。
type Semaphore interface {
	TryAcquire(ctx context.Context) (bool, func(), error)
}

// InMemorySemaphore 基于带缓冲 chan 的进程内计数信号量（保留原有语义，
// 仅用于单测与本地无 Redis 时的回退；生产应使用 RedisSemaphore）。
type InMemorySemaphore struct {
	ch chan struct{}
}

func NewInMemory(max int) *InMemorySemaphore {
	if max <= 0 {
		max = 1
	}
	return &InMemorySemaphore{ch: make(chan struct{}, max)}
}

func (s *InMemorySemaphore) TryAcquire(_ context.Context) (bool, func(), error) {
	select {
	case s.ch <- struct{}{}:
		return true, func() { <-s.ch }, nil
	default:
		return false, nil, nil
	}
}

// NoopSemaphore 始终放行（用于禁用限流的测试场景）。
type NoopSemaphore struct{}

func (n *NoopSemaphore) TryAcquire(_ context.Context) (bool, func(), error) {
	return true, func() {}, nil
}

// RedisSemaphore 基于 Redis SET NX + TTL 的分布式计数信号量。
// 通过多个槽位 key 实现计数：每一许可对应一个独立 key
// `prefix:slot:<idx>`，值为随机 token，TTL 覆盖最长持有时长。
// 获取时依次尝试槽位 SET NX，命中即成功；释放时 Lua 原子校验 token
// 后 DEL，防止误删过期后被他人重占的槽位。
type RedisSemaphore struct {
	client    *redis.Client
	keyPrefix string
	max       int
	ttl       time.Duration
}

// releaseEvalTimeout 是释放（Lua compare-and-del）的独立超时（Round4 J5-5）：
// release 在 defer 路径执行，无超时会因 Redis 抖动阻塞调用方收尾。
const releaseEvalTimeout = 2 * time.Second

func NewRedis(client *redis.Client, keyPrefix string, max int, ttl time.Duration) *RedisSemaphore {
	if client == nil {
		return nil
	}
	if max <= 0 {
		max = 1
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RedisSemaphore{client: client, keyPrefix: keyPrefix, max: max, ttl: ttl}
}

// TryAcquire 尝试获取一个全局许可。
func (s *RedisSemaphore) TryAcquire(ctx context.Context) (bool, func(), error) {
	if s.client == nil {
		return false, nil, nil
	}
	token := uuid.NewString()
	for i := 0; i < s.max; i++ {
		key := s.slotKey(i)
		ok, err := s.client.SetNX(ctx, key, token, s.ttl).Result()
		if err != nil {
			return false, nil, err
		}
		if ok {
			release := func() {
				// Lua compare-and-del：仅当值仍为本 token 时删除，防止误删过期后被他人占用的槽位。
				// 使用独立 Background ctx + 2s 超时：避免原 ctx 已取消导致无法释放，
				// 也避免 Redis 抖动时 defer 路径无限期阻塞（失败仅浪费 TTL 兜底）。
				ctx, cancel := context.WithTimeout(context.Background(), releaseEvalTimeout)
				defer cancel()
				_, _ = s.client.Eval(ctx, `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`, []string{key}, token).Result()
			}
			return true, release, nil
		}
	}
	return false, nil, nil
}

func (s *RedisSemaphore) slotKey(idx int) string {
	// 使用简易 itoa 避免 fmt 开销
	if idx == 0 {
		return s.keyPrefix + ":slot:0"
	}
	buf := [20]byte{}
	pos := len(buf)
	n := idx
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return s.keyPrefix + ":slot:" + string(buf[pos:])
}
