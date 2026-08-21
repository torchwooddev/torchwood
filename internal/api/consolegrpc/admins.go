package consolegrpc

import (
	"context"

	consolev1 "github.com/torchwooddev/torchwood/genproto/console/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/app/console"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AdminsService struct {
	consolev1.UnimplementedAdminsServiceServer
	admins *console.Admins
}

func NewAdminsService(admins *console.Admins) *AdminsService {
	return &AdminsService{admins: admins}
}

func (s *AdminsService) GetCurrentAdmin(ctx context.Context, _ *consolev1.GetCurrentAdminRequest) (*consolev1.Admin, error) {
	p, ok := principalFrom(ctx)
	if !ok || p.ActorKind != shared.ActorKindAdmin || (p.AdminID == "" && p.ActorID == "") {
		return nil, status.Error(codes.Unauthenticated, "admin context missing")
	}
	admin, err := s.admins.Get(ctx, callerID(ctx))
	if err != nil {
		return nil, err
	}
	return mapAdmin(admin), nil
}

// requireAdminActor 纵深防御：AdminsService 仅接受 admin 会话凭证
// （拦截器已拒绝 API Key，此处兜底防止绕过拦截器的直接调用）。
func requireAdminActor(ctx context.Context) error {
	p, ok := principalFrom(ctx)
	if !ok || p.ActorKind != shared.ActorKindAdmin {
		return status.Error(codes.PermissionDenied, "console admin session required")
	}
	return nil
}

func (s *AdminsService) ListAdmins(ctx context.Context, _ *consolev1.ListAdminsRequest) (*consolev1.ListAdminsResponse, error) {
	if err := requireAdminActor(ctx); err != nil {
		return nil, err
	}
	admins, err := s.admins.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*consolev1.Admin, len(admins))
	for i := range admins {
		out[i] = mapAdmin(&admins[i])
	}
	return &consolev1.ListAdminsResponse{Admins: out}, nil
}

func (s *AdminsService) CreateAdmin(ctx context.Context, req *consolev1.CreateAdminRequest) (*consolev1.Admin, error) {
	if err := requireAdminActor(ctx); err != nil {
		return nil, err
	}
	admin, err := s.admins.Create(ctx, console.CreateAdminCommand{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
		Role:     req.GetRole(),
	})
	if err != nil {
		return nil, err
	}
	return mapAdmin(admin), nil
}

func (s *AdminsService) UpdateAdmin(ctx context.Context, req *consolev1.UpdateAdminRequest) (*consolev1.Admin, error) {
	if err := requireAdminActor(ctx); err != nil {
		return nil, err
	}
	// role 为 optional（R10-P1-6）：未设置 = 不修改；设置（含空串）= 更新/清空。
	// use-case 对空串同样按"不修改"处理（UpdateAdminCommand.Role=="" 跳过）。
	cmd := console.UpdateAdminCommand{
		ID:       req.GetId(),
		CallerID: callerID(ctx),
		Password: req.GetPassword(),
	}
	if req.Role != nil {
		cmd.Role = req.GetRole()
	}
	admin, err := s.admins.Update(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return mapAdmin(admin), nil
}

func (s *AdminsService) DeleteAdmin(ctx context.Context, req *consolev1.DeleteAdminRequest) (*sharedv1.Empty, error) {
	if err := requireAdminActor(ctx); err != nil {
		return nil, err
	}
	if err := s.admins.Delete(ctx, req.GetId(), callerID(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func principalFrom(ctx context.Context) (*shared.Principal, bool) {
	return contexts.Principal(ctx)
}

func callerID(ctx context.Context) string {
	p, ok := principalFrom(ctx)
	if !ok {
		return ""
	}
	if p.AdminID != "" {
		return p.AdminID
	}
	return string(p.ActorID)
}

func mapAdmin(a *projects.Admin) *consolev1.Admin {
	if a == nil {
		return nil
	}
	return &consolev1.Admin{
		Id:        a.ID,
		Email:     a.Email,
		Role:      a.Role,
		CreatedAt: timestamppb.New(a.CreatedAt),
		UpdatedAt: timestamppb.New(a.UpdatedAt),
	}
}
