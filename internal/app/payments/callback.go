package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CallbackAck 返回渠道约定的 HTTP 回执（验签失败不走本方法）。
func (p *Payments) CallbackAck(providerName string, success bool) (status int, contentType string, body []byte) {
	if p.providers == nil {
		if success {
			return http.StatusOK, "", nil
		}
		return http.StatusInternalServerError, "", nil
	}
	provider, err := p.providers.Get(providerName)
	if err != nil {
		if success {
			return http.StatusOK, "", nil
		}
		return http.StatusInternalServerError, "", nil
	}
	if acker, ok := provider.(domainpayments.CallbackAcker); ok {
		return acker.CallbackAck(success)
	}
	if success {
		return http.StatusOK, "", nil
	}
	return http.StatusInternalServerError, "", nil
}

// HandleCallback 处理渠道回调（serverhttp 面，设计 §1.4）：
//
//  1. 验签 + 归一化（adapter 内完成）；验签失败返回 ErrSignatureInvalid，
//     handler 映射 401，全程不落任何行。
//  2. 幂等锚点二：(provider, provider_event_id) 冲突 → 幂等 200，不重入状态机。
//  3. 订单 FOR UPDATE → 状态机翻转 → 履约行（Fulfiller hook）→ outbox 事件，
//     同一 sql.Tx（总则 10 / 验收「任一失败整体回滚」）。
//
// 返回 nil 表示渠道应答成功（2xx）。
func (p *Payments) HandleCallback(ctx context.Context, providerName string, headers http.Header, rawBody []byte) error {
	provider, err := p.providers.Get(providerName)
	if err != nil {
		// 未知渠道视同验签失败：401，不落库（不区分渠道是否存在，防探测）。
		return domainpayments.ErrSignatureInvalid
	}
	event, err := provider.VerifyCallback(ctx, headers, rawBody)
	if err != nil {
		if domainpayments.ErrNotConfigured(err) {
			// 渠道未配置：fail-closed，同样按验签失败应答。
			return domainpayments.ErrSignatureInvalid
		}
		if errors.Is(err, domainpayments.ErrSignatureInvalid) {
			paymentCallbackVerifyFailTotal.WithLabelValues(providerName).Inc()
			return err
		}
		// 验签通过但报文畸形：不驱动状态机；登记后按成功应答，
		// 避免渠道无限重推（区分性信息不出网）。
		return p.recordIgnoredCallback(ctx, provider, rawBody)
	}

	// 未识别 / 忽略类型：登记即可。
	if event.Type == "ignored" {
		return p.recordIgnoredCallback(ctx, provider, rawBody)
	}

	if domainpayments.IsSubscriptionEvent(event.Type) {
		return p.db.RunInTx(ctx, func(txCtx context.Context) error {
			inserted, err := p.callbacks.InsertIfAbsent(txCtx, event, "", event.LocalSubscriptionID)
			if err != nil {
				return err
			}
			if !inserted {
				return nil // 重放：幂等 200，不重入状态机。
			}
			if p.subs == nil {
				return nil
			}
			return p.subs.HandleHostedCallback(txCtx, event)
		})
	}

	order, err := p.locateOrder(ctx, event)
	if err != nil {
		return err
	}
	if order == nil {
		// 渠道事件与本地订单无关（如非本服务创建的支付对象）：登记后 200。
		return p.db.RunInTx(ctx, func(txCtx context.Context) error {
			_, err := p.callbacks.InsertIfAbsent(txCtx, event, "", "")
			return err
		})
	}

	now := time.Now()
	return p.db.RunInTx(ctx, func(txCtx context.Context) error {
		inserted, err := p.callbacks.InsertIfAbsent(txCtx, event, order.ProjectID, order.ID)
		if err != nil {
			return err
		}
		if !inserted {
			return nil // 重放：幂等 200，不重入状态机。
		}

		locked, err := p.orders.GetByIDForUpdate(txCtx, order.ProjectID, order.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return nil
		}
		switch event.Type {
		case domainpayments.CallbackPaid:
			return p.applyPaid(txCtx, locked, event, now)
		case domainpayments.CallbackFailed:
			return p.applyTerminal(txCtx, locked, domainpayments.OrderStatusFailed, event, now)
		case domainpayments.CallbackRefunded:
			return p.applyRefunded(txCtx, locked, event, now)
		default:
			return nil
		}
	})
}

// recordIgnoredCallback 登记无法驱动状态机的回调（畸形 / 忽略类型），
// 独立短事务；登记失败按内部错误应答（渠道重推）。
func (p *Payments) recordIgnoredCallback(ctx context.Context, provider domainpayments.PaymentProvider, rawBody []byte) error {
	event := &domainpayments.CallbackEvent{
		Provider:        provider.Name(),
		ProviderEventID: ignoredEventID(rawBody),
		Type:            "ignored",
		Raw:             rawBody,
		ReceivedAt:      time.Now(),
	}
	if event.ProviderEventID == "" {
		// 完全无法识别身份的报文：不登记（无幂等锚点），直接按成功应答丢弃。
		return nil
	}
	return p.db.RunInTx(ctx, func(txCtx context.Context) error {
		_, err := p.callbacks.InsertIfAbsent(txCtx, event, "", "")
		return err
	})
}

// applyPaid 回调 paid：金额校验 → FOR UPDATE 翻转 → 履约行 + Fulfiller
// hook → outbox 事件，同一事务（设计 §1.3/§1.5）。
func (p *Payments) applyPaid(ctx context.Context, order *domainpayments.Order, event *domainpayments.CallbackEvent, now time.Time) error {
	switch order.Status {
	case domainpayments.OrderStatusPaid:
		return nil // 已付（并发回调）：幂等。
	case domainpayments.OrderStatusRefunding, domainpayments.OrderStatusRefunded:
		return nil
	case domainpayments.OrderStatusFailed, domainpayments.OrderStatusClosed:
		// 迟到支付（关单后用户补付完成）：PR1 不自动重开 / 退款（D12 人工兜底），
		// 记日志告警，事件保留在 payment_callback_events 供对账。
		p.logger.Error("payment callback paid after terminal state; manual reconciliation required",
			"order_id", order.ID, "status", order.Status, "provider_event_id", event.ProviderEventID)
		return nil
	case domainpayments.OrderStatusCreated, domainpayments.OrderStatusPaying:
	default:
		return nil
	}
	// 金额 / 币种一致性：渠道到账必须与订单一致，不一致整体回滚（渠道重推，
	// 保留告警线索），绝不带病翻转。
	if event.Amount > 0 && event.Amount != order.Amount {
		return status.Errorf(codes.FailedPrecondition,
			"callback amount mismatch: order %d, callback %d", order.Amount, event.Amount)
	}
	if event.Currency != "" && event.Currency != order.Currency {
		return status.Errorf(codes.FailedPrecondition,
			"callback currency mismatch: order %s, callback %s", order.Currency, event.Currency)
	}
	from := order.Status
	if event.ProviderOrderID != "" && order.ProviderOrderID == "" {
		order.ProviderOrderID = event.ProviderOrderID
	}
	if err := order.Transition(domainpayments.OrderStatusPaid, now); err != nil {
		return err
	}
	if err := p.orders.Update(ctx, order, from); err != nil {
		return err
	}
	paymentOrdersTotal.WithLabelValues(order.Provider, string(order.Status)).Inc()

	// 履约（设计 §1.5）：pending 行 + Fulfiller hook（PR1 占位，PR2 联通
	// topup/item_purchase 真实发放）+ done 标记，全部在本事务内。
	fulfillment := &domainpayments.Fulfillment{
		ID:          newOrderID(),
		OrderID:     order.ID,
		ProjectID:   order.ProjectID,
		PurposeKind: order.PurposeKind,
		Ref:         "order:" + order.ID,
		Status:      domainpayments.FulfillmentPending,
		Detail:      map[string]any{"kind": string(order.PurposeKind)},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	existing, inserted, err := p.fulfillments.InsertPending(ctx, fulfillment)
	if err != nil {
		return err
	}
	if inserted {
		ref, err := p.fulfiller.Fulfill(ctx, order)
		if err != nil {
			return err // 履约失败：整体回滚，订单保持 paying，渠道重推再试。
		}
		if ref == "" {
			ref = fulfillment.Ref
		}
		if err := p.fulfillments.MarkDone(ctx, fulfillment.ID, ref, fulfillment.Detail); err != nil {
			return err
		}
	} else if existing != nil && existing.Status == domainpayments.FulfillmentPending {
		// 理论不可达（与订单翻转同事务）；防御性补齐 done。
		if err := p.fulfillments.MarkDone(ctx, existing.ID, existing.Ref, nil); err != nil {
			return err
		}
	}

	return p.events.Publish(ctx, orderEnvelope(order, domainpayments.EventOrderPaid, now))
}

// applyTerminal 回调 failed：翻终态 + outbox 事件（设计 §5.1 目录）。
func (p *Payments) applyTerminal(ctx context.Context, order *domainpayments.Order, to domainpayments.OrderStatus, event *domainpayments.CallbackEvent, now time.Time) error {
	if order.Status == to || order.Status.IsTerminal() || order.Status == domainpayments.OrderStatusPaid {
		// 已在同一终态（幂等）或已支付（迟到失败事件）：不驱动。
		return nil
	}
	from := order.Status
	if err := order.Transition(to, now); err != nil {
		return nil // 非法迁移（乱序事件）：保留登记，不应答失败防重推风暴。
	}
	if err := p.orders.Update(ctx, order, from); err != nil {
		return err
	}
	paymentOrdersTotal.WithLabelValues(order.Provider, string(order.Status)).Inc()
	return p.events.Publish(ctx, orderEnvelope(order, domainpayments.EventOrderFailed, now))
}

// applyRefunded 回调 refunded：paid/refunding → refunded + 事件（D12：
// 只翻状态 + 事件，不动已发放资产）。
func (p *Payments) applyRefunded(ctx context.Context, order *domainpayments.Order, event *domainpayments.CallbackEvent, now time.Time) error {
	switch order.Status {
	case domainpayments.OrderStatusRefunded:
		return nil
	case domainpayments.OrderStatusPaid, domainpayments.OrderStatusRefunding:
	default:
		return nil // 未支付订单的退款事件：忽略。
	}
	from := order.Status
	if err := order.Transition(domainpayments.OrderStatusRefunded, now); err != nil {
		return nil
	}
	if err := p.orders.Update(ctx, order, from); err != nil {
		return err
	}
	paymentOrdersTotal.WithLabelValues(order.Provider, string(order.Status)).Inc()
	return p.events.Publish(ctx, orderEnvelope(order, domainpayments.EventOrderRefunded, now))
}

// locateOrder 按渠道 metadata 订单 id 或渠道引用定位本地订单（全局：
// 信任根是渠道签名，无项目上下文）。
func (p *Payments) locateOrder(ctx context.Context, event *domainpayments.CallbackEvent) (*domainpayments.Order, error) {
	if event.OrderID != "" {
		order, err := p.orders.GetByID(ctx, "", event.OrderID)
		if err != nil {
			return nil, err
		}
		if order != nil && order.Provider == event.Provider {
			return order, nil
		}
	}
	return p.orders.GetByProviderRef(ctx, "", event.Provider, event.ProviderSessionID, event.ProviderOrderID)
}

// orderEnvelope 组装订单事件信封（经济事件：domain=payments，
// channel=accounts.{userId}，D17；payload 不含隐私字段，设计 §5.1）。
func orderEnvelope(order *domainpayments.Order, event string, now time.Time) domainevents.Envelope {
	return domainevents.Envelope{
		EventID:   newOrderID(),
		Event:     event,
		ProjectID: order.ProjectID,
		Domain:    domainpayments.EventDomain,
		Channel:   domainpayments.AccountsChannel(order.UserID),
		CreatedAt: now,
		Attrs: map[string]any{
			"order_id":     order.ID,
			"user_id":      order.UserID,
			"provider":     order.Provider,
			"amount":       order.Amount, // int64 最小货币单位
			"currency":     order.Currency,
			"status":       string(order.Status),
			"purpose_kind": string(order.PurposeKind),
		},
	}
}

// ignoredEventID 尝试从畸形报文提取幂等锚点（best-effort）。
func ignoredEventID(rawBody []byte) string {
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil {
		return ""
	}
	return probe.ID
}
