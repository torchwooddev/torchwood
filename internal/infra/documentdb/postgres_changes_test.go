package documentdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/events"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

// changesEnv 组装 ChangeFeed 测试环境（真实 PG；事件直接经 outbox publisher
// 落库——ChangeFeed 只读 outbox 表，无需建业务集合）。
func changesEnv(t *testing.T) (databases.DocumentDB, *clients.Database) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	return NewPostgresDocumentDB(db, events.NewEventOutbox(db)), db
}

// publishChange 落一条文档可见性为 user:<userID> 的文档事件，返回落库行。
func publishChange(t *testing.T, db *clients.Database, docID, event, userID string) model.DocumentEventsOutbox {
	t.Helper()
	ev := domainevents.Envelope{
		// EventID 预置：Publish 只在缺省时生成（改内部副本），回查行需要
		// 外侧可用的稳定 ID。
		EventID:      idgen.ULID().String(),
		Event:        event,
		ProjectID:    "default",
		DatabaseID:   "app",
		CollectionID: "docs",
		DocumentID:   docID,
		Version:      1,
		CreatedAt:    time.Now(),
		ACL: domainevents.ACLSnapshot{
			DocumentSecurity:    true,
			DocumentPermissions: []databases.Permission{{Type: "read", Role: "user:" + userID}},
			DocHasPerms:         true,
		},
	}
	if event != domainevents.EventDocumentsDelete {
		ev.Data = &databases.Document{ID: docID, Data: map[string]any{"t": "v"}, Version: 1}
	}
	require.NoError(t, events.NewEventOutbox(db).Publish(context.Background(), ev))
	var row model.DocumentEventsOutbox
	require.NoError(t, db.Conn(context.Background()).NewSelect().Model(&row).
		Where("event_id = ?", ev.EventID).Scan(context.Background()))
	return row
}

func u1Principal() databases.Principal { return databases.Principal{Roles: []string{"users", "user:u1"}} }

// TestListChanges_OrderSinceAndTombstone：seq 升序、since 过滤、delete 为
// tombstone（无 data）。
func TestListChanges_OrderSinceAndTombstone(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	r1 := publishChange(t, db, "d1", domainevents.EventDocumentsCreate, "u1")
	r2 := publishChange(t, db, "d2", domainevents.EventDocumentsCreate, "u1")
	r3 := publishChange(t, db, "d1", domainevents.EventDocumentsDelete, "u1")

	// since=0：全部三事件升序。
	changes, hasMore, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{}, u1Principal())
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, changes, 3)
	require.Equal(t, r1.Seq, changes[0].Seq)
	require.Equal(t, r2.Seq, changes[1].Seq)
	require.Equal(t, r3.Seq, changes[2].Seq)
	require.NotNil(t, changes[0].Data, "create 事件带写后全文档")
	require.Nil(t, changes[2].Data, "delete 事件无 data（tombstone）")
	require.Equal(t, "d1", changes[2].DocumentID)

	// since=r1：仅 r2、r3。
	changes, hasMore, err = docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{SinceSeq: r1.Seq}, u1Principal())
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, changes, 2)
	require.Equal(t, r2.Seq, changes[0].Seq)
}

// TestListChanges_VisibilityFilter：按请求者过滤（快照 ACL + 当前
// principal——与 hub 扇出同语义）。
func TestListChanges_VisibilityFilter(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	publishChange(t, db, "d1", domainevents.EventDocumentsCreate, "u1")
	publishChange(t, db, "d2", domainevents.EventDocumentsCreate, "u2")

	changes, _, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{}, u1Principal())
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "d1", changes[0].DocumentID)

	// 特权主体旁路（BypassesDocumentACL）。
	admin := databases.Principal{Roles: []string{"__system__"}}
	changes, _, err = docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{}, admin)
	require.NoError(t, err)
	require.Len(t, changes, 2)
}

// TestListChanges_LimitAndHasMore：limit 截断 + has_more + 末条 seq 续传链。
func TestListChanges_LimitAndHasMore(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	var lastSeq int64
	for i := 0; i < 5; i++ {
		lastSeq = publishChange(t, db, "d", domainevents.EventDocumentsCreate, "u1").Seq
	}

	changes, hasMore, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{Limit: 2}, u1Principal())
	require.NoError(t, err)
	require.Len(t, changes, 2)
	require.True(t, hasMore)

	// 续传链：以末条 seq 为 since 直至 has_more=false。
	since := changes[1].Seq
	got := len(changes)
	for {
		next, more, err := docDB.ListChanges(ctx, "default", "app", "docs",
			databases.ListChangesOptions{SinceSeq: since, Limit: 2}, u1Principal())
		require.NoError(t, err)
		got += len(next)
		if !more {
			break
		}
		since = next[len(next)-1].Seq
	}
	require.Equal(t, 5, got, "续传链必须无重无漏覆盖全部 5 条")

	// since=最末：空集、无更多。
	changes, hasMore, err = docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{SinceSeq: lastSeq}, u1Principal())
	require.NoError(t, err)
	require.Empty(t, changes)
	require.False(t, hasMore)
}

// TestListChanges_DocumentFilter：documentID 过滤（WS 文档频道重放）。
func TestListChanges_DocumentFilter(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	publishChange(t, db, "d1", domainevents.EventDocumentsCreate, "u1")
	publishChange(t, db, "d2", domainevents.EventDocumentsCreate, "u1")
	publishChange(t, db, "d1", domainevents.EventDocumentsUpdate, "u1")

	changes, _, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{DocumentID: "d1"}, u1Principal())
	require.NoError(t, err)
	require.Len(t, changes, 2)
	for _, c := range changes {
		require.Equal(t, "d1", c.DocumentID)
	}
}

// TestListChanges_ResumeExpired：since_seq 早于集合最老可用事件（模拟
// 保留清理删旧行）→ ErrResumeExpired；since=0 不判过期。
func TestListChanges_ResumeExpired(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	r1 := publishChange(t, db, "d1", domainevents.EventDocumentsCreate, "u1")
	r2 := publishChange(t, db, "d2", domainevents.EventDocumentsCreate, "u1")

	// 模拟清理：删掉最老行。
	_, err := db.Conn(ctx).NewDelete().Model((*model.DocumentEventsOutbox)(nil)).
		Where("seq = ?", r1.Seq).Exec(ctx)
	require.NoError(t, err)

	// since=r1（早于现存最老 r2）→ 过期。
	_, _, err = docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{SinceSeq: r1.Seq}, u1Principal())
	require.ErrorIs(t, err, databases.ErrResumeExpired)

	// since=0：从最老可用事件起，不判过期。
	changes, _, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{}, u1Principal())
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, r2.Seq, changes[0].Seq)

	// since=r2：正常续传（空集）。
	_, _, err = docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{SinceSeq: r2.Seq}, u1Principal())
	require.NoError(t, err)
}

// TestListChanges_TransactionIDPassthrough：批标识随事件透出。
func TestListChanges_TransactionIDPassthrough(t *testing.T) {
	docDB, db := changesEnv(t)
	ctx := context.Background()

	row := publishChange(t, db, "d1", domainevents.EventDocumentsCreate, "u1")
	// publishChange 走单文档路径（无 ctx 批标识）——transaction_id 空。
	changes, _, err := docDB.ListChanges(ctx, "default", "app", "docs",
		databases.ListChangesOptions{}, u1Principal())
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Empty(t, changes[0].TransactionID)
	require.Equal(t, row.Seq, changes[0].Seq)
	require.NotEmpty(t, changes[0].EventID)
}
