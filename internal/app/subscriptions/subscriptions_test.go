package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appassets "github.com/torchwooddev/torchwood/internal/app/assets"
	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	domainsubs "github.com/torchwooddev/torchwood/internal/domain/subscriptions"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type memStore struct {
	plans  map[string]*domainsubs.Plan
	byCode map[string]string
	subs   map[string]*domainsubs.Subscription
	byIdem map[string]string
	outbox []domainevents.Envelope
	now    time.Time
}

func newMemStore(now time.Time) *memStore {
	return &memStore{
		plans:  map[string]*domainsubs.Plan{},
		byCode: map[string]string{},
		subs:   map[string]*domainsubs.Subscription{},
		byIdem: map[string]string{},
		now:    now,
	}
}

func (s *memStore) Run(_ context.Context, fn func(context.Context) error) error {
	snap := s.snapshot()
	if err := fn(context.Background()); err != nil {
		s.restore(snap)
		return err
	}
	return nil
}

// RunInNewTx 模拟独立事务：与 Run 一样快照回滚（内存 fake 无连接概念）。
func (s *memStore) RunInNewTx(ctx context.Context, fn func(context.Context) error) error {
	return s.Run(ctx, fn)
}

func (s *memStore) snapshot() memStore {
	out := memStore{
		plans:  map[string]*domainsubs.Plan{},
		byCode: map[string]string{},
		subs:   map[string]*domainsubs.Subscription{},
		byIdem: map[string]string{},
		outbox: append([]domainevents.Envelope(nil), s.outbox...),
		now:    s.now,
	}
	for k, v := range s.plans {
		cp := *v
		out.plans[k] = &cp
	}
	for k, v := range s.byCode {
		out.byCode[k] = v
	}
	for k, v := range s.subs {
		out.subs[k] = cloneSub(v)
	}
	for k, v := range s.byIdem {
		out.byIdem[k] = v
	}
	return out
}

func (s *memStore) restore(snap memStore) {
	s.plans = snap.plans
	s.byCode = snap.byCode
	s.subs = snap.subs
	s.byIdem = snap.byIdem
	s.outbox = snap.outbox
}

func cloneSub(in *domainsubs.Subscription) *domainsubs.Subscription {
	if in == nil {
		return nil
	}
	cp := *in
	if in.GraceUntil != nil {
		t := *in.GraceUntil
		cp.GraceUntil = &t
	}
	cp.Benefits.Grants = append([]domainsubs.BenefitGrant(nil), in.Benefits.Grants...)
	cp.Benefits.Entitlements = append([]domainsubs.BenefitEntitlement(nil), in.Benefits.Entitlements...)
	return &cp
}

func (s *memStore) Insert(_ context.Context, plan *domainsubs.Plan) error {
	k := plan.ProjectID + "/" + plan.Code
	if _, ok := s.byCode[k]; ok {
		return domainsubs.ErrDuplicateCode
	}
	cp := *plan
	s.plans[plan.ID] = &cp
	s.byCode[k] = plan.ID
	return nil
}

func (s *memStore) getPlan(projectID, id string) *domainsubs.Plan {
	p := s.plans[id]
	if p == nil || (projectID != "" && p.ProjectID != projectID) {
		return nil
	}
	cp := *p
	return &cp
}

func (s *memStore) GetByID(_ context.Context, projectID, planID string) (*domainsubs.Plan, error) {
	return s.getPlan(projectID, planID), nil
}
func (s *memStore) GetByIDForShare(ctx context.Context, projectID, planID string) (*domainsubs.Plan, error) {
	return s.GetByID(ctx, projectID, planID)
}
func (s *memStore) GetByCode(_ context.Context, projectID, code string) (*domainsubs.Plan, error) {
	id := s.byCode[projectID+"/"+code]
	return s.getPlan(projectID, id), nil
}
func (s *memStore) GetByCodeForShare(ctx context.Context, projectID, code string) (*domainsubs.Plan, error) {
	return s.GetByCode(ctx, projectID, code)
}
func (s *memStore) List(context.Context, string, bool, int, time.Time) ([]domainsubs.Plan, error) {
	return nil, nil
}
func (s *memStore) Update(_ context.Context, plan *domainsubs.Plan) error {
	s.plans[plan.ID] = &domainsubs.Plan{}
	cp := *plan
	s.plans[plan.ID] = &cp
	return nil
}

type memSubs struct{ s *memStore }

func (r memSubs) Insert(_ context.Context, sub *domainsubs.Subscription) (*domainsubs.Subscription, bool, error) {
	k := sub.ProjectID + "/" + sub.IdempotencyKey
	if id, ok := r.s.byIdem[k]; ok {
		return cloneSub(r.s.subs[id]), false, nil
	}
	r.s.subs[sub.ID] = cloneSub(sub)
	r.s.byIdem[k] = sub.ID
	return nil, true, nil
}
func (r memSubs) GetByID(_ context.Context, projectID, id string) (*domainsubs.Subscription, error) {
	sub := r.s.subs[id]
	if sub == nil || (projectID != "" && sub.ProjectID != projectID) {
		return nil, nil
	}
	return cloneSub(sub), nil
}
func (r memSubs) GetByIDForUpdate(ctx context.Context, projectID, id string) (*domainsubs.Subscription, error) {
	return r.GetByID(ctx, projectID, id)
}
func (r memSubs) GetByIdempotencyKey(_ context.Context, projectID, key string) (*domainsubs.Subscription, error) {
	id := r.s.byIdem[projectID+"/"+key]
	return cloneSub(r.s.subs[id]), nil
}
func (r memSubs) GetByProviderSubID(_ context.Context, projectID, provider, providerSubID string) (*domainsubs.Subscription, error) {
	for _, sub := range r.s.subs {
		if sub.ProjectID == projectID && sub.Provider == provider && sub.ProviderSubID == providerSubID {
			return cloneSub(sub), nil
		}
	}
	return nil, nil
}
func (r memSubs) GetByProviderSubIDForUpdate(ctx context.Context, projectID, provider, providerSubID string) (*domainsubs.Subscription, error) {
	return r.GetByProviderSubID(ctx, projectID, provider, providerSubID)
}
func (r memSubs) GetCurrentByUser(_ context.Context, projectID, userID, planID string) (*domainsubs.Subscription, error) {
	var best *domainsubs.Subscription
	for _, sub := range r.s.subs {
		if sub.ProjectID != projectID || sub.UserID != userID {
			continue
		}
		if planID != "" && sub.PlanID != planID {
			continue
		}
		if best == nil || (!sub.Status.IsTerminal() && best.Status.IsTerminal()) || sub.CreatedAt.After(best.CreatedAt) {
			best = sub
		}
	}
	return cloneSub(best), nil
}
func (r memSubs) ListNonTerminalByUserPlan(_ context.Context, projectID, userID, planID string) ([]domainsubs.Subscription, error) {
	var out []domainsubs.Subscription
	for _, sub := range r.s.subs {
		if sub.ProjectID == projectID && sub.UserID == userID && sub.PlanID == planID && !sub.Status.IsTerminal() {
			out = append(out, *cloneSub(sub))
		}
	}
	return out, nil
}
func (r memSubs) ListByUser(context.Context, string, string, int, time.Time) ([]domainsubs.Subscription, error) {
	return nil, nil
}
func (r memSubs) ListByProject(context.Context, string, int, time.Time) ([]domainsubs.Subscription, error) {
	return nil, nil
}
func (r memSubs) Update(_ context.Context, sub *domainsubs.Subscription, expect domainsubs.Status) error {
	cur := r.s.subs[sub.ID]
	if cur == nil || cur.Status != expect {
		return domainsubs.ErrConcurrent
	}
	r.s.subs[sub.ID] = cloneSub(sub)
	return nil
}
func (r memSubs) ListDueForBillingInProject(_ context.Context, projectID string, now time.Time, limit int) ([]domainsubs.Subscription, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []domainsubs.Subscription
	for _, sub := range r.s.subs {
		if sub.ProjectID != projectID {
			continue
		}
		if sub.Mode != domainsubs.ModePlatform {
			continue
		}
		switch sub.Status {
		case domainsubs.StatusTrialing, domainsubs.StatusActive, domainsubs.StatusPastDue:
		default:
			continue
		}
		due := sub.PeriodDue(now) || (sub.Status == domainsubs.StatusPastDue && sub.GraceElapsed(now))
		if !due {
			continue
		}
		out = append(out, *cloneSub(sub))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type memPublisher struct{ s *memStore }

func (p memPublisher) Publish(_ context.Context, ev domainevents.Envelope) error {
	p.s.outbox = append(p.s.outbox, ev)
	return nil
}

type stubAssets struct {
	grants   int
	consumes int
	mutates  int
	grantErr error
	consErr  error
	holding  *domainassets.Holding
}

func (a *stubAssets) Grant(context.Context, appassets.GrantCommand) (*appassets.OpResult, error) {
	a.grants++
	if a.grantErr != nil {
		return nil, a.grantErr
	}
	return &appassets.OpResult{}, nil
}
func (a *stubAssets) Consume(context.Context, appassets.ConsumeCommand) (*appassets.OpResult, error) {
	a.consumes++
	if a.consErr != nil {
		return nil, a.consErr
	}
	return &appassets.OpResult{}, nil
}
func (a *stubAssets) Mutate(context.Context, appassets.MutateCommand) (*appassets.OpResult, error) {
	a.mutates++
	return &appassets.OpResult{}, nil
}
func (a *stubAssets) LiveHoldingForUpdate(context.Context, string, string) (*domainassets.Holding, error) {
	return a.holding, nil
}

type listProjectsStub struct {
	projects.Repository
	list []projects.Project
}

func (s *listProjectsStub) ListProjects(context.Context) ([]projects.Project, error) {
	return s.list, nil
}

func userCtx(projectID, userID string) context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorKind:      shared.ActorKindEndUser,
		ProjectID:      projectID,
		UserID:         userID,
		CredentialType: shared.CredentialTypeSession,
	})
}

func adminCtx(projectID string) context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorKind:       shared.ActorKindAdmin,
		ProjectID:       projectID,
		IsPlatformAdmin: true,
		Roles:           []string{"admin"},
		CredentialType:  shared.CredentialTypeSession,
	})
}

func setupSub(t *testing.T, now time.Time, assets assetOps) (*Subscriptions, *memStore, *stubAssets) {
	t.Helper()
	store := newMemStore(now)
	st, _ := assets.(*stubAssets)
	if st == nil {
		st = &stubAssets{}
		assets = st
	}
	uc := newSubscriptions(nil, store, store, memSubs{store}, assets, nil, nil, nil, memPublisher{store}, nil, &listProjectsStub{list: []projects.Project{{ID: "proj", Status: "active"}}}, nil)
	uc.now = func() time.Time { return store.now }
	return uc, store, st
}

func seedPlan(t *testing.T, store *memStore, now time.Time) *domainsubs.Plan {
	t.Helper()
	plan := &domainsubs.Plan{
		ID:           "plan_pro",
		ProjectID:    "proj",
		Code:         "pro",
		Name:         "Pro",
		Amount:       1000,
		Currency:     "USD",
		Interval:     domainsubs.IntervalCustomDays,
		IntervalDays: 30,
		GraceDays:    3,
		Benefits: domainsubs.Benefits{
			Grants:       []domainsubs.BenefitGrant{{AssetCode: "gold", Quantity: 100}},
			Entitlements: []domainsubs.BenefitEntitlement{{AssetCode: "vip", Tier: 1}},
		},
		Status:    domainsubs.PlanStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, store.Insert(context.Background(), plan))
	return plan
}

func seedPlatformSub(t *testing.T, store *memStore, plan *domainsubs.Plan, now time.Time, status domainsubs.Status) *domainsubs.Subscription {
	t.Helper()
	sub := &domainsubs.Subscription{
		ID:                 "sub_1",
		ProjectID:          "proj",
		UserID:             "user_1",
		PlanID:             plan.ID,
		Mode:               domainsubs.ModePlatform,
		Status:             status,
		CurrentPeriodStart: now.Add(-30 * 24 * time.Hour),
		CurrentPeriodEnd:   now,
		Benefits:           plan.Benefits,
		BillingAssetCode:   "gold",
		IdempotencyKey:     "idem-1",
		CreatedAt:          now.Add(-30 * 24 * time.Hour),
		UpdatedAt:          now,
	}
	_, inserted, err := memSubs{store}.Insert(context.Background(), sub)
	require.NoError(t, err)
	require.True(t, inserted)
	return sub
}

func TestBilling_ChargeFailPastDueThenRetryActiveThenExpire(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	assets := &stubAssets{consErr: domainassets.ErrInsufficient}
	uc, store, _ := setupSub(t, now, assets)
	plan := seedPlan(t, store, now)
	seedPlatformSub(t, store, plan, now, domainsubs.StatusActive)

	n, err := uc.RunBillingCycle(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	sub := store.subs["sub_1"]
	require.Equal(t, domainsubs.StatusPastDue, sub.Status)
	require.NotNil(t, sub.GraceUntil)
	require.Equal(t, now.Add(3*24*time.Hour), sub.GraceUntil.UTC())
	require.Equal(t, now, sub.CurrentPeriodEnd) // 状态不前进周期
	require.Equal(t, domainsubs.EventPastDue, store.outbox[len(store.outbox)-1].Event)

	// 宽限内重试成功 → active，周期前进，benefits 履约。
	assets.consErr = nil
	store.now = now.Add(24 * time.Hour)
	n, err = uc.RunBillingCycle(context.Background(), store.now)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	sub = store.subs["sub_1"]
	require.Equal(t, domainsubs.StatusActive, sub.Status)
	require.Nil(t, sub.GraceUntil)
	require.True(t, sub.CurrentPeriodEnd.After(now))
	require.Greater(t, assets.grants, 0)
	require.Equal(t, domainsubs.EventRenewed, store.outbox[len(store.outbox)-1].Event)

	// 再到期且宽限过期：扣款失败 → past_due → 同轮或下轮 expired。
	assets.consErr = domainassets.ErrInsufficient
	store.now = sub.CurrentPeriodEnd
	n, err = uc.RunBillingCycle(context.Background(), store.now)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))
	sub = store.subs["sub_1"]
	require.Equal(t, domainsubs.StatusPastDue, sub.Status)

	store.now = sub.GraceUntil.Add(time.Second)
	n, err = uc.RunBillingCycle(context.Background(), store.now)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, domainsubs.StatusExpired, store.subs["sub_1"].Status)
	require.Equal(t, domainsubs.EventExpired, store.outbox[len(store.outbox)-1].Event)
}

func TestBilling_BenefitFailureDoesNotAdvance(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	assets := &stubAssets{grantErr: errors.New("grant boom")}
	uc, store, _ := setupSub(t, now, assets)
	plan := seedPlan(t, store, now)
	seedPlatformSub(t, store, plan, now, domainsubs.StatusActive)

	err := uc.processDue(context.Background(), store.subs["sub_1"], now)
	require.Error(t, err)
	sub := store.subs["sub_1"]
	require.Equal(t, domainsubs.StatusActive, sub.Status)
	require.Equal(t, now, sub.CurrentPeriodEnd)
	require.Empty(t, store.outbox)
}

func TestCancelAtPeriodEnd(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, _ := setupSub(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	sub := seedPlatformSub(t, store, plan, now.Add(10*24*time.Hour), domainsubs.StatusActive)
	sub.CurrentPeriodEnd = now.Add(10 * 24 * time.Hour)
	store.subs[sub.ID] = cloneSub(sub)

	ctx := userCtx("proj", "user_1")
	got, _, err := uc.CancelAtPeriodEnd(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, got.CancelAtPeriodEnd)
	require.Equal(t, domainsubs.StatusActive, got.Status)

	store.now = got.CurrentPeriodEnd
	n, err := uc.RunBillingCycle(context.Background(), store.now)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, domainsubs.StatusCanceled, store.subs[sub.ID].Status)
	require.Equal(t, domainsubs.EventCanceled, store.outbox[len(store.outbox)-1].Event)
}

func TestHostedWebhookReplayIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	assets := &stubAssets{}
	uc, store, _ := setupSub(t, now, assets)
	plan := seedPlan(t, store, now)
	sub := &domainsubs.Subscription{
		ID:                 "sub_h",
		ProjectID:          "proj",
		UserID:             "user_1",
		PlanID:             plan.ID,
		Mode:               domainsubs.ModeHosted,
		Provider:           domainpayments.ProviderStripe,
		Status:             domainsubs.StatusTrialing,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
		Benefits:           plan.Benefits,
		IdempotencyKey:     "hosted-1",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_, _, err := memSubs{store}.Insert(context.Background(), sub)
	require.NoError(t, err)

	ev := &domainpayments.CallbackEvent{
		Provider:            domainpayments.ProviderStripe,
		ProviderEventID:     "evt_1",
		Type:                domainpayments.CallbackSubscriptionActivated,
		LocalSubscriptionID: "sub_h",
		ProviderSubID:       "sub_stripe_1",
		MetadataProjectID:   "proj",
		PeriodStart:         now,
		PeriodEnd:           now.Add(30 * 24 * time.Hour),
		ReceivedAt:          now,
	}
	require.NoError(t, store.Run(context.Background(), func(ctx context.Context) error {
		return uc.HandleHostedCallback(ctx, ev)
	}))
	require.Equal(t, domainsubs.StatusActive, store.subs["sub_h"].Status)
	require.Equal(t, "sub_stripe_1", store.subs["sub_h"].ProviderSubID)
	require.Greater(t, assets.grants, 0)
	events := len(store.outbox)

	// 同一事件再处理（payments 层锚点二会短路；此处验证再入状态不回退）。
	require.NoError(t, store.Run(context.Background(), func(ctx context.Context) error {
		return uc.HandleHostedCallback(ctx, ev)
	}))
	require.Equal(t, domainsubs.StatusActive, store.subs["sub_h"].Status)
	require.GreaterOrEqual(t, len(store.outbox), events)
}

func TestEntitlementRenewalMutatesExistingHolding(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	holding := &domainassets.Holding{ID: "h_vip", OwnerID: "user_1"}
	assets := &stubAssets{holding: holding}
	uc, store, _ := setupSub(t, now, assets)
	plan := seedPlan(t, store, now)
	seedPlatformSub(t, store, plan, now, domainsubs.StatusActive)

	require.NoError(t, uc.processDue(context.Background(), store.subs["sub_1"], now))
	require.Equal(t, 1, assets.mutates)
	require.Equal(t, 1, assets.grants) // gold grant only
	require.Equal(t, domainsubs.StatusActive, store.subs["sub_1"].Status)
}

func TestSubscribeIdempotentReplay(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, _ := setupSub(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	plan.Amount = 0
	store.plans[plan.ID].Amount = 0

	ctx := userCtx("proj", "user_1")
	r1, err := uc.Subscribe(ctx, SubscribeCommand{PlanCode: "pro", Mode: domainsubs.ModePlatform, IdempotencyKey: "k1"})
	require.NoError(t, err)
	require.False(t, r1.IdempotentReplay)
	require.Equal(t, domainsubs.StatusActive, r1.Subscription.Status)

	r2, err := uc.Subscribe(ctx, SubscribeCommand{PlanCode: "pro", Mode: domainsubs.ModePlatform, IdempotencyKey: "k1"})
	require.NoError(t, err)
	require.True(t, r2.IdempotentReplay)
	require.Equal(t, r1.Subscription.ID, r2.Subscription.ID)
}

func TestCreatePlanRequiresWriteActor(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, _, _ := setupSub(t, now, &stubAssets{})
	_, err := uc.CreatePlan(userCtx("proj", "user_1"), CreatePlanCommand{
		Code: "pro", Name: "Pro", Amount: 1, Currency: "USD", Interval: domainsubs.IntervalMonth,
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())

	plan, err := uc.CreatePlan(adminCtx("proj"), CreatePlanCommand{
		Code: "pro", Name: "Pro", Amount: 1999, Currency: "usd", Interval: domainsubs.IntervalMonth, GraceDays: 7,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1999), plan.Amount)
	require.Equal(t, "USD", plan.Currency)
	require.Equal(t, int32(7), plan.GraceDays)
}

func TestIsSubscriptionEvent(t *testing.T) {
	require.True(t, domainpayments.IsSubscriptionEvent(domainpayments.CallbackSubscriptionPastDue))
	require.False(t, domainpayments.IsSubscriptionEvent(domainpayments.CallbackPaid))
}

func TestProcessDueSkipsHosted(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, assets := setupSub(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	sub := seedPlatformSub(t, store, plan, now, domainsubs.StatusActive)
	sub.Mode = domainsubs.ModeHosted
	store.subs[sub.ID] = cloneSub(sub)
	n, err := uc.RunBillingCycle(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
	require.Equal(t, 0, assets.consumes)
	require.Equal(t, domainsubs.StatusActive, store.subs[sub.ID].Status)
}

func TestFulfillPaidOrderActivates(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, _ := setupSub(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	sub := seedPlatformSub(t, store, plan, now, domainsubs.StatusTrialing)
	order := &domainpayments.Order{
		ID:          "ord_1",
		ProjectID:   "proj",
		UserID:      "user_1",
		PurposeKind: domainpayments.PurposeSubscription,
		Purpose:     purposeJSON(sub.ID, plan.Code, "activate"),
	}
	ref, err := uc.FulfillPaidOrder(withSystemPrincipal(context.Background(), "proj"), order)
	require.NoError(t, err)
	require.Equal(t, "subscription:"+sub.ID, ref)
	require.Equal(t, domainsubs.StatusActive, store.subs[sub.ID].Status)
}

func TestForceExpire(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, _ := setupSub(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	seedPlatformSub(t, store, plan, now, domainsubs.StatusPastDue)
	got, _, err := uc.ForceExpire(adminCtx("proj"), "sub_1")
	require.NoError(t, err)
	require.Equal(t, domainsubs.StatusExpired, got.Status)
}

func TestBillingInsufficientUsesFailedPreconditionMessage(t *testing.T) {
	err := status.Error(codes.FailedPrecondition, "assets: insufficient quantity: have 0, want 10")
	require.True(t, isInsufficient(err))
	require.False(t, isInsufficient(fmt.Errorf("boom")))
}

type memPayStore struct {
	orders map[string]*domainpayments.Order
	byIdem map[string]string
	index  map[string]string
}

func newMemPayStore() *memPayStore {
	return &memPayStore{
		orders: map[string]*domainpayments.Order{},
		byIdem: map[string]string{},
		index:  map[string]string{},
	}
}

func payIdemKey(projectID, key string) string { return projectID + "\x00" + key }
func payIndexKey(provider, kind, ref string) string {
	return provider + "|" + kind + "|" + ref
}

func clonePayOrder(o *domainpayments.Order) *domainpayments.Order {
	if o == nil {
		return nil
	}
	cp := *o
	if o.Purpose != nil {
		cp.Purpose = append(json.RawMessage(nil), o.Purpose...)
	}
	return &cp
}

func (s *memPayStore) Insert(_ context.Context, order *domainpayments.Order) (*domainpayments.Order, bool, error) {
	k := payIdemKey(order.ProjectID, order.IdempotencyKey)
	if id, ok := s.byIdem[k]; ok {
		return clonePayOrder(s.orders[id]), false, nil
	}
	s.orders[order.ID] = clonePayOrder(order)
	s.byIdem[k] = order.ID
	return nil, true, nil
}

func (s *memPayStore) GetByID(_ context.Context, projectID, orderID string) (*domainpayments.Order, error) {
	return s.getPay(projectID, orderID)
}
func (s *memPayStore) GetByIDForUpdate(ctx context.Context, projectID, orderID string) (*domainpayments.Order, error) {
	return s.GetByID(ctx, projectID, orderID)
}
func (s *memPayStore) getPay(projectID, orderID string) (*domainpayments.Order, error) {
	o := s.orders[orderID]
	if o == nil || (projectID != "" && o.ProjectID != projectID) {
		return nil, nil
	}
	return clonePayOrder(o), nil
}
func (s *memPayStore) GetByProviderRef(context.Context, string, string, string, string) (*domainpayments.Order, error) {
	return nil, nil
}
func (s *memPayStore) Update(_ context.Context, order *domainpayments.Order, expect domainpayments.OrderStatus) error {
	cur := s.orders[order.ID]
	if cur == nil || cur.Status != expect {
		return status.Error(codes.Aborted, "payment order concurrently modified")
	}
	s.orders[order.ID] = clonePayOrder(order)
	return nil
}
func (s *memPayStore) ListByUser(context.Context, string, string, int, time.Time) ([]domainpayments.Order, error) {
	return nil, nil
}
func (s *memPayStore) ListByProject(context.Context, string, int, time.Time) ([]domainpayments.Order, error) {
	return nil, nil
}
func (s *memPayStore) CloseExpiredInProject(context.Context, string, time.Time, int) (int64, error) {
	return 0, nil
}
func (s *memPayStore) Lookup(_ context.Context, provider, kind, ref string) (string, error) {
	return s.index[payIndexKey(provider, kind, ref)], nil
}
func (s *memPayStore) Upsert(_ context.Context, provider, kind, ref, projectID string) error {
	s.index[payIndexKey(provider, kind, ref)] = projectID
	return nil
}

type subFakeProvider struct {
	createCalls int
	session     *domainpayments.PaymentSession
}

func (f *subFakeProvider) Name() string { return domainpayments.ProviderStripe }
func (f *subFakeProvider) CreatePayment(_ context.Context, _ domainpayments.CreatePaymentInput) (*domainpayments.PaymentSession, error) {
	f.createCalls++
	if f.session != nil {
		return f.session, nil
	}
	return &domainpayments.PaymentSession{SessionID: "cs_sub", PaymentURL: "https://pay.example/cs_sub"}, nil
}
func (f *subFakeProvider) VerifyCallback(context.Context, http.Header, []byte) (*domainpayments.CallbackEvent, error) {
	return nil, domainpayments.ErrSignatureInvalid
}
func (f *subFakeProvider) Refund(context.Context, domainpayments.RefundInput) (*domainpayments.RefundResult, error) {
	return nil, domainpayments.ErrUnsupported
}

type subFakeRegistry struct {
	p domainpayments.PaymentProvider
}

func (r subFakeRegistry) Get(name string) (domainpayments.PaymentProvider, error) {
	if r.p == nil || r.p.Name() != name {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	return r.p, nil
}

func setupSubPay(t *testing.T, now time.Time, assets assetOps) (*Subscriptions, *memStore, *memPayStore, *subFakeProvider) {
	t.Helper()
	store := newMemStore(now)
	st, _ := assets.(*stubAssets)
	if st == nil {
		st = &stubAssets{}
		assets = st
	}
	pay := newMemPayStore()
	fp := &subFakeProvider{}
	uc := newSubscriptions(nil, store, store, memSubs{store}, assets, pay, subFakeRegistry{fp}, nil, memPublisher{store}, nil, &listProjectsStub{list: []projects.Project{{ID: "proj", Status: "active"}}}, pay)
	uc.now = func() time.Time { return store.now }
	return uc, store, pay, fp
}

func TestSubscribe_PlatformCreatesSubscriptionOrder(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, pay, fp := setupSubPay(t, now, &stubAssets{})
	seedPlan(t, store, now)

	got, err := uc.Subscribe(userCtx("proj", "user_1"), SubscribeCommand{
		PlanCode: "pro", Mode: domainsubs.ModePlatform, IdempotencyKey: "k-pay",
	})
	require.NoError(t, err)
	require.Equal(t, domainsubs.StatusTrialing, got.Subscription.Status)
	require.NotEmpty(t, got.OrderID)
	require.Equal(t, "https://pay.example/cs_sub", got.PaymentURL)
	require.Equal(t, 1, fp.createCalls)
	require.Equal(t, 1, len(pay.orders))

	order := pay.orders[got.OrderID]
	require.NotNil(t, order)
	require.Equal(t, domainpayments.PurposeSubscription, order.PurposeKind)
	require.Equal(t, "sub:"+got.Subscription.ID+":activate", order.IdempotencyKey)
	require.Equal(t, domainpayments.OrderStatusPaying, order.Status)
	require.Equal(t, "proj", pay.index[payIndexKey(order.Provider, domainpayments.IndexKindPaymentSession, order.ID)])
	require.Equal(t, "proj", pay.index[payIndexKey(order.Provider, domainpayments.IndexKindPaymentSession, "cs_sub")])
}

func TestProcessDue_SubscriptionOrderIdempotentByCycle(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	uc, store, pay, fp := setupSubPay(t, now, &stubAssets{})
	plan := seedPlan(t, store, now)
	sub := seedPlatformSub(t, store, plan, now, domainsubs.StatusActive)
	sub.BillingAssetCode = ""
	store.subs[sub.ID] = cloneSub(sub)

	require.NoError(t, uc.processDue(context.Background(), store.subs[sub.ID], now))
	require.Equal(t, domainsubs.StatusActive, store.subs[sub.ID].Status)
	require.Equal(t, 1, fp.createCalls)
	require.Equal(t, 1, len(pay.orders))
	for _, o := range pay.orders {
		require.Equal(t, domainpayments.OrderStatusPaying, o.Status)
	}

	cycle := strconv.FormatInt(sub.CurrentPeriodEnd.UTC().Unix(), 10)
	var order *domainpayments.Order
	for _, o := range pay.orders {
		order = o
	}
	require.NotNil(t, order)
	require.Equal(t, domainpayments.PurposeSubscription, order.PurposeKind)
	require.Equal(t, "sub:"+sub.ID+":"+cycle, order.IdempotencyKey)

	require.NoError(t, uc.processDue(context.Background(), store.subs[sub.ID], now))
	require.Equal(t, 1, fp.createCalls, "同 cycle 幂等不得二次 CreatePayment")
	require.Equal(t, 1, len(pay.orders))
}
