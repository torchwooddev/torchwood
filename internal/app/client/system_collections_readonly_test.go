package client

import (
	"context"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestClientDatabases_SystemCollectionReadPolicy 覆盖客户端读路径：
// teams/buckets 匿名（read:any）放行；users/sessions/identities 全拒。
func TestClientDatabases_SystemCollectionReadPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	serverUC := appserver.NewDatabases(projectRepo, docDB)

	// teams/buckets 经 SystemPrincipal 造数，集合级 read:any 对匿名访客放行。
	for _, coll := range []string{"teams", "buckets"} {
		_, err := docDB.CreateDocument(ctx, projectID, "default", coll, databases.Document{
			ID:   coll + "-1",
			Data: map[string]any{"name": coll + " one"},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	clientUC := NewDatabases(projectRepo, docDB)
	for _, coll := range []string{"teams", "buckets"} {
		list, total, _, err := clientUC.ListDocuments(ctx, projectID, "default", coll, databases.Query{})
		require.NoError(t, err, "anonymous list %s should be allowed", coll)
		require.Equal(t, int64(1), total)
		require.Len(t, list, 1)
		require.Equal(t, coll+" one", list[0].Data["name"])

		got, err := clientUC.GetDocument(ctx, projectID, "default", coll, coll+"-1")
		require.NoError(t, err, "anonymous get %s should be allowed", coll)
		require.NotNil(t, got)

		count, err := clientUC.CountDocuments(ctx, projectID, "default", coll, nil)
		require.NoError(t, err, "anonymous count %s should be allowed", coll)
		require.Equal(t, int64(1), count)
	}

	// users/sessions/identities 读全拒（匿名与认证用户均拒绝）。
	_, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		ID:   "u-1",
		Data: map[string]any{"email": "a@b.c"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	for _, coll := range []string{"users", "sessions", "identities"} {
		_, _, _, err = clientUC.ListDocuments(ctx, projectID, "default", coll, databases.Query{})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "anonymous list %s should be denied", coll)
	}

	// 认证用户读 users 同样拒绝。
	account := NewTestAccount(testConfig(), projectRepo, docDB)
	user, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "syscoll@torchwood.local",
		Password:  "User@123456",
		Name:      "SysColl",
	})
	require.NoError(t, err)

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	_, err = clientUC.GetDocument(userCtx, projectID, "default", "users", "u-1")
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// 用户集合行为不变：认证用户在自定义集合正常读写。
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))
	created, err := clientUC.CreateDocument(userCtx, "app", "notes", "", map[string]any{"title": "Note"}, nil)
	require.NoError(t, err)
	got, err := clientUC.GetDocument(userCtx, projectID, "app", "notes", created.ID)
	require.NoError(t, err)
	require.Equal(t, "Note", got.Data["title"])
}

// TestClientDatabases_SystemCollectionWriteDenied 覆盖客户端写路径全拒，
// 并验证自定义库中 id=users 集合不受名单限制。
func TestClientDatabases_SystemCollectionWriteDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	serverUC := appserver.NewDatabases(projectRepo, docDB)

	account := NewTestAccount(testConfig(), projectRepo, docDB)
	user, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "syscoll-write@torchwood.local",
		Password:  "User@123456",
		Name:      "SysColl Write",
	})
	require.NoError(t, err)

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	clientUC := NewDatabases(projectRepo, docDB)

	// 写路径：全部系统集合拒绝。
	for _, coll := range []string{"users", "sessions", "identities", "teams", "memberships", "buckets", "files"} {
		_, err := clientUC.CreateDocument(userCtx, "default", coll, "", map[string]any{"name": "x"}, nil)
		require.Equal(t, codes.PermissionDenied, status.Code(err), "create into %s should be denied", coll)

		_, err = clientUC.UpdateDocument(userCtx, "default", coll, "doc-1", map[string]any{"name": "x"}, nil, nil)
		require.Equal(t, codes.PermissionDenied, status.Code(err), "update %s should be denied", coll)

		err = clientUC.DeleteDocument(userCtx, "default", coll, "doc-1")
		require.Equal(t, codes.PermissionDenied, status.Code(err), "delete %s should be denied", coll)
	}

	// 自定义库中 id=users 集合不受名单限制（黑名单限定 default 库）：
	// schema 创建、文档读与文档写均正常（adapter 纵深防御同样限定 default 库）。
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "users", "Custom Users", []databases.Attribute{
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	// 自定义库 users 集合的文档写正常（集合级 create:users 放行）。
	created, err := clientUC.CreateDocument(userCtx, "app", "users", "", map[string]any{"name": "custom write"}, nil)
	require.NoError(t, err)
	require.Equal(t, "custom write", created.Data["name"])

	// 自定义库 users 集合的文档读正常。
	got, err := clientUC.GetDocument(userCtx, projectID, "app", "users", created.ID)
	require.NoError(t, err)
	require.Equal(t, "custom write", got.Data["name"])
}
