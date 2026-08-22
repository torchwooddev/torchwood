package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	"github.com/torchwooddev/torchwood/internal/app/assets"
	appbilling "github.com/torchwooddev/torchwood/internal/app/billing"
	appfunctions "github.com/torchwooddev/torchwood/internal/app/functions"
	apppayments "github.com/torchwooddev/torchwood/internal/app/payments"
	appstorage "github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/app/subscriptions"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	domainstorage "github.com/torchwooddev/torchwood/internal/domain/storage"
	infrabilling "github.com/torchwooddev/torchwood/internal/infra/billing"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
	infrafunctions "github.com/torchwooddev/torchwood/internal/infra/functions"
	infrapayments "github.com/torchwooddev/torchwood/internal/infra/payments"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	infraqueue "github.com/torchwooddev/torchwood/internal/infra/queue"
	"github.com/torchwooddev/torchwood/internal/infra/realtime"
	infrastorage "github.com/torchwooddev/torchwood/internal/infra/storage"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/uow"
)

//go:generate wire

// ProviderSet 只装配作业端口与其适配器，避免 app/infra 桶包把
// Account / gRPC / documentdb 拉进进程依赖图。
var ProviderSet = wire.NewSet(
	boot.New,

	appfunctions.NewFunctionsWithUsage,
	apppayments.NewPayments,
	assets.NewAssets,
	subscriptions.NewSubscriptions,
	subscriptions.NewOrderFulfiller,
	wire.Bind(new(domainpayments.SubscriptionCallbackHandler), new(*subscriptions.Subscriptions)),
	appbilling.NewBilling,
	appstorage.NewStorage,

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

	clients.NewDataClients,
	clients.NewDatabase,
	clients.NewRedis,
	wire.Bind(new(uow.Runner), new(*clients.Database)),
	wire.Bind(new(uow.Isolator), new(*clients.Database)),
	bunrepo.NewProjectRepository,
	bunrepo.NewFunctionRepository,
	bunrepo.NewBucketRepository,
	bunrepo.NewFileRepository,
	bunrepo.NewPaymentOrderRepository,
	bunrepo.NewPaymentCallbackEventRepository,
	bunrepo.NewPaymentFulfillmentRepository,
	bunrepo.NewAssetDefRepository,
	bunrepo.NewAssetHoldingRepository,
	bunrepo.NewAssetLedgerRepository,
	bunrepo.NewSubscriptionPlanRepository,
	bunrepo.NewSubscriptionRepository,
	bunrepo.NewProviderIndexRepository,
	bunrepo.NewUsageRepository,
	bunrepo.NewBillingStatementRepository,
	wire.Bind(new(domainstorage.BucketRepository), new(*bunrepo.BucketRepository)),
	wire.Bind(new(domainstorage.FileRepository), new(*bunrepo.FileRepository)),
	infraevents.ProviderSet,
	infrafunctions.NewDockerExecutor,
	infrapayments.ProviderSet,
	infrabilling.ProviderSet,
	infraqueue.ProviderSet,
	infrastorage.ProviderSet,
	realtime.NewStreamTransport,
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
	if _, fallback := config.EncryptionSecret(&c); fallback {
		app.Logger().Warn("security.encryption_key is not set: static encryption (OAuth/TOTP secrets) falls back to security.jwt.secret; configure a dedicated key (env TORCHWOOD_SECURITY_ENCRYPTION_KEY)")
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
