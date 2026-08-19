package serverhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/require"
	apppayments "github.com/torchwooddev/torchwood/internal/app/payments"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	infrapayments "github.com/torchwooddev/torchwood/internal/infra/payments"
	"github.com/torchwooddev/torchwood/internal/infra/payments/stripe"
)

// writeSpyCallbackRepo 记录 InsertIfAbsent 调用次数：验签失败路径
// 不得落 payment_callback_events（设计 §1.4 / 验收「假签无落库」）。
type writeSpyCallbackRepo struct{ inserts int }

func (s *writeSpyCallbackRepo) InsertIfAbsent(context.Context, *domainpayments.CallbackEvent, string, string) (bool, error) {
	s.inserts++
	return false, errors.New("payments: callback persist must not run after verify failure")
}

// newCallbackOnlyPayments 构造只走到「验签」一层即返回的 use-case
// （db / 订单 / 履约 / outbox 为 nil：本组用例验签全部失败，不允许触达
// 任何落库路径——一旦触达即 panic 或 spy 计数 > 0，测试失败）。
func newCallbackOnlyPayments(t *testing.T) (*apppayments.Payments, *writeSpyCallbackRepo) {
	t.Helper()
	adapter := stripe.New(stripe.Config{
		SecretKey:     "sk_test_x",
		WebhookSecret: "whsec_handler_test",
	})
	spy := &writeSpyCallbackRepo{}
	return apppayments.NewPayments(
		nil, nil, nil, spy, nil,
		apppayments.NewRecordOnlyFulfiller(),
		infrapayments.NewRegistry(adapter),
		nil, nil,
	), spy
}

func TestPaymentsHandler_ForgedSignatureReturns401(t *testing.T) {
	uc, spy := newCallbackOnlyPayments(t)
	h, err := NewPaymentsHandler(uc, nil)
	require.NoError(t, err)

	body := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/callbacks/stripe", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", "t=1700000000,v1=0000000000000000000000000000000000000000000000000000000000000000")
	rec := httptest.NewRecorder()
	h.stripeCallback(rec, req, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, rec.Body.String(), "不返回区分性错误")
	require.Equal(t, 0, spy.inserts, "假签不得落 payment_callback_events")
}

func TestPaymentsHandler_MissingSignatureHeaderReturns401(t *testing.T) {
	uc, spy := newCallbackOnlyPayments(t)
	h, err := NewPaymentsHandler(uc, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/payments/callbacks/stripe", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.stripeCallback(rec, req, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, spy.inserts, "缺签不得落库")
}

func TestPaymentsHandler_UnconfiguredProviderReturns401(t *testing.T) {
	// 渠道未配置：fail-closed，同样 401（不区分未配置 / 签名错）。
	adapter := stripe.New(stripe.Config{})
	uc := apppayments.NewPayments(nil, nil, nil, nil, nil,
		apppayments.NewRecordOnlyFulfiller(), infrapayments.NewRegistry(adapter), nil, nil)
	h, err := NewPaymentsHandler(uc, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/payments/callbacks/stripe", strings.NewReader(`{}`))
	req.Header.Set("Stripe-Signature", "t=1700000000,v1=abc")
	rec := httptest.NewRecorder()
	h.stripeCallback(rec, req, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPaymentsHandler_BodyTooLargeReturns401(t *testing.T) {
	uc, spy := newCallbackOnlyPayments(t)
	h, err := NewPaymentsHandler(uc, nil)
	require.NoError(t, err)

	big := strings.Repeat("a", maxCallbackBody+2)
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/callbacks/stripe", strings.NewReader(big))
	rec := httptest.NewRecorder()
	h.stripeCallback(rec, req, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, spy.inserts)
}

// TestPaymentsHandler_RegisterRoute 确认路由挂载在 /v1/payments/callbacks/stripe
// （经 gateway ServeMux 分发；HandlePath 自定义 handler 不进 proto 转码）。
func TestPaymentsHandler_RegisterRoute(t *testing.T) {
	uc, _ := newCallbackOnlyPayments(t)
	h, err := NewPaymentsHandler(uc, nil)
	require.NoError(t, err)
	mux := runtime.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/payments/callbacks/stripe", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestPaymentsHandler_UnknownProvider401 验证 use-case 对未知渠道同样
// 按验签失败应答（PR4 泛化 {provider} 路由前的行为锁定）。
func TestPaymentsHandler_UnknownProvider(t *testing.T) {
	uc, spy := newCallbackOnlyPayments(t)
	err := uc.HandleCallback(context.Background(), "wechat", http.Header{}, []byte(`{}`))
	require.ErrorIs(t, err, domainpayments.ErrSignatureInvalid)
	require.Equal(t, 0, spy.inserts, "未知渠道不得落库")
}
