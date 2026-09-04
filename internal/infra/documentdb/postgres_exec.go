// 执行身份包装（redesign §3.2 工程纪律 / A1，阶段③包 B）：文档面所有入口
// 经 withDocumentTx/withOwnerTx 包进带身份的显式事务——每请求一事务（含读，
// autocommit 退役），事务首条 SET LOCAL ROLE + set_config('app.roles') 注入
//（漏注入 = RLS policy 恒 false，fail-closed；policy 本身在包 C 落地）。
package documentdb

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

// execIdentityFor 把文档主体映射为执行身份（§3.2 #8：系统/平台管理员旁路走
// 独立 BYPASSRLS 连接角色，绝不编码进 GUC 或 policy 白名单）：
// BypassesDocumentACL（SystemPrincipal / PlatformAdmin）→ tw_system；
// 其余 → tw_app。app.roles 注入 ExpandPermissionRoles 展开角色（与
// AllowsDocumentAccess 判定同源输入）。
func execIdentityFor(principal databases.Principal) clients.ExecIdentity {
	expanded := databases.ExpandPermissionRoles(principal.Roles)
	if principal.BypassesDocumentACL() {
		return clients.ExecIdentity{Role: clients.RoleSystem, Roles: expanded}
	}
	return clients.ExecIdentity{Role: clients.RoleApp, Roles: expanded}
}

// systemExecIdentity 是尾随读回（D5）与事件快照取数用的系统身份。
func systemExecIdentity() clients.ExecIdentity {
	return clients.ExecIdentity{Role: clients.RoleSystem, Roles: databases.ExpandPermissionRoles(databases.SystemPrincipal.Roles)}
}

// withDocumentTx 把文档面操作包进带执行身份的事务（读写同构，A1）。
// 已在外层事务中：外层身份一致时直接执行（零额外语句）；不一致（如尾随读回
// SystemPrincipal）则事务中段切换并在退出前恢复外层身份——ctx 身份与连接
// 角色在边界保持一致，中途切换不向调用方泄漏；外层无身份（复合 uow 事务）
// 则切换后 RESET ROLE 回 authenticator。
func (p *postgresDocumentDB) withDocumentTx(ctx context.Context, id clients.ExecIdentity, fn func(ctx context.Context) error) error {
	if clients.InTx(ctx) {
		cur, hasCur := clients.ExecIdentityFrom(ctx)
		if hasCur && clients.SameExecIdentity(cur, id) {
			return fn(ctx)
		}
		if err := clients.InjectExecIdentity(ctx, p.conn(ctx), id); err != nil {
			return err
		}
		err := fn(clients.WithExecIdentity(ctx, id))
		var restoreErr error
		if hasCur {
			restoreErr = clients.InjectExecIdentity(ctx, p.conn(ctx), cur)
		} else {
			restoreErr = clients.ResetExecIdentity(ctx, p.conn(ctx))
		}
		if err != nil {
			return err
		}
		return restoreErr
	}
	return p.db.RunInTx(clients.WithExecIdentity(ctx, id), fn)
}

// withOwnerTx 把 DDL 面操作包进 tw_owner 事务（catalog DML + CREATE ON
// DATABASE）。嵌套语义与 withDocumentTx 相同（中段切换 + 退出恢复）。
func (p *postgresDocumentDB) withOwnerTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return p.withDocumentTx(ctx, clients.ExecIdentity{Role: clients.RoleOwner}, fn)
}
