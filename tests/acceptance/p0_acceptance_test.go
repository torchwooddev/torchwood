package acceptance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/app/client"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	infrAuth "github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestP0_Section6_ConsoleAdminProjectAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, projectCleanup := testutil.CreateTestProject(ctx, db)
	defer projectCleanup()

	cfg := &config.AppConfig{}
	docDB := documentdb.NewPostgresDocumentDB(db)
	env, err := testutil.NewInterceptorEnv(db, cfg, docDB)
	require.NoError(t, err)

	owner, ownerCleanup := testutil.CreateTestConsoleAdmin(ctx, db, "owner")
	defer ownerCleanup()
	viewer, viewerCleanup := testutil.CreateTestConsoleAdmin(ctx, db, "viewer")
	defer viewerCleanup()

	ownerToken, err := testutil.SignConsoleAdminToken(cfg, owner)
	require.NoError(t, err)
	viewerToken, err := testutil.SignConsoleAdminToken(cfg, viewer)
	require.NoError(t, err)

	adminMD := func(token string) metadata.MD {
		return metadata.Pairs(
			"authorization", "Bearer "+token,
			"X-Torchwood-Project", projectID,
		)
	}

	// §6.8 owner with X-Torchwood-Project can access Server API.
	err = env.InvokeUnary(ctx, testutil.MethodListUsers, adminMD(ownerToken))
	require.NoError(t, err)

	// §6.7 viewer without console_admin_projects gets PermissionDenied.
	err = env.InvokeUnary(ctx, testutil.MethodListUsers, adminMD(viewerToken))
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())

	// §6.9 viewer granted project access can call Server API.
	require.NoError(t, testutil.GrantConsoleAdminProject(ctx, db, viewer.ID, projectID))
	err = env.InvokeUnary(ctx, testutil.MethodListUsers, adminMD(viewerToken))
	require.NoError(t, err)
}

func TestP0_Section7_AuditLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	defer projectCleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	env, err := testutil.NewInterceptorEnv(db, cfg, docDB)
	require.NoError(t, err)

	apiSecret, keyCleanup := testutil.CreateTestAPIKey(ctx, db, projectID, []string{"users"})
	defer keyCleanup()

	before, err := env.AuditLogCount(ctx)
	require.NoError(t, err)

	// §7.1 authenticated call writes audit_logs row.
	err = env.InvokeUnary(ctx, testutil.MethodListUsers, metadata.Pairs("x-api-key", apiSecret))
	require.NoError(t, err)

	after, err := env.AuditLogCount(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// §7.2 latest row has action/status/actor fields.
	log, err := env.LatestAuditLog(ctx)
	require.NoError(t, err)
	require.Equal(t, testutil.MethodListUsers, log.Action)
	require.Equal(t, "success", log.Status)
	require.NotEmpty(t, log.ActorID)
	require.NotEmpty(t, log.ActorKind)

	// §7.3 admin request with X-Torchwood-Project records project_id.
	owner, ownerCleanup := testutil.CreateTestConsoleAdmin(ctx, db, "owner")
	defer ownerCleanup()
	ownerToken, err := testutil.SignConsoleAdminToken(cfg, owner)
	require.NoError(t, err)
	require.NoError(t, env.InvokeUnary(ctx, testutil.MethodListUsers, metadata.Pairs(
		"authorization", "Bearer "+ownerToken,
		"X-Torchwood-Project", projectID,
	)))

	log, err = env.LatestAuditLog(ctx)
	require.NoError(t, err)
	require.Equal(t, projectID, log.ProjectID)

	// §7.4 public health must succeed; audit is best-effort and must not fail the request.
	err = env.InvokeUnary(ctx, testutil.MethodHealthCheck, metadata.Pairs())
	require.NoError(t, err)
}

func TestP0_Section8_AccessPermission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	defer projectCleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	env, err := testutil.NewInterceptorEnv(db, cfg, docDB)
	require.NoError(t, err)

	projectRepo := bunrepo.NewProjectRepository(db)
	account := client.NewTestAccount(cfg, projectRepo, docDB)
	_, tokens, _, _, err := account.SignUp(ctx, client.SignUpCommand{
		ProjectID: projectID,
		Email:     "access-perm@torchwood.local",
		Password:  "User@123456",
		Name:      "Access Perm",
	})
	require.NoError(t, err)

	userMD := metadata.Pairs("authorization", "Bearer "+tokens.AccessToken)

	// §8.1 Me requires users role ��� end-user token passes auth interceptor.
	err = env.InvokeUnary(ctx, testutil.MethodAccountMe, userMD)
	require.NoError(t, err)

	// §8.2 SignOut requires users role.
	err = env.InvokeUnary(ctx, testutil.MethodAccountSignOut, userMD)
	require.NoError(t, err)

	// §8.3 principal without users role is rejected on permission-gated methods.
	mockValidator := &principalValidator{
		principal: &shared.Principal{
			ActorKind:      shared.ActorKindEndUser,
			CredentialType: shared.CredentialTypeToken,
			ProjectID:      projectID,
			UserID:         "no-users-role",
			Roles:          []string{"guests"},
		},
	}
	authIC, err := interceptor.NewAuthInterceptor(
		mockValidator,
		nil,
		nil,
		map[string][]string{testutil.MethodAccountMe: {"users"}},
	)
	require.NoError(t, err)
	ctx = metadata.NewIncomingContext(ctx, userMD)
	_, err = authIC.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: testutil.MethodAccountMe,
	}, func(ctx context.Context, req any) (any, error) { return nil, nil })
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestP0_Section9_DynamicDocuments(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	defer projectCleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	projectRepo := bunrepo.NewProjectRepository(db)
	account := client.NewTestAccount(cfg, projectRepo, docDB)
	usersUC := appserver.NewUsers(projectRepo, docDB, infrAuth.NewSessionService(cfg, docDB, client.NewUserRoles(docDB), nil))

	const email = "dsl-query@torchwood.local"
	signedUp, _, _, _, err := account.SignUp(ctx, client.SignUpCommand{
		ProjectID: projectID,
		Email:     email,
		Password:  "User@123456",
		Name:      "DSL Query",
	})
	require.NoError(t, err)

	// §9.1 system users collection contains registered user (API key / keys role).
	docs, total, _, err := usersUC.ListUsers(ctx, projectID, databases.Query{}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, int64(1))
	found := false
	for _, doc := range docs {
		if doc.ID == signedUp.ID {
			found = true
			break
		}
	}
	require.True(t, found, "registered user should appear in users list")

	// §9.2 query filter returns only matching user.
	filtered, filteredTotal, _, err := usersUC.ListUsers(ctx, projectID, databases.Query{
		Queries: []string{`equal("email","` + email + `")`},
	}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), filteredTotal)
	require.Len(t, filtered, 1)
	require.Equal(t, email, filtered[0].Data["email"])

	// §9.4 non-admin list returns only documents with matching _perms.
	privateUser, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		Data: map[string]any{
			"email": "private@torchwood.local",
			"name":  "Private",
		},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	aliceList, err := docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + privateUser.ID + `")`},
	}, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Len(t, aliceList.Documents, 1)

	bobList, err := docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + privateUser.ID + `")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, bobList.Documents, 0)
}

type principalValidator struct {
	principal *shared.Principal
}

func (v *principalValidator) ValidateToken(ctx context.Context, token string) (*shared.Principal, error) {
	return v.ValidateCredential(ctx, token, shared.CredentialTypeToken)
}

func (v *principalValidator) ValidateCredential(ctx context.Context, raw string, credentialType shared.CredentialType) (*shared.Principal, error) {
	return v.principal, nil
}

func (v *principalValidator) ValidateAdminProjectAccess(ctx context.Context, principal *shared.Principal) error {
	return nil
}
