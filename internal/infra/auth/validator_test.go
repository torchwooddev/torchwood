package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testJWTSecret = "validator-test-secret"

func testValidatorConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: testJWTSecret},
		},
	}
}

type memAdminRevokeStore struct {
	revoked map[string]time.Time
}

func newMemAdminRevokeStore() *memAdminRevokeStore {
	return &memAdminRevokeStore{revoked: map[string]time.Time{}}
}

func (s *memAdminRevokeStore) RevokeBefore(_ context.Context, adminID string, revokedAt time.Time, _ time.Duration) error {
	if existing, ok := s.revoked[adminID]; !ok || revokedAt.After(existing) {
		s.revoked[adminID] = revokedAt
	}
	return nil
}

func (s *memAdminRevokeStore) RevokedBefore(_ context.Context, adminID string) (time.Time, error) {
	return s.revoked[adminID], nil
}

var _ domainauth.AdminTokenRevokeStore = (*memAdminRevokeStore)(nil)

type stubAPIKeyRepo struct {
	keys map[string]*projects.APIKey
}

func (r *stubAPIKeyRepo) CreateAPIKey(context.Context, *projects.APIKey) error { return nil }
func (r *stubAPIKeyRepo) GetAPIKey(context.Context, string) (*projects.APIKey, error) {
	return nil, nil
}
func (r *stubAPIKeyRepo) GetAPIKeyBySecretHash(_ context.Context, hash string) (*projects.APIKey, error) {
	return r.keys[hash], nil
}
func (r *stubAPIKeyRepo) ListAPIKeys(context.Context, string) ([]projects.APIKey, error) {
	return nil, nil
}
func (r *stubAPIKeyRepo) DeleteAPIKey(context.Context, string) error { return nil }

type stubAdminRepo struct {
	admins map[string]*projects.ConsoleAdmin
}

func (r *stubAdminRepo) GetConsoleAdmin(_ context.Context, id string) (*projects.ConsoleAdmin, error) {
	return r.admins[id], nil
}
func (r *stubAdminRepo) GetConsoleAdminByEmail(context.Context, string) (*projects.ConsoleAdmin, error) {
	return nil, nil
}

type stubAdminProjectRepo struct {
	access map[string]map[string]struct{}
}

func (r *stubAdminProjectRepo) HasProjectAccess(_ context.Context, adminID, projectID string) (bool, error) {
	if projects, ok := r.access[adminID]; ok {
		_, ok := projects[projectID]
		return ok, nil
	}
	return false, nil
}
func (r *stubAdminProjectRepo) GrantProjectAccess(context.Context, string, string) error { return nil }

type stubDocDB struct {
	users    map[string]map[string]map[string]any
	sessions map[string]map[string]map[string]any
}

func (d *stubDocDB) GetDocument(_ context.Context, projectID, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	switch collectionID {
	case "users":
		if data, ok := d.users[projectID][docID]; ok {
			return &databases.Document{ID: docID, Data: data}, nil
		}
	case "sessions":
		if data, ok := d.sessions[projectID][docID]; ok {
			return &databases.Document{ID: docID, Data: data}, nil
		}
	}
	return nil, nil
}

func (d *stubDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *stubDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *stubDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *stubDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *stubDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *stubDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *stubDocDB) ListCollections(context.Context, string, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *stubDocDB) DeleteCollection(context.Context, string, string, string) error { return nil }
func (d *stubDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *stubDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *stubDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *stubDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *stubDocDB) DeleteIndex(context.Context, string, string, string, string) error { return nil }
func (d *stubDocDB) CreateDocument(context.Context, string, string, string, databases.Document, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *stubDocDB) UpdateDocument(context.Context, string, string, string, databases.DocumentUpdate, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *stubDocDB) DeleteDocument(context.Context, string, string, string, string, databases.Principal) error {
	return nil
}
func (d *stubDocDB) ListDocuments(context.Context, string, string, string, databases.Query, databases.Principal) (*databases.DocumentList, error) {
	return nil, nil
}
func (d *stubDocDB) CountDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *stubDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *stubDocDB) BulkDeleteDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *stubDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func signToken(t *testing.T, claims jwtparser.Claims) string {
	t.Helper()
	purpose := jwtparser.PurposeEndUserJWT
	if claims.ActorKind == "admin" {
		purpose = jwtparser.PurposeAdminJWT
	}
	token, err := jwtparser.Generate(jwtparser.DeriveKey(testJWTSecret, purpose), claims)
	require.NoError(t, err)
	return token
}

func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, want, st.Code())
}

func TestValidator_ValidateAPIKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	secret := "torchwood-test-api-key"
	key := &projects.APIKey{
		ID:        "key-1",
		ProjectID: "proj-1",
		Scopes:    []string{"storage", "users"},
		Enabled:   true,
	}
	repo := &stubAPIKeyRepo{keys: map[string]*projects.APIKey{hashSecret(secret): key}}
	v := auth.NewValidator(testValidatorConfig(), repo, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, &stubDocDB{}, nil)

	p, err := v.ValidateCredential(ctx, secret, shared.CredentialTypeAPIKey)
	require.NoError(t, err)
	require.Equal(t, shared.ActorKindService, p.ActorKind)
	require.Equal(t, "proj-1", p.ProjectID)
	require.Equal(t, []string{"storage", "users"}, p.Permissions)

	key.Enabled = false
	_, err = v.ValidateCredential(ctx, secret, shared.CredentialTypeAPIKey)
	requireCode(t, err, codes.Unauthenticated)
}

func TestValidator_ValidateAdminJWT(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	admin := &projects.ConsoleAdmin{
		ID:    "admin-1",
		Email: "admin@torchwood.local",
		Role:  "member",
	}
	admins := &stubAdminRepo{admins: map[string]*projects.ConsoleAdmin{admin.ID: admin}}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, admins, &stubAdminProjectRepo{}, nil, &stubDocDB{}, nil)

	token := signToken(t, jwtparser.Claims{
		UserID:    admin.ID,
		Username:  admin.Email,
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	p, err := v.ValidateToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, shared.ActorKindAdmin, p.ActorKind)
	require.False(t, p.IsPlatformAdmin)
}

func TestValidator_ValidateAdminJWT_RejectsRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	admin := &projects.ConsoleAdmin{
		ID:    "admin-1",
		Email: "admin@torchwood.local",
		Role:  "owner",
	}
	admins := &stubAdminRepo{admins: map[string]*projects.ConsoleAdmin{admin.ID: admin}}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, admins, &stubAdminProjectRepo{}, nil, &stubDocDB{}, nil)

	token := signToken(t, jwtparser.Claims{
		UserID:    admin.ID,
		Username:  admin.Email,
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeRefresh,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	_, err := v.ValidateToken(ctx, token)
	requireCode(t, err, codes.Unauthenticated)
}

func TestValidator_ValidateAdminJWT_Revoked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issuedAt := time.Now().Add(-time.Hour).Unix()
	admin := &projects.ConsoleAdmin{ID: "admin-1", Email: "admin@torchwood.local", Role: "owner"}
	revokeStore := newMemAdminRevokeStore()
	require.NoError(t, revokeStore.RevokeBefore(ctx, admin.ID, time.Now(), time.Hour))

	v := auth.NewValidator(
		testValidatorConfig(),
		&stubAPIKeyRepo{},
		&stubAdminRepo{admins: map[string]*projects.ConsoleAdmin{admin.ID: admin}},
		&stubAdminProjectRepo{},
		revokeStore,
		&stubDocDB{},
		nil,
	)
	token := signToken(t, jwtparser.Claims{
		UserID:    admin.ID,
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  issuedAt,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	_, err := v.ValidateToken(ctx, token)
	requireCode(t, err, codes.Unauthenticated)
}

func TestValidator_ValidateEndUserJWT(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectID := idgen.UUID().String()
	userID := idgen.UUID().String()
	docDB := &stubDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
	}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, docDB, nil)

	token := signToken(t, jwtparser.Claims{
		UserID:    userID,
		ProjectID: projectID,
		ActorKind: "end_user",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	p, err := v.ValidateToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, shared.ActorKindEndUser, p.ActorKind)
	require.True(t, p.HasRole("users"))
}

func TestValidator_ValidateEndUserJWT_RejectsRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	token := signToken(t, jwtparser.Claims{
		UserID:    "user-1",
		ProjectID: "proj-1",
		ActorKind: "end_user",
		TokenType: jwtparser.TokenTypeRefresh,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, &stubDocDB{}, nil)
	_, err := v.ValidateToken(ctx, token)
	requireCode(t, err, codes.Unauthenticated)
}

func TestValidator_ValidateAdminProjectAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &stubAdminProjectRepo{access: map[string]map[string]struct{}{
		"admin-1": {"proj-allowed": {}},
	}}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, repo, nil, &stubDocDB{}, nil)

	require.NoError(t, v.ValidateAdminProjectAccess(ctx, &shared.Principal{
		ActorKind:       shared.ActorKindAdmin,
		UserID:          "admin-1",
		ProjectID:       "proj-allowed",
		IsPlatformAdmin: false,
	}))
	requireCode(t, v.ValidateAdminProjectAccess(ctx, &shared.Principal{
		ActorKind: shared.ActorKindAdmin,
		UserID:    "admin-1",
		ProjectID: "proj-denied",
	}), codes.PermissionDenied)
	require.NoError(t, v.ValidateAdminProjectAccess(ctx, &shared.Principal{
		ActorKind:       shared.ActorKindAdmin,
		UserID:          "admin-1",
		ProjectID:       "proj-denied",
		IsPlatformAdmin: true,
	}))
}

func TestValidator_EndUserJWT_CorruptExpireAtFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectID := idgen.UUID().String()
	userID := idgen.UUID().String()
	sessionID := idgen.UUID().String()
	docDB := &stubDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
		sessions: map[string]map[string]map[string]any{
			projectID: {sessionID: {"user_id": userID, "expire_at": "not-a-timestamp"}},
		},
	}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, docDB, nil)

	token := signToken(t, jwtparser.Claims{
		UserID:    userID,
		ProjectID: projectID,
		SessionID: sessionID,
		ActorKind: "end_user",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	_, err := v.ValidateToken(ctx, token)
	requireCode(t, err, codes.Unauthenticated)
}

func signSessionCookie(projectID, sessionID string) string {
	codec := auth.NewSessionCookieCodec(string(jwtparser.DeriveKey(testJWTSecret, jwtparser.PurposeSessionCookie)))
	return codec.Sign(projectID, sessionID)
}

func TestValidator_SessionCookie_Valid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectID := idgen.UUID().String()
	userID := idgen.UUID().String()
	sessionID := idgen.UUID().String()
	docDB := &stubDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
		sessions: map[string]map[string]map[string]any{
			projectID: {sessionID: {
				"user_id":   userID,
				"expire_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano),
			}},
		},
	}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, docDB, nil)

	p, err := v.ValidateCredential(ctx, signSessionCookie(projectID, sessionID), shared.CredentialTypeSession)
	require.NoError(t, err)
	require.Equal(t, shared.ActorKindEndUser, p.ActorKind)
	require.Equal(t, sessionID, p.SessionID)
}

func TestValidator_SessionCookie_CorruptExpireAtFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectID := idgen.UUID().String()
	userID := idgen.UUID().String()
	sessionID := idgen.UUID().String()
	docDB := &stubDocDB{
		users: map[string]map[string]map[string]any{
			projectID: {userID: {"status": "active"}},
		},
		sessions: map[string]map[string]map[string]any{
			// Unsupported value type must be treated as expired, not valid.
			projectID: {sessionID: {"user_id": userID, "expire_at": 12345}},
		},
	}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, &stubAdminRepo{}, &stubAdminProjectRepo{}, nil, docDB, nil)

	_, err := v.ValidateCredential(ctx, signSessionCookie(projectID, sessionID), shared.CredentialTypeSession)
	requireCode(t, err, codes.Unauthenticated)
}

func TestValidator_CrossPurposeTokenRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	admin := &projects.ConsoleAdmin{ID: "admin-1", Email: "admin@torchwood.local", Role: "owner"}
	admins := &stubAdminRepo{admins: map[string]*projects.ConsoleAdmin{admin.ID: admin}}
	v := auth.NewValidator(testValidatorConfig(), &stubAPIKeyRepo{}, admins, &stubAdminProjectRepo{}, nil, &stubDocDB{}, nil)

	// A token signed with the raw master secret must no longer validate.
	rawToken, err := jwtparser.Generate([]byte(testJWTSecret), jwtparser.Claims{
		UserID:    admin.ID,
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	_, err = v.ValidateToken(ctx, rawToken)
	requireCode(t, err, codes.Unauthenticated)
}
