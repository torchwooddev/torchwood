package shared

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 本文件覆盖 v2 设计「测试最低集」PR4 条（真实 Postgres 集成测试）。
// 写路径用端用户 u1 主体（集合默认权限含 users；文档级 _perms 由
// ownerPerms(u1) 授予），与真实 Client 调用链同口径。

// txTestEnv 是事务集成测试环境：真实 Postgres + outbox publisher +
// "app"."docs" 用户集合（documentSecurity=true，文档级写权限由 _perms 决定）。
type txTestEnv struct {
	core      *Transactions
	docDB     databases.DocumentDB
	db        *clients.Database
	projectID string
}

func setupTxTest(t *testing.T, ctx context.Context) *txTestEnv {
	t.Helper()
	db := testutil.SetupTestDB(t)
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)
	docDB := documentdb.NewPostgresDocumentDB(db, infraevents.NewEventOutbox(db))
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, nil, databases.DefaultCollectionPermissions(), true))
	return &txTestEnv{
		core:      NewTransactions(bunrepo.NewTransactionRepository(db), docDB, db),
		docDB:     docDB,
		db:        db,
		projectID: projectID,
	}
}

func txUserCtx(ctx context.Context, projectID, userID string) context.Context {
	return contexts.WithPrincipal(ctx, &domainshared.Principal{
		ActorID:   idgen.ID(userID),
		ActorKind: domainshared.ActorKindEndUser,
		ProjectID: projectID,
		UserID:    userID,
		Roles:     []string{"users", "user:" + userID},
	})
}

func txKeyCtx(ctx context.Context, projectID, keyID string, scopes []string) context.Context {
	return contexts.WithPrincipal(ctx, &domainshared.Principal{
		ActorID:     idgen.ID(keyID),
		ActorKind:   domainshared.ActorKindService,
		ProjectID:   projectID,
		APIKeyID:    keyID,
		Permissions: scopes,
		Roles:       []string{"keys"},
	})
}

func txAdminCtx(ctx context.Context) context.Context {
	return contexts.WithPrincipal(ctx, &domainshared.Principal{
		ActorID:         "admin-1",
		ActorKind:       domainshared.ActorKindAdmin,
		IsPlatformAdmin: true,
	})
}

func txUserPrincipal(userID string) databases.Principal {
	return databases.Principal{Roles: []string{"users", "user:" + userID}}
}

func ownerPerms(userID string) []databases.Permission {
	return []databases.Permission{
		{Type: "read", Role: "user:" + userID},
		{Type: "update", Role: "user:" + userID},
		{Type: "delete", Role: "user:" + userID},
	}
}

// prepare 是测试用 op 校验（对齐 Client/Server prepare 的公共部分：
// 系统集合拒、create/upsert 需 data、update/delete 需 version presence）。
func (e *txTestEnv) prepare(databaseID string) PrepareTransactionOpFunc {
	return func(ctx context.Context, op databases.TransactionOp) (databases.TransactionOp, error) {
		if err := e.core.CheckTransactionCollection(ctx, e.projectID, databaseID, op.CollectionID); err != nil {
			return op, err
		}
		switch op.OpType {
		case databases.TransactionOpCreate, databases.TransactionOpUpsert:
			if len(op.Data) == 0 {
				return op, status.Error(codes.InvalidArgument, "data is required")
			}
		case databases.TransactionOpUpdate, databases.TransactionOpDelete:
			if op.Version == nil {
				return op, status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
			}
		}
		return op, nil
	}
}

func (e *txTestEnv) appendCreate(t *testing.T, ctx context.Context, databaseID, txID, docID string, perms []databases.Permission) *databases.TransactionOp {
	t.Helper()
	op, err := e.core.Append(ctx, e.projectID, databaseID, txID, databases.TransactionOp{
		OpType:       databases.TransactionOpCreate,
		CollectionID: "docs",
		DocumentID:   docID,
		Data:         map[string]any{"title": docID},
		Permissions:  perms,
	}, e.prepare(databaseID))
	require.NoError(t, err)
	require.Equal(t, txID, op.TransactionID)
	return op
}

func (e *txTestEnv) appendUpdate(t *testing.T, ctx context.Context, databaseID, txID, collectionID, docID string, data map[string]any, version int64) *databases.TransactionOp {
	t.Helper()
	op, err := e.core.Append(ctx, e.projectID, databaseID, txID, databases.TransactionOp{
		OpType:       databases.TransactionOpUpdate,
		CollectionID: collectionID,
		DocumentID:   docID,
		Data:         data,
		Version:      &version,
	}, e.prepare(databaseID))
	require.NoError(t, err)
	return op
}

func (e *txTestEnv) outboxRows(t *testing.T, ctx context.Context) []model.DocumentEventsOutbox {
	t.Helper()
	var rows []model.DocumentEventsOutbox
	err := e.db.Conn(ctx).NewSelect().Model(&rows).Where("project_id = ?", e.projectID).Order("created_at ASC", "event_id ASC").Scan(ctx)
	require.NoError(t, err)
	return rows
}

func outboxTxID(t *testing.T, row model.DocumentEventsOutbox) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(row.Payload, &m))
	v, _ := m["transaction_id"].(string)
	return v
}

func (e *txTestEnv) getTxStatus(t *testing.T, ctx context.Context, txID string) string {
	t.Helper()
	m := new(model.DocumentTransaction)
	require.NoError(t, e.db.Conn(ctx).NewSelect().Model(m).Where("id = ?", txID).Scan(ctx))
	return m.Status
}

func requireGRPCCode(t *testing.T, err error, code codes.Code, msg string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, status.Code(err), "err=%v", err)
	if msg != "" {
		require.Contains(t, err.Error(), msg)
	}
}

// 两文档 create+commit：外部可见 version=1，两条 create 事件同一 transaction_id，
// 且 outbox 每个 op 恰好一排（无二次 INSERT）。
func TestTransactions_CommitTwoCreates_SingleOutboxTransactionID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	u1Ctx := txUserCtx(ctx, env.projectID, "u1")

	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusPending, tx.Status)
	require.Equal(t, "user:u1", tx.CreatedBy)

	env.appendCreate(t, u1Ctx, "app", tx.ID, "d1", ownerPerms("u1"))
	env.appendCreate(t, u1Ctx, "app", tx.ID, "d2", ownerPerms("u1"))

	committed, ops, err := env.core.Commit(u1Ctx, env.projectID, "app", tx.ID, txUserPrincipal("u1"))
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusCommitted, committed.Status)
	require.Len(t, ops, 2)
	require.Equal(t, int32(1), ops[0].Seq)
	require.Equal(t, int32(2), ops[1].Seq)

	for _, id := range []string{"d1", "d2"} {
		doc, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", id, databases.SystemPrincipal)
		require.NoError(t, err)
		require.NotNil(t, doc)
		require.Equal(t, int64(1), doc.Version)
	}

	rows := env.outboxRows(t, ctx)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, tx.ID, outboxTxID(t, row))
	}
}

// create 再 update：传 1 成功（版本接力）；传 0 整单回滚，GetTransaction 为
// rolled_back，再 Commit → transaction_not_pending。
func TestTransactions_CreateThenUpdate_VersionRelayAndRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	u1Ctx := txUserCtx(ctx, env.projectID, "u1")
	principal := txUserPrincipal("u1")

	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	env.appendCreate(t, u1Ctx, "app", tx.ID, "d1", ownerPerms("u1"))
	env.appendUpdate(t, u1Ctx, "app", tx.ID, "docs", "d1", map[string]any{"title": "d1-updated"}, 1)
	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx.ID, principal)
	require.NoError(t, err)
	doc, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "d1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(2), doc.Version)
	require.Equal(t, "d1-updated", doc.Data["title"])

	// 传 0：整单回滚（version_required 属 version 类失败 → 另开短事务 rolled_back）。
	tx2, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	env.appendCreate(t, u1Ctx, "app", tx2.ID, "d2", ownerPerms("u1"))
	env.appendUpdate(t, u1Ctx, "app", tx2.ID, "docs", "d2", map[string]any{"title": "x"}, 0)
	outboxBefore := len(env.outboxRows(t, ctx))
	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx2.ID, principal)
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrVersionRequired.Error())

	// 无部分落库、无本事务 outbox 行。
	gone, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "d2", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, gone)
	rowsAfter := env.outboxRows(t, ctx)
	require.Len(t, rowsAfter, outboxBefore)
	for _, row := range rowsAfter {
		require.NotEqual(t, tx2.ID, outboxTxID(t, row))
	}

	got, _, err := env.core.Get(u1Ctx, env.projectID, "app", tx2.ID)
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusRolledBack, got.Status)

	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx2.ID, principal)
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrTransactionNotPending.Error())
}

// 外部 Update +1 后 Commit version 失败；数据保持外部写；事务 rolled_back。
func TestTransactions_ExternalWriteConflict_RollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	u1Ctx := txUserCtx(ctx, env.projectID, "u1")

	created, err := env.docDB.CreateDocument(ctx, env.projectID, "app", "docs", databases.Document{
		ID:   "d1",
		Data: map[string]any{"title": "original"},
	}, ownerPerms("u1"), txUserPrincipal("u1"))
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	env.appendUpdate(t, u1Ctx, "app", tx.ID, "docs", "d1", map[string]any{"title": "staged"}, 1)

	// 外部写入先 +1（pending 期间不锁用户行，互斥全靠 _version）。
	_, err = env.docDB.UpdateDocument(ctx, env.projectID, "app", "docs", databases.DocumentUpdate{
		Document:        databases.Document{ID: "d1", Data: map[string]any{"title": "external"}},
		ExpectedVersion: 1,
	}, txUserPrincipal("u1"))
	require.NoError(t, err)

	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx.ID, txUserPrincipal("u1"))
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrVersionMismatch.Error())

	doc, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "d1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "external", doc.Data["title"])
	require.Equal(t, int64(2), doc.Version)
	require.Equal(t, databases.TransactionStatusRolledBack, env.getTxStatus(t, ctx, tx.ID))
}

// 系统集合入事务拒（system_collection_not_allowed），追加失败不改 status。
func TestTransactions_SystemCollectionRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	keyCtx := txKeyCtx(ctx, env.projectID, "k1", []string{"databases"})

	_, err := env.core.Create(keyCtx, env.projectID, databases.SystemDatabaseID)
	require.Equal(t, codes.InvalidArgument, status.Code(err), "事务不得指向 sentinel")
}

// 第二笔 pending 拒（部分唯一索引 → transaction_already_pending）。
func TestTransactions_SecondPendingRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	userCtx := txUserCtx(ctx, env.projectID, "u1")

	_, err := env.core.Create(userCtx, env.projectID, "app")
	require.NoError(t, err)
	_, err = env.core.Create(userCtx, env.projectID, "app")
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrTransactionAlreadyPending.Error())

	// 另一用户的同库不受影响。
	_, err = env.core.Create(txUserCtx(ctx, env.projectID, "u2"), env.projectID, "app")
	require.NoError(t, err)
}

// 过期 Commit → FailedPrecondition transaction_expired，行就地 SET expired。
func TestTransactions_ExpiredCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	keyCtx := txKeyCtx(ctx, env.projectID, "k1", []string{"databases"})

	tx, err := env.core.Create(keyCtx, env.projectID, "app")
	require.NoError(t, err)
	env.appendCreate(t, keyCtx, "app", tx.ID, "d1", ownerPerms("u1"))

	_, err = env.db.Conn(ctx).NewUpdate().Model((*model.DocumentTransaction)(nil)).
		Set("expire_at = NOW() - INTERVAL '1 second'").Where("id = ?", tx.ID).Exec(ctx)
	require.NoError(t, err)

	_, _, err = env.core.Commit(keyCtx, env.projectID, "app", tx.ID, databases.Principal{Roles: []string{"keys"}})
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrTransactionExpired.Error())
	require.Equal(t, databases.TransactionStatusExpired, env.getTxStatus(t, ctx, tx.ID))

	// 未 apply、无 outbox。
	gone, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "d1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, gone)
	require.Empty(t, env.outboxRows(t, ctx))
}

// 空事务 Commit 成功、无事件。
func TestTransactions_EmptyCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	keyCtx := txKeyCtx(ctx, env.projectID, "k1", []string{"databases"})

	tx, err := env.core.Create(keyCtx, env.projectID, "app")
	require.NoError(t, err)
	committed, ops, err := env.core.Commit(keyCtx, env.projectID, "app", tx.ID, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusCommitted, committed.Status)
	require.Empty(t, ops)
	require.Empty(t, env.outboxRows(t, ctx))
}

// 权限不足 op 使 Commit 全滚：无部分行、无本事务 outbox 行、status=rolled_back。
func TestTransactions_PermissionDenied_RollsBackAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)

	// u2 的私有文档（documentSecurity=true，仅 user:u2 可写）。
	other, err := env.docDB.CreateDocument(ctx, env.projectID, "app", "docs", databases.Document{
		ID:   "other",
		Data: map[string]any{"title": "u2 doc"},
	}, ownerPerms("u2"), txUserPrincipal("u2"))
	require.NoError(t, err)

	u1Ctx := txUserCtx(ctx, env.projectID, "u1")
	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	env.appendCreate(t, u1Ctx, "app", tx.ID, "mine", ownerPerms("u1"))
	env.appendUpdate(t, u1Ctx, "app", tx.ID, "docs", other.ID, map[string]any{"title": "hijack"}, 1)
	outboxBefore := len(env.outboxRows(t, ctx))

	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx.ID, txUserPrincipal("u1"))
	requireGRPCCode(t, err, codes.PermissionDenied, "")

	// 无部分行：create 的 "mine" 未落库；他人文档未被改动。
	gone, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "mine", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, gone)
	doc, err := env.docDB.GetDocument(ctx, env.projectID, "app", "docs", "other", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "u2 doc", doc.Data["title"])
	require.Equal(t, int64(1), doc.Version)
	// 无本事务的 outbox 行（无二次 INSERT、无部分提交）。
	rowsAfter := env.outboxRows(t, ctx)
	require.Len(t, rowsAfter, outboxBefore)
	for _, row := range rowsAfter {
		require.NotEqual(t, tx.ID, outboxTxID(t, row))
	}
	require.Equal(t, databases.TransactionStatusRolledBack, env.getTxStatus(t, ctx, tx.ID))
}

// upsert-insert 后再 update 传 1 成功（版本接力：upsert-insert → 1）。
func TestTransactions_UpsertInsertThenUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	require.NoError(t, env.docDB.CreateCollection(ctx, env.projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, databases.DefaultCollectionPermissions(), true))

	u1Ctx := txUserCtx(ctx, env.projectID, "u1")
	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	_, err = env.core.Append(u1Ctx, env.projectID, "app", tx.ID, databases.TransactionOp{
		OpType:          databases.TransactionOpUpsert,
		CollectionID:    "members",
		DocumentID:      "m1",
		Data:            map[string]any{"email": "a@example.com", "name": "Alice"},
		ConflictColumns: []string{"email"},
		Permissions:     ownerPerms("u1"),
	}, env.prepare("app"))
	require.NoError(t, err)
	env.appendUpdate(t, u1Ctx, "app", tx.ID, "members", "m1", map[string]any{"name": "Alice Updated"}, 1)

	_, _, err = env.core.Commit(u1Ctx, env.projectID, "app", tx.ID, txUserPrincipal("u1"))
	require.NoError(t, err)
	doc, err := env.docDB.GetDocument(ctx, env.projectID, "app", "members", "m1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "Alice Updated", doc.Data["name"])
	require.Equal(t, int64(2), doc.Version)
}

// 并发 Append 在 Commit 持锁期间被行锁挡住；锁内提交 committed 后追加得到
// transaction_not_pending，不会出现已 committed 上的孤儿 op。
func TestTransactions_ConcurrentAppendBlockedByRowLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	keyCtx := txKeyCtx(ctx, env.projectID, "k1", []string{"databases"})

	tx, err := env.core.Create(keyCtx, env.projectID, "app")
	require.NoError(t, err)

	bunTx, err := env.db.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	locked := new(model.DocumentTransaction)
	require.NoError(t, bunTx.NewSelect().Model(locked).Where("id = ?", tx.ID).For("UPDATE").Scan(ctx))
	lockHeld := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(lockHeld)
		time.Sleep(300 * time.Millisecond)
		// 持锁期间把事务置 committed（等价于进行中的 Commit）。
		_, err := bunTx.NewUpdate().Model((*model.DocumentTransaction)(nil)).
			Set("status = ?", databases.TransactionStatusCommitted).
			Where("id = ?", tx.ID).Exec(ctx)
		if err == nil {
			err = bunTx.Commit()
		}
		if err != nil {
			_ = bunTx.Rollback()
			t.Errorf("commit locker tx: %v", err)
		}
	}()

	<-lockHeld
	start := time.Now()
	_, err = env.core.Append(keyCtx, env.projectID, "app", tx.ID, databases.TransactionOp{
		OpType:       databases.TransactionOpCreate,
		CollectionID: "docs",
		DocumentID:   "blocked",
		Data:         map[string]any{"title": "blocked"},
	}, env.prepare("app"))
	wg.Wait()
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrTransactionNotPending.Error())
	require.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond, "追加应被 FOR UPDATE 行锁挡住至锁释放")

	ops, err := bunrepo.NewTransactionRepository(env.db).ListOps(ctx, tx.ID)
	require.NoError(t, err)
	require.Empty(t, ops, "已 committed 事务上不得出现孤儿 op")
}

// 操作者规则：非创建者 Client（端用户）Get/Commit 拒 PermissionDenied；
// platform admin 与 databases 写 scope 的 API Key 可干预任意 pending。
func TestTransactions_OperatorRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)

	u1Ctx := txUserCtx(ctx, env.projectID, "u1")
	u2Ctx := txUserCtx(ctx, env.projectID, "u2")

	tx, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	require.Equal(t, "user:u1", tx.CreatedBy)

	// 非创建者端用户：Get / Commit 均拒。
	_, _, err = env.core.Get(u2Ctx, env.projectID, "app", tx.ID)
	requireGRPCCode(t, err, codes.PermissionDenied, "")
	_, _, err = env.core.Commit(u2Ctx, env.projectID, "app", tx.ID, txUserPrincipal("u2"))
	requireGRPCCode(t, err, codes.PermissionDenied, "")
	require.Equal(t, databases.TransactionStatusPending, env.getTxStatus(t, ctx, tx.ID))

	// 只读 scope 的 API Key 不可干预。
	_, err = env.core.Rollback(txKeyCtx(ctx, env.projectID, "k-read", []string{"databases.read"}), env.projectID, "app", tx.ID)
	requireGRPCCode(t, err, codes.PermissionDenied, "")

	// platform admin 可回滚他人 pending。
	rolled, err := env.core.Rollback(txAdminCtx(ctx), env.projectID, "app", tx.ID)
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusRolledBack, rolled.Status)

	// databases 写 scope 的 API Key 可回滚他人 pending。
	tx2, err := env.core.Create(u1Ctx, env.projectID, "app")
	require.NoError(t, err)
	rolled2, err := env.core.Rollback(txKeyCtx(ctx, env.projectID, "k2", []string{"databases.write"}), env.projectID, "app", tx2.ID)
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusRolledBack, rolled2.Status)
}

// 追加 update/delete 缺 version（presence）→ version_required，status 保持 pending。
func TestTransactions_AppendRequiresVersionPresence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	env := setupTxTest(t, ctx)
	keyCtx := txKeyCtx(ctx, env.projectID, "k1", []string{"databases"})

	tx, err := env.core.Create(keyCtx, env.projectID, "app")
	require.NoError(t, err)
	_, err = env.core.Append(keyCtx, env.projectID, "app", tx.ID, databases.TransactionOp{
		OpType:       databases.TransactionOpUpdate,
		CollectionID: "docs",
		DocumentID:   "d1",
		Data:         map[string]any{"title": "x"},
	}, env.prepare("app"))
	requireGRPCCode(t, err, codes.FailedPrecondition, databases.ErrVersionRequired.Error())
	require.Equal(t, databases.TransactionStatusPending, env.getTxStatus(t, ctx, tx.ID))
}
