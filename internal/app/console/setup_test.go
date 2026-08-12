package console

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- fakes（仅覆盖 Setup 用到的仓库方法） ---

type fakeAdminRepo struct {
	admins  []projects.Admin
	created []string
	deleted []string
}

func (r *fakeAdminRepo) GetAdmin(_ context.Context, _ string) (*projects.Admin, error) {
	return nil, nil
}

func (r *fakeAdminRepo) GetAdminByEmail(_ context.Context, email string) (*projects.Admin, error) {
	for i := range r.admins {
		if r.admins[i].Email == email {
			return &r.admins[i], nil
		}
	}
	return nil, nil
}

func (r *fakeAdminRepo) ListAdmins(context.Context) ([]projects.Admin, error) {
	return r.admins, nil
}

func (r *fakeAdminRepo) CreateAdmin(_ context.Context, a *projects.Admin) error {
	r.admins = append(r.admins, *a)
	r.created = append(r.created, a.ID)
	return nil
}

func (r *fakeAdminRepo) UpdateAdmin(context.Context, *projects.Admin) error {
	return nil
}

func (r *fakeAdminRepo) DeleteAdmin(_ context.Context, id string) error {
	for i := range r.admins {
		if r.admins[i].ID == id {
			r.admins = append(r.admins[:i], r.admins[i+1:]...)
			break
		}
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *fakeAdminRepo) CountAdminsByRole(context.Context, string) (int64, error) {
	return 0, nil
}

func (r *fakeAdminRepo) WithBootstrapLock(_ context.Context, _ int64, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

var _ projects.AdminRepository = (*fakeAdminRepo)(nil)

type fakeProjectRepo struct {
	projects map[string]*projects.Project
	deleted  []string
}

func (r *fakeProjectRepo) CreateProject(_ context.Context, p *projects.Project) error {
	if r.projects == nil {
		r.projects = map[string]*projects.Project{}
	}
	r.projects[p.ID] = p
	return nil
}

func (r *fakeProjectRepo) GetProject(_ context.Context, id string) (*projects.Project, error) {
	if r.projects == nil {
		return nil, nil
	}
	return r.projects[id], nil
}

func (r *fakeProjectRepo) GetProjectByName(context.Context, string) (*projects.Project, error) {
	return nil, nil
}

func (r *fakeProjectRepo) ListProjects(context.Context) ([]projects.Project, error) {
	return nil, nil
}

func (r *fakeProjectRepo) UpdateProject(context.Context, *projects.Project) error {
	return nil
}

func (r *fakeProjectRepo) DeleteProject(_ context.Context, id string) error {
	if r.projects != nil {
		delete(r.projects, id)
	}
	r.deleted = append(r.deleted, id)
	return nil
}

var _ projects.Repository = (*fakeProjectRepo)(nil)

type fakeAdminProjectRepo struct {
	grants []string // "adminID:projectID"
	err    error
}

func (r *fakeAdminProjectRepo) HasProjectAccess(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *fakeAdminProjectRepo) GrantProjectAccess(_ context.Context, adminID, projectID string) error {
	if r.err != nil {
		return r.err
	}
	r.grants = append(r.grants, adminID+":"+projectID)
	return nil
}

var _ projects.AdminProjectRepository = (*fakeAdminProjectRepo)(nil)

// fakeProjects 注入 projectCreator（CreateProjectInternal 失败场景）；
// 成功时模拟真实行为把项目写入 projectRepo。
type fakeProjects struct {
	err         error
	projectRepo *fakeProjectRepo
}

func (f *fakeProjects) CreateProjectInternal(_ context.Context, cmd server.CreateProjectCommand) (*projects.Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	p := &projects.Project{ID: "default", Name: cmd.Name}
	if f.projectRepo != nil {
		_ = f.projectRepo.CreateProject(context.Background(), p)
	}
	return p, nil
}

// fakeAPIKeys 注入 apiKeyCreator（Create/Delete 失败场景）。
type fakeAPIKeys struct {
	createErr error
	deleted   []string
}

func (f *fakeAPIKeys) CreateInternal(_ context.Context, _ server.CreateAPIKeyCommand) (*projects.APIKey, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	return &projects.APIKey{ID: "key-1", ProjectID: "default"}, "secret-1", nil
}

func (f *fakeAPIKeys) Delete(_ context.Context, projectID, id string) error {
	f.deleted = append(f.deleted, projectID+":"+id)
	return nil
}

type fakeAuth struct {
	signedInEmail string
}

func (f *fakeAuth) SignIn(_ context.Context, cmd SignInCommand) (*TokenPair, error) {
	f.signedInEmail = cmd.Email
	return &TokenPair{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: 1}, nil
}

const (
	setupEmail    = "admin@torchwood.local"
	setupPassword = "Pass@1234"
	setupToken    = "test-setup-token"
)

// setupWithFakes 构造 Setup 并用 fake 实现替换内部依赖（NewSetup 构造参数为
// 具体类型，字段收窄为接口；测试直接改字段注入 fake 即可）。
func setupWithFakes(adminRepo *fakeAdminRepo, projectRepo *fakeProjectRepo, adminProjectRepo *fakeAdminProjectRepo, projectsCreator *fakeProjects, apiKeys *fakeAPIKeys, auth *fakeAuth) *Setup {
	cfg := &config.AppConfig{Security: &config.Security{SetupToken: setupToken}}
	s := NewSetup(cfg, NewAdmins(adminRepo), nil, nil, nil, adminRepo, adminProjectRepo, projectRepo)
	projectsCreator.projectRepo = projectRepo
	s.projects = projectsCreator
	s.apiKeys = apiKeys
	s.auth = auth
	return s
}

func TestSetup_GetSetupStatus(t *testing.T) {
	ctx := context.Background()

	empty := &fakeAdminRepo{}
	status, err := setupWithFakes(empty, &fakeProjectRepo{}, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{}).GetSetupStatus(ctx)
	require.NoError(t, err)
	require.True(t, status)

	filled := &fakeAdminRepo{admins: []projects.Admin{{ID: "a-1", Email: "a@b.c"}}}
	status, err = setupWithFakes(filled, &fakeProjectRepo{}, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{}).GetSetupStatus(ctx)
	require.NoError(t, err)
	require.False(t, status)
}

func TestSetup_SignUp_FirstSuccess(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	projectRepo := &fakeProjectRepo{}
	adminProjectRepo := &fakeAdminProjectRepo{}
	auth := &fakeAuth{}
	setup := setupWithFakes(adminRepo, projectRepo, adminProjectRepo, &fakeProjects{}, &fakeAPIKeys{}, auth)

	result, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	require.NoError(t, err)

	// admin 为 owner，密码已哈希。
	require.Equal(t, AdminRoleOwner, result.Admin.Role)
	require.NotEmpty(t, result.Admin.PasswordHash)
	// project id = default。
	require.NotNil(t, projectRepo.projects["default"])
	// API key secret 非空。
	require.Equal(t, "secret-1", result.APIKeySecret)
	// tokens 已签发。
	require.NotNil(t, result.Tokens)
	require.Equal(t, "access-1", result.Tokens.AccessToken)
	// admin_projects 关联已写入。
	require.Equal(t, []string{result.Admin.ID + ":default"}, adminProjectRepo.grants)
	// 用规范化后的 email 签发 token。
	require.Equal(t, setupEmail, auth.signedInEmail)
}

func TestSetup_SignUp_SecondCallFails(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	projectRepo := &fakeProjectRepo{}
	setup := setupWithFakes(adminRepo, projectRepo, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	require.NoError(t, err)

	_, err = setup.SignUp(ctx, "another@torchwood.local", setupPassword, setupToken)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSetup_SignUp_AlreadyInitializedFails(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{admins: []projects.Admin{{ID: "a-1", Email: "a@b.c"}}}
	setup := setupWithFakes(adminRepo, &fakeProjectRepo{}, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSetup_SignUp_RollbackAdminOnProjectFailure(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	projectRepo := &fakeProjectRepo{}
	setup := setupWithFakes(adminRepo, projectRepo, &fakeAdminProjectRepo{}, &fakeProjects{err: errors.New("project boom")}, &fakeAPIKeys{}, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	require.Error(t, err)

	// admin 已回删，project 未创建。
	require.Len(t, adminRepo.deleted, 1)
	require.Len(t, adminRepo.admins, 0)
	require.Nil(t, projectRepo.projects["default"])
}

func TestSetup_SignUp_RollbackOnAPIKeyFailure(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	projectRepo := &fakeProjectRepo{}
	apiKeys := &fakeAPIKeys{createErr: errors.New("key boom")}
	setup := setupWithFakes(adminRepo, projectRepo, &fakeAdminProjectRepo{}, &fakeProjects{}, apiKeys, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	require.Error(t, err)

	// admin 与 project 均回删。
	require.Len(t, adminRepo.deleted, 1)
	require.Len(t, adminRepo.admins, 0)
	require.Contains(t, projectRepo.deleted, "default")
}

func TestSetup_SignUp_RollbackOnGrantFailure(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	projectRepo := &fakeProjectRepo{}
	setup := setupWithFakes(adminRepo, projectRepo, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{})
	setup.adminProjectRepo = &fakeAdminProjectRepo{err: errors.New("grant boom")}

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, setupToken)
	require.Error(t, err)

	require.Len(t, adminRepo.deleted, 1)
	require.Contains(t, projectRepo.deleted, "default")
}

func TestSetup_SignUp_WeakPasswordRejected(t *testing.T) {
	ctx := context.Background()
	setup := setupWithFakes(&fakeAdminRepo{}, &fakeProjectRepo{}, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, "short", setupToken)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSetup_SignUp_RejectedWhenTokenNotConfigured(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	cfg := &config.AppConfig{Security: &config.Security{}}
	s := NewSetup(cfg, NewAdmins(adminRepo), nil, nil, nil, adminRepo, &fakeAdminProjectRepo{}, &fakeProjectRepo{})

	_, err := s.SignUp(ctx, setupEmail, setupPassword, "")
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
	require.Len(t, adminRepo.admins, 0)
}

func TestSetup_SignUp_RejectedOnTokenMismatch(t *testing.T) {
	ctx := context.Background()
	adminRepo := &fakeAdminRepo{}
	setup := setupWithFakes(adminRepo, &fakeProjectRepo{}, &fakeAdminProjectRepo{}, &fakeProjects{}, &fakeAPIKeys{}, &fakeAuth{})

	_, err := setup.SignUp(ctx, setupEmail, setupPassword, "wrong-token")
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.Len(t, adminRepo.admins, 0)
}

func TestSetup_SetupTokenConfigured(t *testing.T) {
	require.False(t, NewSetup(&config.AppConfig{}, nil, nil, nil, nil, nil, nil, nil).SetupTokenConfigured())
	require.True(t, NewSetup(&config.AppConfig{Security: &config.Security{SetupToken: "t"}}, nil, nil, nil, nil, nil, nil, nil).SetupTokenConfigured())
}
