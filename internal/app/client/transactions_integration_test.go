package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// clientTxUserCtx 返回携带端用户 principal（含 project 绑定）的上下文。
func clientTxUserCtx(ctx context.Context, projectID, userID string) context.Context {
	return contexts.WithPrincipal(ctx, &domainshared.Principal{
		ActorID:   domainshared.Principal{}.ActorID,
		ActorKind: domainshared.ActorKindEndUser,
		ProjectID: projectID,
		UserID:    userID,
		Roles:     []string{"users", "user:" + userID},
	})
}

// TestClientTransactions_CommitWithOwnerPerms 覆盖 Client 事务包装层：
// 追加 create 不带 permissions → 归一化为 owner 默认权限；Commit 落库后
// 文档 _perms 仅含创建者；非创建者 Commit → PermissionDenied（v2 设计 §5.2）。
func TestClientTransactions_CommitWithOwnerPerms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, infraevents.NewEventOutbox(db))
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, databases.DefaultCollectionPermissions(), true))

	projectRepo := bunrepo.NewProjectRepository(db)
	uc := NewTransactions(projectRepo, shared.NewTransactions(bunrepo.NewTransactionRepository(db), docDB, db))

	u1Ctx := clientTxUserCtx(ctx, projectID, "u1")
	tx, err := uc.CreateTransaction(u1Ctx, "app")
	require.NoError(t, err)
	require.Equal(t, "user:u1", tx.CreatedBy)
	require.Equal(t, databases.TransactionStatusPending, tx.Status)

	op, err := uc.CreateTransactionDocument(u1Ctx, "app", tx.ID, "docs", "d1", map[string]any{"title": "hello"}, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), op.Seq)
	// Client 侧归一化：空 permissions → owner 默认权限。
	require.Len(t, op.Permissions, 3)

	committed, ops, err := uc.CommitTransaction(u1Ctx, "app", tx.ID)
	require.NoError(t, err)
	require.Equal(t, databases.TransactionStatusCommitted, committed.Status)
	require.Len(t, ops, 1)

	doc, err := docDB.GetDocument(ctx, projectID, "app", "docs", "d1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, int64(1), doc.Version)
	for _, p := range doc.Permissions {
		require.Equal(t, "user:u1", p.Role)
	}

	// 非创建者 Commit 他人的第二笔事务 → PermissionDenied。
	tx2, err := uc.CreateTransaction(u1Ctx, "app")
	require.NoError(t, err)
	_, _, err = uc.CommitTransaction(clientTxUserCtx(ctx, projectID, "u2"), "app", tx2.ID)
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}
