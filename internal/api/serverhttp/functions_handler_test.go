package serverhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const functionsTestJWTSecret = "functions-handler-test-secret"

func functionsTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{Jwt: &config.Security_Jwt{Secret: functionsTestJWTSecret}},
	}
}

type functionsAPIKeyRepo struct {
	keys map[string]*projects.APIKey
}

func (r *functionsAPIKeyRepo) CreateAPIKey(context.Context, *projects.APIKey) error { return nil }
func (r *functionsAPIKeyRepo) GetAPIKey(context.Context, string) (*projects.APIKey, error) {
	return nil, nil
}
func (r *functionsAPIKeyRepo) GetAPIKeyBySecretHash(_ context.Context, hash string) (*projects.APIKey, error) {
	return r.keys[hash], nil
}
func (r *functionsAPIKeyRepo) ListAPIKeys(context.Context, string) ([]projects.APIKey, error) {
	return nil, nil
}
func (r *functionsAPIKeyRepo) DeleteAPIKey(context.Context, string) error { return nil }

type functionsAdminRepo struct {
	admins map[string]*projects.Admin
}

func (r *functionsAdminRepo) GetAdmin(_ context.Context, id string) (*projects.Admin, error) {
	return r.admins[id], nil
}
func (r *functionsAdminRepo) GetAdminByEmail(context.Context, string) (*projects.Admin, error) {
	return nil, nil
}
func (r *functionsAdminRepo) ListAdmins(context.Context) ([]projects.Admin, error) { return nil, nil }
func (r *functionsAdminRepo) CreateAdmin(context.Context, *projects.Admin) error   { return nil }
func (r *functionsAdminRepo) UpdateAdmin(context.Context, *projects.Admin) error   { return nil }
func (r *functionsAdminRepo) DeleteAdmin(context.Context, string) error            { return nil }
func (r *functionsAdminRepo) CountAdminsByRole(context.Context, string) (int64, error) {
	return 1, nil
}
func (r *functionsAdminRepo) WithBootstrapLock(_ context.Context, _ int64, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

type functionsAdminProjectRepo struct{}

func (functionsAdminProjectRepo) HasProjectAccess(context.Context, string, string) (bool, error) {
	return true, nil
}
func (functionsAdminProjectRepo) GrantProjectAccess(context.Context, string, string) error {
	return nil
}

type functionsDocDB struct {
	users map[string]map[string]map[string]any
}

func (d *functionsDocDB) GetDocument(_ context.Context, projectID, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	if collectionID == "users" {
		if data, ok := d.users[projectID][docID]; ok {
			return &databases.Document{ID: docID, Data: data}, nil
		}
	}
	return nil, nil
}
func (d *functionsDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *functionsDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *functionsDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *functionsDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *functionsDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *functionsDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *functionsDocDB) ListCollections(context.Context, string, string, databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	return nil, databases.ListMeta{}, nil
}
func (d *functionsDocDB) DeleteCollection(context.Context, string, string, string) error { return nil }
func (d *functionsDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *functionsDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *functionsDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *functionsDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *functionsDocDB) DeleteIndex(context.Context, string, string, string, string) error {
	return nil
}
func (d *functionsDocDB) CreateDocument(context.Context, string, string, string, databases.Document, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *functionsDocDB) UpdateDocument(context.Context, string, string, string, databases.DocumentUpdate, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *functionsDocDB) UpsertDocument(context.Context, string, string, string, databases.Document, []string, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *functionsDocDB) DeleteDocument(context.Context, string, string, string, string, databases.Principal) error {
	return nil
}
func (d *functionsDocDB) ListDocuments(context.Context, string, string, string, databases.Query, databases.Principal) (*databases.DocumentList, error) {
	return nil, nil
}
func (d *functionsDocDB) CountDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *functionsDocDB) SumDocumentField(context.Context, string, string, string, string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *functionsDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *functionsDocDB) BulkDeleteDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *functionsDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }

func functionsHashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func functionsSignToken(t *testing.T, claims jwtparser.Claims) string {
	t.Helper()
	purpose := jwtparser.PurposeEndUserJWT
	if claims.ActorKind == "admin" {
		purpose = jwtparser.PurposeAdminJWT
	}
	token, err := jwtparser.Generate(jwtparser.DeriveKey(functionsTestJWTSecret, purpose), claims)
	require.NoError(t, err)
	return token
}

func newFunctionsHandler(t *testing.T, validator *auth.Validator) *FunctionsHandler {
	t.Helper()
	h, err := NewFunctionsHandler(functionsTestConfig(), validator, nil, nil)
	require.NoError(t, err)
	return h
}

func newFunctionsValidator(docDB *functionsDocDB) *auth.Validator {
	return auth.NewValidator(functionsTestConfig(), &functionsAPIKeyRepo{}, &functionsAdminRepo{}, &functionsAdminProjectRepo{}, nil, docDB, nil)
}

// TestFunctionsHandler_RejectsEndUserDeploymentUpload（F2-3）：端用户 Bearer JWT
// 上传 deployment 代码包必须 403 PermissionDenied。
func TestFunctionsHandler_RejectsEndUserDeploymentUpload(t *testing.T) {
	t.Parallel()

	projectID := "proj-1"
	userID := "user-1"
	docDB := &functionsDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
	}
	validator := newFunctionsValidator(docDB)
	h := newFunctionsHandler(t, validator)

	token := functionsSignToken(t, jwtparser.Claims{
		UserID:    userID,
		ProjectID: projectID,
		ActorKind: "end_user",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	r := httptest.NewRequest(http.MethodPost, "/v1/server/functions/fn-1/deployments/code", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.upload(rec, r, map[string]string{"functionId": "fn-1"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestFunctionsHandler_RejectsEndUserSessionCookie（F2-3）：端用户会话 cookie
// 同样被拒。
func TestFunctionsHandler_RejectsEndUserSessionCookie(t *testing.T) {
	t.Parallel()

	projectID := "proj-1"
	userID := "user-1"
	docDB := &functionsDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
	}
	validator := newFunctionsValidator(docDB)
	h := newFunctionsHandler(t, validator)

	token := functionsSignToken(t, jwtparser.Claims{
		UserID:    userID,
		ProjectID: projectID,
		ActorKind: "end_user",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	r := httptest.NewRequest(http.MethodPost, "/v1/server/functions/fn-1/deployments/code", nil)
	r.AddCookie(&http.Cookie{Name: "TORCHWOOD_session_proj-1", Value: token})
	rec := httptest.NewRecorder()
	h.upload(rec, r, map[string]string{"functionId": "fn-1"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestFunctionsHandler_Authorize_AdminAndAPIKey（F2-3 对照）：admin 会话与带
// functions 写 scope 的 API Key 放行；无 scope 的 API Key 拒绝。
func TestFunctionsHandler_Authorize_AdminAndAPIKey(t *testing.T) {
	t.Parallel()

	admin := &projects.Admin{ID: "admin-1", Email: "admin@torchwood.local", Role: "owner"}
	validatorWithAdmin := auth.NewValidator(functionsTestConfig(), &functionsAPIKeyRepo{}, &functionsAdminRepo{
		admins: map[string]*projects.Admin{admin.ID: admin},
	}, &functionsAdminProjectRepo{}, nil, &functionsDocDB{}, nil)
	hAdmin := newFunctionsHandler(t, validatorWithAdmin)

	adminToken := functionsSignToken(t, jwtparser.Claims{
		UserID:    admin.ID,
		Username:  admin.Email,
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	t.Run("admin session allowed", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/v1/server/functions/fn-1/deployments/code", nil)
		r.Header.Set("Authorization", "Bearer "+adminToken)
		p, err := hAdmin.authorize(r)
		require.NoError(t, err)
		require.Equal(t, "admin", string(p.ActorKind))
	})

	t.Run("api key with functions scope allowed", func(t *testing.T) {
		repo := &functionsAPIKeyRepo{keys: map[string]*projects.APIKey{
			functionsHashSecret("fn-key-ok"): {
				ID:        "key-1",
				ProjectID: "proj-1",
				Scopes:    []string{"functions.write"},
				Enabled:   true,
			},
		}}
		v := auth.NewValidator(functionsTestConfig(), repo, &functionsAdminRepo{}, &functionsAdminProjectRepo{}, nil, &functionsDocDB{}, nil)
		h := newFunctionsHandler(t, v)
		r := httptest.NewRequest(http.MethodPost, "/v1/server/functions/fn-1/deployments/code", nil)
		r.Header.Set("X-Api-Key", "fn-key-ok")
		p, err := h.authorize(r)
		require.NoError(t, err)
		require.Equal(t, "service", string(p.ActorKind))
	})

	t.Run("api key without functions scope denied", func(t *testing.T) {
		repo := &functionsAPIKeyRepo{keys: map[string]*projects.APIKey{
			functionsHashSecret("fn-key-bad"): {
				ID:        "key-2",
				ProjectID: "proj-1",
				Scopes:    []string{"users"},
				Enabled:   true,
			},
		}}
		v := auth.NewValidator(functionsTestConfig(), repo, &functionsAdminRepo{}, &functionsAdminProjectRepo{}, nil, &functionsDocDB{}, nil)
		h := newFunctionsHandler(t, v)
		r := httptest.NewRequest(http.MethodPost, "/v1/server/functions/fn-1/deployments/code", nil)
		r.Header.Set("X-Api-Key", "fn-key-bad")
		_, err := h.authorize(r)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}
