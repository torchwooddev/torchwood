// Package bootkit 收纳 cmd/server 与 cmd/worker 两个组装根共享的 Lynx
// 装配样板（Round4 J4-1 去重）：日志暴露、组件注册顺序、项目 schema
// 启动钩子。两处逐字复制的实现统一收敛于此，防止后续再次分叉。
package bootkit

import (
	"context"
	"log/slog"

	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
)

// NewLogger 暴露 app 装配的 *slog.Logger（zap 后端），供各层构造器注入。
func NewLogger(app lynx.App) *slog.Logger {
	return app.Logger()
}

// NewComponentBuilders 返回空工厂集（服务均为静态构造，无延迟装配需求）。
func NewComponentBuilders() []lynx.ServiceFactory {
	return nil
}

// NewOnStarts 注册启动钩子集：项目 schema 确保钩子（对全部项目幂等 EnsureAll）、
// roles 签名密钥同步钩子（把进程内派生的 roles 签名密钥 UPSERT 进
// tw_secrets，阶段③-b 包 C：tw_roles() 验签依赖）与调用方注入的可选扩展钩子。
// grantsReconcile 由组合根注入（cmd/server 传 documentdb 列授权全量 reconcile
// 闭包，门禁 A1；cmd/worker 传 nil——worker 不碰文档层，import guard 守此边界）。
func NewOnStarts(repo projects.Repository, db *clients.Database, logger *slog.Logger, grantsReconcile func(context.Context) error) boot.OnStartHooks {
	hooks := boot.OnStartHooks{
		ProjectSchemaEnsureHook(repo, db, logger),
		RolesSigKeySyncHook(db, logger),
	}
	if grantsReconcile != nil {
		hooks = append(hooks, lynx.HookFunc(grantsReconcile))
	}
	return hooks
}

// NewOnStops 返回空钩子集：底层资源清理不进 Lynx（见 cmd/*/main.go 注释，
// 在 runner.RunE() 返回后由 main 统一执行）。
func NewOnStops() boot.OnStopHooks {
	return boot.OnStopHooks{}
}

// ProjectSchemaEnsureHook 返回启动期项目数据面 schema 确保钩子：
// 列出全部项目并对每个 tw_<project.id> 幂等执行 EnsureAll。
func ProjectSchemaEnsureHook(repo projects.Repository, db *clients.Database, logger *slog.Logger) lynx.HookFunc {
	return func(ctx context.Context) error {
		if repo == nil || db == nil {
			return nil
		}
		list, err := repo.ListProjects(ctx)
		if err != nil {
			if logger != nil {
				logger.Error("list projects for schema ensure", "error", err)
			}
			return err
		}
		ids := make([]string, len(list))
		for i := range list {
			ids[i] = list[i].ID
		}
		return projectschema.EnsureAll(ctx, db, ids)
	}
}

// RolesSigKeySyncHook 返回启动期 roles 签名密钥同步钩子（阶段③-b 包 C）：
// 把进程内派生的密钥 UPSERT 进 public.tw_secrets，供 tw_roles() 验签。
// 密钥未初始化（InitRolesSigSigning 未跑）时跳过——tw_app 查询将因 sig
// 缺失 fail-closed，错误会在首个业务查询暴露而非静默放行。
func RolesSigKeySyncHook(db *clients.Database, logger *slog.Logger) lynx.HookFunc {
	return func(ctx context.Context) error {
		if db == nil {
			return nil
		}
		if _, ok := clients.RolesSigKeyHex(); !ok {
			if logger != nil {
				logger.Warn("roles sig key not initialized; tw_app queries will fail closed until InitRolesSigSigning runs")
			}
			return nil
		}
		if err := clients.SyncRolesSigKey(ctx, db); err != nil {
			if logger != nil {
				logger.Error("sync roles sig key", "error", err)
			}
			return err
		}
		return nil
	}
}

// （CollectionGrantsReconcileHook 已移至 cmd/server 组合根：列授权全量
// reconcile 是 documentdb 域职责（门禁 A1），bootkit 为 server/worker 共享
// 装配包，直接实现会把 documentdb 拖进 worker 的依赖闭包（import guard
// TestWorkerDepsGraph 守此边界）。server 侧经 NewOnStarts 的 grantsReconcile
// 参数注入闭包，worker 传 nil。）
