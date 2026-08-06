package idgen

import (
	"context"
	"fmt"

	domainidgen "github.com/torchwoodio/torchwood/internal/domain/idgen"
	pkgidgen "github.com/torchwoodio/torchwood/pkg/idgen"
)

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
		added, err := s.rdb.SAdd(ctx, setKey, candidate).Result()
		if err != nil {
			return "", fmt.Errorf("random id reservation failed: %w", err)
		}
		if added == 1 {
			return candidate, nil
		}
	}
	return "", pkgidgen.ErrRandomReservationFailed
}
