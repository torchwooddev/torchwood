// 启动钩子接线测试（转出 POC 门禁 A1）：CollectionGrantsReconcileHook 必须
// 注册进 NewOnStarts（cmd/server 与 cmd/worker 共享装配），且执行钩子等价于
// 执行全量扫描——构造授权偏离终态的表，跑完钩子后恢复终态。
package bootkit

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func TestCollectionGrantsReconcileHook_WiredInOnStarts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	// 构造授权故意偏离终态的业务集合表。
	p := documentdb.NewPostgresDocumentDB(db, nil)
	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 0)
	t.Cleanup(cleanup)
	require.NoError(t, p.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, p.CreateCollection(ctx, projectID, "app", "docs", "Docs",
		[]databases.Attribute{{ID: "title", Key: "title", Type: "string", Size: 256}},
		nil, []databases.Permission{{Type: "read", Role: "any"}}, true))
	schema, err := ident.SchemaName(projectID, "app")
	require.NoError(t, err)
	var physical string
	require.NoError(t, db.NewSelect().TableExpr("public.catalog_collections").
		Column("physical_name").
		Where("project_id = ? AND database_id = ? AND collection_id = ?", projectID, "app", "docs").
		Scan(ctx, &physical))
	tbl := schema + "." + physical
	_, err = db.ExecContext(ctx,
		fmt.Sprintf(`GRANT UPDATE (_acl) ON %s TO tw_app`, tbl))
	require.NoError(t, err, "偏离播种：R13a 旧形态多授")

	// 接线断言：三钩子装配（schema ensure / roles sig / grants reconcile）。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hooks := NewOnStarts(nil, db, logger)
	require.Len(t, hooks, 3, "NewOnStarts 必须包含列授权 reconcile 钩子（A1 接线锁定）")
	for i, hook := range hooks {
		require.NoError(t, hook(ctx), "hook %d", i)
	}

	// 钩子执行后偏离被矫正：column_privileges 回到终态（UPDATE 排除 _acl）。
	rows, err := db.QueryContext(ctx,
		`SELECT column_name || ':' || privilege_type
		 FROM information_schema.column_privileges
		 WHERE table_schema = ? AND table_name = ? AND grantee = 'tw_app'
		 ORDER BY column_name, privilege_type`, schema, physical)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var grants []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		grants = append(grants, s)
	}
	require.NoError(t, rows.Err())
	sort.Strings(grants)
	require.NotContains(t, grants, "_acl:UPDATE", "钩子执行后 R13a 旧形态多授必须被收回")
	require.Contains(t, grants, "title:UPDATE", "钩子执行后数据列 UPDATE 授权必须在场")
}
