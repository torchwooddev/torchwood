package serverhttp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/audit"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"google.golang.org/grpc/status"
)

// auditFromHTTP 把特权 HTTP 操作写入审计仓库（P2-6：手工 HTTP 入口此前
// 只有 slog，特权写无持久审计轨迹）。语义对齐 gRPC AuditInterceptor：
// 3s 超时 + WithoutCancel，失败仅 Warn，不阻塞响应；不记录凭证。
func auditFromHTTP(r *http.Request, ip string, repo audit.Repository, logger *slog.Logger, action string, metadata map[string]any, resourceID string, principal *shared.Principal, err error) {
	if repo == nil {
		return
	}
	entry := &audit.Entry{
		Action:     action,
		ResourceID: resourceID,
		Status:     "error",
		CreatedAt:  time.Now().UTC(),
		Metadata:   metadata,
	}
	if err == nil {
		entry.Status = "success"
	} else if st, ok := status.FromError(err); ok && st.Code().String() != "" {
		entry.Status = st.Code().String()
	}
	entry.IP, entry.UserAgent = ip, r.UserAgent()
	if principal != nil {
		entry.ActorID = string(principal.ActorID)
		entry.ActorKind = string(principal.ActorKind)
		entry.ProjectID = principal.ProjectID
	}
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	if insertErr := repo.Insert(insertCtx, entry); insertErr != nil {
		if logger != nil {
			logger.Warn("http audit insert failed", slog.String("action", action), slog.String("error", insertErr.Error()))
		}
	}
}
