// vector_search 端到端（会话 #10 包 C，预决策 3/4/6）：RLS 真实路径的稀疏
// 可见行召回（iterative scan 灵魂语义——1000 行中 5 行可见仍返回 k=5 近邻
// 而非"先取全局 k 再滤"）、distances 回传与 ground truth 一致、max_distance
// 后置过滤、metric/索引/维度/算子互斥全矩阵拒绝、execute-tx data 通道可写。
package documentdb

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/query"
)

func setupKNNCollection(ctx context.Context, t *testing.T) (databases.DocumentDB, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	t.Cleanup(cleanup)
	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "knn", "KNN", []databases.Attribute{
		{ID: "emb", Key: "emb", Type: "vector", Dims: 3},
	}, nil, nil, true))
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "knn", databases.Index{
		ID: "emb_cos", Type: "hnsw", Attributes: []string{"emb"}, DistanceMetric: "COSINE",
	}))
	return docDB, projectID
}

// knnQuery 构造 vector_search 的 domain Query。
func knnQuery(attr string, values []float64, metric string, maxDist *float64, k int32, filter *query.Filter) databases.Query {
	return databases.Query{AST: &query.Query{
		VectorSearch: &query.VectorSearch{Attribute: attr, Values: values, Metric: metric, MaxDistance: maxDist},
		PageSize:     k,
		Filter:       filter,
	}}
}

// TestVectorSearch_SparseVisibilityRecall：RLS policy 真实路径的稀疏可见行
// 召回——1000 行中仅 5 行对 principal 可见（其余 _acl 限定他角色），且全部
// 995 个不可见行比可见行更近。错误实现（iterative scan off = 先取全局
// top-5 再滤）返回 0 行；正确实现返回全部 5 个可见近邻（与 ground truth
// SQL 同序同距）。这是对本会话灵魂语义（vector 与文档同 RLS 判定管辖）的
// 锁定。
//
// 数据几何（L2 度量）：查询向量取原点，不可见行均匀铺满半径 0.05..0.5 的
// 球体（方向确定性伪随机、彼此分散），可见行在半径 0.6..1.0。注意不可见
// 行必须分散：近重复簇（全部挤在查询向量极近处）会使 HNSW 图在簇内导航
// 饱和、iterative scan 无法穿透到远处的可见行——那是 HNSW 近似的固有
// 边界而非 iterative scan 语义缺陷（开发期实证：近重复簇数据 50% 概率
// 返回 0 行；分散数据稳定全召回）。
func TestVectorSearch_SparseVisibilityRecall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID := setupKNNCollection(ctx, t)
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "knn", databases.Index{
		ID: "emb_l2", Type: "hnsw", Attributes: []string{"emb"}, DistanceMetric: "L2",
	}))

	otherOnly := []databases.Permission{{Type: "read", Role: "other-role"}}
	anyRead := []databases.Permission{{Type: "read", Role: "any"}}

	// 995 个不可见行：半径 0.05..0.5 球体、方向伪随机分散。
	for i := 1; i <= 995; i++ {
		r := 0.05 + 0.45*(float64(i)/995.0)
		emb := []any{r * cos64(float64(i)*2.4), r * sin64(float64(i)*2.4), r * cos64(float64(i)*0.7)}
		_, err := docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
			ID:   fmt.Sprintf("hidden-%03d", i),
			Data: map[string]any{"emb": emb},
		}, otherOnly, databases.SystemPrincipal)
		require.NoError(t, err)
	}
	// 5 个可见行：L2 距离原点 0.6..1.0（全部比不可见行远）。
	visible := map[string][]any{
		"va": {0.6, 0.0, 0.0},
		"vb": {0.0, 0.7, 0.0},
		"vc": {0.0, 0.0, 0.8},
		"vd": {-0.9, 0.0, 0.0},
		"ve": {0.0, -1.0, 0.0},
	}
	for id, emb := range visible {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
			ID: id, Data: map[string]any{"emb": emb},
		}, anyRead, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	principal := databases.Principal{Roles: []string{"any"}}
	list, err := docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{0, 0, 0}, "L2", nil, 5, nil), principal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 5, "iterative scan must return k=5 *visible* neighbors")
	require.Len(t, list.Distances, 5)

	// Ground truth：绕过 KNN 管道直接对 5 个可见行算距离排序（同库 SQL）。
	db := docDB.(*postgresDocumentDB)
	rows, err := db.conn(ctx).QueryContext(ctx, `
		SELECT _id, (emb <-> '[0,0,0]'::vector) AS dist
		FROM tw_`+projectID+`_app.`+knPhysical(ctx, t, docDB, projectID)+`
		WHERE _id IN ('va','vb','vc','vd','ve')
		ORDER BY dist`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var wantIDs []string
	var wantDists []float64
	for rows.Next() {
		var id string
		var dist float64
		require.NoError(t, rows.Scan(&id, &dist))
		wantIDs = append(wantIDs, id)
		wantDists = append(wantDists, dist)
	}
	require.NoError(t, rows.Err())
	require.Len(t, wantIDs, 5)

	gotIDs := make([]string, 0, len(list.Documents))
	for _, d := range list.Documents {
		gotIDs = append(gotIDs, d.ID)
	}
	require.Equal(t, wantIDs, gotIDs, "KNN result must match ground-truth distance order of visible rows")
	for i := range wantDists {
		require.InDelta(t, wantDists[i], list.Distances[i], 1e-9, "distance %d", i)
	}
	// distances 单调不减（top-k 升序）。
	for i := 1; i < len(list.Distances); i++ {
		require.LessOrEqual(t, list.Distances[i-1], list.Distances[i])
	}
}

// cos64/sin64 是确定性三角函数（测试数据生成用；math 包直接别名导出无意义，
// 显式包装表达"仅测试消费"）。
func cos64(x float64) float64 { return math.Cos(x) }
func sin64(x float64) float64 { return math.Sin(x) }

func knPhysical(ctx context.Context, t *testing.T, docDB databases.DocumentDB, projectID string) string {
	t.Helper()
	db := docDB.(*postgresDocumentDB)
	var physical string
	err := db.conn(ctx).QueryRowContext(ctx, `
		SELECT physical_name FROM catalog_collections
		WHERE project_id = ? AND database_id = 'app' AND collection_id = 'knn'`, projectID).Scan(&physical)
	require.NoError(t, err)
	return physical
}

// TestVectorSearch_FilterAndMaxDistance：普通 filter 可组合（AND）；max_distance
// 后置过滤（top-k 中距离超阈值的行被剔除，documents/distances 同步）。
func TestVectorSearch_FilterAndMaxDistance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID := setupKNNCollection(ctx, t)

	seed := map[string][]any{
		"a": {1.0, 0.0, 0.0}, // dist 0
		"b": {0.7, 0.7, 0.0}, // dist ~0.293
		"c": {0.0, 1.0, 0.0}, // dist 1
	}
	for id, emb := range seed {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
			ID: id, Data: map[string]any{"emb": emb},
		}, anyPerms(), databases.SystemPrincipal)
		require.NoError(t, err)
	}
	principal := databases.Principal{Roles: []string{"any"}}

	// 无阈值 top-2。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "COSINE", nil, 2, nil), principal)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, []string{list.Documents[0].ID, list.Documents[1].ID})

	// max_distance=0.1：top-2 中仅 a 保留（阈值不引入新行——b 距离 > 0.1）。
	md := 0.1
	list, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "COSINE", &md, 2, nil), principal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "a", list.Documents[0].ID)
	require.Len(t, list.Distances, 1)

	// 与普通 filter 组合（AND）：equal 限定 _id。
	list, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "COSINE", nil, 3, query.Eq("_id", "c")), principal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "c", list.Documents[0].ID)
}

// TestVectorSearch_Metrics：L2 与 INNER_PRODUCT 的 KNN 路径（三 metric 索引
// 与算子映射）。
func TestVectorSearch_Metrics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID := setupKNNCollection(ctx, t)
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "knn", databases.Index{
		ID: "emb_l2", Type: "hnsw", Attributes: []string{"emb"}, DistanceMetric: "L2",
	}))
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "knn", databases.Index{
		ID: "emb_ip", Type: "hnsw", Attributes: []string{"emb"}, DistanceMetric: "INNER_PRODUCT",
	}))

	_, err := docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
		ID: "near", Data: map[string]any{"emb": []any{1.0, 0.0, 0.0}},
	}, anyPerms(), databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
		ID: "far", Data: map[string]any{"emb": []any{0.0, 1.0, 0.0}},
	}, anyPerms(), databases.SystemPrincipal)
	require.NoError(t, err)
	principal := databases.Principal{Roles: []string{"any"}}

	// L2：near 距 [1,0,0] 为 0，far 为 √2 ≈ 1.4142（<-> 是欧氏距离）。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "L2", nil, 2, nil), principal)
	require.NoError(t, err)
	require.Equal(t, "near", list.Documents[0].ID)
	require.InDelta(t, 0.0, list.Distances[0], 1e-9)
	require.Equal(t, "far", list.Documents[1].ID)
	require.InDelta(t, 1.4142135623730951, list.Distances[1], 1e-9)

	// INNER_PRODUCT：<#> = 负内积；[1,0,0]·[1,0,0]=1 → -1；[1,0,0]·[0,1,0]=0 → 0（更"远"）。
	list, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "INNER_PRODUCT", nil, 2, nil), principal)
	require.NoError(t, err)
	require.Equal(t, "near", list.Documents[0].ID)
	require.InDelta(t, -1.0, list.Distances[0], 1e-9)
	require.InDelta(t, 0.0, list.Distances[1], 1e-9)
}

// TestVectorSearch_RejectionMatrix：前置校验全矩阵——非 vector 列/维度不匹配/
// 无匹配 metric 索引/vector 进普通 filter（非 isNull）/vector 进 order/
// count 组合。
func TestVectorSearch_RejectionMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID := setupKNNCollection(ctx, t)
	// 集合只有 COSINE 索引（setup 建立）。
	_, err := docDB.CreateDocument(ctx, projectID, "app", "knn", databases.Document{
		ID: "s1", Data: map[string]any{"emb": []any{1.0, 0.0, 0.0}},
	}, anyPerms(), databases.SystemPrincipal)
	require.NoError(t, err)
	principal := databases.Principal{Roles: []string{"any"}}

	// 维度不匹配（2 维 vs dims=3）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0}, "COSINE", nil, 5, nil), principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "expected 3")

	// metric 无匹配索引（L2 索引未建——本用例集合仅 COSINE）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "INNER_PRODUCT", nil, 5, nil), principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "requires an hnsw index")

	// 非 vector 列。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn",
		knnQuery("_id", []float64{1, 0, 0}, "COSINE", nil, 5, nil), principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "requires a vector attribute")

	// vector 进普通 filter（非 isNull/isNotNull）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn", databases.Query{AST: &query.Query{
		Filter: query.Eq("emb", "x"),
	}}, principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "isNull/isNotNull")

	// isNull/isNotNull 是 vector 属性唯一合法的普通 filter。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn", databases.Query{AST: &query.Query{
		Filter: query.IsNull("emb"),
	}}, principal)
	require.NoError(t, err)

	// vector 进 order。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "knn", databases.Query{AST: &query.Query{
		Orders: []query.Order{{Attribute: "emb", Desc: true}},
	}}, principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "cannot be an order key")

	// count/aggregate 是整集语义，KNN top-k 不相容。
	_, err = docDB.CountDocuments(ctx, projectID, "app", "knn",
		knnQuery("emb", []float64{1, 0, 0}, "COSINE", nil, 5, nil), principal)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "vector_search is not supported on count")
}

// TestVectorSearch_ExecuteTxDataChannel：execute-tx 的 data 通道对 vector 列
// 照常可写（复用 createDocument 编码，无特殊化——"明确不做"清单的确认项）。
func TestVectorSearch_ExecuteTxDataChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID := setupKNNCollection(ctx, t)

	results, err := docDB.ExecuteTransactions(ctx, projectID, "app", []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "knn", DocumentID: "tx1",
			Data:        map[string]any{"emb": []any{0.5, 0.5, 0.5}},
			Permissions: anyPerms()},
	}, databases.TransactionModeAtomic, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].OK)
	// 读回确认 vector 列经 execute-tx 落库且形态为 JSON 数组。
	got, err := docDB.GetDocument(ctx, projectID, "app", "knn", "tx1", databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, []any{0.5, 0.5, 0.5}, got.Data["emb"])
}
