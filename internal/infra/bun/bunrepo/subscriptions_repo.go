package bunrepo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/subscriptions"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/uptrace/bun/driver/pgdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type subscriptionPlanRepo struct {
	db *clients.Database
}

// NewSubscriptionPlanRepository 构造计划仓储。
func NewSubscriptionPlanRepository(db *clients.Database) subscriptions.PlanRepo {
	return &subscriptionPlanRepo{db: db}
}

func (r *subscriptionPlanRepo) Insert(ctx context.Context, plan *subscriptions.Plan) error {
	_, err := r.db.Conn(ctx).NewInsert().Model(mapPlanToModel(plan)).Exec(ctx)
	if isSubUniqueViolation(err) {
		return subscriptions.ErrDuplicateCode
	}
	return err
}

func (r *subscriptionPlanRepo) GetByID(ctx context.Context, projectID, planID string) (*subscriptions.Plan, error) {
	return r.selectPlan(ctx, projectID, "sp.id = ?", planID, "")
}

func (r *subscriptionPlanRepo) GetByCode(ctx context.Context, projectID, code string) (*subscriptions.Plan, error) {
	return r.selectPlan(ctx, projectID, "sp.code = ?", code, "")
}

func (r *subscriptionPlanRepo) GetByCodeForShare(ctx context.Context, projectID, code string) (*subscriptions.Plan, error) {
	return r.selectPlan(ctx, projectID, "sp.code = ?", code, "SHARE")
}

func (r *subscriptionPlanRepo) GetByIDForShare(ctx context.Context, projectID, planID string) (*subscriptions.Plan, error) {
	return r.selectPlan(ctx, projectID, "sp.id = ?", planID, "SHARE")
}

func (r *subscriptionPlanRepo) selectPlan(ctx context.Context, projectID, pred string, arg any, lock string) (*subscriptions.Plan, error) {
	q := r.db.Conn(ctx).NewSelect().Model((*model.SubscriptionPlan)(nil)).
		Where("sp.project_id = ?", projectID).
		Where(pred, arg)
	if lock != "" {
		q = q.For(lock)
	}
	m := new(model.SubscriptionPlan)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapPlanToDomain(m)
}

func (r *subscriptionPlanRepo) List(ctx context.Context, projectID string, includeArchived bool, limit int, before time.Time) ([]subscriptions.Plan, error) {
	var rows []model.SubscriptionPlan
	q := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("sp.project_id = ?", projectID).
		Where("sp.created_at < ?", before)
	if !includeArchived {
		q = q.Where("sp.status = ?", string(subscriptions.PlanStatusActive))
	}
	if err := q.Order("sp.created_at DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]subscriptions.Plan, 0, len(rows))
	for i := range rows {
		p, err := mapPlanToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, nil
}

func (r *subscriptionPlanRepo) Update(ctx context.Context, plan *subscriptions.Plan) error {
	_, err := r.db.Conn(ctx).NewUpdate().Model(mapPlanToModel(plan)).
		WherePK().
		Where("sp.project_id = ?", plan.ProjectID).
		Exec(ctx)
	return err
}

type subscriptionRepo struct {
	db *clients.Database
}

// NewSubscriptionRepository 构造订阅仓储。
func NewSubscriptionRepository(db *clients.Database) subscriptions.SubscriptionRepo {
	return &subscriptionRepo{db: db}
}

func (r *subscriptionRepo) Insert(ctx context.Context, sub *subscriptions.Subscription) (*subscriptions.Subscription, bool, error) {
	m, err := mapSubToModel(sub)
	if err != nil {
		return nil, false, err
	}
	res, err := r.db.Conn(ctx).NewInsert().Model(m).
		On("CONFLICT (project_id, idempotency_key) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil, true, nil
	}
	existing, err := r.GetByIdempotencyKey(ctx, sub.ProjectID, sub.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, errors.New("subscriptions: idempotent insert conflict but row not found")
	}
	return existing, false, nil
}

func (r *subscriptionRepo) GetByID(ctx context.Context, projectID, id string) (*subscriptions.Subscription, error) {
	return r.selectSub(ctx, projectID, "ss.id = ?", id, "")
}

func (r *subscriptionRepo) GetByIDForUpdate(ctx context.Context, projectID, id string) (*subscriptions.Subscription, error) {
	return r.selectSub(ctx, projectID, "ss.id = ?", id, "UPDATE")
}

func (r *subscriptionRepo) GetByIdempotencyKey(ctx context.Context, projectID, key string) (*subscriptions.Subscription, error) {
	return r.selectSub(ctx, projectID, "ss.idempotency_key = ?", key, "")
}

func (r *subscriptionRepo) GetByProviderSubID(ctx context.Context, provider, providerSubID string) (*subscriptions.Subscription, error) {
	return r.selectByProvider(ctx, provider, providerSubID, "")
}

func (r *subscriptionRepo) GetByProviderSubIDForUpdate(ctx context.Context, provider, providerSubID string) (*subscriptions.Subscription, error) {
	return r.selectByProvider(ctx, provider, providerSubID, "UPDATE")
}

func (r *subscriptionRepo) selectByProvider(ctx context.Context, provider, providerSubID, lock string) (*subscriptions.Subscription, error) {
	if provider == "" || providerSubID == "" {
		return nil, nil
	}
	q := r.db.Conn(ctx).NewSelect().Model((*model.Subscription)(nil)).
		Where("ss.provider = ?", provider).
		Where("ss.provider_sub_id = ?", providerSubID)
	if lock != "" {
		q = q.For(lock)
	}
	m := new(model.Subscription)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapSubToDomain(m)
}

func (r *subscriptionRepo) selectSub(ctx context.Context, projectID, pred string, arg any, lock string) (*subscriptions.Subscription, error) {
	q := r.db.Conn(ctx).NewSelect().Model((*model.Subscription)(nil)).
		Where(pred, arg)
	if projectID != "" {
		q = q.Where("ss.project_id = ?", projectID)
	}
	if lock != "" {
		q = q.For(lock)
	}
	m := new(model.Subscription)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapSubToDomain(m)
}

func (r *subscriptionRepo) GetCurrentByUser(ctx context.Context, projectID, userID, planID string) (*subscriptions.Subscription, error) {
	q := r.db.Conn(ctx).NewSelect().Model((*model.Subscription)(nil)).
		Where("ss.project_id = ?", projectID).
		Where("ss.user_id = ?", userID)
	if planID != "" {
		q = q.Where("ss.plan_id = ?", planID)
	}
	q = q.OrderExpr("CASE WHEN ss.status IN ('trialing','active','past_due') THEN 0 ELSE 1 END").
		Order("ss.created_at DESC").
		Limit(1)
	m := new(model.Subscription)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapSubToDomain(m)
}

func (r *subscriptionRepo) ListNonTerminalByUserPlan(ctx context.Context, projectID, userID, planID string) ([]subscriptions.Subscription, error) {
	var rows []model.Subscription
	err := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("ss.project_id = ?", projectID).
		Where("ss.user_id = ?", userID).
		Where("ss.plan_id = ?", planID).
		Where("ss.status IN ('trialing','active','past_due')").
		Order("ss.created_at").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return mapSubsToDomain(rows)
}

func (r *subscriptionRepo) ListByUser(ctx context.Context, projectID, userID string, limit int, before time.Time) ([]subscriptions.Subscription, error) {
	var rows []model.Subscription
	err := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("ss.project_id = ?", projectID).
		Where("ss.user_id = ?", userID).
		Where("ss.created_at < ?", before).
		Order("ss.created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return mapSubsToDomain(rows)
}

func (r *subscriptionRepo) ListByProject(ctx context.Context, projectID string, limit int, before time.Time) ([]subscriptions.Subscription, error) {
	var rows []model.Subscription
	err := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("ss.project_id = ?", projectID).
		Where("ss.created_at < ?", before).
		Order("ss.created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return mapSubsToDomain(rows)
}

func (r *subscriptionRepo) Update(ctx context.Context, sub *subscriptions.Subscription, expectStatus subscriptions.Status) error {
	m, err := mapSubToModel(sub)
	if err != nil {
		return err
	}
	res, err := r.db.Conn(ctx).NewUpdate().Model(m).
		WherePK().
		Where("ss.status = ?", string(expectStatus)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return status.Error(codes.Aborted, "subscription concurrently modified")
	}
	return nil
}

func (r *subscriptionRepo) ListDueForBilling(ctx context.Context, now time.Time, limit int) ([]subscriptions.Subscription, error) {
	var rows []model.Subscription
	q := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("ss.mode = ?", string(subscriptions.ModePlatform)).
		Where("ss.status IN ('trialing','active','past_due')").
		Where("(ss.current_period_end <= ? OR (ss.status = 'past_due' AND (ss.grace_until IS NULL OR ss.grace_until <= ?)))", now, now).
		Order("ss.current_period_end").
		Limit(limit)
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	return mapSubsToDomain(rows)
}

func mapPlanToModel(p *subscriptions.Plan) *model.SubscriptionPlan {
	benefits, _ := subscriptions.MarshalBenefits(p.Benefits)
	overrides, _ := subscriptions.MarshalOverrides(p.ProviderOverrides)
	return &model.SubscriptionPlan{
		ID:                p.ID,
		ProjectID:         p.ProjectID,
		Code:              p.Code,
		Name:              p.Name,
		Amount:            p.Amount,
		Currency:          p.Currency,
		Interval:          string(p.Interval),
		IntervalDays:      p.IntervalDays,
		GraceDays:         p.GraceDays,
		TrialDays:         p.TrialDays,
		Benefits:          benefits,
		ProviderOverrides: overrides,
		Status:            string(p.Status),
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
	}
}

func mapPlanToDomain(m *model.SubscriptionPlan) (*subscriptions.Plan, error) {
	benefits, err := subscriptions.UnmarshalBenefits(m.Benefits)
	if err != nil {
		return nil, err
	}
	overrides, err := subscriptions.UnmarshalOverrides(m.ProviderOverrides)
	if err != nil {
		return nil, err
	}
	return &subscriptions.Plan{
		ID:                m.ID,
		ProjectID:         m.ProjectID,
		Code:              m.Code,
		Name:              m.Name,
		Amount:            m.Amount,
		Currency:          m.Currency,
		Interval:          subscriptions.Interval(m.Interval),
		IntervalDays:      m.IntervalDays,
		GraceDays:         m.GraceDays,
		TrialDays:         m.TrialDays,
		Benefits:          benefits,
		ProviderOverrides: overrides,
		Status:            subscriptions.PlanStatus(m.Status),
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}, nil
}

func mapSubToModel(s *subscriptions.Subscription) (*model.Subscription, error) {
	benefits, err := subscriptions.MarshalBenefits(s.Benefits)
	if err != nil {
		return nil, err
	}
	return &model.Subscription{
		ID:                 s.ID,
		ProjectID:          s.ProjectID,
		UserID:             s.UserID,
		PlanID:             s.PlanID,
		Mode:               string(s.Mode),
		Provider:           nullIfEmpty(s.Provider),
		ProviderSubID:      nullIfEmpty(s.ProviderSubID),
		Status:             string(s.Status),
		CurrentPeriodStart: s.CurrentPeriodStart,
		CurrentPeriodEnd:   s.CurrentPeriodEnd,
		CancelAtPeriodEnd:  s.CancelAtPeriodEnd,
		GraceUntil:         s.GraceUntil,
		BillingAssetCode:   nullIfEmpty(s.BillingAssetCode),
		Benefits:           benefits,
		IdempotencyKey:     s.IdempotencyKey,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}, nil
}

func mapSubToDomain(m *model.Subscription) (*subscriptions.Subscription, error) {
	benefits, err := subscriptions.UnmarshalBenefits(m.Benefits)
	if err != nil {
		return nil, err
	}
	return &subscriptions.Subscription{
		ID:                 m.ID,
		ProjectID:          m.ProjectID,
		UserID:             m.UserID,
		PlanID:             m.PlanID,
		Mode:               subscriptions.Mode(m.Mode),
		Provider:           derefString(m.Provider),
		ProviderSubID:      derefString(m.ProviderSubID),
		Status:             subscriptions.Status(m.Status),
		CurrentPeriodStart: m.CurrentPeriodStart,
		CurrentPeriodEnd:   m.CurrentPeriodEnd,
		CancelAtPeriodEnd:  m.CancelAtPeriodEnd,
		GraceUntil:         m.GraceUntil,
		BillingAssetCode:   derefString(m.BillingAssetCode),
		Benefits:           benefits,
		IdempotencyKey:     m.IdempotencyKey,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}, nil
}

func mapSubsToDomain(rows []model.Subscription) ([]subscriptions.Subscription, error) {
	out := make([]subscriptions.Subscription, 0, len(rows))
	for i := range rows {
		s, err := mapSubToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, nil
}

func isSubUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23505"
	}
	s := err.Error()
	return strings.Contains(s, "SQLSTATE 23505") || strings.Contains(s, "unique constraint")
}
