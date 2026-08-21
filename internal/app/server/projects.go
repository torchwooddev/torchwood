package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	appshared "github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxProjectDescriptionLen 是项目 description 的长度上限，
// CreateProject 与 UpdateProject 两侧一致约束（口径 a）。
const maxProjectDescriptionLen = 512

type Projects struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	db          *clients.Database
}

func NewProjects(projectRepo projects.Repository, docDB databases.DocumentDB, db *clients.Database) *Projects {
	return &Projects{projectRepo: projectRepo, docDB: docDB, db: db}
}

type CreateProjectCommand struct {
	ID              string
	Name            string
	Description     string
	FirstDatabaseID string // 缺省 "default"；CreateProject 内部调 infra 建空业务库
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
	return s.CreateProjectInternal(ctx, cmd)
}

// CreateProjectInternal 创建项目（校验 id/name/description、事务内插入
// project、Apply 静态表并建第一业务库）。不做 principal 检查，仅供 bootstrap 等
// 系统路径调用，调用方负责授权；外部入口 CreateProject 保留平台 admin 校验后
// 委托本方法。
func (s *Projects) CreateProjectInternal(ctx context.Context, cmd CreateProjectCommand) (*projects.Project, error) {
	if err := ident.ValidateSchemaResourceID(cmd.ID); err != nil {
		return nil, appshared.MapIdentError(err)
	}
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if len(cmd.Description) > maxProjectDescriptionLen {
		return nil, status.Error(codes.InvalidArgument, "description must be at most 512 characters")
	}
	firstDBID := strings.TrimSpace(cmd.FirstDatabaseID)
	if firstDBID == "" {
		firstDBID = "default"
	}
	if err := ident.ValidateSchemaResourceID(firstDBID); err != nil {
		return nil, appshared.MapIdentError(err)
	}
	p := &projects.Project{
		ID:          cmd.ID,
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
		schema, err := ident.ProjectSchemaName(p.ID)
		if err != nil {
			return appshared.MapIdentError(err)
		}
		if _, err := s.db.Conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema))); err != nil {
			return fmt.Errorf("create project schema: %w", err)
		}
		if err := projectschema.Apply(txCtx, s.db, p.ID); err != nil {
			return fmt.Errorf("apply project schema: %w", err)
		}
		if err := s.docDB.CreateDatabase(txCtx, p.ID, firstDBID, firstDBID); err != nil {
			return fmt.Errorf("create first database: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// DeleteProject 对外删除入口：仅平台 admin。校验存在后委托 DeleteProjectInternal。
func (s *Projects) DeleteProject(ctx context.Context, id string) error {
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if principal.ActorKind != shared.ActorKindAdmin || !principal.IsPlatformAdmin {
		return status.Error(codes.PermissionDenied, "platform admin required to delete projects")
	}
	if err := ident.ValidateSchemaResourceID(id); err != nil {
		return appshared.MapIdentError(err)
	}
	p, err := s.projectRepo.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if p == nil {
		return status.Error(codes.NotFound, "project not found")
	}
	return s.DeleteProjectInternal(ctx, id)
}

// DeleteProjectInternal 级联删除项目（不做 principal 检查）。setup 回滚与
// 测试清理必须走这里；外部入口 DeleteProject 保留平台 admin 校验后委托本方法。
// 同一事务：业务 schema DROP → 清理 public 行 → DROP tw_<project> → 删 projects 行。
func (s *Projects) DeleteProjectInternal(ctx context.Context, id string) error {
	if err := ident.ValidateSchemaResourceID(id); err != nil {
		return appshared.MapIdentError(err)
	}
	return s.db.RunInTx(ctx, func(txCtx context.Context) error {
		dbs, err := s.docDB.ListDatabases(txCtx, id)
		if err != nil {
			return fmt.Errorf("list databases: %w", err)
		}
		for _, db := range dbs {
			if db.ID == ident.ProjectDataPlaneID {
				continue
			}
			if err := s.docDB.DeleteDatabase(txCtx, id, db.ID); err != nil {
				return fmt.Errorf("drop business schema %s: %w", db.ID, err)
			}
		}
		if err := s.deletePublicProjectRows(txCtx, id); err != nil {
			return err
		}
		schema, err := ident.ProjectSchemaName(id)
		if err != nil {
			return appshared.MapIdentError(err)
		}
		if _, err := s.db.Conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, quoteIdent(schema))); err != nil {
			return fmt.Errorf("drop project schema: %w", err)
		}
		if err := s.projectRepo.DeleteProject(txCtx, id); err != nil {
			return fmt.Errorf("delete project row: %w", err)
		}
		return nil
	})
}

func (s *Projects) deletePublicProjectRows(ctx context.Context, projectID string) error {
	conn := s.db.Conn(ctx)
	if _, err := conn.NewDelete().Model((*model.DocumentEventsOutbox)(nil)).Where("project_id = ?", projectID).Exec(ctx); err != nil {
		return fmt.Errorf("delete outbox: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM document_events_outbox_dead WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("delete outbox dead: %w", err)
	}
	if _, err := conn.NewDelete().Model((*model.APIKey)(nil)).Where("project_id = ?", projectID).Exec(ctx); err != nil {
		return fmt.Errorf("delete api_keys: %w", err)
	}
	if _, err := conn.NewDelete().Model((*model.AuditLog)(nil)).Where("project_id = ?", projectID).Exec(ctx); err != nil {
		return fmt.Errorf("delete audit_logs: %w", err)
	}
	if _, err := conn.NewDelete().Model((*model.AdminProject)(nil)).Where("project_id = ?", projectID).Exec(ctx); err != nil {
		return fmt.Errorf("delete admin_projects: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM provider_resource_index WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("delete provider_resource_index: %w", err)
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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
	// AdminProjectRepository 端口仅提供单项目授权判定（HasProjectAccess），
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

type UpdateProjectCommand struct {
	ProjectID   string // 目标项目 id
	Name        *string
	Description *string
	// 无 Principal 字段：use-case 内从 contexts.Principal(ctx) 取
	// （与 CreateProject/GetProject/ListProjects 的仓库模式一致）。
}

func (s *Projects) UpdateProject(ctx context.Context, cmd UpdateProjectCommand) (*projects.Project, error) {
	if cmd.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project id is required")
	}
	// "nothing to update" 前置检查放在取数之前（对齐 storage.UpdateFile 先例），
	// 避免"项目不存在 + 全空请求"返回 NotFound 的语义歧义。
	if cmd.Name == nil && cmd.Description == nil {
		return nil, status.Error(codes.InvalidArgument, "nothing to update")
	}
	principal, ok := contexts.Principal(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	// 越权保护：非平台 admin 仅能更新其绑定项目，越权返回 NotFound（防枚举，
	// 与 GetProject 语义一致）。
	if !principal.IsPlatformAdmin && (principal.ProjectID == "" || principal.ProjectID != cmd.ProjectID) {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	project, err := s.projectRepo.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if cmd.Name != nil {
		// 编辑场景对空白名拒绝（有意收紧，严格于 CreateProject 的空名回落默认 id）。
		name := strings.TrimSpace(*cmd.Name)
		if name == "" {
			return nil, status.Error(codes.InvalidArgument, "name is required")
		}
		if name != project.Name {
			// 撞名查重：name 唯一索引存在，不查重会命中 DB unique violation → 500。
			existing, err := s.projectRepo.GetProjectByName(ctx, name)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				return nil, status.Error(codes.InvalidArgument, "project name already exists")
			}
		}
		project.Name = name
	}
	if cmd.Description != nil {
		if len(*cmd.Description) > maxProjectDescriptionLen {
			return nil, status.Error(codes.InvalidArgument, "description must be at most 512 characters")
		}
		project.Description = *cmd.Description
	}
	// repo 的 UpdateProject 是全列覆盖写，不置当前时间则 updated_at 永远停滞。
	project.UpdatedAt = time.Now()
	if err := s.projectRepo.UpdateProject(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}
