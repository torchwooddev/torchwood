package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	appshared "github.com/torchwooddev/torchwood/internal/app/shared"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/groups"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/password"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Users struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	sessions    domainauth.SessionService
	db          *clients.Database
}

func NewUsers(projectRepo projects.Repository, docDB databases.DocumentDB, sessions domainauth.SessionService, db *clients.Database) *Users {
	return &Users{projectRepo: projectRepo, docDB: docDB, sessions: sessions, db: db}
}

func (u *Users) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := u.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := u.docDB.EnsureSystemCollections(ctx, p.ID, p.InternalID); err != nil {
		return nil, err
	}
	return p, nil
}

func (u *Users) ListUsers(ctx context.Context, projectID string, q databases.Query, principal databases.Principal) ([]databases.Document, int64, string, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "users", q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (u *Users) GetUser(ctx context.Context, projectID, userID string, principal databases.Principal) (*databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return u.docDB.GetDocument(ctx, projectID, "default", "users", userID, principal)
}

var userUpdateProtectedFields = map[string]struct{}{
	"password_hash": {},
}

// CreateUser 服务端创建用户：校验 email 唯一性与密码强度，写入 users 文档。
// 与 Client SignUp 共用同一套权限与存储语义。
// 纵深防御（G2-2）：业务写主体（console admin 会话 / API key）才允许经
// SystemPrincipal 写库；viewer 角色细粒度由拦截器 adminRoleMethodRules 把关。
func (u *Users) CreateUser(ctx context.Context, projectID string, cmd CreateUserCommand) (*databases.Document, error) {
	if err := appshared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	email := normalizeEmail(cmd.Email)
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if err := users.ValidatePasswordStrength(cmd.Password); err != nil {
		return nil, err
	}
	if cmd.Status != "" {
		if err := users.ValidateStatus(cmd.Status); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	} else {
		cmd.Status = users.StatusActive
	}

	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if len(list.Documents) > 0 {
		return nil, status.Error(codes.AlreadyExists, "email already registered")
	}

	hash, err := password.Hash(cmd.Password)
	if err != nil {
		return nil, err
	}

	userID := idgen.UUID().String()
	userDoc := databases.Document{
		ID: userID,
		Data: map[string]any{
			"email":          email,
			"password_hash":  hash,
			"name":           cmd.Name,
			"status":         cmd.Status,
			"email_verified": false,
			"labels":         []any{},
			"prefs":          map[string]any{},
		},
	}
	if cmd.Labels != nil {
		userDoc.Data["labels"] = cmd.Labels
	}
	if cmd.Prefs != nil {
		userDoc.Data["prefs"] = cmd.Prefs
	}
	if _, err := u.docDB.CreateDocument(ctx, projectID, "default", "users", userDoc, userDocumentPermissions(userID), databases.SystemPrincipal); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &userDoc, nil
}

func (u *Users) UpdateUser(ctx context.Context, projectID, userID string, updates map[string]any, principal databases.Principal) (*databases.Document, error) {
	// 纵深防御（Round3 H1-3）：UpdateUser 可改 email/status（接管面），
	// 必须由 Server 写主体（console admin 会话 / API key）调用；端用户/匿名
	// 即使绕过拦截器也不得以 SystemPrincipal 改他人资料。owner/admin 角色
	// 细粒度由拦截器 adminRoleMethodRules 把关。
	if err := appshared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if raw, ok := updates["status"]; ok {
		s, ok := raw.(string)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "status must be a string")
		}
		if err := users.ValidateStatus(s); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	if raw, ok := updates["email"]; ok {
		email, ok := raw.(string)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "email must be a string")
		}
		email = normalizeEmail(email)
		if email == "" {
			return nil, status.Error(codes.InvalidArgument, "email must not be empty")
		}
		// 改邮箱查重（排除自身 userID）：users_email_unique 唯一索引兜底并发冲突。
		existing, err := u.docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
			Queries:  []string{query.BuildEqual("email", email), "limit(1)"},
			PageSize: 1,
		}, databases.SystemPrincipal)
		if err != nil {
			return nil, err
		}
		for _, dup := range existing.Documents {
			if dup.ID != userID {
				return nil, status.Error(codes.AlreadyExists, "email already registered")
			}
		}
		updates["email"] = email
		// 与 UpdateAccount 一致：改邮箱必须重新验证。
		updates["email_verified"] = false
	}
	filtered := make(map[string]any, len(updates))
	for k, v := range updates {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if _, blocked := userUpdateProtectedFields[k]; blocked {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied (password_hash is managed via the dedicated password endpoint)")
	}
	// 用例层即权限层：keys 角色已由拦截器 scope 把关，docDB 调用统一走 SystemPrincipal，
	// 避免非 System 主体触发系统集合写保护（安全评审 C1 方案 (a)）。
	doc := databases.Document{ID: userID, Data: filtered}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(doc, nil), databases.SystemPrincipal)
	if err != nil {
		// 并发下唯一索引冲突（ErrDuplicateKey/23505）映射为 AlreadyExists。
		return nil, fmt.Errorf("update user: %w", appshared.MapDocumentDBError(err))
	}
	return &updated, nil
}

// UpdateUserPassword 服务端直接重置密码，并撤销该用户全部会话（与客户端
// 改密后清会话语义一致），避免旧令牌继续有效。
func (u *Users) UpdateUserPassword(ctx context.Context, projectID, userID, newPassword string) (*databases.Document, error) {
	if err := appshared.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if err := users.ValidatePasswordStrength(newPassword); err != nil {
		return nil, err
	}
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	hash, err := password.Hash(newPassword)
	if err != nil {
		return nil, err
	}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userID,
		Data: map[string]any{"password_hash": hash},
	}, nil), databases.SystemPrincipal)
	if err != nil {
		return nil, fmt.Errorf("update user password: %w", err)
	}
	if err := u.sessions.DeleteSessionsByUser(ctx, projectID, userID); err != nil {
		return nil, fmt.Errorf("revoke sessions after password reset: %w", err)
	}
	return &updated, nil
}

// ListUserSessions 列出指定用户的全部会话。
func (u *Users) ListUserSessions(ctx context.Context, projectID, userID string) ([]databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "sessions", databases.Query{
		Queries:  []string{query.BuildEqual("user_id", userID)},
		PageSize: 100,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	return list.Documents, nil
}

func (u *Users) DeleteUserSession(ctx context.Context, projectID, userID, sessionID string) error {
	// 纵深防御（G2-2）：管理员会话 / API key 主体才允许以 SystemPrincipal
	// 删除会话文档；owner/admin 角色细粒度由拦截器 adminRoleMethodRules 把关。
	if err := appshared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return err
	}
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "sessions", sessionID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	if doc == nil {
		return status.Error(codes.NotFound, "session not found")
	}
	if uid, _ := doc.Data["user_id"].(string); uid != userID {
		return status.Error(codes.NotFound, "session not found")
	}
	return u.docDB.DeleteDocument(ctx, projectID, "default", "sessions", sessionID, databases.DeleteOptions{}, databases.SystemPrincipal)
}

// CreateUserToken 模拟登录：以指定用户身份创建会话并签发 token（调试/客服场景）。
// 注意：签发的 token 生命周期为默认会话 TTL（7 天），仅供调试使用，
// 不应作为长期凭证用于生产路径。
func (u *Users) CreateUserToken(ctx context.Context, projectID, userID string) (*domainauth.TokenBundle, error) {
	if err := appshared.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	if !users.CanAuthenticate(stringValue(doc.Data["status"])) {
		return nil, status.Error(codes.FailedPrecondition, "user account is not active")
	}
	email, _ := doc.Data["email"].(string)
	bundle, _, err := u.sessions.CreateSessionAndTokens(ctx, projectID, userID, email, "server_token")
	if err != nil {
		return nil, err
	}
	// 审计标记：记录谁为哪个用户模拟签发了 token（session provider 为 server_token）。
	if p, ok := contexts.Principal(ctx); ok {
		slog.Info("create user token",
			"project", projectID,
			"user", userID,
			"caller_actor", p.ActorID,
			"caller_kind", p.ActorKind,
			"caller_credential", p.CredentialType,
		)
	}
	return bundle, nil
}

func (u *Users) DeleteUser(ctx context.Context, projectID, userID string, principal databases.Principal) error {
	if err := appshared.RequirePlatformAdmin(ctx); err != nil {
		return err
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return err
	}
	// M10：删除 users 文档前级联清理 sessions/identities/memberships，
	// 避免 identity 残留阻塞同 provider 重新注册、memberships 残留遗留孤儿用户组角色。
	// 级联与主文档删除包在同一事务：中途失败整体回滚，不残留半删除状态
	// （docDB 文档操作经 conn(ctx) 感知外层事务）。
	err := u.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := u.deleteUserCascade(txCtx, projectID, userID); err != nil {
			return err
		}
		return u.docDB.DeleteDocument(txCtx, projectID, "default", "users", userID, databases.DeleteOptions{}, databases.SystemPrincipal)
	})
	if err != nil {
		return err
	}
	return nil
}

// deleteUserCascade 以 SystemPrincipal 清理用户的 sessions/identities/memberships，
// 并按 group_id 聚合递减 groups 文档 total（与 Groups.adjustGroupTotal 语义一致，
// 内联实现避免跨用例结构体耦合）。
func (u *Users) deleteUserCascade(ctx context.Context, projectID, userID string) error {
	for _, coll := range []string{"sessions", "identities"} {
		docs, err := cascadeListAll(ctx, u.docDB, projectID, coll, []string{query.BuildEqual("user_id", userID)})
		if err != nil {
			return fmt.Errorf("list %s for user: %w", coll, err)
		}
		for _, doc := range docs {
			if err := u.docDB.DeleteDocument(ctx, projectID, "default", coll, doc.ID, databases.DeleteOptions{}, databases.SystemPrincipal); err != nil {
				return fmt.Errorf("delete %s: %w", coll, err)
			}
		}
	}

	// memberships：仅 accepted 状态计入用户组 total（与 CreateMembership/DeleteMembership 一致）。
	groupsToAdjust := map[string]struct{}{}
	docs, err := cascadeListAll(ctx, u.docDB, projectID, "memberships", []string{query.BuildEqual("user_id", userID)})
	if err != nil {
		return fmt.Errorf("list memberships for user: %w", err)
	}
	for _, doc := range docs {
		if statusVal, _ := doc.Data["status"].(string); statusVal == groups.StatusAccepted {
			if groupID, _ := doc.Data["group_id"].(string); groupID != "" {
				groupsToAdjust[groupID] = struct{}{}
			}
		}
		if err := u.docDB.DeleteDocument(ctx, projectID, "default", "memberships", doc.ID, databases.DeleteOptions{}, databases.SystemPrincipal); err != nil {
			return fmt.Errorf("delete membership: %w", err)
		}
	}
	for groupID := range groupsToAdjust {
		if err := u.adjustGroupTotal(ctx, projectID, groupID, -1); err != nil {
			return err
		}
	}
	return nil
}

// adjustGroupTotal 递增/递减用户组 total（与 Groups.adjustGroupTotal 逻辑一致，
// 此处针对 Users 内联实现，避免跨用例结构体依赖）。
func (u *Users) adjustGroupTotal(ctx context.Context, projectID, groupID string, delta int) error {
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "groups", groupID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	total := int64(0)
	switch v := doc.Data["total"].(type) {
	case float64:
		total = int64(v)
	case int64:
		total = v
	case int:
		total = int64(v)
	}
	total += int64(delta)
	if total < 0 {
		total = 0
	}
	_, err = u.docDB.UpdateDocument(ctx, projectID, "default", "groups", databases.SimpleDocumentUpdate(databases.Document{
		ID:   groupID,
		Data: map[string]any{"total": total},
	}, nil), databases.SystemPrincipal)
	return err
}

// CreateUserCommand 是 CreateUser 的输入。
type CreateUserCommand struct {
	Email    string
	Password string
	Name     string
	Status   string
	Labels   []any
	Prefs    map[string]any
}

// cascadePageSize 是级联清理的分页大小：ListDocuments 默认页太小（DSL 无显式
// limit 时为 50 条），大账号的会话/成员数据会被截断，必须显式设大并循环拉取。
const cascadePageSize = 1000

// cascadeListAll 以固定页大小循环拉取集合内全部匹配文档（级联删除用），
// 直至 NextPageToken 为空；DSL 附加 limit(1000) 覆盖 ParseMany 的默认 50 注入。
func cascadeListAll(ctx context.Context, docDB databases.DocumentDB, projectID, collectionID string, queries []string) ([]databases.Document, error) {
	var out []databases.Document
	token := ""
	for {
		list, err := docDB.ListDocuments(ctx, projectID, "default", collectionID, databases.Query{
			Queries:   append(append([]string{}, queries...), "limit(1000)"),
			PageSize:  cascadePageSize,
			PageToken: token,
		}, databases.SystemPrincipal)
		if err != nil {
			return nil, err
		}
		out = append(out, list.Documents...)
		if list.NextPageToken == "" {
			return out, nil
		}
		token = list.NextPageToken
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func userDocumentPermissions(userID string) []databases.Permission {
	// users 文档的 keys 只读：UsersService 已改 SystemPrincipal 调 docDB，
	// end-user 自助路径走 user:<id> owner 权限（安全评审 C1 第 3 层）。
	return []databases.Permission{
		{Type: "read", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "delete", Role: "admin"},
	}
}
