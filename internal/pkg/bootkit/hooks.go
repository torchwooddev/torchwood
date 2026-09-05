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
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
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
// tw_secrets，阶段③-b 包 C：tw_roles() 验签依赖）与存量列授权全量 reconcile
// 钩子（门禁 A1）。cmd/server 与 cmd/worker 共享本装配。
func NewOnStarts(repo projects.Repository, db *clients.Database, logger *slog.Logger) boot.OnStartHooks {
	return boot.OnStartHooks{
		ProjectSchemaEnsureHook(repo, db, logger),
		RolesSigKeySyncHook(db, logger),
		CollectionGrantsReconcileHook(db, logger),
	}
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

// CollectionGrantsReconcileHook 返回启动期存量列授权全量 reconcile 钩子
//（转出 POC 门禁 A1，docs/developer/15-exit-poc.md）：遍历 catalog 全部业务
// 集合物理表，按 R13a/R16 终态口径逐表幂等重建列级 GRANT（refreshColumnGrants），
// 矫正只靠 DDL touch 收敛的存量旧授权形态。挂在 ProjectSchemaEnsureHook 之后：
// catalog 表属 public 全局迁移，本就无需项目 schema 就绪，排序仅保持装配可读。
//
// 失败语义：单表失败不阻断启动（钩子恒返回 nil）——documentdb 扫描内部已按
// "日志 + 失败计数指标" 告警，扫不掉的表由下一次 DDL touch 或下次启动重试；
// 仅 catalog 清单不可读时内部同样记 Error 后照常放行（授权漂移是存量风险，
// 不值得为它挡住整个服务可用性）。OnStart 在服务监听之前串行执行，流量进入
// 时扫描已完成——矫正先于暴露。
func CollectionGrantsReconcileHook(db *clients.Database, logger *slog.Logger) lynx.HookFunc {
	return func(ctx context.Context) error {
		if db == nil {
			return nil
		}
		res, err := documentdb.ReconcileCollectionColumnGrants(ctx, db)
		if err != nil {
			if logger != nil {
				logger.Error("collection column grants reconcile: catalog enumeration failed",
					"error", err)
			}
			return nil
		}
		if logger != nil {
			logger.Info("collection column grants reconcile done",
				"scanned", res.Scanned, "reconciled", res.Reconciled,
				"missing", res.Missing, "failed", res.Failed)
		}
		return nil
	}
}
