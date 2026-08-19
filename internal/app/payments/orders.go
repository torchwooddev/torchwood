package payments

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// currencyPattern 限定 ISO-4217 三字母货币码。
var currencyPattern = regexp.MustCompile(`^[A-Za-z]{3}$`)

// maxIdempotencyKeyLen 是建单幂等键长度上限。
const maxIdempotencyKeyLen = 128

// CreateOrderCommand 是建单入参（Client 面：终端用户为自己下单）。
type CreateOrderCommand struct {
	Provider       string
	Amount         int64 // 最小货币单位（bigint，禁止 float）
	Currency       string
	PurposeKind    domainpayments.PurposeKind
	Purpose        map[string]any
	IdempotencyKey string
	// ExpiresIn 可选有效期；缺省 defaultOrderTTL。
	ExpiresIn time.Duration
}

// CreateOrderResult 是建单结果：订单 + 客户端完成支付所需载荷。
// IdempotentReplay=true 表示命中幂等键返回原单（未向渠道重复下单）。
type CreateOrderResult struct {
	Order            *domainpayments.Order
	PaymentURL       string
	IdempotentReplay bool
}

// CreateOrder 建单（Client API，session JWT）：订单先落库（created，
// 幂等键唯一），再向渠道下单翻 paying（设计 §1.3 状态机）。
func (p *Payments) CreateOrder(ctx context.Context, cmd CreateOrderCommand) (*CreateOrderResult, error) {
	projectID, userID, err := p.endUser(ctx)
	if err != nil {
		return nil, err
	}
	if cmd.Amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be a positive integer in the smallest currency unit")
	}
	if !currencyPattern.MatchString(cmd.Currency) {
		return nil, status.Error(codes.InvalidArgument, "currency must be a 3-letter ISO-4217 code")
	}
	if !cmd.PurposeKind.IsValid() {
		return nil, status.Errorf(codes.InvalidArgument, "invalid purpose_kind %q", cmd.PurposeKind)
	}
	if cmd.PurposeKind == domainpayments.PurposeSubscription {
		// 订阅在 PR3 接入（设计 §3）；本期不允许建订阅订单。
		return nil, status.Error(codes.InvalidArgument, "subscription orders are not supported yet")
	}
	if strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if len(cmd.IdempotencyKey) > maxIdempotencyKeyLen {
		return nil, status.Errorf(codes.InvalidArgument, "idempotency_key exceeds %d characters", maxIdempotencyKeyLen)
	}
	provider, err := p.providers.Get(cmd.Provider)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported provider %q", cmd.Provider)
	}

	purpose, err := json.Marshal(cmd.Purpose)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid purpose payload")
	}

	ttl := cmd.ExpiresIn
	if ttl == 0 {
		ttl = defaultOrderTTL
	}
	if ttl < minOrderTTL || ttl > maxOrderTTL {
		return nil, status.Errorf(codes.InvalidArgument, "expires_in out of range (%s ~ %s)", minOrderTTL, maxOrderTTL)
	}
	now := time.Now()

	// 幂等锚点一：同 (project_id, idempotency_key) 返回原单，不新建。
	order := &domainpayments.Order{
		ID:             newOrderID(),
		ProjectID:      projectID,
		UserID:         userID,
		Provider:       provider.Name(),
		IdempotencyKey: cmd.IdempotencyKey,
		Amount:         cmd.Amount,
		Currency:       strings.ToUpper(cmd.Currency),
		PurposeKind:    cmd.PurposeKind,
		Purpose:        purpose,
		Status:         domainpayments.OrderStatusCreated,
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(ttl),
	}
	existing, inserted, err := p.orders.Insert(ctx, order)
	if err != nil {
		return nil, err
	}
	if !inserted {
		// 命中幂等键：返回原单。原单可能仍在 created（上次渠道下单失败）——
		// 重放建单允许对 created 原单继续走下单流程；否则原样返回。
		if existing.Status != domainpayments.OrderStatusCreated {
			return &CreateOrderResult{Order: existing, IdempotentReplay: true}, nil
		}
		order = existing
	}

	// 渠道下单（Checkout Session）：成功后同一短事务回填渠道引用并翻 paying。
	session, err := provider.CreatePayment(ctx, domainpayments.CreatePaymentInput{
		OrderID:        order.ID,
		Amount:         order.Amount,
		Currency:       order.Currency,
		Description:    orderDescription(order),
		ExpiresAt:      order.ExpiresAt,
		IdempotencyKey: "order:" + order.ID,
	})
	if err != nil {
		// 订单保持 created，等渠道重试或到期由 worker 关单。
		return nil, mapProviderError(err)
	}
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		locked, err := p.orders.GetByIDForUpdate(txCtx, order.ProjectID, order.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return status.Error(codes.NotFound, "payment order not found")
		}
		if locked.Status != domainpayments.OrderStatusCreated {
			return nil // 并发已推进（重放 / 回调先到）：保持现状态。
		}
		locked.ProviderSessionID = session.SessionID
		if err := locked.Transition(domainpayments.OrderStatusPaying, time.Now()); err != nil {
			return err
		}
		return p.orders.Update(txCtx, locked, domainpayments.OrderStatusCreated)
	}); err != nil {
		return nil, err
	}
	locked, err := p.orders.GetByID(ctx, order.ProjectID, order.ID)
	if err != nil {
		return nil, err
	}
	if locked == nil {
		return nil, status.Error(codes.NotFound, "payment order not found")
	}
	paymentOrdersTotal.WithLabelValues(locked.Provider, string(locked.Status)).Inc()
	return &CreateOrderResult{Order: locked, PaymentURL: session.PaymentURL, IdempotentReplay: !inserted}, nil
}

// orderDescription 返回收银台展示名（按用途拼接，不含敏感信息）。
func orderDescription(order *domainpayments.Order) string {
	return "Torchwood order " + order.ID
}

// GetMyOrder 返回本人订单（Client 面）。
func (p *Payments) GetMyOrder(ctx context.Context, orderID string) (*domainpayments.Order, error) {
	projectID, userID, err := p.endUser(ctx)
	if err != nil {
		return nil, err
	}
	order, err := p.orders.GetByID(ctx, projectID, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil || order.UserID != userID {
		return nil, status.Error(codes.NotFound, "order not found")
	}
	return order, nil
}

// ListMyOrders 返回本人订单列表（Client 面，created_at 倒序游标分页）。
func (p *Payments) ListMyOrders(ctx context.Context, limit int, before time.Time) ([]domainpayments.Order, error) {
	projectID, userID, err := p.endUser(ctx)
	if err != nil {
		return nil, err
	}
	limit, before = normalizeList(limit, before)
	return p.orders.ListByUser(ctx, projectID, userID, limit, before)
}

// GetOrder 返回项目内订单（Server 面：admin 会话 / payments.read Key）。
func (p *Payments) GetOrder(ctx context.Context, orderID string) (*domainpayments.Order, error) {
	projectID, err := p.projectScope(ctx)
	if err != nil {
		return nil, err
	}
	order, err := p.orders.GetByID(ctx, projectID, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, status.Error(codes.NotFound, "order not found")
	}
	return order, nil
}

// ListOrders 返回项目订单列表（Server 面，created_at 倒序游标分页）。
func (p *Payments) ListOrders(ctx context.Context, limit int, before time.Time) ([]domainpayments.Order, error) {
	projectID, err := p.projectScope(ctx)
	if err != nil {
		return nil, err
	}
	limit, before = normalizeList(limit, before)
	return p.orders.ListByProject(ctx, projectID, limit, before)
}

// CloseExpiredOrders 把超时未付（created/paying 超 expires_at）订单翻
// closed（worker 周期任务，设计 §1.3；closed 不在 §5.1 事件目录，不发事件）。
func (p *Payments) CloseExpiredOrders(ctx context.Context, now time.Time) (int64, error) {
	return p.orders.CloseExpired(ctx, now, closeExpiredBatch)
}

// endUser 解析 Client 面调用者：必须为登录终端用户（session JWT）。
func (p *Payments) endUser(ctx context.Context) (projectID, userID string, err error) {
	principal, ok := contexts.Principal(ctx)
	if !ok || principal.ProjectID == "" || principal.UserID == "" {
		return "", "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return principal.ProjectID, principal.UserID, nil
}

// projectScope 解析 Server 面调用的项目上下文。
func (p *Payments) projectScope(ctx context.Context) (string, error) {
	principal, ok := contexts.Principal(ctx)
	if !ok || principal.ProjectID == "" {
		return "", status.Error(codes.Unauthenticated, "missing project context")
	}
	return principal.ProjectID, nil
}

// normalizeList 归一化分页参数。
func normalizeList(limit int, before time.Time) (int, time.Time) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if before.IsZero() {
		before = time.Now().Add(time.Hour)
	}
	return limit, before
}
