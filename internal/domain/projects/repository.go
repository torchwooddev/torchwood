package projects

import "context"

type Repository interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	GetProjectByName(ctx context.Context, name string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, id string) error
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
