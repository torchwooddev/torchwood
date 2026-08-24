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

// NewOnStarts 注册项目 schema 确保钩子：启动时对全部项目幂等 EnsureAll。
func NewOnStarts(repo projects.Repository, db *clients.Database, logger *slog.Logger) boot.OnStartHooks {
	return boot.OnStartHooks{ProjectSchemaEnsureHook(repo, db, logger)}
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
