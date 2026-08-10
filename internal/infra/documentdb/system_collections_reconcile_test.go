package documentdb

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// oldTeamsAttrs 模拟旧版本 spec 的 teams 集合属性（无 prefs）。
var oldTeamsAttrs = []databases.Attribute{
	{ID: "teams_name", Key: "name", Type: "string", Size: 256},
	{ID: "teams_permissions", Key: "permissions", Type: "json"},
	{ID: "teams_total", Key: "total", Type: "integer", Default: 0},
}

// setupOldSpecTeamsCollection 模拟存量项目：default 库元数据 + 旧 spec 的 teams 集合
// （不调 EnsureSystemCollections，即不触发任何 reconcile）。
func setupOldSpecTeamsCollection(t *testing.T, ctx context.Context, db *clients.Database, docDB databases.DocumentDB, projectID string) {
	t.Helper()
	now := time.Now()
	_, err := db.NewInsert().Model(&model.DocumentDatabase{
		ID:        "default",
		ProjectID: projectID,
		Name:      "default",
		CreatedAt: now,
		UpdatedAt: now,
	}).Exec(ctx)
	require.NoError(t, err)
	spec := systemCollectionSpecs(projectID)["teams"]
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "default", "teams", spec.name, oldTeamsAttrs, spec.indexes, spec.permissions, true))
}

func attrKeyCount(attrs []databases.Attribute, key string) int {
	n := 0
	for _, a := range attrs {
		if a.Key == key {
			n++
		}
	}
	return n
}

// TestEnsureSystemCollections_ReconcileExistingCollectionAttrs：存量旧 spec 集合 → 补列
// （物理列 + document_attributes 元数据）；重复调用幂等。
func TestEnsureSystemCollections_ReconcileExistingCollectionAttrs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	setupOldSpecTeamsCollection(t, ctx, db, docDB, projectID)

	coll, err := docDB.GetCollection(ctx, projectID, "default", "teams")
	require.NoError(t, err)
	require.NotNil(t, coll)
	require.Zero(t, attrKeyCount(coll.Attributes, "prefs"), "旧 spec 集合不应有 prefs 属性")

	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	coll, err = docDB.GetCollection(ctx, projectID, "default", "teams")
	require.NoError(t, err)
	require.Equal(t, 1, attrKeyCount(coll.Attributes, "prefs"), "reconcile 应补齐 prefs 元数据")

	// 物理列存在：写一条含 prefs 的文档成功（旧 spec 下会报 42703）。
	created, err := docDB.CreateDocument(ctx, projectID, "default", "teams", databases.Document{
		Data: map[string]any{"name": "Reconciled", "prefs": map[string]any{"theme": "dark"}},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	got, err := docDB.GetDocument(ctx, projectID, "default", "teams", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark"}, got.Data["prefs"])

	// 幂等：重复调用无错误，属性不重复。
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	coll, err = docDB.GetCollection(ctx, projectID, "default", "teams")
	require.NoError(t, err)
	require.Equal(t, 1, attrKeyCount(coll.Attributes, "prefs"), "reconcile 必须幂等")
}

// TestEnsureSystemCollections_ReconcileConcurrent：两 goroutine 同时
// EnsureSystemCollections → 均无错误（属性元数据 INSERT 撞唯一约束 23505 被吞）。
// 预建全部集合与 schema，将并发面收窄到 reconcile 的属性补列路径，避免既有
// 建表竞态（pg_type_typname_nsp_index，已另行修复）干扰本测试的确定性。
func TestEnsureSystemCollections_ReconcileConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	ddb := NewPostgresDocumentDB(db)
	docDB := ddb.(*postgresDocumentDB)

	internalID, err := docDB.resolveInternalID(ctx, projectID)
	require.NoError(t, err)
	require.NoError(t, docDB.ensureSchemaAndPerms(ctx, schemaName(internalID, "default")))
	now := time.Now()
	_, err = db.NewInsert().Model(&model.DocumentDatabase{
		ID:        "default",
		ProjectID: projectID,
		Name:      "default",
		CreatedAt: now,
		UpdatedAt: now,
	}).Exec(ctx)
	require.NoError(t, err)

	specs := systemCollectionSpecs(projectID)
	for _, id := range databases.SystemCollectionIDs {
		spec := specs[id]
		attrs := spec.attrs
		if id == "teams" {
			attrs = oldTeamsAttrs
		}
		require.NoError(t, docDB.CreateCollection(ctx, projectID, "default", id, spec.name, attrs, spec.indexes, spec.permissions, true))
	}
	coll, err := docDB.GetCollection(ctx, projectID, "default", "teams")
	require.NoError(t, err)
	require.Zero(t, attrKeyCount(coll.Attributes, "prefs"))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- docDB.EnsureSystemCollections(ctx, projectID, internalID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "并发 EnsureSystemCollections 不应报错（23505 应被吞）")
	}

	coll, err = docDB.GetCollection(ctx, projectID, "default", "teams")
	require.NoError(t, err)
	require.Equal(t, 1, attrKeyCount(coll.Attributes, "prefs"), "并发 reconcile 后 prefs 元数据应恰好一条")
}
