package documentdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

func TestPermissions_CollectionLevelFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

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

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

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

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

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

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		// 不含 read 授权：在 documentSecurity OR 语义下 collection 级的 read:any
		// 会对所有角色（含 keys）放行，无法检验 keys 不 bypass 文档权限。
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

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

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

	_, err = docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.SimpleDocumentUpdate(databases.Document{
		ID:   created.ID,
		Data: map[string]any{"title": "Updated by admin"},
	}, nil), adminPrincipal)
	require.NoError(t, err)

	err = docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, adminPrincipal)
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

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

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
// （安全评审 C1 第 3 层 / M2）；teams 的 keys 管理权限是合法语义，保留。
func TestPermissions_KeysCannotWriteSystemCollections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	keysPrincipal := databases.Principal{Roles: []string{"keys"}}

	// 构造一个"遗留文档"：文档级 _perms 仍含 keys 的 update/delete（模拟升级前数据）。
	userDoc, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		ID:   "legacy-user",
		Data: map[string]any{"email": "legacy@example.com", "status": "active"},
	}, []databases.Permission{
		{Type: "read", Role: "keys"},
		{Type: "update", Role: "keys"},
		{Type: "delete", Role: "keys"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// keys 更新/删除 users 文档被拒（集合级与文档级均已收窄）。
	_, err = docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userDoc.ID,
		Data: map[string]any{"name": "hacked"},
	}, nil), keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)
	err = docDB.DeleteDocument(ctx, projectID, "default", "users", userDoc.ID, keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)

	// keys 读 users 文档仍然放行（read:keys 保留）。
	got, err := docDB.GetDocument(ctx, projectID, "default", "users", userDoc.ID, keysPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)

	// teams 的 keys 管理权限保留：keys 可创建团队文档（create:keys 集合级授权）。
	_, err = docDB.CreateDocument(ctx, projectID, "default", "teams", databases.Document{
		ID:   "team-1",
		Data: map[string]any{"name": "Team A"},
	}, nil, keysPrincipal)
	require.NoError(t, err)
}

// TestCleanup_KeysWritePermsLegacyProject 验证启动期存量清理：对模拟的遗留项目
// （文档级 _perms 与集合级元数据仍含 keys 的 update/delete），EnsureSystemCollections
// 幂等清除 users/sessions/identities 的写权限且不影响 teams/memberships。
func TestCleanup_KeysWritePermsLegacyProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	schema := fmt.Sprintf(`"TORCHWOOD_%d_default"`, internalID)

	// 模拟升级前遗留状态：文档级 _perms 存在 keys 的 update/delete 行
	// （users/sessions/identities 各若干 + teams 一行作为对照）。
	seedSQL := fmt.Sprintf(
		`INSERT INTO %s._perms (_tenant, _collection, _document, _type, _permission) VALUES
		 (?, 'users', 'u1', 'update', 'keys'),
		 (?, 'users', 'u1', 'delete', 'keys'),
		 (?, 'sessions', 's1', 'update', 'keys'),
		 (?, 'identities', 'i1', 'delete', 'keys'),
		 (?, 'teams', 't1', 'update', 'keys')`,
		schema)
	_, err := db.DB.ExecContext(ctx, seedSQL, internalID, internalID, internalID, internalID, internalID)
	require.NoError(t, err)
	// 集合级元数据：系统集合与 teams 都补上 keys 写权限（teams 作为对照）。
	_, err = db.DB.ExecContext(ctx,
		`UPDATE document_collections SET permissions = permissions || ARRAY['update:keys','delete:keys']
		 WHERE project_id = ? AND database_id = 'default' AND id IN ('users','sessions','identities','teams')`,
		projectID)
	require.NoError(t, err)

	// 新实例触发清理（进程内"已清理"标记不跨实例）。
	fresh := NewPostgresDocumentDB(db)
	require.NoError(t, fresh.EnsureSystemCollections(ctx, projectID, internalID))

	permsTable := fmt.Sprintf("%s._perms", schema)
	// 文档级：三个系统集合的 keys 写权限全部清除。
	var keysWriteRows int64
	row := db.DB.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE _permission = 'keys' AND _type IN ('update','delete') AND _collection IN ('users','sessions','identities')`, permsTable))
	require.NoError(t, row.Scan(&keysWriteRows))
	require.Zero(t, keysWriteRows)

	// teams 的 keys 写权限行保留。
	var teamKeysWrite int64
	row = db.DB.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE _permission = 'keys' AND _type IN ('update','delete') AND _collection = 'teams'`, permsTable))
	require.NoError(t, row.Scan(&teamKeysWrite))
	require.Equal(t, int64(1), teamKeysWrite)

	// 集合级元数据：系统集合不再含 keys 写权限；teams 保留。
	var metaKeysWrite int64
	row = db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_collections WHERE project_id = ? AND database_id = 'default'
		 AND id IN ('users','sessions','identities') AND permissions @> ARRAY['update:keys','delete:keys']`, projectID)
	require.NoError(t, row.Scan(&metaKeysWrite))
	require.Zero(t, metaKeysWrite)

	var teamMeta int64
	row = db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_collections WHERE project_id = ? AND database_id = 'default'
		 AND id = 'teams' AND permissions @> ARRAY['update:keys','delete:keys']`, projectID)
	require.NoError(t, row.Scan(&teamMeta))
	require.Equal(t, int64(1), teamMeta)

	// 幂等：再次清理无错误、无新副作用。
	third := NewPostgresDocumentDB(db)
	require.NoError(t, third.EnsureSystemCollections(ctx, projectID, internalID))
	row = db.DB.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE _permission = 'keys' AND _type IN ('update','delete') AND _collection IN ('users','sessions','identities')`, permsTable))
	require.NoError(t, row.Scan(&keysWriteRows))
	require.Zero(t, keysWriteRows)
}
