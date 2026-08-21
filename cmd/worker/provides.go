package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	"github.com/torchwooddev/torchwood/internal/app"
	appstorage "github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/domain"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
)

//go:generate wire

var ProviderSet = wire.NewSet(
	boot.New,
	app.ProviderSet,
	infra.ProviderSet,
	domain.ProviderSet,

	NewLogger,
	NewComponents,
	NewComponentBuilders,
	NewOnStarts,
	NewOnStops,
	NewAppConfig,
	NewWorker,
	NewChunkCleaner,
	NewOutboxWorkerService,
	NewPaymentCloser,
	NewAssetExpirer,
	NewSubscriptionBiller,
	NewUsageRollupWorker,
	wire.Bind(new(chunkCleaner), new(*appstorage.Storage)),
)

// NewLogger 暴露 app 装配的 *slog.Logger（zap 后端），供各层构造器注入。
func NewLogger(app lynx.App) *slog.Logger {
	return app.Logger()
}

func NewAppConfig(app lynx.App) (*config.AppConfig, error) {
	var c config.AppConfig
	if err := config.UnmarshalConfig(app.Config(), &c); err != nil {
		return nil, err
	}
	if c.GetData().GetDatabase().GetSource() == "" {
		return nil, errors.New("data.database.source must be set (env TORCHWOOD_DATA_DATABASE_SOURCE)")
	}
	return &c, nil
}

func NewComponents(worker *Worker, cleaner *ChunkCleaner, outbox *OutboxWorkerService, paymentCloser *PaymentCloser, assetExpirer *AssetExpirer, subscriptionBiller *SubscriptionBiller, usageRollup *UsageRollupWorker) []lynx.Service {
	return []lynx.Service{worker, cleaner, outbox, paymentCloser, assetExpirer, subscriptionBiller, usageRollup}
}

func NewComponentBuilders() []lynx.ServiceFactory {
	return nil
}

func NewOnStarts(repo projects.Repository, db *clients.Database, logger *slog.Logger) boot.OnStartHooks {
	return boot.OnStartHooks{projectSchemaEnsureHook(repo, db, logger)}
}

func projectSchemaEnsureHook(repo projects.Repository, db *clients.Database, logger *slog.Logger) lynx.HookFunc {
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

func NewOnStops() boot.OnStopHooks {
	return boot.OnStopHooks{}
}
