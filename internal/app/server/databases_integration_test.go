package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestDatabases_AcceptanceChain covers manual checklist §4.14–4.18:
// create database → collection → attribute → index, then delete in reverse order.
func TestDatabases_AcceptanceChain(t *testing.T) {
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

	const (
		dbID    = "app"
		collID  = "posts"
		attrKey = "title"
		indexID = "idx_title"
	)

	require.NoError(t, uc.CreateDatabase(ctx, projectID, dbID, "Application DB"))

	dbs, err := uc.ListDatabases(ctx, projectID)
	require.NoError(t, err)
	require.NotEmpty(t, dbs)

	gotDB, err := uc.GetDatabase(ctx, projectID, dbID)
	require.NoError(t, err)
	require.NotNil(t, gotDB)
	require.Equal(t, dbID, gotDB.ID)

	require.NoError(t, uc.CreateCollection(ctx, projectID, dbID, collID, "Posts", nil, nil, nil, true))

	colls, _, _, err := uc.ListCollections(ctx, projectID, dbID, databases.ListQuery{})
	require.NoError(t, err)
	require.Len(t, colls, 1)

	gotColl, err := uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Equal(t, collID, gotColl.ID)

	require.NoError(t, uc.CreateAttribute(ctx, projectID, dbID, collID, databases.Attribute{
		ID:   attrKey,
		Key:  attrKey,
		Type: "string",
		Size: 256,
	}))

	require.NoError(t, uc.CreateIndex(ctx, projectID, dbID, collID, databases.Index{
		ID:         indexID,
		Type:       "unique",
		Attributes: []string{attrKey},
	}))

	gotColl, err = uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Len(t, gotColl.Attributes, 1)
	require.Equal(t, attrKey, gotColl.Attributes[0].Key)
	require.Len(t, gotColl.Indexes, 1)
	require.Equal(t, indexID, gotColl.Indexes[0].ID)

	require.NoError(t, uc.DeleteCollection(ctx, projectID, dbID, collID))
	gotColl, err = uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Nil(t, gotColl)

	require.NoError(t, uc.DeleteDatabase(ctx, projectID, dbID))
	gotDB, err = uc.GetDatabase(ctx, projectID, dbID)
	require.NoError(t, err)
	require.Nil(t, gotDB)
}

// TestDatabases_ServerCreateDocument_EmptyPermissions (#1): Server API 创建文档
// 不带 permissions 时不再展开为默认集合权限（文档级权限为空）；显式传入保持原行为。
func TestDatabases_ServerCreateDocument_EmptyPermissions(t *testing.T) {
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
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	created, err := uc.CreateDocument(ctx, projectID, "app", "posts", "", map[string]any{"title": "no perms"}, nil, principal)
	require.NoError(t, err)
	require.Empty(t, created.Permissions)

	explicit, err := uc.CreateDocument(ctx, projectID, "app", "posts", "", map[string]any{"title": "explicit"}, []databases.Permission{
		{Type: "read", Role: "any"},
	}, principal)
	require.NoError(t, err)
	require.Len(t, explicit.Permissions, 1)
	require.Equal(t, "read", explicit.Permissions[0].Type)
	require.Equal(t, "any", explicit.Permissions[0].Role)
}

// TestDatabases_ListDocuments_NextPageToken (#5): NextPageToken 可续页且与 offset
// 语义一致、无重叠。
func TestDatabases_ListDocuments_NextPageToken(t *testing.T) {
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
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "n", Key: "n", Type: "integer"},
	}, nil, nil, true))

	const total = 12
	for i := 0; i < total; i++ {
		_, err := uc.CreateDocument(ctx, projectID, "app", "docs", fmt.Sprintf("doc-%04d", i), map[string]any{"n": i}, nil, principal)
		require.NoError(t, err)
	}

	page1, total1, next, err := uc.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries:  []string{`orderAsc("$id")`, `limit(10)`},
		PageSize: 10,
	}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(total), total1)
	require.Len(t, page1, 10)
	require.NotEmpty(t, next)
	ids1 := docIDsOf(page1)

	page2, total2, next2, err := uc.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries:   []string{`orderAsc("$id")`, `limit(10)`},
		PageSize:  10,
		PageToken: next,
	}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(total), total2)
	require.Len(t, page2, 2)
	require.Empty(t, next2)
	ids2 := docIDsOf(page2)
	for _, id := range ids2 {
		require.NotContains(t, ids1, id, "page 2 must not overlap page 1")
	}

	offsetPage, _, _, err := uc.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`orderAsc("$id")`, `limit(10)`, `offset(10)`},
	}, principal)
	require.NoError(t, err)
	require.Equal(t, ids2, docIDsOf(offsetPage), "token page must match offset page")
}

// TestDatabases_ListCollections_Pagination (#10): page_size/page_token 生效，
// NextPageToken 可续页，三页覆盖全部集合且无重叠。
func TestDatabases_ListCollections_Pagination(t *testing.T) {
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

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))

	const total = 12
	for i := 0; i < total; i++ {
		require.NoError(t, uc.CreateCollection(ctx, projectID, "app", fmt.Sprintf("coll_%02d", i), fmt.Sprintf("Collection %02d", i), nil, nil, nil, true))
		time.Sleep(2 * time.Millisecond)
	}

	var all []string
	pageToken := ""
	for page := 0; page < 3; page++ {
		pageColls, totalCount, next, err := uc.ListCollections(ctx, projectID, "app", databases.ListQuery{PageSize: 5, PageToken: pageToken})
		require.NoError(t, err)
		require.Equal(t, int64(total), totalCount)
		switch page {
		case 0, 1:
			require.Len(t, pageColls, 5)
			require.NotEmpty(t, next)
		case 2:
			require.Len(t, pageColls, 2)
			require.Empty(t, next)
		}
		for _, c := range pageColls {
			require.NotContains(t, all, c.ID, "collection must not repeat across pages")
			all = append(all, c.ID)
		}
		pageToken = next
	}
	require.Len(t, all, total)
}

// TestDatabases_CreateDocument_PermissionTemplates (#2a): user:{id}/team:{id}
// 模板在权限校验前替换为调用者真实角色并落库。
func TestDatabases_CreateDocument_PermissionTemplates(t *testing.T) {
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

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "user:alice"},
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
	}, true))

	userPrincipal := databases.Principal{Roles: []string{"users", "user:alice"}}
	perms, err := databases.ParsePermissionStrings([]string{"read:user:{id}"})
	require.NoError(t, err)
	created, err := uc.CreateDocument(ctx, projectID, "app", "docs", "", map[string]any{"title": "t"}, perms, userPrincipal)
	require.NoError(t, err)
	require.Len(t, created.Permissions, 1)
	require.Equal(t, "read", created.Permissions[0].Type)
	require.Equal(t, "user:alice", created.Permissions[0].Role)

	teamPrincipal := databases.Principal{Roles: []string{"users", "user:alice", "team:t1"}}
	upPerms, err := databases.ParsePermissionStrings([]string{"update:team:{id}"})
	require.NoError(t, err)
	updated, err := uc.UpdateDocument(ctx, projectID, "app", "docs", created.ID, nil, upPerms, nil, teamPrincipal)
	require.NoError(t, err)
	require.Len(t, updated.Permissions, 1)
	require.Equal(t, "update", updated.Permissions[0].Type)
	require.Equal(t, "team:t1", updated.Permissions[0].Role)
}

func docIDsOf(docs []databases.Document) []string {
	out := make([]string, len(docs))
	for i := range docs {
		out[i] = docs[i].ID
	}
	return out
}
