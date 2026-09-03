package documentdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

func setupTxTest(t *testing.T) (databases.DocumentDB, string, string) {
	t.Helper()
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	t.Cleanup(cleanup)
	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 128},
	}, nil, []databases.Permission{
		{Type: "create", Role: "keys"},
		{Type: "read", Role: "keys"},
		{Type: "update", Role: "keys"},
		{Type: "delete", Role: "keys"},
	}, true))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 128},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, []databases.Permission{{Type: "create", Role: "keys"}}, true))
	return docDB, projectID, "app"
}

var txKeys = databases.Principal{Roles: []string{"keys"}, KeyID: "ktx"}

// ATOMIC：任一 op 失败整批回滚（第一个 op 的写入不留痕），错误带域码与 op index。
func TestExecuteTransactions_AtomicRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, dbID := setupTxTest(t)
	seed := []databases.Permission{{Type: "read", Role: "keys"}, {Type: "update", Role: "keys"}}
	v2 := int64(2)

	_, err := docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "n1", Data: map[string]any{"title": "one"}, Permissions: seed},
		// update 不存在的文档 → ErrDocumentNotFound → 整批回滚。
		{Type: databases.TransactionOpUpdate, CollectionID: "notes", DocumentID: "missing", Data: map[string]any{"title": "x"}, ExpectedVersion: &v2},
	}, databases.TransactionModeAtomic, txKeys)
	require.Error(t, err)
	oe := databases.AsOpError(err)
	require.NotNil(t, oe)
	require.Equal(t, 1, oe.Index)
	require.Equal(t, databases.ErrCodeNotFound, databases.ErrorDomainCode(oe.Err))

	// 第一个 op 已回滚：n1 不存在。
	got, err := docDB.GetDocument(ctx, projectID, dbID, "notes", "n1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, got)
}

// ATOMIC 正向链路：create v1 → update（CAS v1→v2）→ upsert 插入 → 全部留痕。
func TestExecuteTransactions_AtomicMixedChain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, dbID := setupTxTest(t)
	seed := []databases.Permission{{Type: "read", Role: "keys"}, {Type: "update", Role: "keys"}, {Type: "delete", Role: "keys"}}
	v1 := int64(1)

	results, err := docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "n1", Data: map[string]any{"title": "one"}, Permissions: seed},
		{Type: databases.TransactionOpUpdate, CollectionID: "notes", DocumentID: "n1", Data: map[string]any{"title": "two"}, ExpectedVersion: &v1},
		{Type: databases.TransactionOpUpsert, CollectionID: "members", DocumentID: "m1", Data: map[string]any{"email": "a@b.c"}, ConflictColumns: []string{"email"}},
	}, databases.TransactionModeAtomic, txKeys)
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.True(t, results[0].OK)
	require.Equal(t, int64(1), results[0].Version)
	require.True(t, results[1].OK)
	require.Equal(t, int64(2), results[1].Version)
	require.True(t, results[2].OK)
	require.Equal(t, "m1", results[2].DocumentID)

	note, err := docDB.GetDocument(ctx, projectID, dbID, "notes", "n1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "two", note.Data["title"])
	require.Equal(t, "key:ktx", note.CreatedBy) // 归因贯穿批路径
}

// PARTIAL：失败 op 记录 per-op 结果（域码），已成功不回滚，后续 op 继续。
func TestExecuteTransactions_Partial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, dbID := setupTxTest(t)
	seed := []databases.Permission{{Type: "read", Role: "keys"}, {Type: "update", Role: "keys"}}
	v2 := int64(2)

	results, err := docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "n1", Data: map[string]any{"title": "one"}, Permissions: seed},
		{Type: databases.TransactionOpUpdate, CollectionID: "notes", DocumentID: "missing", Data: map[string]any{"title": "x"}, ExpectedVersion: &v2},
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "n2", Data: map[string]any{"title": "two"}, Permissions: seed},
	}, databases.TransactionModePartial, txKeys)
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.True(t, results[0].OK)
	require.False(t, results[1].OK)
	require.Equal(t, databases.ErrCodeNotFound, results[1].ErrCode)
	require.True(t, results[2].OK)

	// 已成功 op 留痕。
	got, err := docDB.GetDocument(ctx, projectID, dbID, "notes", "n1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)
	got2, err := docDB.GetDocument(ctx, projectID, dbID, "notes", "n2", databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got2)
}

// PARTIAL 权限拒绝：批内越权 op 记录 PERMISSION_DENIED 域码，其余照常。
func TestExecuteTransactions_PartialPermissionDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, dbID := setupTxTest(t)
	seed := []databases.Permission{{Type: "read", Role: "keys"}, {Type: "update", Role: "keys"}}
	// guests 对 notes 无任何权限。
	guest := databases.Principal{Roles: []string{"guests"}}

	results, err := docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "g1", Data: map[string]any{"title": "x"}, Permissions: seed},
	}, databases.TransactionModePartial, guest)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].OK)
	require.Equal(t, databases.ErrCodePermissionDenied, results[0].ErrCode)

	got, err := docDB.GetDocument(ctx, projectID, dbID, "notes", "g1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, got)
}

// 输入校验：空批/超限/非法类型/非法模式。
func TestExecuteTransactions_Validation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, dbID := setupTxTest(t)

	_, err := docDB.ExecuteTransactions(ctx, projectID, dbID, nil, databases.TransactionModeAtomic, txKeys)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: "bogus", CollectionID: "notes", DocumentID: "x", Data: map[string]any{"title": "t"}},
	}, databases.TransactionModeAtomic, txKeys)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = docDB.ExecuteTransactions(ctx, projectID, dbID, []databases.TransactionOp{
		{Type: databases.TransactionOpCreate, CollectionID: "notes", DocumentID: "x", Data: map[string]any{"title": "t"}},
	}, databases.TransactionMode("weird"), txKeys)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
