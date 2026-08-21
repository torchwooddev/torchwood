package subscriptions

import (
	"context"
	"strings"
	"time"

	appassets "github.com/torchwooddev/torchwood/internal/app/assets"
	apppayments "github.com/torchwooddev/torchwood/internal/app/payments"
	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	domainsubs "github.com/torchwooddev/torchwood/internal/domain/subscriptions"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SubscribeCommand 是终端用户订阅入参。
type SubscribeCommand struct {
	PlanCode         string
	Mode             domainsubs.Mode
	IdempotencyKey   string
	BillingAssetCode string
}

// SubscribeResult 是 Subscribe 结果。
type SubscribeResult struct {
	Subscription     *domainsubs.Subscription
	Plan             *domainsubs.Plan
	PaymentURL       string
	OrderID          string
	IdempotentReplay bool
}

// Subscribe 创建订阅（Client 面）。
func (s *Subscriptions) Subscribe(ctx context.Context, cmd SubscribeCommand) (*SubscribeResult, error) {
	projectID, userID, err := endUser(ctx)
	if err != nil {
		return nil, err
	}
	if !cmd.Mode.IsValid() {
		return nil, status.Errorf(codes.InvalidArgument, "mode must be hosted or platform")
	}
	key, err := validateIdempotency(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	code, err := validateCode(cmd.PlanCode)
	if err != nil {
		return nil, err
	}

	plan, err := s.plans.GetByCode(ctx, projectID, code)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, status.Error(codes.NotFound, "plan not found")
	}
	if plan.Status != domainsubs.PlanStatusActive {
		return nil, status.Error(codes.FailedPrecondition, domainsubs.ErrPlanArchived.Error())
	}

	now := s.ts()
	periodStart := now
	periodEnd, err := domainsubs.NextPeriodEnd(periodStart, plan.Interval, plan.IntervalDays)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	initial := domainsubs.StatusActive
	if plan.TrialDays > 0 {
		initial = domainsubs.StatusTrialing
		periodEnd = periodStart.Add(time.Duration(plan.TrialDays) * 24 * time.Hour)
	}

	sub := &domainsubs.Subscription{
		ID:                 newID(),
		ProjectID:          projectID,
		UserID:             userID,
		PlanID:             plan.ID,
		Mode:               cmd.Mode,
		Status:             initial,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Benefits:           plan.Benefits,
		IdempotencyKey:     key,
		BillingAssetCode:   strings.TrimSpace(cmd.BillingAssetCode),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if cmd.Mode == domainsubs.ModeHosted {
		sub.Provider = domainpayments.ProviderStripe
		if initial != domainsubs.StatusTrialing {
			sub.Status = domainsubs.StatusTrialing // 等 webhook 确认后转 active
		}
	}

	var result SubscribeResult
	result.Plan = plan
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		existing, inserted, err := s.subs.Insert(txCtx, sub)
		if err != nil {
			return err
		}
		if !inserted {
			result.Subscription = existing
			result.IdempotentReplay = true
			return nil
		}
		live, err := s.subs.ListNonTerminalByUserPlan(txCtx, projectID, userID, plan.ID)
		if err != nil {
			return err
		}
		for i := range live {
			if live[i].ID != sub.ID {
				return domainsubs.ErrAlreadySubscribed
			}
		}

		switch cmd.Mode {
		case domainsubs.ModeHosted:
			return s.startHosted(txCtx, sub, plan, &result)
		case domainsubs.ModePlatform:
			return s.startPlatform(txCtx, sub, plan, &result, now)
		}
		return domainsubs.ErrInvalidMode
	})
	if err != nil {
		return nil, mapError(err)
	}
	if result.Subscription == nil {
		result.Subscription = sub
	}
	return &result, nil
}

func (s *Subscriptions) startHosted(ctx context.Context, sub *domainsubs.Subscription, plan *domainsubs.Plan, result *SubscribeResult) error {
	if s.hosted == nil {
		return domainsubs.ErrNotConfigured
	}
	if plan.ProviderOverrides.StripePriceID == "" {
		return status.Error(codes.FailedPrecondition, "plan is missing stripe_price_id for hosted mode")
	}
	success, cancel := s.checkoutURLs()
	sess, err := s.hosted.CreateCheckout(ctx, domainsubs.HostedCheckoutInput{
		SubscriptionID: sub.ID,
		ProjectID:      sub.ProjectID,
		PriceID:        plan.ProviderOverrides.StripePriceID,
		SuccessURL:     success,
		CancelURL:      cancel,
		IdempotencyKey: "sub:" + sub.ID,
	})
	if err != nil {
		return err
	}
	result.PaymentURL = sess.PaymentURL
	return s.upsertIndex(ctx, sub.Provider, domainpayments.IndexKindSubscription, sub.ID, sub.ProjectID)
}

func (s *Subscriptions) startPlatform(ctx context.Context, sub *domainsubs.Subscription, plan *domainsubs.Plan, result *SubscribeResult, now time.Time) error {
	// 试用期：发放 benefits，不扣款。
	if sub.Status == domainsubs.StatusTrialing {
		if err := s.fulfillBenefits(ctx, sub, sub.CurrentPeriodEnd); err != nil {
			return err
		}
		return s.publish(ctx, sub, domainsubs.EventActivated, now)
	}
	if plan.Amount == 0 {
		if err := s.fulfillBenefits(ctx, sub, sub.CurrentPeriodEnd); err != nil {
			return err
		}
		return s.publish(ctx, sub, domainsubs.EventActivated, now)
	}
	if sub.BillingAssetCode != "" {
		ctx = withSystemPrincipal(ctx, sub.ProjectID)
		if _, err := s.assets.Consume(ctx, appassets.ConsumeCommand{
			OwnerType:      domainassets.OwnerTypeUser,
			OwnerID:        sub.UserID,
			DefCode:        sub.BillingAssetCode,
			Quantity:       plan.Amount,
			IdempotencyKey: "sub:" + sub.ID + ":activate",
			RefType:        "subscription",
			RefID:          sub.ID,
		}); err != nil {
			return err
		}
		if err := s.fulfillBenefits(ctx, sub, sub.CurrentPeriodEnd); err != nil {
			return err
		}
		return s.publish(ctx, sub, domainsubs.EventActivated, now)
	}
	// 生成支付订单：订阅保持 trialing，paid 履约后转 active。
	from := sub.Status
	sub.Status = domainsubs.StatusTrialing
	sub.UpdatedAt = now
	if from != domainsubs.StatusTrialing {
		if err := s.subs.Update(ctx, sub, from); err != nil {
			return err
		}
	}
	order, url, err := s.createBillingOrder(ctx, sub, plan, "activate")
	if err != nil {
		return err
	}
	result.OrderID = order.ID
	result.PaymentURL = url
	return nil
}

func (s *Subscriptions) checkoutURLs() (success, cancel string) {
	base := "https://localhost"
	if s.cfg != nil && s.cfg.GetServer().GetHttp().GetPublicUrl() != "" {
		base = strings.TrimRight(s.cfg.GetServer().GetHttp().GetPublicUrl(), "/")
	}
	return base + "/?checkout=success&session_id={CHECKOUT_SESSION_ID}", base + "/?checkout=cancel"
}

// createBillingOrder 生成订阅扣款订单（Stripe，两段式，对齐 payments.CreateOrder）：
//
//  1. 经 payments.InsertCreatedOrder 落单 + payment_session index，在**独立事务**
//     提交——本函数可能被 Subscribe / processDue 的外层订阅事务调用，渠道下单
//     （外部 HTTP）不得拖长外层事务；index 行必须在回调可到达之前持久可见
//     （设计 §9.2：在调 CreatePayment 之前 COMMIT）。
//  2. CreatePayment 在事务之外。
//  3. 回填渠道引用并翻 paying + cs_/pi_ index upsert，第二个独立事务。
//
// CreatePayment 失败时订单保持 created：外层按各自语义处理（Subscribe 回滚订阅、
// processDue 记日志下轮重试），订单由到期 worker 关单；幂等键 sub:<id>:<cycle>
// 保证重放不重复建单。
func (s *Subscriptions) createBillingOrder(ctx context.Context, sub *domainsubs.Subscription, plan *domainsubs.Plan, cycle string) (*domainpayments.Order, string, error) {
	if s.orders == nil || s.providers == nil {
		return nil, "", status.Error(codes.FailedPrecondition, "payments are required to bill this subscription")
	}
	providerName := domainpayments.ProviderStripe
	provider, err := s.providers.Get(providerName)
	if err != nil {
		return nil, "", status.Errorf(codes.FailedPrecondition, "unsupported provider %q", providerName)
	}
	now := s.ts()
	order, err := apppayments.NewCreatedOrder(apppayments.CreatedOrderSpec{
		ProjectID:      sub.ProjectID,
		UserID:         sub.UserID,
		Provider:       provider.Name(),
		Amount:         plan.Amount,
		Currency:       plan.Currency,
		PurposeKind:    domainpayments.PurposeSubscription,
		Purpose:        purposeJSON(sub.ID, plan.Code, cycle),
		IdempotencyKey: "sub:" + sub.ID + ":" + cycle,
		Now:            now,
	})
	if err != nil {
		return nil, "", err
	}
	var existing *domainpayments.Order
	var inserted bool
	if err := s.db.RunInNewTx(ctx, func(txCtx context.Context) error {
		var err error
		existing, inserted, err = apppayments.InsertCreatedOrder(txCtx, s.orders, s.index, order)
		return err
	}); err != nil {
		return nil, "", err
	}
	if !inserted {
		return existing, "", nil
	}
	session, err := provider.CreatePayment(ctx, domainpayments.CreatePaymentInput{
		OrderID:        order.ID,
		ProjectID:      order.ProjectID,
		Amount:         order.Amount,
		Currency:       order.Currency,
		Description:    "Torchwood subscription " + sub.ID,
		ExpiresAt:      order.ExpiresAt,
		IdempotencyKey: "order:" + order.ID,
	})
	if err != nil {
		return nil, "", err
	}
	var locked *domainpayments.Order
	if err := s.db.RunInNewTx(ctx, func(txCtx context.Context) error {
		var err error
		locked, err = s.orders.GetByIDForUpdate(txCtx, order.ProjectID, order.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return status.Error(codes.NotFound, "payment order not found")
		}
		if locked.Status != domainpayments.OrderStatusCreated {
			return nil // 并发已推进（回调先到）：保持现状态。
		}
		locked.ProviderSessionID = session.SessionID
		if session.ProviderOrderID != "" {
			locked.ProviderOrderID = session.ProviderOrderID
		}
		if err := locked.Transition(domainpayments.OrderStatusPaying, now); err != nil {
			return err
		}
		if err := s.orders.Update(txCtx, locked, domainpayments.OrderStatusCreated); err != nil {
			return err
		}
		if err := s.upsertIndex(txCtx, locked.Provider, domainpayments.IndexKindPaymentSession, session.SessionID, locked.ProjectID); err != nil {
			return err
		}
		if session.ProviderOrderID != "" {
			return s.upsertIndex(txCtx, locked.Provider, domainpayments.IndexKindPaymentOrder, session.ProviderOrderID, locked.ProjectID)
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	if locked != nil {
		return locked, session.PaymentURL, nil
	}
	order.ProviderSessionID = session.SessionID
	if session.ProviderOrderID != "" {
		order.ProviderOrderID = session.ProviderOrderID
	}
	return order, session.PaymentURL, nil
}
