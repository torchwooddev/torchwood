package clients

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/pkg/uow"
	"github.com/uptrace/bun"
)

var _ uow.Runner = (*Database)(nil)

type txContextKey struct{}

// WithTx stores a bun transaction in context for repository adapters.
func WithTx(ctx context.Context, tx bun.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// Conn returns the active transaction when present, otherwise the root database.
func (d *Database) Conn(ctx context.Context) bun.IDB {
	if tx, ok := ctx.Value(txContextKey{}).(bun.Tx); ok {
		return tx
	}
	return d.DB
}

// InTx reports whether ctx carries an active transaction (see WithTx).
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txContextKey{}).(bun.Tx)
	return ok
}

// ---------------------------------------------------------------------------
// 执行身份（redesign §3.2 工程纪律 / A1，阶段③包 B）
// ---------------------------------------------------------------------------

// DB 角色分层（§4.3）：单一变色龙 authenticator（现有 DSN 用户，成员含三角色）
// + 事务首条 SET LOCAL ROLE 切换，替代多连接池（PostgREST authenticator 模式）。
const (
	// RoleApp 是运行时角色（非 owner 无 BYPASSRLS）——文档 CRUD/查询事务用；
	// 对 c_* 表的 DML 由建表路径按列授予（阶段③包 C）。
	RoleApp = "tw_app"
	// RoleOwner 是 DDL/迁移专用角色（catalog DML、CREATE ON DATABASE），
	// 不跑业务查询。
	RoleOwner = "tw_owner"
	// RoleSystem 是内部旁路角色（BYPASSRLS）——SystemPrincipal / PlatformAdmin。
	RoleSystem = "tw_system"
)

// rolesSeparator 是 app.roles GUC 的元素分隔符（\x1f 控制字符，不出现在
// 角色字符串中；SQL 侧 string_to_array(..., chr(31)) 对应解包）。
const rolesSeparator = "\x1f"

// ExecIdentity 是事务的执行身份：SET LOCAL ROLE 的目标角色 + app.roles GUC
// 的展开角色集（ExpandPermissionRoles 产物）。漏注入 roles → GUC 缺省 NULL →
// RLS policy 恒 false（fail-closed）；SET LOCAL 事务结束自动失效零残留。
type ExecIdentity struct {
	Role  string
	Roles []string
}

type execIdentityKey struct{}

// WithExecIdentity 把执行身份挂入 ctx（documentdb 的 withDocumentTx 入口构造）。
func WithExecIdentity(ctx context.Context, id ExecIdentity) context.Context {
	return context.WithValue(ctx, execIdentityKey{}, id)
}

// ExecIdentityFrom 读取 ctx 的执行身份；未注入时 ok=false。
func ExecIdentityFrom(ctx context.Context) (ExecIdentity, bool) {
	id, ok := ctx.Value(execIdentityKey{}).(ExecIdentity)
	return id, ok
}

// SameExecIdentity 报告两个身份是否等价（角色与展开角色集一致）。
func SameExecIdentity(a, b ExecIdentity) bool {
	if a.Role != b.Role || len(a.Roles) != len(b.Roles) {
		return false
	}
	for i := range a.Roles {
		if a.Roles[i] != b.Roles[i] {
			return false
		}
	}
	return true
}

// InjectExecIdentity 在当前事务连接上注入执行身份（SET LOCAL ROLE + set_config）。
// 供两类调用方使用：RunInTx 的 BEGIN 后首注，与事务中段的身份切换（如尾随
// 读回 SystemPrincipal）；中段切换的调用方负责退出前恢复（ResetExecIdentity
// 或回注外层身份），保证 ctx 身份与连接角色在边界一致。
func InjectExecIdentity(ctx context.Context, idb bun.IDB, id ExecIdentity) error {
	switch id.Role {
	case RoleApp, RoleOwner, RoleSystem:
	default:
		// 角色名拼入 SQL：仅接受固定白名单，拒绝任意输入。
		return fmt.Errorf("invalid exec role %q", id.Role)
	}
	// pgdriver 全程 simple protocol（客户端插参）：两条注入语句合并为单次
	// 往返（A1 原型验证结论）。
	_, err := idb.ExecContext(ctx,
		fmt.Sprintf(`SET LOCAL ROLE %s; SELECT set_config('app.roles', ?, true)`, id.Role),
		strings.Join(id.Roles, rolesSeparator),
	)
	if err != nil {
		return fmt.Errorf("inject exec identity %s: %w", id.Role, err)
	}
	return nil
}

// ResetExecIdentity 把连接恢复为 authenticator 本身份（RESET ROLE）并清空
// app.roles——用于无外层身份的事务中段切换退出（复合 uow 事务回到 DSN 用户）。
func ResetExecIdentity(ctx context.Context, idb bun.IDB) error {
	if _, err := idb.ExecContext(ctx,
		`RESET ROLE; SELECT set_config('app.roles', '', true)`,
	); err != nil {
		return fmt.Errorf("reset exec identity: %w", err)
	}
	return nil
}

// Run 实现 uow.Runner：已在工作单元内则加入，否则开启新事务。
// 委托 RunInTx，嵌套行为不变。
func (d *Database) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.RunInTx(ctx, fn)
}

// RunInTx runs fn inside a database transaction. When ctx already carries an
// active transaction (see WithTx), fn runs on that transaction instead of
// opening a nested one — nested transactions on separate connections would
// deadlock on DDL locks held by the outer transaction.
// ctx 带 ExecIdentity 时（阶段③包 B），BEGIN 后立即注入 SET LOCAL ROLE +
// app.roles（每请求一事务，A1）；未带身份的事务保持 authenticator 本身份
//（静态系统表面 = 边界邻居，独立应用层授权，redesign 预决策 9）。
func (d *Database) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if InTx(ctx) {
		return fn(ctx)
	}
	return d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if id, ok := ExecIdentityFrom(ctx); ok {
			if err := InjectExecIdentity(ctx, tx, id); err != nil {
				return err
			}
		}
		return fn(WithTx(ctx, tx))
	})
}

// RunInNewTx runs fn in a fresh transaction even when ctx already carries one.
// 用于「子操作必须先于外层事务提交」的路径（如订单 + provider index 在渠道
// 下单之前 COMMIT，见设计 §9.2）：外层失败回滚自己的部分，子事务保持已提交。
// 调用方自行确保两张事务不触碰会互相加锁的行。
// 与 RunInTx 同规则注入 ExecIdentity（未带身份则保持 authenticator）。
func (d *Database) RunInNewTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if id, ok := ExecIdentityFrom(ctx); ok {
			if err := InjectExecIdentity(ctx, tx, id); err != nil {
				return err
			}
		}
		return fn(WithTx(ctx, tx))
	})
}
