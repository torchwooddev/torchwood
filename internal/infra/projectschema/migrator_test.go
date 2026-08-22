package projectschema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func TestApply_IdempotentCatalogAndOAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	require.NoError(t, projectschema.Apply(ctx, db, projectID))
	require.NoError(t, projectschema.Apply(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(9), version)

	var dirty bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT COALESCE(bool_or(dirty), false) FROM "+quoted+".schema_migrations").Scan(&dirty))
	require.False(t, dirty)

	for _, table := range []string{
		"document_databases", "document_collections", "document_attributes", "document_indexes",
		"project_oauth_providers", "functions", "function_deployments", "function_variables", "function_executions",
		"payment_orders", "asset_defs", "subscriptions", "usage_rollups", "billing_statements",
		"users", "sessions", "identities", "groups", "memberships", "buckets", "files",
	} {
		var reg any
		require.NoError(t, db.DB.QueryRowContext(ctx,
			`SELECT to_regclass(?)`, quoted+"."+table).Scan(&reg), table)
		require.NotNil(t, reg, "expected %s.%s", quoted, table)
	}

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	var ns any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, schema).Scan(&ns))
	require.NotNil(t, ns)
}

func TestApply_RejectsInvalidProjectID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	require.Error(t, projectschema.Apply(ctx, db, "_"))
	require.Error(t, projectschema.Apply(ctx, db, "Default"))
	require.Error(t, projectschema.Apply(ctx, db, ""))
}

func TestEnsureAll_AppliesListedProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	p1, _, c1 := testutil.CreateTestProject(ctx, db)
	defer c1()
	p2, _, c2 := testutil.CreateTestProject(ctx, db)
	defer c2()

	require.NoError(t, projectschema.EnsureAll(ctx, db, []string{p1, p2}))
	require.Error(t, projectschema.EnsureAll(ctx, db, []string{p1, "_"}))
}

func TestKickoffEnsureAll_EmptyIsNoop(t *testing.T) {
	projectschema.KickoffEnsureAll(nil, nil, nil)
	projectschema.KickoffEnsureAll(nil, []string{}, nil)
}

// TestApply_FailureMarksDirtyPersistently 注入坏 DDL（把 subscriptions 表替换
// 为缺列残缺版，重跑 000006 时 CREATE INDEX 失败），断言：
//  1. Apply 返回错误且事务回滚（无 version=6 成功行）；
//  2. dirty 标记经独立连接持久化（事务 ROLLBACK 撤不掉）；
//  3. 后续 Apply 拒绝脏项目，不再尝试后续版本。
func TestApply_FailureMarksDirtyPersistently(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	quoted := testutil.CatalogQuoted(projectID)
	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)

	// 回退到「已应用 1–5」：删 6/7/8 版本行（否则 MAX 停在 8，Apply 会跳过 000006），
	// 把 subscriptions 表替换为缺列残缺版（provider/provider_sub_id 不存在），
	// 令 000006 的 subscriptions_provider_sub 建索引失败。
	//
	// CreateTestProject 已做过全量 Apply，就绪缓存命中会直通跳过迁移检查——
	// 迁移器之外的带外 schema 状态改动必须先 Invalidate（与项目删除路径
	// 同一契约）。
	projectschema.Invalidate(db, projectID)
	_, err = db.DB.ExecContext(ctx,
		`DELETE FROM `+quoted+`.schema_migrations WHERE version IN (6, 7, 8, 9)`)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `DROP TABLE `+quoted+`.subscriptions CASCADE`)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `CREATE TABLE `+quoted+`.subscriptions (id TEXT PRIMARY KEY)`)
	require.NoError(t, err)

	err = projectschema.Apply(ctx, db, projectID)
	require.ErrorContains(t, err, "apply 000006")

	// 事务内无成功行：version 6 未被记为 applied。
	var applied6 bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+quoted+`.schema_migrations WHERE version = 6 AND NOT dirty)`).Scan(&applied6))
	require.False(t, applied6)

	// dirty 标记在 ROLLBACK 之后仍持久可见（独立连接补写）。
	var dirty bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COALESCE(bool_or(dirty), false) FROM `+quoted+`.schema_migrations`).Scan(&dirty))
	require.True(t, dirty)

	// 后续 Apply 拒绝脏项目。
	err = projectschema.Apply(ctx, db, projectID)
	require.ErrorContains(t, err, "is dirty")
	require.Contains(t, err.Error(), schema)
}

// TestApply_ReadyCacheAndInvalidate 验证就绪缓存契约（2026-08 评审 P0-4）：
// Apply 成功后缓存命中直通（外部 DROP SCHEMA 后不再重建）；Invalidate 清除
// 缓存后 Apply 重建。这正是项目删除（DROP SCHEMA）路径必须调用 Invalidate
// 的原因——否则同 ID 重建项目时缓存直通会跳过 schema 重建。
func TestApply_ReadyCacheAndInvalidate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	// 显式 Apply 确保本进程缓存条目存在。
	require.NoError(t, projectschema.Apply(ctx, db, projectID))

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	quoted := `"` + schema + `"`

	// 模拟项目删除路径的外部 DROP。
	_, err = db.DB.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoted+` CASCADE`)
	require.NoError(t, err)

	// 缓存命中：Apply 直通，不重建。
	require.NoError(t, projectschema.Apply(ctx, db, projectID))
	var ns any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, schema).Scan(&ns))
	require.Nil(t, ns, "缓存命中时不得重建 schema")

	// Invalidate 后 Apply 重建。
	projectschema.Invalidate(db, projectID)
	require.NoError(t, projectschema.Apply(ctx, db, projectID))
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, schema).Scan(&ns))
	require.NotNil(t, ns, "Invalidate 后 Apply 必须重建 schema")
}
