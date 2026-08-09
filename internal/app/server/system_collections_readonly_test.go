package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestSystemCollections_IsSystemFlag 覆盖回填断言：EnsureSystemCollections 之后
// 系统集合 is_system=true，用户集合与自定义库同名集合 is_system=false。
func TestSystemCollections_IsSystemFlag(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)

	for _, id := range []string{"users", "sessions", "identities", "teams", "memberships", "buckets", "files"} {
		coll, err := uc.GetCollection(ctx, projectID, "default", id)
		require.NoError(t, err)
		require.NotNil(t, coll)
		require.True(t, coll.IsSystem, "collection %s should be marked is_system", id)
	}

	// 用户集合 is_system=false。
	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", nil, nil, nil, true))
	posts, err := uc.GetCollection(ctx, projectID, "app", "posts")
	require.NoError(t, err)
	require.False(t, posts.IsSystem)

	// 自定义库中 id=users 的集合不受系统名单限制（黑名单限定 default 库）。
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "users", "Custom Users", []databases.Attribute{
		{ID: "name", Key: "name", Type: "string", Size: 256},
		{ID: "password_hash", Key: "password_hash", Type: "string", Size: 512},
	}, nil, nil, true))
	users, err := uc.GetCollection(ctx, projectID, "app", "users")
	require.NoError(t, err)
	require.False(t, users.IsSystem)

	// 自定义库同名集合读取不脱敏（脱敏仅限 default 库系统集合）。
	_, err = docDB.CreateDocument(ctx, projectID, "app", "users", databases.Document{
		ID:   "cu-1",
		Data: map[string]any{"name": "custom", "password_hash": "hash"},
	}, []databases.Permission{{Type: "read", Role: "keys"}}, databases.SystemPrincipal)
	require.NoError(t, err)
	customDoc, err := uc.GetDocument(ctx, projectID, "app", "users", "cu-1", databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, "hash", customDoc.Data["password_hash"])
}

// TestSystemCollections_SchemaOpsDenied 覆盖 schema 级 7 操作全拒。
func TestSystemCollections_SchemaOpsDenied(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	perms := []databases.Permission{{Type: "read", Role: "keys"}}

	assertDenied := func(err error) {
		require.Error(t, err)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}

	assertDenied(uc.CreateCollection(ctx, projectID, "default", "users", "Users", nil, nil, nil, true))
	assertDenied(uc.UpdateCollection(ctx, projectID, "default", "users", databases.CollectionPatch{Name: "renamed"}, databases.Principal{Roles: []string{"keys"}}))
	assertDenied(uc.DeleteCollection(ctx, projectID, "default", "users"))
	assertDenied(uc.CreateAttribute(ctx, projectID, "default", "users", databases.Attribute{ID: "x", Key: "x", Type: "string"}))
	assertDenied(uc.DeleteAttribute(ctx, projectID, "default", "users", "email"))
	assertDenied(uc.CreateIndex(ctx, projectID, "default", "users", databases.Index{ID: "i", Type: "key", Attributes: []string{"email"}}))
	assertDenied(uc.DeleteIndex(ctx, projectID, "default", "users", "users_email_unique"))

	// teams 等其他系统集合同样拒绝（UpdateCollection 的权限补丁）。
	assertDenied(uc.UpdateCollection(ctx, projectID, "default", "teams", databases.CollectionPatch{Permissions: &perms}, databases.Principal{Roles: []string{"keys"}}))

	// 自定义库同名集合不受影响。
	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "users", "Custom Users", nil, nil, nil, true))
	require.NoError(t, uc.DeleteCollection(ctx, projectID, "app", "users"))
}

// TestSystemCollections_DocumentReadPolicy 覆盖文档读策略：
// teams/memberships/buckets/files 对 keys 主体放行；users 读仅 PlatformAdmin
// 允许且脱敏；其余主体拒绝。
func TestSystemCollections_DocumentReadPolicy(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	adminPrincipal := databases.Principal{PlatformAdmin: true}

	// 4 个放行集合：经 SystemPrincipal 造数后 keys 主体可读。
	for _, coll := range []string{"teams", "buckets", "files", "memberships"} {
		data := map[string]any{"name": coll + " one"}
		if coll == "memberships" {
			data = map[string]any{"team_id": "teams-1", "user_id": "u-1", "name": "memberships one"}
		}
		_, err := docDB.CreateDocument(ctx, projectID, "default", coll, databases.Document{
			ID:   coll + "-1",
			Data: data,
		}, []databases.Permission{{Type: "read", Role: "keys"}}, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	for _, coll := range []string{"teams", "buckets", "files", "memberships"} {
		list, total, _, err := uc.ListDocuments(ctx, projectID, "default", coll, databases.Query{}, keysPrincipal)
		require.NoError(t, err, "list %s should be allowed for keys principal", coll)
		require.Equal(t, int64(1), total)
		require.Len(t, list, 1)

		got, err := uc.GetDocument(ctx, projectID, "default", coll, coll+"-1", keysPrincipal)
		require.NoError(t, err, "get %s should be allowed for keys principal", coll)
		require.NotNil(t, got)

		count, err := uc.CountDocuments(ctx, projectID, "default", coll, nil, keysPrincipal)
		require.NoError(t, err, "count %s should be allowed for keys principal", coll)
		require.Equal(t, int64(1), count)
	}

	// users：keys 主体拒绝读。
	var err error
	_, err = docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		ID: "user-1",
		Data: map[string]any{
			"email":          "admin@torchwood.local",
			"name":           "Admin",
			"password_hash":  "secret-hash",
			"phone":          "+8613800000000",
			"phone_verified": true,
			"labels":         []any{"l1"},
			"prefs":          map[string]any{"theme": "dark"},
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	_, _, _, err = uc.ListDocuments(ctx, projectID, "default", "users", databases.Query{}, keysPrincipal)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = uc.GetDocument(ctx, projectID, "default", "users", "user-1", keysPrincipal)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// PlatformAdmin 允许读且脱敏。
	list, _, _, err := uc.ListDocuments(ctx, projectID, "default", "users", databases.Query{}, adminPrincipal)
	require.NoError(t, err)
	require.Len(t, list, 1)
	for _, f := range []string{"password_hash", "phone", "phone_verified", "labels", "prefs"} {
		require.NotContains(t, list[0].Data, f, "field %s should be redacted", f)
	}
	require.Equal(t, "admin@torchwood.local", list[0].Data["email"])

	got, err := uc.GetDocument(ctx, projectID, "default", "users", "user-1", adminPrincipal)
	require.NoError(t, err)
	for _, f := range []string{"password_hash", "phone", "phone_verified", "labels", "prefs"} {
		require.NotContains(t, got.Data, f, "field %s should be redacted", f)
	}

	// sessions / identities 脱敏断言。
	_, err = docDB.CreateDocument(ctx, projectID, "default", "sessions", databases.Document{
		ID: "session-1",
		Data: map[string]any{
			"user_id":     "user-1",
			"secret_hash": "secret-hash",
			"factors":     map[string]any{"totp": true},
			"user_agent":  "curl",
			"ip":          "127.0.0.1",
			"country":     "CN",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, "default", "identities", databases.Document{
		ID: "identity-1",
		Data: map[string]any{
			"user_id":       "user-1",
			"provider":      "google",
			"provider_uid":  "g-1",
			"provider_data": map[string]any{"scope": "openid"},
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	session, err := uc.GetDocument(ctx, projectID, "default", "sessions", "session-1", adminPrincipal)
	require.NoError(t, err)
	for _, f := range []string{"secret_hash", "factors", "user_agent", "ip", "country"} {
		require.NotContains(t, session.Data, f, "field %s should be redacted", f)
	}

	identity, err := uc.GetDocument(ctx, projectID, "default", "identities", "identity-1", adminPrincipal)
	require.NoError(t, err)
	for _, f := range []string{"provider_data", "provider_uid"} {
		require.NotContains(t, identity.Data, f, "field %s should be redacted", f)
	}
	require.Equal(t, "google", identity.Data["provider"])
}

// TestSystemCollections_DocumentWriteDenied 覆盖文档写路径（含 Bulk）全拒。
func TestSystemCollections_DocumentWriteDenied(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	adminPrincipal := databases.Principal{PlatformAdmin: true}

	assertDenied := func(err error) {
		require.Error(t, err)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}

	// users 文档写：keys 与 PlatformAdmin 均拒绝（admin 也是 Databases API 调用方）。
	for _, principal := range []databases.Principal{keysPrincipal, adminPrincipal} {
		_, err := uc.CreateDocument(ctx, projectID, "default", "users", "", map[string]any{"email": "a@b.c"}, nil, principal)
		assertDenied(err)

		_, err = uc.UpdateDocument(ctx, projectID, "default", "users", "user-1", map[string]any{"name": "x"}, nil, nil, principal)
		assertDenied(err)

		err = uc.DeleteDocument(ctx, projectID, "default", "users", "user-1", principal)
		assertDenied(err)

		_, err = uc.BulkUpdateDocuments(ctx, projectID, "default", "users", []string{"user-1"}, map[string]any{"name": "x"}, nil, principal)
		assertDenied(err)

		_, err = uc.BulkDeleteDocuments(ctx, projectID, "default", "users", []string{"user-1"}, principal)
		assertDenied(err)
	}

	// teams 等其他系统集合文档写同样拒绝。
	_, err := uc.CreateDocument(ctx, projectID, "default", "teams", "", map[string]any{"name": "T"}, nil, keysPrincipal)
	assertDenied(err)
}

// TestSystemCollections_UpdateCollectionPermissionValidation 覆盖 UpdateCollection
// 授予未持有角色时 InvalidArgument；持 keys 角色（privileged）可正常授予。
func TestSystemCollections_UpdateCollectionPermissionValidation(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", nil, nil, nil, true))

	// 非 privileged 主体授予未持有角色（写类 + any）→ InvalidArgument。
	userPrincipal := databases.Principal{Roles: []string{"users", "user:u1"}}
	badPerms := []databases.Permission{{Type: "update", Role: "any"}}
	err := uc.UpdateCollection(ctx, projectID, "app", "posts", databases.CollectionPatch{Permissions: &badPerms}, userPrincipal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// keys 角色为 privileged，可授予集合级权限。
	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	goodPerms := []databases.Permission{{Type: "read", Role: "keys"}, {Type: "create", Role: "users"}}
	require.NoError(t, uc.UpdateCollection(ctx, projectID, "app", "posts", databases.CollectionPatch{Permissions: &goodPerms}, keysPrincipal))
}
