package console

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"strings"

	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
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

// bootstrapLockKey 是引导流程的 pg_advisory_xact_lock 常量键（"Torchwoo"
// 的 ASCII 编码），用于串行化首个管理员检查+创建。
const bootstrapLockKey = int64(0x546F726368776F6F)

// Setup 处理首个管理员 bootstrap：查询初始化状态（GetSetupStatus）与首个
// 管理员注册（SignUp）。SignUp 是公开端点（ConsoleAuthService 服务级
// ACCESS_PUBLIC），「仅首次可用 + 需 setup token」的保证必须在本 use-case
// 内完成，不依赖拦截器。
type Setup struct {
	setupToken       string
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
	CreateInternal(ctx context.Context, cmd server.CreateAPIKeyCommand) (*projects.APIKey, string, error)
	Delete(ctx context.Context, projectID, id string) error
}

type adminCreator interface {
	Create(ctx context.Context, cmd CreateAdminCommand) (*projects.Admin, error)
}

type tokenIssuer interface {
	SignIn(ctx context.Context, cmd SignInCommand) (*TokenPair, error)
}

func NewSetup(
	cfg *config.AppConfig,
	admins *Admins,
	projects *server.Projects,
	apiKeys *server.APIKeys,
	auth *Auth,
	adminRepo projects.AdminRepository,
	adminProjectRepo projects.AdminProjectRepository,
	projectRepo projects.Repository,
) *Setup {
	setupToken := ""
	if cfg != nil && cfg.GetSecurity() != nil {
		setupToken = strings.TrimSpace(cfg.GetSecurity().GetSetupToken())
	}
	return &Setup{
		setupToken:       setupToken,
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

// SetupTokenConfigured 返回部署方是否配置了引导令牌（security.setup_token）。
func (s *Setup) SetupTokenConfigured() bool {
	return s.setupToken != ""
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
func (s *Setup) SignUp(ctx context.Context, email, password, setupToken string) (*SignUpResult, error) {
	// setup token 校验先于一切状态检查：未配置即拒绝（引导入口默认关闭），
	// 请求令牌与配置不一致同样拒绝，防止无凭据抢占首个 owner。
	if s.setupToken == "" {
		return nil, status.Error(codes.FailedPrecondition, "setup token is not configured; set TORCHWOOD_SECURITY_SETUP_TOKEN before bootstrapping")
	}
	if !setupTokenEqual(setupToken, s.setupToken) {
		return nil, status.Error(codes.PermissionDenied, "invalid setup token")
	}

	// 并发兜底：首次性检查与首个 admin 创建在 pg_advisory_xact_lock 事务内
	// 串行化——并发的 SignUp 只有一个能通过「admins 为空」检查，其余看到
	// 已提交的首个 admin 后被拒绝，杜绝抢占 owner。事务内（含后续 repo
	// 调用）通过注入的 ctx 复用同一连接。
	var (
		admin *projects.Admin
		err   error
	)
	if err := s.adminRepo.WithBootstrapLock(ctx, bootstrapLockKey, func(txCtx context.Context) error {
		admins, err := s.adminRepo.ListAdmins(txCtx)
		if err != nil {
			return status.Errorf(codes.Internal, "check setup state: %v", err)
		}
		if len(admins) > 0 {
			return status.Error(codes.FailedPrecondition, "setup already completed")
		}
		admin, err = s.admins.Create(txCtx, CreateAdminCommand{
			Email:    email,
			Password: password,
			Role:     AdminRoleOwner,
		})
		return err
	}); err != nil {
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

	key, secret, err := s.apiKeys.CreateInternal(ctx, server.CreateAPIKeyCommand{
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

// setupTokenEqual 常量时间比较 setup token，避免时序侧信道。
func setupTokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
