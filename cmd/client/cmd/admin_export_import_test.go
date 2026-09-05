package cmd

import (
	"bytes"
	"context"
	"net/url"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/infra/events"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// executeAdminCmd 设置 flags 并执行（CLI 子命令集成路径：cobra → RunE →
// 直连 DB → documentdb 导出/导入执行体）。
func executeAdminCmd(t *testing.T, cmd *cobra.Command, flags map[string]string) {
	t.Helper()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	for k, v := range flags {
		require.NoError(t, cmd.Flags().Set(k, v))
	}
	require.NoError(t, cmd.Execute())
}

// TestAdminExportImportRoundTripViaCLI 是 B5 CLI 子命令集成测试（有测试库
// 时真跑，未设置 TORCHWOOD_TEST_* 时 skip，与仓库集成测试惯例一致）：
// 经 cobra 命令完整执行 export → drop → import，验证库/集合/行恢复。
// 命令体直连 DSN（--dsn），不经 InvokeJSON/API 面——import_guard 允许
//（禁的是 genproto/grpc/protobuf 字面 import）。
func TestAdminExportImportRoundTripViaCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dsn := testutil.TestDSN()
	if dsn == "" || testutil.AdminDSN() == "" {
		t.Skip("TORCHWOOD_TEST_* not set (run via `task test`, which loads .env)")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	// CLI 命令直连的 dsn 指向 SetupTestDB 的动态隔离库（current_database()
	// 回读库名，替换 base DSN 的 path）。
	var dbName string
	require.NoError(t, db.NewSelect().ColumnExpr("current_database()").Scan(ctx, &dbName))
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + dbName
	dsn = u.String()

	p := documentdb.NewPostgresDocumentDB(db, events.NewEventOutbox(db))
	projectID, _, cleanup := testutil.CreateTestProjectT(ctx, t, db)
	t.Cleanup(cleanup)
	require.NoError(t, p.CreateDatabase(ctx, projectID, "app", "App"))
	require.NoError(t, p.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 128},
	}, nil, nil, true))
	for i := 0; i < 3; i++ {
		_, err := p.CreateDocument(ctx, projectID, "app", "notes", databases.Document{
			Data: map[string]any{"title": "cli-" + string(rune('a'+i))},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	outDir := t.TempDir()
	executeAdminCmd(t, newAdminExportCmd(), map[string]string{
		"project": projectID,
		"out":     outDir,
		"dsn":     dsn,
	})

	// drop 后经 CLI import 恢复。
	require.NoError(t, p.DeleteDatabase(ctx, projectID, "app"))
	executeAdminCmd(t, newAdminImportCmd(), map[string]string{
		"project": projectID,
		"in":      outDir,
		"dsn":     dsn,
	})

	// 恢复断言：catalog 行 + 3 行文档（经现役查询路径，RLS/列授权健全）。
	dbCount, err := db.NewSelect().Model((*model.DocumentDatabase)(nil)).
		Where("project_id = ? AND database_id = ?", projectID, "app").Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, dbCount)
	collCount, err := db.NewSelect().Model((*model.DocumentCollection)(nil)).
		Where("project_id = ? AND database_id = ?", projectID, "app").Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, collCount)
	list, err := p.ListDocuments(ctx, projectID, "app", "notes", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)
}
