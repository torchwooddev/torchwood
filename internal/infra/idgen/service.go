package idgen

import (
	"context"
	"fmt"
	"strings"

	domainidgen "github.com/torchwooddev/torchwood/internal/domain/idgen"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	pkgidgen "github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/redis/go-redis/v9"
)

// Service implements domainidgen.Generator using platform config and optional project overrides.
type Service struct {
	cfg             *config.AppConfig
	rdb             *redis.Client
	projectRepo     projects.Repository
	snowflake         *pkgidgen.Snowflake
	randomCfg         pkgidgen.RandomConfig
	randomPrefix      string
	seqPrefix         string
	defaultStrategy string
	resourceUsers   string
	resourceSessions string
	resourceDocuments string
}

func NewService(cfg *config.AppConfig, rdb *redis.Client, projectRepo projects.Repository) (*Service, error) {
	nodeID := int64(0)
	randomLen := int32(10)
	randomCharset := pkgidgen.RandomCharsetNumeric
	randomPrefix := "Torchwood:id:random"
	randomMaxRetries := int32(10)
	seqPrefix := "Torchwood:seq"
	defaultStrategy := pkgidgen.StrategyUUID
	resourceUsers := ""
	resourceSessions := ""
	resourceDocuments := ""

	if cfg != nil && cfg.GetIdgen() != nil {
		idCfg := cfg.GetIdgen()
		defaultStrategy = pkgidgen.NormalizeStrategy(idCfg.GetDefaultStrategy())
		if idCfg.GetSnowflake() != nil {
			nodeID = int64(idCfg.GetSnowflake().GetNodeId())
		}
		if idCfg.GetRandom() != nil {
			randomLen = idCfg.GetRandom().GetLength()
			if c := strings.TrimSpace(idCfg.GetRandom().GetCharset()); c != "" {
				randomCharset = c
			}
			if p := strings.TrimSpace(idCfg.GetRandom().GetRedisKeyPrefix()); p != "" {
				randomPrefix = p
			}
			if idCfg.GetRandom().GetMaxRetries() > 0 {
				randomMaxRetries = idCfg.GetRandom().GetMaxRetries()
			}
		}
		if idCfg.GetSequence() != nil && strings.TrimSpace(idCfg.GetSequence().GetRedisKeyPrefix()) != "" {
			seqPrefix = strings.TrimSpace(idCfg.GetSequence().GetRedisKeyPrefix())
		}
		if idCfg.GetResources() != nil {
			resourceUsers = strings.TrimSpace(idCfg.GetResources().GetUsers())
			resourceSessions = strings.TrimSpace(idCfg.GetResources().GetSessions())
			resourceDocuments = strings.TrimSpace(idCfg.GetResources().GetDocuments())
		}
	}

	sf, err := pkgidgen.NewSnowflake(nodeID)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:               cfg,
		rdb:               rdb,
		projectRepo:       projectRepo,
		snowflake:         sf,
		randomCfg: pkgidgen.RandomConfig{
			Length:     int(randomLen),
			Charset:    randomCharset,
			MaxRetries: int(randomMaxRetries),
		}.WithDefaults(),
		randomPrefix:      randomPrefix,
		seqPrefix:         seqPrefix,
		defaultStrategy:   defaultStrategy,
		resourceUsers:     resourceUsers,
		resourceSessions:  resourceSessions,
		resourceDocuments: resourceDocuments,
	}, nil
}

var _ domainidgen.Generator = (*Service)(nil)

func (s *Service) NewID(ctx context.Context, projectID string, resource domainidgen.Resource) (string, error) {
	switch s.resolveStrategy(ctx, projectID, resource) {
	case pkgidgen.StrategyULID:
		return pkgidgen.ULID().String(), nil
	case pkgidgen.StrategySnowflake:
		return s.snowflake.NextString(), nil
	case pkgidgen.StrategySequence:
		return s.nextSequence(ctx, projectID, resource)
	case pkgidgen.StrategyRandom:
		return s.nextRandom(ctx, projectID, resource)
	default:
		return pkgidgen.UUID().String(), nil
	}
}

func (s *Service) resolveStrategy(ctx context.Context, projectID string, resource domainidgen.Resource) string {
	platformDefault := s.defaultStrategy
	switch resource {
	case domainidgen.ResourceUsers:
		if s.resourceUsers != "" {
			platformDefault = pkgidgen.NormalizeStrategy(s.resourceUsers)
		}
	case domainidgen.ResourceSessions:
		if s.resourceSessions != "" {
			platformDefault = pkgidgen.NormalizeStrategy(s.resourceSessions)
		}
	case domainidgen.ResourceDocuments:
		if s.resourceDocuments != "" {
			platformDefault = pkgidgen.NormalizeStrategy(s.resourceDocuments)
		}
	}

	if projectID == "" || s.projectRepo == nil {
		return platformDefault
	}
	project, err := s.projectRepo.GetProject(ctx, projectID)
	if err != nil || project == nil {
		return platformDefault
	}
	return pkgidgen.NormalizeStrategy(projects.IDGenStrategyForResource(project.Settings, string(resource), platformDefault))
}

func (s *Service) nextSequence(ctx context.Context, projectID string, resource domainidgen.Resource) (string, error) {
	if s.rdb == nil {
		return pkgidgen.UUID().String(), nil
	}
	key := fmt.Sprintf("%s:%s:%s", s.seqPrefix, projectID, resource)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("sequence id generation failed: %w", err)
	}
	return fmt.Sprintf("%d", n), nil
}
