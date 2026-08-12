package clientgrpc

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/messaging"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	inframessaging "github.com/torchwooddev/torchwood/internal/infra/messaging"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// fakeDocDB 是内存版 DocumentDB（仅实现 account 测试路径所需语义：
// users/sessions 集合的 List/Get/Create/Delete/BulkDelete）。
type fakeDocDB struct {
	users    map[string]map[string]map[string]any // projectID -> userID -> data
	sessions map[string]map[string]map[string]any // projectID -> sessionID -> data
}

func (d *fakeDocDB) listByQuery(col string, projectID string, q databases.Query) []databases.Document {
	parsed, err := query.ParseMany(q.Queries)
	if err != nil {
		return nil
	}
	src := d.users
	if col == "sessions" {
		src = d.sessions
	}
	var docs []databases.Document
	for id, data := range src[projectID] {
		match := true
		for _, f := range parsed.Filters {
			if f.Op == "equal" && len(f.Values) > 0 && data[f.Attribute] != f.Values[0] {
				match = false
			}
		}
		if match {
			docs = append(docs, databases.Document{ID: id, Data: data})
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	if parsed.Limit > 0 && len(docs) > parsed.Limit {
		docs = docs[:parsed.Limit]
	}
	return docs
}

func (d *fakeDocDB) CreateDocument(_ context.Context, projectID, _, collectionID string, doc databases.Document, _ []databases.Permission, _ databases.Principal) (databases.Document, error) {
	if d.users == nil {
		d.users = map[string]map[string]map[string]any{}
	}
	if d.sessions == nil {
		d.sessions = map[string]map[string]map[string]any{}
	}
	switch collectionID {
	case "users":
		if d.users[projectID] == nil {
			d.users[projectID] = map[string]map[string]any{}
		}
		d.users[projectID][doc.ID] = doc.Data
	case "sessions":
		if d.sessions[projectID] == nil {
			d.sessions[projectID] = map[string]map[string]any{}
		}
		d.sessions[projectID][doc.ID] = doc.Data
	}
	return doc, nil
}

func (d *fakeDocDB) GetDocument(_ context.Context, projectID, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	src := d.users
	if collectionID == "sessions" {
		src = d.sessions
	}
	if src == nil || src[projectID] == nil {
		return nil, nil
	}
	if data, ok := src[projectID][docID]; ok {
		return &databases.Document{ID: docID, Data: data}, nil
	}
	return nil, nil
}

func (d *fakeDocDB) DeleteDocument(_ context.Context, projectID, _, collectionID, docID string, _ databases.Principal) error {
	src := d.users
	if collectionID == "sessions" {
		src = d.sessions
	}
	if src != nil && src[projectID] != nil {
		delete(src[projectID], docID)
	}
	return nil
}

func (d *fakeDocDB) ListDocuments(_ context.Context, projectID, _, collectionID string, q databases.Query, _ databases.Principal) (*databases.DocumentList, error) {
	return &databases.DocumentList{Documents: d.listByQuery(collectionID, projectID, q)}, nil
}

func (d *fakeDocDB) BulkDeleteDocuments(_ context.Context, projectID, _, collectionID string, ids []string, _ databases.Principal) (int64, error) {
	src := d.users
	if collectionID == "sessions" {
		src = d.sessions
	}
	var n int64
	if src != nil && src[projectID] != nil {
		for _, id := range ids {
			if _, ok := src[projectID][id]; ok {
				delete(src[projectID], id)
				n++
			}
		}
	}
	return n, nil
}

func (d *fakeDocDB) UpdateDocument(_ context.Context, projectID, _, collectionID string, update databases.DocumentUpdate, _ databases.Principal) (databases.Document, error) {
	src := d.users
	if collectionID == "sessions" {
		src = d.sessions
	}
	if src == nil || src[projectID] == nil || src[projectID][update.Document.ID] == nil {
		return databases.Document{}, nil
	}
	for k, v := range update.Document.Data {
		src[projectID][update.Document.ID][k] = v
	}
	return databases.Document{ID: update.Document.ID, Data: src[projectID][update.Document.ID]}, nil
}

func (d *fakeDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *fakeDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *fakeDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *fakeDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *fakeDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *fakeDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *fakeDocDB) ListCollections(context.Context, string, string, databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	return nil, databases.ListMeta{}, nil
}
func (d *fakeDocDB) DeleteCollection(context.Context, string, string, string) error { return nil }
func (d *fakeDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *fakeDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *fakeDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *fakeDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *fakeDocDB) DeleteIndex(context.Context, string, string, string, string) error { return nil }
func (d *fakeDocDB) UpsertDocument(context.Context, string, string, string, databases.Document, []string, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *fakeDocDB) CountDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *fakeDocDB) SumDocumentField(context.Context, string, string, string, string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *fakeDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *fakeDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }

// fakeProjectRepo 返回单个固定 project。
type fakeProjectRepo struct {
	project *projects.Project
}

func (r *fakeProjectRepo) GetProject(_ context.Context, id string) (*projects.Project, error) {
	if r.project == nil || r.project.ID != id {
		return nil, nil
	}
	return r.project, nil
}
func (r *fakeProjectRepo) GetProjectByName(context.Context, string) (*projects.Project, error) {
	return nil, nil
}
func (r *fakeProjectRepo) ListProjects(context.Context) ([]projects.Project, error) { return nil, nil }
func (r *fakeProjectRepo) CreateProject(context.Context, *projects.Project) error   { return nil }
func (r *fakeProjectRepo) UpdateProject(context.Context, *projects.Project) error   { return nil }
func (r *fakeProjectRepo) DeleteProject(context.Context, string) error              { return nil }

// fakeOAuthProviderRepo 空实现（SignUp/CreateMagicURLSession 不依赖）。
type fakeOAuthProviderRepo struct{}

func (fakeOAuthProviderRepo) GetOAuthProvider(context.Context, string, string) (*projects.OAuthProvider, error) {
	return nil, nil
}
func (fakeOAuthProviderRepo) ListOAuthProviders(context.Context, string) ([]projects.OAuthProvider, error) {
	return nil, nil
}
func (fakeOAuthProviderRepo) UpsertOAuthProvider(context.Context, *projects.OAuthProvider) error {
	return nil
}
func (fakeOAuthProviderRepo) DeleteOAuthProvider(context.Context, string, string) error { return nil }

// clientgrpcCaptureMailer 记录发送的邮件（含收件人）。
type clientgrpcCaptureMailer struct {
	to   []string
	body []string
}

func (m *clientgrpcCaptureMailer) Send(_ context.Context, to, _, body string) error {
	m.to = append(m.to, to)
	m.body = append(m.body, body)
	return nil
}

var _ messaging.Mailer = (*clientgrpcCaptureMailer)(nil)

type stubRoleResolver struct{}

func (stubRoleResolver) LoadUserRoles(_ context.Context, _, userID string) ([]string, error) {
	return []string{"users", "user:" + userID}, nil
}

// setupClientGRPC 组装 handler 测试环境（纯内存 + miniredis，无需数据库）。
func setupClientGRPC(t *testing.T) (context.Context, *AccountService, *fakeDocDB, *clientgrpcCaptureMailer, string) {
	t.Helper()
	ctx := context.Background()
	cfg := &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "clientgrpc-test-secret"},
		},
	}

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	docDB := &fakeDocDB{}
	projectID := "proj-grpc-1"
	projectRepo := &fakeProjectRepo{project: &projects.Project{
		ID: projectID, InternalID: 1, Name: "Test Project", Status: "active",
	}}

	roles := stubRoleResolver{}
	rotation := auth.NewRedisRefreshRotationStore(rdb)
	sessions := auth.NewSessionService(cfg, docDB, roles, rotation)
	mailer := &clientgrpcCaptureMailer{}
	account := client.NewAccount(
		cfg,
		projectRepo,
		fakeOAuthProviderRepo{},
		docDB,
		sessions,
		auth.NewRedisOTPChallengeStore(rdb, cfg),
		auth.NewRedisOAuthStateStore(rdb),
		auth.NewRedisAccountTokenStore(rdb),
		nil, // loginThrottle
		rotation,
		nil, // idGen
		mailer,
		inframessaging.NewSMSService(cfg),
		auth.NewRedisRateLimiter(rdb),
		roles,
		auth.NewTOTPService(cfg, rdb),
		auth.NewRedisMFAChallengeStore(rdb),
		auth.NewRedisOneTimeTokenStore(rdb),
		nil, // auditRepo
	)
	return ctx, NewAccountService(account), docDB, mailer, projectID
}

func signUpViaHandler(t *testing.T, ctx context.Context, s *AccountService, projectID, email string) string {
	t.Helper()
	resp, err := s.SignUp(ctx, &clientv1.SignUpRequest{
		ProjectId: projectID,
		Email:     email,
		Password:  "User@123",
		Name:      "GRPC User",
	})
	require.NoError(t, err)
	return resp.Account.GetId()
}

func principalCtx(ctx context.Context, projectID, userID, sessionID string) context.Context {
	return contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    userID,
		SessionID: sessionID,
		Email:     "user@torchwood.local",
		Roles:     []string{"users", "user:" + userID},
	})
}

// TestAccountService_CreateMagicURLSession_ResponseContainsOnlyChallengeID
// （R04-P1-1）：响应只含 challengeId（+expireAt），secret 仅存在于邮件中，
// 不得出现在响应里。
func TestAccountService_CreateMagicURLSession_ResponseContainsOnlyChallengeID(t *testing.T) {
	ctx, s, _, mailer, projectID := setupClientGRPC(t)
	userID := signUpViaHandler(t, ctx, s, projectID, "magic@example.com")

	authCtx := principalCtx(ctx, projectID, userID, "session-1")
	resp, err := s.CreateMagicURLSession(authCtx, &clientv1.CreateMagicURLSessionRequest{
		ProjectId: projectID,
		Email:     "magic@example.com",
		Url:       "http://localhost/verify",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetChallengeId())
	require.NotNil(t, resp.GetExpireAt())

	// secret 只出现在邮件 body（userId=...&secret=...），响应 JSON 不得包含。
	re := regexp.MustCompile(`secret=([a-f0-9]+)`)
	require.Len(t, mailer.body, 1)
	matches := re.FindStringSubmatch(mailer.body[0])
	require.Len(t, matches, 2)
	raw, err := protojson.Marshal(resp)
	require.NoError(t, err)
	require.NotContains(t, string(raw), matches[1], "响应不得泄露一次性 secret")
}

// TestAccountService_DeleteSessions_KeepCurrentPassthrough（R04-P1-1）：
// keep_current 透传给 use-case——true 保留当前会话，false 全删。
func TestAccountService_DeleteSessions_KeepCurrentPassthrough(t *testing.T) {
	ctx, s, docDB, _, projectID := setupClientGRPC(t)
	userID := signUpViaHandler(t, ctx, s, projectID, "sessions@example.com")

	// SignUp 产生一个真实会话文档，取其 ID 作为"当前会话"。
	currentSessionID := ""
	for id := range docDB.sessions[projectID] {
		currentSessionID = id
	}
	require.NotEmpty(t, currentSessionID)
	authCtx := principalCtx(ctx, projectID, userID, currentSessionID)

	// 额外插入一个"其他会话"。
	_, err := docDB.CreateDocument(ctx, projectID, "default", "sessions", databases.Document{
		ID:   "session-2",
		Data: map[string]any{"user_id": userID, "expire_at": "2099-01-01T00:00:00Z"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	// keep_current=true：当前会话保留，其他会话删除。
	_, err = s.DeleteSessions(authCtx, &clientv1.DeleteSessionsRequest{KeepCurrent: true})
	require.NoError(t, err)
	require.NotNil(t, docDB.sessions[projectID][currentSessionID])
	require.Nil(t, docDB.sessions[projectID]["session-2"])

	// keep_current=false：全部删除。
	_, err = s.DeleteSessions(authCtx, &clientv1.DeleteSessionsRequest{KeepCurrent: false})
	require.NoError(t, err)
	require.Nil(t, docDB.sessions[projectID][currentSessionID])
}

// TestAccountService_ErrorCodeMapping（R04-P1-1）：handler 错误码映射。
func TestAccountService_ErrorCodeMapping(t *testing.T) {
	ctx, s, _, _, projectID := setupClientGRPC(t)
	userID := signUpViaHandler(t, ctx, s, projectID, "errors@torchwood.local")
	authCtx := principalCtx(ctx, projectID, userID, "session-1")

	// DeleteSession 空 session_id → InvalidArgument（handler 层，R04-P3-2）。
	_, err := s.DeleteSession(authCtx, &clientv1.DeleteSessionRequest{SessionId: ""})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 不存在的会话 → NotFound（use-case 透传）。
	_, err = s.DeleteSession(authCtx, &clientv1.DeleteSessionRequest{SessionId: "no-such-session"})
	require.Equal(t, codes.NotFound, status.Code(err))

	// 不存在的 project → NotFound。
	_, err = s.CreateMagicURLSession(authCtx, &clientv1.CreateMagicURLSessionRequest{
		ProjectId: "no-such-project",
		Email:     "errors@torchwood.local",
		Url:       "http://localhost/verify",
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	// 非法 URL → InvalidArgument（不依赖 user 存在，独立验证）。
	_, err = s.CreateMagicURLSession(authCtx, &clientv1.CreateMagicURLSessionRequest{
		ProjectId: projectID,
		Email:     "errors@torchwood.local",
		Url:       "javascript:alert(1)",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestAccountService_SignUp_DuplicateEmail（R04-P1-1）：重复邮箱注册 →
// AlreadyExists。
func TestAccountService_SignUp_DuplicateEmail(t *testing.T) {
	ctx, s, _, _, projectID := setupClientGRPC(t)
	signUpViaHandler(t, ctx, s, projectID, "errors@torchwood.local")

	_, err := s.SignUp(ctx, &clientv1.SignUpRequest{
		ProjectId: projectID,
		Email:     "errors@torchwood.local",
		Password:  "User@123",
	})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

// TestAccountService_SignOut_WithoutPrincipalIsIdempotent（R04-P2-4）：
// SignOut 不再重复 Principal 校验——无 principal 时幂等成功。
func TestAccountService_SignOut_WithoutPrincipalIsIdempotent(t *testing.T) {
	ctx, s, _, _, _ := setupClientGRPC(t)
	_, err := s.SignOut(ctx, &clientv1.SignOutRequest{})
	require.NoError(t, err)
}
