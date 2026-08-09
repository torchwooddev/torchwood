// Package health 提供依赖健康检查器：单依赖探测（lynx.Checker）与
// 并行探测集合（Details），供 gRPC/HTTP 健康端点与 lynx readiness 使用。
package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lynx-go/lynx"
	"github.com/redis/go-redis/v9"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

// DefaultTimeout 是未显式指定超时时的探测超时。
const DefaultTimeout = 2 * time.Second

// DependencyChecker 单依赖探测；实现 lynx.Checker（CheckHealth() error，
// 无 ctx，超时在内部自控）。
type DependencyChecker struct {
	Name    string
	Timeout time.Duration // 默认 2s
	Check   func(ctx context.Context) error
}

// CheckHealth 以独立超时执行探测，实现 lynx.Checker。
func (c *DependencyChecker) CheckHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	return c.Check(ctx)
}

func (c *DependencyChecker) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

var _ lynx.Checker = (*DependencyChecker)(nil)

// Checkers 是依赖集合（只读，并发安全）。
type Checkers struct {
	deps []*DependencyChecker
}

// NewCheckers 构建依赖探测集合：Postgres（PingContext）、Redis（Ping）、
// MinIO（BucketExists）。
func NewCheckers(db *clients.Database, rdb *redis.Client, obj storage.ObjectStore) *Checkers {
	return &Checkers{
		deps: []*DependencyChecker{
			{Name: "postgres", Check: db.PingContext},
			{Name: "redis", Check: func(ctx context.Context) error { return rdb.Ping(ctx).Err() }},
			{Name: "minio", Check: obj.Ping},
		},
	}
}

// NewCheckersFromDeps 构建显式依赖集合（测试与扩展场景使用）。
func NewCheckersFromDeps(deps ...*DependencyChecker) *Checkers {
	return &Checkers{deps: deps}
}

// Deps 返回全部依赖（供 lynx.HealthCheckersFunc 使用）。
func (c *Checkers) Deps() []lynx.Checker {
	deps := make([]lynx.Checker, 0, len(c.deps))
	for _, d := range c.deps {
		deps = append(deps, d)
	}
	return deps
}

// Details 并行探测各依赖（各自超时 + recover 兜底），返回逐依赖状态；
// 探测失败不影响其他依赖。
func (c *Checkers) Details(ctx context.Context) []*serverv1.DependencyStatus {
	results := make([]*serverv1.DependencyStatus, len(c.deps))
	var wg sync.WaitGroup
	for i, dep := range c.deps {
		wg.Add(1)
		go func(i int, dep *DependencyChecker) {
			defer wg.Done()
			results[i] = checkOne(dep, ctx)
		}(i, dep)
	}
	wg.Wait()
	return results
}

// checkOne 探测单个依赖并返回状态；panic 兜底为 unavailable。
func checkOne(dep *DependencyChecker, ctx context.Context) (st *serverv1.DependencyStatus) {
	st = &serverv1.DependencyStatus{Name: dep.Name, Status: "ok"}
	defer func() {
		if r := recover(); r != nil {
			st.Status = "unavailable"
			st.Error = fmt.Sprintf("panic: %v", r)
		}
	}()
	probeCtx, cancel := context.WithTimeout(ctx, dep.timeout())
	defer cancel()
	if err := dep.Check(probeCtx); err != nil {
		st.Status = "unavailable"
		st.Error = err.Error()
	}
	return st
}
