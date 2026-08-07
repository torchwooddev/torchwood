package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// projectIDRe 是项目 ID 白名单：小写字母、数字、连字符，长度 1-64
// （与安全评审 M7 / P2-9 的标识符约束合并）。
var projectIDRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

type Projects struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	db          *clients.Database
}

func NewProjects(projectRepo projects.Repository, docDB databases.DocumentDB, db *clients.Database) *Projects {
	return &Projects{projectRepo: projectRepo, docDB: docDB, db: db}
}

type CreateProjectCommand struct {
	Name        string
	Description string
}

func (s *Projects) CreateProject(ctx context.Context, cmd CreateProjectCommand) (*projects.Project, error) {
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	// 项目是平台级资源，创建仅限平台 admin（console 会话的 owner/admin 角色）；
	// API key 与受限 admin（viewer/member）一律拒绝（安全评审 M7）。
	if principal.ActorKind != shared.ActorKindAdmin || !principal.IsPlatformAdmin {
		return nil, status.Error(codes.PermissionDenied, "platform admin required to create projects")
	}
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	id := strings.ToLower(strings.ReplaceAll(cmd.Name, " ", "-"))
	id = strings.Trim(id, "-")
	if id == "" {
		id = "project-" + idgen.UUID().String()
	}
	// 项目 ID 白名单校验：非法字符（如非 ASCII、下划线）一律拒绝，防注入与混淆。
	if !projectIDRe.MatchString(id) {
		return nil, status.Error(codes.InvalidArgument, "project id must match ^[a-z0-9-]{1,64}$")
	}
	p := &projects.Project{
		ID:          id,
		Name:        cmd.Name,
		Description: cmd.Description,
		Status:      "active",
		Settings:    map[string]any{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err := s.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.projectRepo.CreateProject(txCtx, p); err != nil {
			return fmt.Errorf("insert project: %w", err)
		}
		if err := s.docDB.EnsureSystemCollections(txCtx, p.ID, p.InternalID); err != nil {
			return fmt.Errorf("ensure system collections: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *Projects) ListProjects(ctx context.Context, pageSize int32, pageToken, filter, orderBy string) ([]projects.Project, *crud.PaginationInfo, error) {
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return nil, nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	params, err := crud.ParseListParams(pageSize, pageToken, filter, orderBy)
	if err != nil {
		return nil, nil, err
	}
	// 非平台 admin（API key 或受限 console 管理员）无权列出全量项目：
	// ConsoleAdminProjectRepository 端口仅提供单项目授权判定（HasProjectAccess），
	// 无"列出授权项目"方法，故对非平台 admin 返回空列表（防跨项目信息泄露），
	// 项目级访问经 GetProject 按 principal.ProjectID 白名单放行（安全评审 M7）。
	if !principal.IsPlatformAdmin {
		info := crud.BuildPaginationInfo(params, 0, false)
		return []projects.Project{}, &info, nil
	}
	all, err := s.projectRepo.ListProjects(ctx)
	if err != nil {
		return nil, nil, err
	}
	start := params.Offset
	if start > len(all) {
		start = len(all)
	}
	end := start + int(params.PageSize)
	if end > len(all) {
		end = len(all)
	}
	page := all[start:end]
	hasMore := end < len(all)
	info := crud.BuildPaginationInfo(params, len(all), hasMore)
	return page, &info, nil
}

func (s *Projects) GetProject(ctx context.Context, id string) (*projects.Project, error) {
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	// 非平台 admin 仅能访问其绑定项目（API key 的所属项目 / admin 的
	// X-Torchwood-Project 已由拦截器 ValidateAdminProjectAccess 校验授权），
	// 越权一律返回 NotFound，避免项目存在性探测（安全评审 M7）。
	if !principal.IsPlatformAdmin && (principal.ProjectID == "" || principal.ProjectID != id) {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return s.projectRepo.GetProject(ctx, id)
}
