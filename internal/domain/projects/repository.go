package projects

import "context"

type Repository interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	GetProjectByName(ctx context.Context, name string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, id string) error
	// DeleteProjectControlPlaneRows 清理 public 控制面中该项目的派生行
	// （outbox/死信/api_keys/audit_logs/admin_projects/provider_resource_index）。
	// 感知调用方事务：项目删除事务内执行，与 schema DROP 原子提交。
	DeleteProjectControlPlaneRows(ctx context.Context, projectID string) error
}

// SchemaManager 管理项目数据面 schema 的生命周期（infra/projectschema 适配）。
// Ensure/DropCascade 感知调用方事务：ctx 携带事务时并入同一事务，
// 与项目行写入/删除原子提交。
type SchemaManager interface {
	// Ensure 幂等确保 tw_<projectID> schema 存在且迁移到最新版本。
	Ensure(ctx context.Context, projectID string) error
	// DropCascade 删除项目数据面 schema（CASCADE，连带其全部对象）。
	DropCascade(ctx context.Context, projectID string) error
	// Invalidate 清除本进程的 schema 就绪缓存（DropCascade 后自动调用；
	// 迁移器之外带外改动 schema 状态时同样需要）。
	Invalidate(projectID string)
}

type APIKeyRepository interface {
	CreateAPIKey(ctx context.Context, key *APIKey) error
	GetAPIKey(ctx context.Context, projectID, id string) (*APIKey, error)
	GetAPIKeyBySecretHash(ctx context.Context, hash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, projectID string) ([]APIKey, error)
	DeleteAPIKey(ctx context.Context, projectID, id string) error
}

type AdminRepository interface {
	GetAdmin(ctx context.Context, id string) (*Admin, error)
	GetAdminByEmail(ctx context.Context, email string) (*Admin, error)
	ListAdmins(ctx context.Context) ([]Admin, error)
	CreateAdmin(ctx context.Context, admin *Admin) error
	UpdateAdmin(ctx context.Context, admin *Admin) error
	DeleteAdmin(ctx context.Context, id string) error
	CountAdminsByRole(ctx context.Context, role string) (int64, error)
	// WithBootstrapLock 在事务内持 pg_advisory_xact_lock(key) 执行 fn，
	// 串行化首个管理员引导的首次性检查与创建。fn 内通过 ctx 使用同一事务
	// （ListAdmins/CreateAdmin/DeleteAdmin 等方法自动感知事务上下文）；
	// 锁随事务提交/回滚自动释放。
	WithBootstrapLock(ctx context.Context, key int64, fn func(ctx context.Context) error) error
}
