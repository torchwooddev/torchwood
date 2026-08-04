package interceptor

import (
	"context"

	"github.com/deeploop-ai/graviton/internal/domain/audit"
	"github.com/deeploop-ai/graviton/internal/pkg/contexts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AuditInterceptor struct {
	repo audit.Repository
}

func NewAuditInterceptor(repo audit.Repository) *AuditInterceptor {
	return &AuditInterceptor{repo: repo}
}

func (a *AuditInterceptor) UnaryAuditMiddleware(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if a.repo == nil {
		return resp, err
	}

	entry := &audit.Entry{
		Action: info.FullMethod,
		Status: auditStatus(err),
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		// 优先使用 ClientInfoInterceptor 写入的、经过 trusted-proxy 校验的 IP；
		// 仅在链路中没有 ClientInfo（如测试直挂 audit）时退化为直接读头部。
		if ci := contexts.ClientInfoFrom(ctx); ci.IP != "" || ci.UserAgent != "" {
			entry.IP = ci.IP
			entry.UserAgent = ci.UserAgent
		} else {
			entry.IP = FirstForwardedHop(firstMetadataValue(md, "x-forwarded-for"))
			if entry.IP == "" {
				entry.IP = firstMetadataValue(md, "x-real-ip")
			}
			entry.UserAgent = firstMetadataValue(md, "grpcgateway-user-agent")
			if entry.UserAgent == "" {
				entry.UserAgent = firstMetadataValue(md, "user-agent")
			}
		}
	}
	if p, ok := contexts.Principal(ctx); ok && p != nil {
		entry.ActorID = string(p.ActorID)
		entry.ActorKind = string(p.ActorKind)
		entry.ProjectID = p.ProjectID
	}
	if resID := contexts.AuditResource(ctx); resID != "" {
		entry.ResourceID = resID
	}
	if logErr := a.repo.Insert(context.Background(), entry); logErr != nil {
		_ = logErr
	}
	return resp, err
}

func auditStatus(err error) string {
	if err == nil {
		return "success"
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return st.Code().String()
	}
	return "error"
}
