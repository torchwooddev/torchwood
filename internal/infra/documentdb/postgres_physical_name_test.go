// 物理名解耦（阶段②包 B，redesign §4.2 标识符治理 / 预决策 2/4/6）：DDL 与
// 行查询走服务端分配的物理表名；_perms/事件保持逻辑 collectionID；ddl_seq
// CAS 乐观锁（CATALOG.DDL_CONFLICT）；索引名组合校验对物理名自然满足。
package documentdb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// TestPhysicalName_TableIsolation：两个逻辑名不同的集合各得独立物理表——
// 同属性互不影响；文档互不可见；物理表名均为 c_<base32> 形态。
func TestPhysicalName_TableIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	attrs := []databases.Attribute{{ID: "title", Key: "title", Type: "string", Size: 256}}
	for _, coll := range []string{"posts", "articles"} {
		require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", coll, coll, attrs, nil, nil, true))
	}
	schema := testSchema(t, projectID, "app")
	postsPhys := testPhysicalName(t, ctx, db, projectID, "app", "posts")
	articlesPhys := testPhysicalName(t, ctx, db, projectID, "app", "articles")
	require.NotEqual(t, postsPhys, articlesPhys)
	for _, phys := range []string{postsPhys, articlesPhys} {
		require.Regexp(t, `^c_[a-z2-7]{8}$`, phys)
	}

	// 各自写入，物理表隔离：一集合的行对另一集合不可见。
	_, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		ID: "p1", Data: map[string]any{"title": "post"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	list, err := docDB.ListDocuments(ctx, projectID, "app", "articles", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Empty(t, list.Documents, "posts 的文档不得出现在 articles")

	// 同库下不存在逻辑名物理表（表名全部服务端分配）。
	var logicalTables int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name IN ('posts', 'articles')`,
		schema).Scan(&logicalTables))
	require.Zero(t, logicalTables, "业务集合不得再以逻辑名建物理表")
	for _, phys := range []string{postsPhys, articlesPhys} {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?)`,
			schema, phys).Scan(&exists))
		require.True(t, exists, "物理表 %s 必须存在", phys)
	}

	// 删除一个集合只清理自己的物理表。
	require.NoError(t, docDB.DeleteCollection(ctx, projectID, "app", "posts"))
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?)`,
		schema, postsPhys).Scan(&exists))
	require.False(t, exists, "DeleteCollection 必须按物理名 DROP")
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?)`,
		schema, articlesPhys).Scan(&exists))
	require.True(t, exists, "相邻集合的物理表不受影响")
}

// TestPhysicalName_NotExposedInAPI：物理名是内部实现细节——GetCollection/
// ListCollections 返回的 domain 形状（JSON 序列化后）不含物理名；逻辑 ID 原样。
func TestPhysicalName_NotExposedInAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts",
		[]databases.Attribute{{ID: "title", Key: "title", Type: "string", Size: 256}},
		[]databases.Index{{ID: "title_key", Type: "key", Attributes: []string{"title"}}},
		nil, true))
	physical := testPhysicalName(t, ctx, db, projectID, "app", "posts")

	got, err := docDB.GetCollection(ctx, projectID, "app", "posts")
	require.NoError(t, err)
	require.Equal(t, "posts", got.ID)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), physical, "GetCollection 响应不得泄漏物理名")

	list, _, err := docDB.ListCollections(ctx, projectID, "app", databases.ListQuery{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	raw, err = json.Marshal(list)
	require.NoError(t, err)
	require.NotContains(t, string(raw), physical, "ListCollections 响应不得泄漏物理名")
}

// TestPhysicalName_BypassPaths：System/bypass 主体（跳过 GetCollection 的
// 聚合/列表/点查路径）经 resolvePhysicalTable 单条 catalog 点查正常寻址。
func TestPhysicalName_BypassPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts",
		[]databases.Attribute{{ID: "views", Key: "views", Type: "integer"}}, nil, nil, true))
	for i := range 3 {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
			ID:   fmt.Sprintf("d%d", i),
			Data: map[string]any{"views": i + 1},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	sys := databases.SystemPrincipal
	got, err := docDB.GetDocument(ctx, projectID, "app", "posts", "d1", sys)
	require.NoError(t, err)
	require.NotNil(t, got)

	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts",
		databases.Query{AST: &query.Query{Filter: query.Gte("views", "2")}}, sys)
	require.NoError(t, err)
	require.Len(t, list.Documents, 2)

	count, err := docDB.CountDocuments(ctx, projectID, "app", "posts", databases.Query{}, sys)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)

	groups, err := docDB.AggregateDocuments(ctx, projectID, "app", "posts", databases.Query{},
		[]databases.AggregateSpec{{Function: databases.AggregateSum, Field: "views"}}, "", sys)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, databases.AggregateValueInt64, groups[0].Values[0].Kind)
	require.Equal(t, int64(6), groups[0].Values[0].Int64)

	n, err := docDB.BulkUpdateDocuments(ctx, projectID, "app", "posts",
		[]string{"d0", "d1"}, map[string]any{"views": 10}, nil, sys)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
}

// TestPhysicalNameACLEmbeddedInPhysicalTable：_acl 内嵌物理表行内（阶段③
// 包 A，_perms 退役）——文档 ACE 随行存储于服务端分配的物理表，realtime 频道
// 契约与逻辑 collectionID 的耦合点随之消失（频道名只来自事件信封的逻辑 ID）。
func TestPhysicalNameACLEmbeddedInPhysicalTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", nil, nil, nil, true))
	_, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		ID: "p1", Data: map[string]any{},
	}, []databases.Permission{{Type: "read", Role: "user:u1"}}, databases.SystemPrincipal)
	require.NoError(t, err)

	physical := testPhysicalName(t, ctx, db, projectID, "app", "posts")
	tbl := testSchema(t, projectID, "app") + "." + physical
	var acl string
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT _acl::text FROM %s WHERE _id = 'p1'`, tbl)).Scan(&acl))
	require.Equal(t, `{read:user:u1}`, acl)
}

// TestDDLSeq_IncrementAcrossDDLPaths：五个元数据写路径的 CAS 递增——
// UpdateCollection/CreateAttribute/CreateIndex/DeleteAttribute/DeleteIndex
// 依次执行后 ddl_seq 1→6。
func TestDDLSeq_IncrementAcrossDDLPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs",
		[]databases.Attribute{{ID: "n", Key: "n", Type: "integer"}},
		[]databases.Index{{ID: "n_key", Type: "key", Attributes: []string{"n"}}}, nil, true))

	seq := func() int64 {
		var v int64
		require.NoError(t, db.NewSelect().Model((*model.DocumentCollection)(nil)).
			Column("ddl_seq").
			Where("project_id = ? AND database_id = ? AND collection_id = ?", projectID, "app", "docs").
			Scan(ctx, &v))
		return v
	}
	require.Equal(t, int64(1), seq(), "创建后 ddl_seq=1")

	require.NoError(t, docDB.UpdateCollection(ctx, projectID, "app", "docs", databases.CollectionPatch{Name: "Renamed"}))
	require.Equal(t, int64(2), seq())
	require.NoError(t, docDB.CreateAttribute(ctx, projectID, "app", "docs", databases.Attribute{ID: "m", Key: "m", Type: "integer"}))
	require.Equal(t, int64(3), seq())
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "docs", databases.Index{ID: "m_key", Type: "key", Attributes: []string{"m"}}))
	require.Equal(t, int64(4), seq())
	// 先删索引再删属性（DeleteAttribute 会级联清理引用该属性的索引）。
	require.NoError(t, docDB.DeleteIndex(ctx, projectID, "app", "docs", "m_key"))
	require.Equal(t, int64(5), seq())
	require.NoError(t, docDB.DeleteAttribute(ctx, projectID, "app", "docs", "m"))
	require.Equal(t, int64(6), seq())
}

// TestDDLSeq_ConcurrentConflict：悬挂事务持旧 ddl_seq，另一 DDL 先行提交后
// CAS 0 行 → ErrDDLConflict（CATALOG.DDL_CONFLICT / Aborted / retryable）。
func TestDDLSeq_ConcurrentConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs",
		[]databases.Attribute{{ID: "n", Key: "n", Type: "integer"}}, nil, nil, true))
	p := docDB.(*postgresDocumentDB)

	loaded := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- p.db.RunInTx(ctx, func(txCtx context.Context) error {
			row, err := p.loadCollectionRow(txCtx, projectID, "app", "docs")
			if err != nil {
				return err
			}
			close(loaded)
			<-release
			// 与 UpdateCollection 同形态的 CAS 写（旧 ddl_seq）。
			res, err := p.conn(txCtx).ExecContext(txCtx,
				`UPDATE catalog_collections SET name = 'stale-rename', updated_at = NOW(), ddl_seq = ddl_seq + 1
				 WHERE project_id = ? AND database_id = ? AND collection_id = ? AND ddl_seq = ?`,
				projectID, "app", "docs", row.DDLSeq)
			if err != nil {
				return err
			}
			return requireCASApplied(res)
		})
	}()
	<-loaded
	// 悬挂事务已读到 ddl_seq=1；主线程的 DDL 先行提交（ddl_seq 1→2）。
	require.NoError(t, docDB.CreateAttribute(ctx, projectID, "app", "docs", databases.Attribute{ID: "m", Key: "m", Type: "integer"}))
	close(release)
	err := <-done
	require.ErrorIs(t, err, databases.ErrDDLConflict)

	// 域码映射：Aborted（R12 裁决：CAS 冲突非参数错误，对齐
	// IDEMPOTENCY.IN_PROGRESS 先例）/ CATALOG.DDL_CONFLICT / retryable=true。
	mapped := shared.MapDocumentDBError(err)
	require.Equal(t, codes.Aborted, status.Code(mapped))
	st := status.Convert(mapped)
	var info *errdetails.ErrorInfo
	for _, d := range st.Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			info = ei
		}
	}
	require.NotNil(t, info, "ErrorInfo detail 必须存在")
	require.Equal(t, databases.ErrCodeDDLConflict, info.GetReason())
	require.Equal(t, "true", info.GetMetadata()["retryable"])

	// 悬挂事务回滚：stale rename 不生效。
	got, err := docDB.GetCollection(ctx, projectID, "app", "docs")
	require.NoError(t, err)
	require.Equal(t, "Docs", got.Name)
	require.Len(t, got.Attributes, 2, "先行提交的 CreateAttribute 保留")
}

// TestIndexName_PhysicalNaturalFit：物理名 ≤10 字符使 idx_<phys>_<id> 对任意
// 合法索引 ID（≤40）自然 ≤63——组合校验在物理名上恒过；逻辑名组合校验保留
//（防直调：40+40 组合仍拒绝）。
func TestIndexName_PhysicalNaturalFit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	longColl := strings.Repeat("c", 36) // 逻辑组合 4+36+1+20=61 ≤63 合法
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", longColl, "Long",
		[]databases.Attribute{{ID: "n", Key: "n", Type: "integer"}}, nil, nil, true))
	physical := testPhysicalName(t, ctx, db, projectID, "app", longColl)
	require.LessOrEqual(t, len(physical), 10)

	// 物理名上：任意 40 字符索引 ID 组合都 ≤63（自然满足）。
	require.NoError(t, validateIndexNameLen(physical, strings.Repeat("i", 40)))
	require.NoError(t, validateIndexNameLen(physical, "tenant_created"))

	// 逻辑名组合校验保留（防直调）：40+40 组合仍被拒绝。
	require.Error(t, validateIndexNameLen(strings.Repeat("c", 40), strings.Repeat("i", 40)))

	// 物理索引名确实由物理名前缀构成（阶段③包 A 起表还带 idx_<phys>_acl
	// GIN 索引，按精确名点查避免歧义）。
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", longColl, databases.Index{
		ID: strings.Repeat("i", 20), Type: "key", Attributes: []string{"n"},
	}))
	userIdx := fmt.Sprintf("idx_%s_%s", physical, strings.Repeat("i", 20))
	aclIdx := fmt.Sprintf("idx_%s_acl", physical)
	var gotNames []string
	rows, err := db.QueryContext(ctx,
		`SELECT indexname FROM pg_indexes WHERE schemaname = ? AND tablename = ? AND indexname IN (?, ?)`,
		testSchema(t, projectID, "app"), physical, userIdx, aclIdx)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		gotNames = append(gotNames, n)
	}
	require.NoError(t, rows.Err())
	require.ElementsMatch(t, []string{userIdx, aclIdx}, gotNames)
}
