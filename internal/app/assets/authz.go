package assets

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const systemActorID = "system"

// requireAssetWrite 断言资产写路径主体（红线 D6）：仅 console admin
// 会话（ActorKind=admin）或 API key / 系统主体（ActorKind=service）。
// 终端用户一律 PermissionDenied——use-case 直接调用也不得写资产。
func requireAssetWrite(ctx context.Context) error {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	switch p.ActorKind {
	case shared.ActorKindAdmin, shared.ActorKindService:
		return nil
	default:
		return status.Error(codes.PermissionDenied, "asset write not allowed for this principal")
	}
}

// withSystemPrincipal 为 worker / 支付履约注入 system 主体（仍走 requireAssetWrite）。
func withSystemPrincipal(ctx context.Context, projectID string) context.Context {
	if p, ok := contexts.Principal(ctx); ok && p != nil && p.ActorKind == shared.ActorKindService {
		if p.ProjectID == "" {
			cp := *p
			cp.ProjectID = projectID
			return contexts.WithPrincipal(ctx, &cp)
		}
		return ctx
	}
	return contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:        idgen.ID(systemActorID),
		ActorKind:      shared.ActorKindService,
		CredentialType: shared.CredentialTypeAPIKey,
		ProjectID:      projectID,
		Roles:          []string{"system"},
	})
}
