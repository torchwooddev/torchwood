// Package serverhttp 之 payments 回调入口（v3 设计 §1.4 / 决策 D7）：
// 渠道 webhook 需要原始 body 验签，走裸 HTTP handler（不进 gRPC gateway
// 的 JSON 反序列化）。验签失败一律 401、不落任何行、不回执业务码；
// 渠道回执格式（Stripe 空 body 2xx）由 use-case 成功与否决定。
package serverhttp

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	apppayments "github.com/torchwooddev/torchwood/internal/app/payments"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
)

// maxCallbackBody 是渠道回调体上限（1 MiB；正常 Stripe webhook 远小于此）。
const maxCallbackBody = 1 << 20

// PaymentsHandler 是支付渠道回调 HTTP handler。
type PaymentsHandler struct {
	payments *apppayments.Payments
	logger   *slog.Logger
}

// NewPaymentsHandler creates the payments callback handler.
func NewPaymentsHandler(payments *apppayments.Payments, logger *slog.Logger) (*PaymentsHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &PaymentsHandler{payments: payments, logger: logger}, nil
}

// Register attaches the callback route to the gateway mux.
// PR4 渠道补齐时泛化为 /v1/payments/callbacks/{provider}。
func (h *PaymentsHandler) Register(mux *runtime.ServeMux) {
	_ = mux.HandlePath("POST", "/v1/payments/callbacks/stripe", h.stripeCallback)
}

// stripeCallback 处理 Stripe webhook：
//   - 原始 body 直读（禁止先进 JSON 中间件，D7）；
//   - 验签失败（含未配置 / 未知渠道）→ 401，不落库、无区分性错误
//     （设计 §Security 1）；
//   - 处理成功 → 200 空 body（Stripe 约定；具体回执格式在 adapter/use-case）；
//   - 处理失败 → 500，Stripe 按退避重推（事务已回滚，重推安全）。
func (h *PaymentsHandler) stripeCallback(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody+1))
	if err != nil || len(raw) > maxCallbackBody {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := h.payments.HandleCallback(r.Context(), domainpayments.ProviderStripe, r.Header, raw); err != nil {
		if errors.Is(err, domainpayments.ErrSignatureInvalid) {
			// 不落任何行；不区分「签名错 / 缺头 / 时间戳过期」。
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.logger.Error("payment callback processing failed",
			"provider", domainpayments.ProviderStripe, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
