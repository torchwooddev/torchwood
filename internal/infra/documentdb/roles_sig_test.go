// roles_sig 验签测试（阶段③-b 包 C，redesign §3.2/§11-J A2 简化版）：
// tw_roles() 是 SECURITY DEFINER 验签函数——仅 tw_app 身份、sig =
// HMAC-SHA256(密钥, roles||'|'||exp) 未过期时返回角色；三态 fail-closed
//（无 sig / 错 sig / 过期 sig → 零角色，与漏注入同语义）。附
// tw_set_document_acl 的注入面（p_table 白名单拒绝）。
package documentdb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// setupRolesSigFixture 建业务集合（集合级 read:any，docsec=true）+ 一行空 _acl
// 文档：可见性完全取决于 tw_roles() 能否解包出角色（fastPath 要求
// cardinality(roles) > 0 且集合级 read）。
func setupRolesSigFixture(ctx context.Context, t *testing.T) (*postgresDocumentDB, *clients.Database, string, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	docDB := NewPostgresDocumentDB(db, nil).(*postgresDocumentDB)
	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 0)
	t.Cleanup(cleanup)
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "update", Role: "any"},
	}, true))
	_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		ID: "r1", Data: map[string]any{"title": "visible"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	physical := testPhysicalName(t, ctx, db, projectID, "app", "docs")
	return docDB, db, projectID, testSchema(t, projectID, "app") + "." + physical
}

// asAppWithGUC 以 tw_app 身份 + 手工 GUC（绕过 InjectExecIdentity 的签名）
// 执行 fn——用于伪造场景。
func asAppWithGUC(ctx context.Context, db *clients.Database, roles, sig string, fn func(txCtx context.Context) error) error {
	return db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := db.Conn(txCtx).ExecContext(txCtx,
			`SET LOCAL ROLE tw_app; SELECT set_config('app.roles', ?, true), set_config('app.roles_sig', ?, true)`,
			roles, sig); err != nil {
			return err
		}
		return fn(txCtx)
	})
}

// countVisible 在当前 tw_app 会话下统计可见行（经 SELECT policy）。
func countVisible(t *testing.T, db *clients.Database, tbl string) func(ctx context.Context) int {
	return func(ctx context.Context) int {
		var n int
		require.NoError(t, db.Conn(ctx).QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tbl)).Scan(&n))
		return n
	}
}

func probeRoles(t *testing.T, db *clients.Database) func(ctx context.Context) int {
	return func(ctx context.Context) int {
		var n int
		require.NoError(t, db.Conn(ctx).QueryRowContext(ctx,
			`SELECT cardinality(public.tw_roles())`).Scan(&n))
		return n
	}
}

// TestRolesSig_FailClosed 三态：无 sig / 错 sig / 过期 sig → 零角色 + 不可见；
// 合法注入（InjectExecIdentity 自动签名）→ 角色解包 + 可见。
func TestRolesSig_FailClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	_, db, _, tbl := setupRolesSigFixture(ctx, t)
	count := countVisible(t, db, tbl)
	roles := probeRoles(t, db)
	keyHex, ok := clients.RolesSigKeyHex()
	require.True(t, ok, "testutil.SetupTestDB 已初始化签名密钥")

	// 态①：无 sig（GUC 缺失）——手工伪造 roles 的老通道，验签后封死。
	require.NoError(t, asAppWithGUC(ctx, db, "any", "", func(txCtx context.Context) error {
		require.Zero(t, roles(txCtx), "无 sig 必须解包为零角色")
		require.Zero(t, count(txCtx), "零角色 → policy 恒 false → 不可见")
		return nil
	}))

	// 态②：错 sig（格式合法、mac 不匹配）。
	require.NoError(t, asAppWithGUC(ctx, db, "any", "9999999999|deadbeef", func(txCtx context.Context) error {
		require.Zero(t, roles(txCtx))
		require.Zero(t, count(txCtx))
		return nil
	}))

	// 态③：过期 sig（密钥正确、exp 在过去）。
	expired := clients.SignRolesSig(keyHex, "any", time.Now().Add(-2*time.Minute))
	require.NoError(t, asAppWithGUC(ctx, db, "any", expired, func(txCtx context.Context) error {
		require.Zero(t, roles(txCtx), "过期 sig 必须解包为零角色")
		require.Zero(t, count(txCtx))
		return nil
	}))

	// 篡改 roles（用 A 的签名配 B 的 roles）→ 验签失败。
	require.NoError(t, asAppWithGUC(ctx, db, "users", clients.SignRolesSig(keyHex, "any", time.Now()), func(txCtx context.Context) error {
		require.Zero(t, roles(txCtx))
		return nil
	}))

	// 合法注入：InjectExecIdentity 自动签名 → 角色解包 + 可见（回归全链路）。
	require.NoError(t, db.RunInTx(clients.WithExecIdentity(ctx, clients.ExecIdentity{
		Role: clients.RoleApp, Roles: []string{"any"},
	}), func(txCtx context.Context) error {
		require.Equal(t, 1, roles(txCtx), "合法 sig 必须解包出角色")
		require.Equal(t, 1, count(txCtx), "角色有效 → policy 放行 → 可见")
		return nil
	}))

	// 非 tw_app 身份（authenticator 本体，role setting = none）即使 sig 合法
	// 也不得经 tw_roles() 取得角色。
	require.NoError(t, db.RunInTx(ctx, func(txCtx context.Context) error {
		_, err := db.Conn(txCtx).ExecContext(txCtx,
			`SELECT set_config('app.roles', ?, true), set_config('app.roles_sig', ?, true)`,
			"any", clients.SignRolesSig(keyHex, "any", time.Now()))
		if err != nil {
			return err
		}
		var n int
		require.NoError(t, db.Conn(txCtx).QueryRowContext(txCtx,
			`SELECT cardinality(public.tw_roles())`).Scan(&n))
		require.Zero(t, n, "仅 tw_app 身份验签；其余身份零角色")
		return nil
	}))
}

// TestRolesSig_LegitPathRegression：合法注入路径全链路（创建→读→改权限→删）
// 经 docDB 公共 API 回归——sig 随 InjectExecIdentity 自动注入，无须调用方感知。
func TestRolesSig_LegitPathRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, _, projectID, _ := setupRolesSigFixture(ctx, t)
	alice := databases.Principal{Roles: []string{"users", "user:alice"}}

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", "r1", alice)
	require.NoError(t, err)
	require.NotNil(t, got, "集合级 read:any + 空 _acl → 可见（sig 验签通过）")

	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{}, alice)
	require.NoError(t, err)
	require.Equal(t, int64(1), list.TotalCount)

	// 权限替换（tw_set_document_acl 路径）+ 自锁后读回（system 尾随读回）。
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document:        databases.Document{ID: "r1", Data: map[string]any{"title": "v2"}},
		Permissions:     []databases.Permission{{Type: "read", Role: "user:alice"}},
		ExpectedVersion: got.Version,
	}, alice)
	require.NoError(t, err)
	require.Equal(t, "v2", updated.Data["title"])
	require.Equal(t, []databases.Permission{{Type: "read", Role: "user:alice"}}, updated.Permissions)

	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "app", "docs", "r1", databases.DeleteOptions{ExpectedVersion: updated.Version}, databases.SystemPrincipal))
}

// TestSetDocumentACL_InjectionSurface：伪造 p_table（不在 catalog physical_name
// 白名单）→ 函数拒绝（42704），事务回滚；EXECUTE 面对 tw_app 开放（合法通道）。
func TestSetDocumentACL_InjectionSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	_, db, _, tbl := setupRolesSigFixture(ctx, t)

	err := db.RunInTx(clients.WithExecIdentity(ctx, clients.ExecIdentity{
		Role: clients.RoleApp, Roles: []string{"any"},
	}), func(txCtx context.Context) error {
		var n int
		return db.Conn(txCtx).QueryRowContext(txCtx,
			`SELECT public.tw_set_document_acl(?, ?, ?, ?, ?::text[])`,
			"public", "pg_class", 1, "r1", `{read:any}`).Scan(&n)
	})
	require.Error(t, err, "伪造表名必须被 catalog 白名单拒绝")
	require.Contains(t, err.Error(), "unknown collection table")

	// 白名单外的 schema + 合法物理名（catalog 命中）在错误 schema 下不可达：
	// %I 引证保证无注入，表不存在即报错（不产生任何写入）。
	err = db.RunInTx(clients.WithExecIdentity(ctx, clients.ExecIdentity{
		Role: clients.RoleApp, Roles: []string{"any"},
	}), func(txCtx context.Context) error {
		var n int
		physical := tbl[strings.LastIndex(tbl, ".")+1:]
		return db.Conn(txCtx).QueryRowContext(txCtx,
			`SELECT public.tw_set_document_acl(?, ?, ?, ?, ?::text[])`,
			"public", physical, 1, "r1", `{read:any}`).Scan(&n)
	})
	require.Error(t, err)
}
