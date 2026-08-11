package console

import (
	"context"
	"log/slog"

	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Bootstrap 默认资源常量：首个管理员注册时自动创建的 project 与 API Key。
// project id 由 CreateProjectInternal 从名称派生（"Default" -> "default"）。
const (
	defaultProjectName = "Default"
	defaultProjectDesc = "Auto-created default project"
	defaultAPIKeyName  = "Default API Key"
	defaultAPIKeyScope = "all"
)

// Setup 处理首个管理员 bootstrap：查询初始化状态（GetSetupStatus）与首个
// 管理员注册（SignUp）。SignUp 是公开端点（ConsoleAuthService 服务级
// ACCESS_PUBLIC），「仅首次可用」的保证必须在本 use-case 内完成，
// 不依赖拦截器。
type Setup struct {
	admins           adminCreator
	projects         projectCreator
	apiKeys          apiKeyCreator
	auth             tokenIssuer
	adminRepo        projects.AdminRepository
	adminProjectRepo projects.AdminProjectRepository
	projectRepo      projects.Repository
}

// 以下接口由 internal/app 的对应 use-case 实现（构造参数为具体类型，
// 字段收窄为接口仅用于单元测试注入失败场景）：
//   - projectCreator: *server.Projects
//   - apiKeyCreator:  *server.APIKeys
//   - adminCreator:   *Admins
//   - tokenIssuer:    *Auth
type projectCreator interface {
	CreateProjectInternal(ctx context.Context, cmd server.CreateProjectCommand) (*projects.Project, error)
}

type apiKeyCreator interface {
	Create(ctx context.Context, cmd server.CreateAPIKeyCommand) (*projects.APIKey, string, error)
	Delete(ctx context.Context, projectID, id string) error
}

type adminCreator interface {
	Create(ctx context.Context, cmd CreateAdminCommand) (*projects.Admin, error)
}

type tokenIssuer interface {
	SignIn(ctx context.Context, cmd SignInCommand) (*TokenPair, error)
}

func NewSetup(
	admins *Admins,
	projects *server.Projects,
	apiKeys *server.APIKeys,
	auth *Auth,
	adminRepo projects.AdminRepository,
	adminProjectRepo projects.AdminProjectRepository,
	projectRepo projects.Repository,
) *Setup {
	return &Setup{
		admins:           admins,
		projects:         projects,
		apiKeys:          apiKeys,
		auth:             auth,
		adminRepo:        adminRepo,
		adminProjectRepo: adminProjectRepo,
		projectRepo:      projectRepo,
	}
}

// GetSetupStatus 返回是否尚未初始化（admins 表为空即 needs_setup=true）。
func (s *Setup) GetSetupStatus(ctx context.Context) (bool, error) {
	admins, err := s.adminRepo.ListAdmins(ctx)
	if err != nil {
		return false, status.Errorf(codes.Internal, "check setup status: %v", err)
	}
	return len(admins) == 0, nil
}

type SignUpResult struct {
	Admin        *projects.Admin
	Tokens       *TokenPair
	APIKeySecret string
}

// SignUp 注册首个管理员并完成引导：admin(owner) + 默认 project + 默认
// API Key(scope=all) + admin_projects 关联 + 签发 TokenPair。
// 任一后续步骤失败时 best-effort 回删已建资源，避免「admin 已建但 project
// 缺失」导致无法重试也无法登录的死锁；补偿失败只记日志。
// 并发窗口：两个并发 SignUp 可能同时通过首次性检查，MVP 接受（首次部署
// 单人操作；收紧可引入 Postgres advisory lock，列为可选增强）。
func (s *Setup) SignUp(ctx context.Context, email, password string) (*SignUpResult, error) {
	admins, err := s.adminRepo.ListAdmins(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check setup state: %v", err)
	}
	if len(admins) > 0 {
		return nil, status.Error(codes.FailedPrecondition, "setup already completed")
	}

	admin, err := s.admins.Create(ctx, CreateAdminCommand{
		Email:    email,
		Password: password,
		Role:     AdminRoleOwner,
	})
	if err != nil {
		return nil, err
	}

	var (
		project *projects.Project
		key     *projects.APIKey
	)
	rollback := func() {
		if key != nil {
			if err := s.apiKeys.Delete(ctx, key.ProjectID, key.ID); err != nil {
				slog.Warn("setup rollback: delete api key failed", "project", key.ProjectID, "key", key.ID, "error", err)
			}
		}
		if project != nil {
			if err := s.projectRepo.DeleteProject(ctx, project.ID); err != nil {
				slog.Warn("setup rollback: delete project failed", "project", project.ID, "error", err)
			}
		}
		// 直接走 repo 删除：Admins.Delete 会拒绝删除最后一个 owner，
		// 与失败补偿的语义冲突。
		if err := s.adminRepo.DeleteAdmin(ctx, admin.ID); err != nil {
			slog.Warn("setup rollback: delete admin failed", "admin", admin.ID, "error", err)
		}
	}

	project, err = s.projects.CreateProjectInternal(ctx, server.CreateProjectCommand{
		Name:        defaultProjectName,
		Description: defaultProjectDesc,
	})
	if err != nil {
		rollback()
		return nil, status.Errorf(codes.Internal, "create default project: %v", err)
	}

	key, secret, err := s.apiKeys.Create(ctx, server.CreateAPIKeyCommand{
		ProjectID: project.ID,
		Name:      defaultAPIKeyName,
		Scopes:    []string{defaultAPIKeyScope},
	})
	if err != nil {
		rollback()
		return nil, status.Errorf(codes.Internal, "create default api key: %v", err)
	}

	// 保持 admin_projects 关联数据完整（owner 实际会被
	// ValidateAdminProjectAccess 放行，但表应反映真实授权关系）。
	if err := s.adminProjectRepo.GrantProjectAccess(ctx, admin.ID, project.ID); err != nil {
		rollback()
		return nil, status.Errorf(codes.Internal, "grant project access: %v", err)
	}

	// 用规范化后的 email（Admins.Create 内部已小写化）签发 TokenPair。
	tokens, err := s.auth.SignIn(ctx, SignInCommand{Email: admin.Email, Password: password})
	if err != nil {
		rollback()
		return nil, err
	}

	return &SignUpResult{
		Admin:        admin,
		Tokens:       tokens,
		APIKeySecret: secret,
	}, nil
}
