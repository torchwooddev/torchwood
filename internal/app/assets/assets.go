// Package assets 是 v3 统一资产系统的 use-case 聚合（设计 §2）：
// Def CRUD、Grant/Consume/Transfer/Mutate/Expire、只读查询、到期扫描与对账。
package assets

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultListLimit = 25
	maxListLimit     = 100
	maxIdempotency   = 128
	expireBatch      = 500
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var (
	assetOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_asset_ops_total",
		Help: "Total asset operations by kind, class and result.",
	}, []string{"kind", "class", "result"})
	assetNegativeBlockedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "torchwood_asset_negative_quantity_blocked_total",
		Help: "Asset operations rejected to prevent negative quantity.",
	})
	assetLedgerDriftTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "torchwood_asset_ledger_drift_total",
		Help: "Holdings vs ledger replay drifts found by reconcile.",
	})
)

func init() {
	prometheus.MustRegister(assetOpsTotal, assetNegativeBlockedTotal, assetLedgerDriftTotal)
}

type txRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Assets 是资产子域 use-case 聚合。
type Assets struct {
	db       txRunner
	defs     domainassets.DefRepo
	holdings domainassets.HoldingRepo
	ledger   domainassets.LedgerRepo
	events   shared.EventPublisher
	projects projects.Repository
	logger   *slog.Logger
	now      func() time.Time
}

// NewAssets 构造 use-case 聚合（Wire：*clients.Database 满足 txRunner）。
func NewAssets(
	db *clients.Database,
	defs domainassets.DefRepo,
	holdings domainassets.HoldingRepo,
	ledger domainassets.LedgerRepo,
	events shared.EventPublisher,
	logger *slog.Logger,
	projectRepo projects.Repository,
) *Assets {
	return newAssets(db, defs, holdings, ledger, events, logger, projectRepo)
}

func newAssets(
	db txRunner,
	defs domainassets.DefRepo,
	holdings domainassets.HoldingRepo,
	ledger domainassets.LedgerRepo,
	events shared.EventPublisher,
	logger *slog.Logger,
	projectRepo projects.Repository,
) *Assets {
	if logger == nil {
		logger = slog.Default()
	}
	return &Assets{
		db:       db,
		defs:     defs,
		holdings: holdings,
		ledger:   ledger,
		events:   events,
		projects: projectRepo,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (a *Assets) ts() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now().UTC()
}

func newID() string { return idgen.ULID().String() }

func normalizeList(limit int, before time.Time) (int, time.Time) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if before.IsZero() {
		before = time.Now().UTC().Add(time.Hour)
	}
	return limit, before
}

func normalizeExpiry(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC().Truncate(time.Microsecond)
	return &u
}

func sameExpiry(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

func validateCode(code string) (string, error) {
	c := domainassets.NormalizeCode(code)
	if !codePattern.MatchString(c) {
		return "", status.Error(codes.InvalidArgument, "def code must match ^[a-z][a-z0-9_]{0,63}$")
	}
	return c, nil
}

func validateIdempotency(key string) (string, error) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", status.Error(codes.InvalidArgument, domainassets.ErrIdempotencyRequired.Error())
	}
	if len(k) > maxIdempotency {
		return "", status.Errorf(codes.InvalidArgument, "idempotency_key exceeds %d characters", maxIdempotency)
	}
	return k, nil
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, domainassets.ErrMatrix),
		errors.Is(err, domainassets.ErrInvalidQuantity),
		errors.Is(err, domainassets.ErrExpiresAtRequired),
		errors.Is(err, domainassets.ErrInvalidCode),
		errors.Is(err, domainassets.ErrIdempotencyRequired),
		errors.Is(err, domainassets.ErrInvalidOwnerType):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domainassets.ErrInsufficient),
		errors.Is(err, domainassets.ErrMaxQuantity),
		errors.Is(err, domainassets.ErrNotTradable),
		errors.Is(err, domainassets.ErrUniquePerOwner),
		errors.Is(err, domainassets.ErrDefArchived),
		errors.Is(err, domainassets.ErrDuplicateCode):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domainassets.ErrDefNotFound),
		errors.Is(err, domainassets.ErrHoldingNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domainassets.ErrConcurrent):
		return status.Error(codes.Aborted, err.Error())
	default:
		return err
	}
}

func operatorFrom(ctx context.Context) json.RawMessage {
	p, ok := contexts.Principal(ctx)
	if !ok || p == nil {
		return domainassets.MarshalOperator(domainassets.OperatorSnapshot{IsSystem: true})
	}
	return domainassets.MarshalOperator(domainassets.OperatorSnapshot{
		ActorKind:      string(p.ActorKind),
		ActorID:        string(p.ActorID),
		UserID:         p.UserID,
		APIKeyID:       p.APIKeyID,
		CredentialType: string(p.CredentialType),
		IsSystem:       p.ActorKind == shared.ActorKindService && p.APIKeyID == "",
	})
}

func projectScope(ctx context.Context) (string, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" {
		return "", status.Error(codes.Unauthenticated, "missing project context")
	}
	return p.ProjectID, nil
}

func endUser(ctx context.Context) (projectID, userID string, err error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" || p.UserID == "" {
		return "", "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return p.ProjectID, p.UserID, nil
}
