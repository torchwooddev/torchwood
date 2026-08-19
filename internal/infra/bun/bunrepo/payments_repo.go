package bunrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/payments"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// paymentOrderRepo 实现 payments.OrderRepo（public.payment_orders）。
// 所有查询走 clients.Conn(ctx)：在 RunInTx 内调用时复用外层事务
// （总则 10：订单翻转 / 履约 / outbox 同一 sql.Tx，禁止另开连接）。
type paymentOrderRepo struct {
	db *clients.Database
}

// NewPaymentOrderRepository 构造订单仓储。
func NewPaymentOrderRepository(db *clients.Database) payments.OrderRepo {
	return &paymentOrderRepo{db: db}
}

func (r *paymentOrderRepo) Insert(ctx context.Context, order *payments.Order) (*payments.Order, bool, error) {
	m := mapOrderToModel(order)
	res, err := r.db.Conn(ctx).NewInsert().Model(m).
		// 幂等锚点一：同 (project_id, idempotency_key) 不新建，返回原单。
		On("CONFLICT (project_id, idempotency_key) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil, true, nil
	}
	// 冲突：按幂等键取原单（注意不能按新单 id 查——那是刚被拒插的 id）。
	existing, err := r.GetByIDempotencyKey(ctx, order.ProjectID, order.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, errors.New("payments: idempotent insert conflict but order not found")
	}
	return existing, false, nil
}

// GetByIDempotencyKey 按建单幂等键取单（幂等重放返回原单）。
func (r *paymentOrderRepo) GetByIDempotencyKey(ctx context.Context, projectID, key string) (*payments.Order, error) {
	m := new(model.PaymentOrder)
	err := r.db.Conn(ctx).NewSelect().Model(m).
		Where("po.project_id = ?", projectID).
		Where("po.idempotency_key = ?", key).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapOrderToDomain(m), nil
}

func (r *paymentOrderRepo) GetByID(ctx context.Context, projectID, orderID string) (*payments.Order, error) {
	return r.selectOne(ctx, projectID, orderID, "")
}

func (r *paymentOrderRepo) GetByIDForUpdate(ctx context.Context, projectID, orderID string) (*payments.Order, error) {
	return r.selectOne(ctx, projectID, orderID, "UPDATE")
}

// selectOne 按 id 取单；projectID 为空时不做项目过滤（回调路径：订单 id
// 来自已验签的渠道 metadata，信任根是渠道签名而非项目上下文）。
// lock 非空时附加 FOR UPDATE（要求 ctx 已在事务内）。
func (r *paymentOrderRepo) selectOne(ctx context.Context, projectID, orderID, lock string) (*payments.Order, error) {
	q := r.db.Conn(ctx).NewSelect().Model((*model.PaymentOrder)(nil)).
		Where("po.id = ?", orderID)
	if projectID != "" {
		q = q.Where("po.project_id = ?", projectID)
	}
	if lock != "" {
		q = q.For(lock)
	}
	m := new(model.PaymentOrder)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapOrderToDomain(m), nil
}

func (r *paymentOrderRepo) GetByProviderRef(ctx context.Context, projectID, provider, providerSessionID, providerOrderID string) (*payments.Order, error) {
	if providerSessionID == "" && providerOrderID == "" {
		return nil, nil
	}
	q := r.db.Conn(ctx).NewSelect().Model((*model.PaymentOrder)(nil)).
		Where("po.provider = ?", provider)
	if projectID != "" {
		q = q.Where("po.project_id = ?", projectID)
	}
	if providerSessionID != "" && providerOrderID != "" {
		q = q.Where("(po.provider_session_id = ? OR po.provider_order_id = ?)", providerSessionID, providerOrderID)
	} else if providerSessionID != "" {
		q = q.Where("po.provider_session_id = ?", providerSessionID)
	} else {
		q = q.Where("po.provider_order_id = ?", providerOrderID)
	}
	q = q.Order("po.created_at").Limit(1)
	m := new(model.PaymentOrder)
	if err := q.Scan(ctx, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapOrderToDomain(m), nil
}

// Update 写回订单；expectStatus 为状态前置条件（乐观并发：行不在期望状态
// 即失败），rows=0 视为并发修改冲突。
func (r *paymentOrderRepo) Update(ctx context.Context, order *payments.Order, expectStatus payments.OrderStatus) error {
	res, err := r.db.Conn(ctx).NewUpdate().Model((*model.PaymentOrder)(nil)).
		Set("status = ?", string(order.Status)).
		Set("updated_at = ?", order.UpdatedAt).
		Set("paid_at = ?", order.PaidAt).
		Set("provider_session_id = ?", nullIfEmpty(order.ProviderSessionID)).
		Set("provider_order_id = ?", nullIfEmpty(order.ProviderOrderID)).
		Where("po.id = ?", order.ID).
		Where("po.status = ?", string(expectStatus)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return status.Error(codes.Aborted, "payment order concurrently modified")
	}
	return nil
}

func (r *paymentOrderRepo) ListByUser(ctx context.Context, projectID, userID string, limit int, before time.Time) ([]payments.Order, error) {
	var rows []model.PaymentOrder
	err := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("po.project_id = ?", projectID).
		Where("po.user_id = ?", userID).
		Where("po.created_at < ?", before).
		Order("po.created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return mapOrdersToDomain(rows), nil
}

func (r *paymentOrderRepo) ListByProject(ctx context.Context, projectID string, limit int, before time.Time) ([]payments.Order, error) {
	var rows []model.PaymentOrder
	err := r.db.Conn(ctx).NewSelect().Model(&rows).
		Where("po.project_id = ?", projectID).
		Where("po.created_at < ?", before).
		Order("po.created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return mapOrdersToDomain(rows), nil
}

// CloseExpired 把 created/paying 且超时的订单翻 closed（worker 周期任务，
// 设计 §1.3；closed 不在事件目录 §5.1，不发 outbox 事件）。
// 状态机非法迁移（并发已翻转）由 WHERE status 条件自动跳过；
// FOR UPDATE SKIP LOCKED 保证多副本 worker 互不阻塞、不重复关单。
func (r *paymentOrderRepo) CloseExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	res, err := r.db.Conn(ctx).NewRaw(
		`UPDATE payment_orders po
		 SET status = ?, updated_at = ?
		 WHERE po.id IN (
		     SELECT id FROM payment_orders
		     WHERE status IN ('created', 'paying') AND expires_at <= ?
		     ORDER BY expires_at
		     LIMIT ?
		     FOR UPDATE SKIP LOCKED
		 )`,
		string(payments.OrderStatusClosed), now, now, limit).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return n, err
}

// paymentCallbackEventRepo 实现 payments.CallbackEventRepo。
type paymentCallbackEventRepo struct {
	db *clients.Database
}

// NewPaymentCallbackEventRepository 构造回调事件登记仓储。
func NewPaymentCallbackEventRepository(db *clients.Database) payments.CallbackEventRepo {
	return &paymentCallbackEventRepo{db: db}
}

// InsertIfAbsent 幂等锚点二：(provider, provider_event_id) 冲突时返回 false。
func (r *paymentCallbackEventRepo) InsertIfAbsent(ctx context.Context, event *payments.CallbackEvent, projectID, orderID string) (bool, error) {
	payload, err := json.Marshal(map[string]any{
		"provider":            event.Provider,
		"provider_event_id":   event.ProviderEventID,
		"provider_session_id": event.ProviderSessionID,
		"provider_order_id":   event.ProviderOrderID,
		"type":                event.Type,
		"amount":              event.Amount,
		"currency":            event.Currency,
		"order_id":            event.OrderID,
		"received_at":         event.ReceivedAt.UTC().Format(time.RFC3339),
		"raw":                 json.RawMessage(event.Raw),
	})
	if err != nil {
		return false, err
	}
	res, err := r.db.Conn(ctx).NewInsert().Model(&model.PaymentCallbackEvent{
		ID:              idgen.ULID().String(),
		ProjectID:       nullIfEmpty(projectID),
		Provider:        event.Provider,
		ProviderEventID: event.ProviderEventID,
		EventType:       event.Type,
		OrderID:         nullIfEmpty(orderID),
		Payload:         payload,
		CreatedAt:       event.ReceivedAt,
	}).On("CONFLICT (provider, provider_event_id) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// paymentFulfillmentRepo 实现 payments.FulfillmentRepo。
type paymentFulfillmentRepo struct {
	db *clients.Database
}

// NewPaymentFulfillmentRepository 构造履约记录仓储。
func NewPaymentFulfillmentRepository(db *clients.Database) payments.FulfillmentRepo {
	return &paymentFulfillmentRepo{db: db}
}

func (r *paymentFulfillmentRepo) InsertPending(ctx context.Context, f *payments.Fulfillment) (*payments.Fulfillment, bool, error) {
	res, err := r.db.Conn(ctx).NewInsert().Model(&model.PaymentFulfillment{
		ID:          f.ID,
		OrderID:     f.OrderID,
		ProjectID:   f.ProjectID,
		PurposeKind: string(f.PurposeKind),
		Ref:         f.Ref,
		Status:      string(payments.FulfillmentPending),
		Detail:      f.Detail,
		CreatedAt:   f.CreatedAt,
		UpdatedAt:   f.UpdatedAt,
	}).On("CONFLICT (order_id, purpose_kind) DO NOTHING").Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil, true, nil
	}
	existing, err := r.GetByOrder(ctx, f.ProjectID, f.OrderID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (r *paymentFulfillmentRepo) MarkDone(ctx context.Context, fulfillmentID, ref string, detail map[string]any) error {
	_, err := r.db.Conn(ctx).NewUpdate().Model((*model.PaymentFulfillment)(nil)).
		Set("status = ?", string(payments.FulfillmentDone)).
		Set("ref = ?", ref).
		Set("detail = ?", detail).
		Set("updated_at = ?", time.Now()).
		Where("pf.id = ?", fulfillmentID).
		Exec(ctx)
	return err
}

func (r *paymentFulfillmentRepo) MarkFailed(ctx context.Context, fulfillmentID, reason string) error {
	_, err := r.db.Conn(ctx).NewUpdate().Model((*model.PaymentFulfillment)(nil)).
		Set("status = ?", string(payments.FulfillmentFailed)).
		Set("detail = ?", map[string]any{"reason": reason}).
		Set("updated_at = ?", time.Now()).
		Where("pf.id = ?", fulfillmentID).
		Exec(ctx)
	return err
}

func (r *paymentFulfillmentRepo) GetByOrder(ctx context.Context, projectID, orderID string) (*payments.Fulfillment, error) {
	m := new(model.PaymentFulfillment)
	err := r.db.Conn(ctx).NewSelect().Model(m).
		Where("pf.project_id = ?", projectID).
		Where("pf.order_id = ?", orderID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &payments.Fulfillment{
		ID:          m.ID,
		OrderID:     m.OrderID,
		ProjectID:   m.ProjectID,
		PurposeKind: payments.PurposeKind(m.PurposeKind),
		Ref:         m.Ref,
		Status:      payments.FulfillmentStatus(m.Status),
		Detail:      m.Detail,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func mapOrderToModel(order *payments.Order) *model.PaymentOrder {
	return &model.PaymentOrder{
		ID:                order.ID,
		ProjectID:         order.ProjectID,
		UserID:            order.UserID,
		Provider:          order.Provider,
		IdempotencyKey:    order.IdempotencyKey,
		ProviderSessionID: nullIfEmpty(order.ProviderSessionID),
		ProviderOrderID:   nullIfEmpty(order.ProviderOrderID),
		Amount:            order.Amount,
		Currency:          order.Currency,
		PurposeKind:       string(order.PurposeKind),
		Purpose:           order.Purpose,
		Status:            string(order.Status),
		CreatedAt:         order.CreatedAt,
		UpdatedAt:         order.UpdatedAt,
		PaidAt:            order.PaidAt,
		ExpiresAt:         order.ExpiresAt,
	}
}

func mapOrderToDomain(m *model.PaymentOrder) *payments.Order {
	return &payments.Order{
		ID:                m.ID,
		ProjectID:         m.ProjectID,
		UserID:            m.UserID,
		Provider:          m.Provider,
		IdempotencyKey:    m.IdempotencyKey,
		ProviderSessionID: derefString(m.ProviderSessionID),
		ProviderOrderID:   derefString(m.ProviderOrderID),
		Amount:            m.Amount,
		Currency:          m.Currency,
		PurposeKind:       payments.PurposeKind(m.PurposeKind),
		Purpose:           m.Purpose,
		Status:            payments.OrderStatus(m.Status),
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
		PaidAt:            m.PaidAt,
		ExpiresAt:         m.ExpiresAt,
	}
}

func mapOrdersToDomain(rows []model.PaymentOrder) []payments.Order {
	out := make([]payments.Order, len(rows))
	for i := range rows {
		out[i] = *mapOrderToDomain(&rows[i])
	}
	return out
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
