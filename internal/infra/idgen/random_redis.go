package idgen

import (
	"context"
	"fmt"
	"time"

	domainidgen "github.com/torchwooddev/torchwood/internal/domain/idgen"
	pkgidgen "github.com/torchwooddev/torchwood/pkg/idgen"
)

// randomSetTTL 是 random 策略保留集合的 TTL：集合只增不减，长期运行会
// 无界增长；每次成功保留 ID 时刷新 TTL，空闲后集合自动过期（释放后 ID
// 理论上可被重新发放，与随机空间大小相比可忽略）。
const randomSetTTL = 30 * 24 * time.Hour

func (s *Service) nextRandom(ctx context.Context, projectID string, resource domainidgen.Resource) (string, error) {
	if s.rdb == nil {
		return "", pkgidgen.ErrRandomRedisRequired
	}
	cfg := s.randomCfg
	setKey := fmt.Sprintf("%s:%s:%s", s.randomPrefix, projectID, resource)

	for range cfg.MaxRetries {
		candidate, err := pkgidgen.RandomString(cfg)
		if err != nil {
			return "", err
		}
		pipe := s.rdb.TxPipeline()
		sadd := pipe.SAdd(ctx, setKey, candidate)
		pipe.Expire(ctx, setKey, randomSetTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			return "", fmt.Errorf("random id reservation failed: %w", err)
		}
		added, err := sadd.Result()
		if err != nil {
			return "", fmt.Errorf("random id reservation failed: %w", err)
		}
		if added == 1 {
			return candidate, nil
		}
	}
	return "", pkgidgen.ErrRandomReservationFailed
}
