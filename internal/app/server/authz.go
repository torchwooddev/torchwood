package server

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// requirePlatformAdmin 拒绝非平台 admin 主体（API key、受限 console 管理员、
// 端用户）。对齐 Projects.CreateProject 的平台级资源守门模式（安全评审 M7）。
func requirePlatformAdmin(ctx context.Context) error {
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if principal.ActorKind != shared.ActorKindAdmin || !principal.IsPlatformAdmin {
		return status.Error(codes.PermissionDenied, "platform admin required")
	}
	return nil
}
