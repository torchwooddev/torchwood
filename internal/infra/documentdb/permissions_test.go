package documentdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// TestPermissions_ListFilterTenantIsolation (A5): 列表权限过滤的 EXISTS 子查询必须
// 关联 d._tenant，异租户同 collection/文档 ID 的 _perms 行不得放行本租户列表。
func TestPermissions_ListFilterTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	// 集合无 read 授权：强制走逐文档 EXISTS 过滤路径（无 SkipDocumentPermissionFilter）。
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "update", Role: "users"},
	}, true))

	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		ID:   "doc-1",
		Data: map[string]any{"title": "t1"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	permsTable := permsTableName(testSchema(t, projectID, "app"))
	// 异租户 _perms 行（同 collection/文档/类型/角色）不得影响本租户列表。
	foreignTenant := internalID + 1000
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES (?, ?, ?, 'read', 'user:bob')`,
		permsTable), foreignTenant, "docs", created.ID)
	require.NoError(t, err)

	bob := databases.Principal{Roles: []string{"user:bob"}}
	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","doc-1")`},
	}, bob)
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// 同租户 _perms 行放行（正对照）。
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES (?, ?, ?, 'read', 'user:bob')`,
		permsTable), internalID, "docs", created.ID)
	require.NoError(t, err)
	list, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","doc-1")`},
	}, bob)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
}

func TestPermissions_CollectionLevelFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}

	created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		Data: map[string]any{"title": "Hello"},
	}, nil, alice)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	got, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "Hello", got.Data["title"])
}

func TestPermissions_DocumentLevelOverridesCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "update", Role: "any"},
		{Type: "delete", Role: "any"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	_, err = docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, alice)
	require.NoError(t, err)
	require.Equal(t, "Secret", got.Data["title"])
}

func TestPermissions_CreateCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "locked", "Locked", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "user:alice"},
		{Type: "read", Role: "any"},
	}, true))

	_, err := docDB.CreateDocument(ctx, projectID, "app", "locked", databases.Document{
		Data: map[string]any{"title": "test"},
	}, nil, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	_, err = docDB.CreateDocument(ctx, projectID, "app", "locked", databases.Document{
		Data: map[string]any{"title": "test"},
	}, nil, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
}

func TestPermissions_KeysNotBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		// 不含 read 授权：在 documentSecurity 文档级优先语义（B1）下，集合级
		// read:any 会对无 _perms 文档放行，无法检验 keys 不 bypass 文档权限。
		{Type: "create", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Owned by alice"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	_, err = docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)
}

func TestPermissions_PlatformAdminBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	adminPrincipal := databases.Principal{PlatformAdmin: true}
	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, adminPrincipal)
	require.NoError(t, err)
	require.Equal(t, "Secret", got.Data["title"])

	_, err = docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   created.ID,
			Data: map[string]any{"title": "Updated by admin"},
		},
		ExpectedVersion: 1,
	}, adminPrincipal)
	require.NoError(t, err)

	err = docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, databases.DeleteOptions{ExpectedVersion: 2}, adminPrincipal)
	require.NoError(t, err)
}

func TestPermissions_ValidateGrantablePermissions(t *testing.T) {
	shared := databases.Principal{Roles: []string{"users", "user:u1"}}
	tests := []struct {
		name       string
		grantor    databases.Principal
		perms      []databases.Permission
		privileged bool
		wantErr    bool
	}{
		{
			name:    "grant read:any succeeds",
			grantor: shared,
			perms:   []databases.Permission{{Type: "read", Role: "any"}},
		},
		{
			name:    "grant create:any succeeds",
			grantor: shared,
			perms:   []databases.Permission{{Type: "create", Role: "any"}},
		},
		{
			name:    "grant update:any rejected",
			grantor: shared,
			perms:   []databases.Permission{{Type: "update", Role: "any"}},
			wantErr: true,
		},
		{
			name:    "grant delete:any rejected",
			grantor: shared,
			perms:   []databases.Permission{{Type: "delete", Role: "any"}},
			wantErr: true,
		},
		{
			name:    "grant write:any rejected",
			grantor: shared,
			perms:   []databases.Permission{{Type: "write", Role: "any"}},
			wantErr: true,
		},
		{
			name:    "grant unheld role rejected",
			grantor: shared,
			perms:   []databases.Permission{{Type: "read", Role: "user:other"}},
			wantErr: true,
		},
		{
			name:    "grant own role succeeds",
			grantor: shared,
			perms:   []databases.Permission{{Type: "update", Role: "user:u1"}},
		},
		{
			name:       "privileged grantor bypasses synthetic check",
			grantor:    shared,
			perms:      []databases.Permission{{Type: "update", Role: "any"}},
			privileged: true,
		},
		{
			name:    "platform admin bypasses synthetic check",
			grantor: databases.Principal{PlatformAdmin: true},
			perms:   []databases.Permission{{Type: "delete", Role: "any"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := databases.ValidateGrantablePermissions(tt.grantor, tt.perms, tt.privileged)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPermissions_SystemPrincipalBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "user:alice"},
		{Type: "read", Role: "user:alice"},
	}, true))

	_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "System created"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
}

// TestPermissions_KeysCannotWriteSystemCollections 验证 keys 角色对系统敏感集合
// （users/sessions/identities）的 update/delete 在集合级与文档级均被收窄
// （安全评审 C1 第 3 层 / M2）；groups 的 keys 管理权限是合法语义，保留。
func TestPermissions_KeysCannotWriteSystemCollections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, testutil.SeedLegacySystemDocumentCollections(ctx, db, docDB, projectID))

	keysPrincipal := databases.Principal{Roles: []string{"keys"}}

	// 构造一个"遗留文档"：文档级 _perms 仍含 keys 的 update/delete（模拟升级前数据）。
	userDoc, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "legacy-user",
		Data: map[string]any{"email": "legacy@example.com", "status": "active"},
	}, []databases.Permission{
		{Type: "read", Role: "keys"},
		{Type: "update", Role: "keys"},
		{Type: "delete", Role: "keys"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// keys 更新/删除 users 文档被拒（集合级与文档级均已收窄）。
	_, err = docDB.UpdateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userDoc.ID,
		Data: map[string]any{"name": "hacked"},
	}, nil), keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)
	err = docDB.DeleteDocument(ctx, projectID, databases.SystemDatabaseID, "users", userDoc.ID, databases.DeleteOptions{}, keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)

	// keys 读 users 文档仍然放行（read:keys 保留）。
	got, err := docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", userDoc.ID, keysPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)

	// groups 的 keys 管理权限保留：keys 可创建用户组文档（create:keys 集合级授权）。
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "groups", databases.Document{
		ID:   "group-1",
		Data: map[string]any{"name": "Group A"},
	}, nil, keysPrincipal)
	require.NoError(t, err)
}

// TestPermissions_ListORFallback (B1): 用户集合 documentSecurity=true 且集合级有
// read:any——有 _perms 的文档须匹配文档读权限（覆盖集合级）；无 _perms 的文档由
// 集合级 read 兜底（列表过滤 NOT EXISTS 分支，与 AllowsDocumentAccess 的
// docHasPerms=false -> collOK 一致）。
func TestPermissions_ListORFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	// 私有文档：文档级 read:user:alice 覆盖集合级 read:any。
	privateDoc, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		ID:   "private-1",
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{{Type: "read", Role: "user:alice"}}, alice)
	require.NoError(t, err)

	// 无 _perms 文档（SystemPrincipal 造数）：集合级 read:any 兜底。
	_, err = docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		ID:   "public-1",
		Data: map[string]any{"title": "Public"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	// bob：私有文档不可见（文档权限覆盖），无 _perms 文档可见（NOT EXISTS 兜底）。
	bobList, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","` + privateDoc.ID + `")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, bobList.Documents, 0)

	bobPublic, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","public-1")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, bobPublic.Documents, 1)

	// alice：私有文档与公开文档均可见。
	aliceList, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","private-1")`},
	}, alice)
	require.NoError(t, err)
	require.Len(t, aliceList.Documents, 1)
}

// TestPermissions_WriteRowTypeConsistency (B1 step 7): _perms 表可能存在
// _type='write' 行（ParsePermissionStrings 会展开为三行，但直调 adapter 的路径
// 可能不展开）。单文档路径 matchTypes 使 create/update/delete 检查命中 write 行，
// 列表过滤只匹配 _type='read'——两者语义一致（write 不隐含 read），验证而不改行为。
func TestPermissions_WriteRowTypeConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	// 集合级仅 create 授权（无 read/update/delete）：读写检查完全依赖文档级。
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "legacy"},
	}, nil, alice)
	require.NoError(t, err)

	// 直插 _type='write' 行（模拟未展开的直调路径）。
	permsTable := permsTableName(testSchema(t, projectID, "app"))
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES (?, ?, ?, 'write', 'user:alice')`,
		permsTable), internalID, "docs", created.ID)
	require.NoError(t, err)

	// write 行命中 update 检查（matchTypes 展开）→ 可更新（D3：无 read 预检）。
	_, err = docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   created.ID,
			Data: map[string]any{"title": "updated"},
		},
		ExpectedVersion: 1,
	}, alice)
	require.NoError(t, err)

	// write 不隐含 read：GetDocument 的 read 检查拒绝。
	_, err = docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, alice)
	require.ErrorIs(t, err, ErrPermissionDenied)

	// 列表过滤只匹配 _type='read'：文档对 alice 不可见（一致性确认）。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, alice)
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// write 行命中 delete 检查 → 可删除。
	err = docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, databases.DeleteOptions{ExpectedVersion: 2}, alice)
	require.NoError(t, err)
}
