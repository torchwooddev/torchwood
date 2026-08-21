package assets

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// requireAssetWrite 断言资产写路径主体（红线 D6）：仅 console admin
// 会话（ActorKind=admin）、API key（Service）或 System。
// 终端用户一律 PermissionDenied——use-case 直接调用也不得写资产。
func requireAssetWrite(ctx context.Context) error {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	switch p.ActorKind {
	case shared.ActorKindAdmin, shared.ActorKindService, shared.ActorKindSystem:
		return nil
	default:
		return status.Error(codes.PermissionDenied, "asset write not allowed for this principal")
	}
}

// withSystemPrincipal 为 worker / 支付履约注入 System 主体（仍走 requireAssetWrite）。
func withSystemPrincipal(ctx context.Context, projectID string) context.Context {
	if p, ok := contexts.Principal(ctx); ok && p != nil && (p.IsSystem() || p.ActorKind == shared.ActorKindService) {
		if p.ProjectID == "" {
			cp := *p
			cp.ProjectID = projectID
			return contexts.WithPrincipal(ctx, &cp)
		}
		return ctx
	}
	return contexts.WithPrincipal(ctx, shared.NewSystemPrincipal(projectID))
}
