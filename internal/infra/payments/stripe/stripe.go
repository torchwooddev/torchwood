// Package stripe 是 PaymentProvider 端口的 Stripe 适配器（v3 设计 §1.1/§1.2）：
// 下单走 Checkout Session，回调验签走 Stripe-Signature（HMAC-SHA256），
// 渠道差异全部收敛在本包，use-case 只见归一化 CallbackEvent。
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/payments"
)

const (
	// DefaultAPIBaseURL 是 Stripe REST API 地址（测试可注入 httptest 地址）。
	DefaultAPIBaseURL = "https://api.stripe.com"
	// signatureHeader 是 webhook 签名头：t=<unix>,v1=<hex hmac>。
	signatureHeader = "Stripe-Signature"
	// defaultTolerance 是签名时间戳容忍窗口（对齐 stripe-go 默认 5min）。
	defaultTolerance = 5 * time.Minute
	// maxCallbackBody 限制回调体大小（Stripe webhook 一般 < 64KiB）。
	maxCallbackBody = 1 << 20
	// providerName 对齐 payments.ProviderStripe。
	providerName = payments.ProviderStripe
)

// Config 是 Stripe 适配器配置（secret 一律来自环境变量，不进 config.yaml）。
type Config struct {
	SecretKey     string // sk_...（TORCHWOOD_PAYMENTS_STRIPE_SECRET_KEY）
	WebhookSecret string // whsec_...（TORCHWOOD_PAYMENTS_STRIPE_WEBHOOK_SECRET）
	APIBaseURL    string // 默认 https://api.stripe.com；测试注入 httptest
}

// Adapter 实现 payments.PaymentProvider。
type Adapter struct {
	cfg       Config
	client    *http.Client
	tolerance time.Duration
	now       func() time.Time
}

// New 构造 Stripe 适配器。secret 未配置时构造不失败（服务可启动），
// 但 CreatePayment / VerifyCallback 一律 fail-closed（未配置即拒绝）。
func New(cfg Config) *Adapter {
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = DefaultAPIBaseURL
	}
	return &Adapter{
		cfg:       cfg,
		client:    &http.Client{Timeout: 30 * time.Second},
		tolerance: defaultTolerance,
		now:       time.Now,
	}
}

func (a *Adapter) Name() string { return providerName }

// configured 报告渠道是否已配置凭据。
func (a *Adapter) configured() bool {
	return a.cfg.SecretKey != "" && a.cfg.WebhookSecret != ""
}

// CreatePayment 创建 Checkout Session（设计 §1.2：Stripe 下单渠道）。
func (a *Adapter) CreatePayment(ctx context.Context, in payments.CreatePaymentInput) (*payments.PaymentSession, error) {
	if a.cfg.SecretKey == "" {
		return nil, payments.ErrProviderNotConfigured(providerName)
	}
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("client_reference_id", in.OrderID)
	form.Set("metadata[order_id]", in.OrderID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", strings.ToLower(in.Currency))
	// 金额最小货币单位（bigint）直接透传，任何浮点换算都不允许。
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(in.Amount, 10))
	form.Set("line_items[0][price_data][product_data][name]", in.Description)
	if !in.ExpiresAt.IsZero() {
		form.Set("expires_at", strconv.FormatInt(in.ExpiresAt.Unix(), 10))
	}
	if in.SuccessURL != "" {
		form.Set("success_url", in.SuccessURL)
	}
	if in.CancelURL != "" {
		form.Set("cancel_url", in.CancelURL)
	}

	var out checkoutSessionResponse
	reqID := in.IdempotencyKey
	if reqID == "" {
		reqID = in.OrderID
	}
	if err := a.do(ctx, http.MethodPost, "/v1/checkout/sessions", form, reqID, &out); err != nil {
		return nil, err
	}
	if out.ID == "" || out.URL == "" {
		return nil, fmt.Errorf("stripe: checkout session response missing id/url")
	}
	return &payments.PaymentSession{SessionID: out.ID, PaymentURL: out.URL}, nil
}

// checkoutSessionResponse 是 Checkout Session 的最小响应形状。
type checkoutSessionResponse struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	PaymentIntent string `json:"payment_intent"`
}

// webhookEvent 是 Stripe webhook 事件的形状（只取归一化所需字段）。
type webhookEvent struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Created int64            `json:"created"`
	Data    webhookEventData `json:"data"`
}

type webhookEventData struct {
	Object json.RawMessage `json:"object"`
}

// checkoutSessionObject 是 checkout.session.* 事件载荷。
type checkoutSessionObject struct {
	ID                string            `json:"id"`
	PaymentIntent     string            `json:"payment_intent"`
	ClientReferenceID string            `json:"client_reference_id"`
	PaymentStatus     string            `json:"payment_status"`
	AmountTotal       int64             `json:"amount_total"`
	Currency          string            `json:"currency"`
	Metadata          map[string]string `json:"metadata"`
}

// chargeObject 是 charge.refunded 事件载荷。
type chargeObject struct {
	PaymentIntent    string `json:"payment_intent"`
	AmountRefunded   int64  `json:"amount_refunded"`
	Amount           int64  `json:"amount"`
	CurrencyRefunded string `json:"currency"`
}

// paymentIntentObject 是 payment_intent.payment_failed 事件载荷。
type paymentIntentObject struct {
	ID       string            `json:"id"`
	Amount   int64             `json:"amount"`
	Currency string            `json:"currency"`
	Metadata map[string]string `json:"metadata"`
}

// VerifyCallback 验签并归一化 Stripe webhook（设计 §1.4：原始 body 直读，
// 验签失败 ErrSignatureInvalid → 401，不落库）。
func (a *Adapter) VerifyCallback(ctx context.Context, headers http.Header, rawBody []byte) (*payments.CallbackEvent, error) {
	if len(rawBody) > maxCallbackBody {
		return nil, payments.ErrSignatureInvalid
	}
	if err := a.verifySignature(headers, rawBody); err != nil {
		return nil, err
	}
	return a.normalize(rawBody)
}

// verifySignature 校验 Stripe-Signature：v1 = HMAC-SHA256(webhook_secret,
// "t" + "." + rawBody)，时间戳超容忍窗口拒绝（防重放）。
func (a *Adapter) verifySignature(headers http.Header, rawBody []byte) error {
	if a.cfg.WebhookSecret == "" {
		return payments.ErrProviderNotConfigured(providerName)
	}
	sigHeader := headers.Get(signatureHeader)
	if sigHeader == "" {
		return payments.ErrSignatureInvalid
	}
	var timestamp, provided string
	for _, part := range strings.Split(sigHeader, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			timestamp = v
		case "v1":
			provided = v
		}
	}
	if timestamp == "" || provided == "" {
		return payments.ErrSignatureInvalid
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return payments.ErrSignatureInvalid
	}
	if a.tolerance > 0 && a.now().Sub(time.Unix(ts, 0)) > a.tolerance {
		return payments.ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(a.cfg.WebhookSecret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	expect := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expect), []byte(provided)) {
		return payments.ErrSignatureInvalid
	}
	return nil
}

// normalize 把验签通过的 Stripe 事件归一化为 CallbackEvent（渠道差异
// 收敛点，设计 §1.1）。
func (a *Adapter) normalize(rawBody []byte) (*payments.CallbackEvent, error) {
	var ev webhookEvent
	if err := json.Unmarshal(rawBody, &ev); err != nil {
		return nil, fmt.Errorf("stripe: decode webhook: %w", err)
	}
	if ev.ID == "" || ev.Type == "" {
		return nil, fmt.Errorf("stripe: webhook missing id/type")
	}
	out := &payments.CallbackEvent{
		Provider:        providerName,
		ProviderEventID: ev.ID,
		Raw:             append([]byte(nil), rawBody...),
		ReceivedAt:      a.now(),
	}
	switch {
	case ev.Type == "checkout.session.completed" || ev.Type == "checkout.session.async_payment_succeeded":
		var obj checkoutSessionObject
		if err := json.Unmarshal(ev.Data.Object, &obj); err != nil {
			return nil, fmt.Errorf("stripe: decode checkout session: %w", err)
		}
		out.ProviderSessionID = obj.ID
		out.ProviderOrderID = obj.PaymentIntent
		out.OrderID = firstNonEmpty(obj.ClientReferenceID, obj.Metadata["order_id"])
		out.Amount = obj.AmountTotal
		out.Currency = strings.ToUpper(obj.Currency)
		if obj.PaymentStatus == "paid" {
			out.Type = payments.CallbackPaid
		} else {
			// async_payment_succeeded 之外的非终态：忽略（不驱动状态机）。
			out.Type = "ignored"
		}
	case ev.Type == "checkout.session.async_payment_failed" || ev.Type == "checkout.session.expired":
		var obj checkoutSessionObject
		if err := json.Unmarshal(ev.Data.Object, &obj); err != nil {
			return nil, fmt.Errorf("stripe: decode checkout session: %w", err)
		}
		out.ProviderSessionID = obj.ID
		out.ProviderOrderID = obj.PaymentIntent
		out.OrderID = firstNonEmpty(obj.ClientReferenceID, obj.Metadata["order_id"])
		out.Amount = obj.AmountTotal
		out.Currency = strings.ToUpper(obj.Currency)
		out.Type = payments.CallbackFailed
	case ev.Type == "payment_intent.payment_failed":
		var obj paymentIntentObject
		if err := json.Unmarshal(ev.Data.Object, &obj); err != nil {
			return nil, fmt.Errorf("stripe: decode payment intent: %w", err)
		}
		out.ProviderOrderID = obj.ID
		out.OrderID = obj.Metadata["order_id"]
		out.Amount = obj.Amount
		out.Currency = strings.ToUpper(obj.Currency)
		out.Type = payments.CallbackFailed
	case ev.Type == "charge.refunded":
		var obj chargeObject
		if err := json.Unmarshal(ev.Data.Object, &obj); err != nil {
			return nil, fmt.Errorf("stripe: decode charge: %w", err)
		}
		out.ProviderOrderID = obj.PaymentIntent
		out.Amount = obj.AmountRefunded
		out.Currency = strings.ToUpper(obj.CurrencyRefunded)
		out.Type = payments.CallbackRefunded
	default:
		// 订阅等其余事件：PR3 接入；本期归一化为 ignored，调用方记录后 200。
		out.Type = "ignored"
	}
	return out, nil
}

// Refund 对 PaymentIntent 发起退款（POST /v1/refunds）。
func (a *Adapter) Refund(ctx context.Context, in payments.RefundInput) (*payments.RefundResult, error) {
	if a.cfg.SecretKey == "" {
		return nil, payments.ErrProviderNotConfigured(providerName)
	}
	if in.ProviderOrderID == "" {
		return nil, fmt.Errorf("stripe: refund requires provider order id (payment intent)")
	}
	form := url.Values{}
	form.Set("payment_intent", in.ProviderOrderID)
	if in.Amount > 0 {
		form.Set("amount", strconv.FormatInt(in.Amount, 10))
	}
	var out refundResponse
	if err := a.do(ctx, http.MethodPost, "/v1/refunds", form, in.IdempotencyKey, &out); err != nil {
		return nil, err
	}
	return &payments.RefundResult{
		RefundID:  out.ID,
		Succeeded: out.Status == "succeeded",
	}, nil
}

type refundResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// do 执行一次 Stripe REST 调用（form 编码 + Bearer 认证 + Idempotency-Key 头）。
func (a *Adapter) do(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.APIBaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCallbackBody))
	if err != nil {
		return fmt.Errorf("stripe: read response %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 渠道错误统一包装 domain ProviderError（use-case 映射 gRPC 状态，
		// 渠道原文不跨层透出）。
		return &payments.ProviderError{Provider: providerName, Status: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("stripe: decode response %s: %w", path, err)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var _ payments.PaymentProvider = (*Adapter)(nil)
