package main

import (
	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	lynxgrpc "github.com/lynx-go/lynx/server/grpc"
	"github.com/torchwooddev/torchwood/internal/api"
	apirealtime "github.com/torchwooddev/torchwood/internal/api/realtime"
	"github.com/torchwooddev/torchwood/internal/api/servergrpc"
	"github.com/torchwooddev/torchwood/internal/api/serverhttp"
	"github.com/torchwooddev/torchwood/internal/app"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	appstorage "github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/domain"
	databases "github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	domainstorage "github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/internal/infra"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/infra/health"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/pkg/bootkit"
	"github.com/torchwooddev/torchwood/internal/pkg/buildinfo"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/runtime"
)

//go:generate wire

var ProviderSet = wire.NewSet(
	boot.New,
	api.ProviderSet,
	app.ProviderSet,
	infra.ProviderSet,
	domain.ProviderSet,
	runtime.ProviderSet,

	// 与 cmd/worker 共享的装配样板收敛在 bootkit（Round4 J4-1）。
	bootkit.NewLogger,
	bootkit.NewComponentBuilders,
	bootkit.NewOnStarts,
	bootkit.NewOnStops,
	NewAppConfig,
	NewComponents,
	NewBuildInfo,
	// SchemaManager 桥接 documentdb internalIDCache 失效回调（Round4 J5-3），
	// 取代 infra.ProviderSet 内的裸 projectschema.NewSchemaManager。
	NewSchemaManager,
	wire.Bind(new(projects.SchemaManager), new(*projectschema.SchemaManager)),
	NewProjectsOptions,
	NewStorageOptions,
	NewRealtimeSubscriberService,
	// Realtime 握手校验复用 auth.Validator（api.ProviderSet 与
	// infra.ProviderSet 在此组合，Bind 放组合根）。
	wire.Bind(new(apirealtime.CredentialValidator), new(*auth.Validator)),
	// HTTP / gRPC handler 窄接口绑定（J4-5）：消费端仅依赖最小方法集，
	// 具体类型 *auth.Validator / *health.Checkers 仅在组合根出现。
	wire.Bind(new(serverhttp.AuthValidator), new(*auth.Validator)),
	wire.Bind(new(servergrpc.HealthCheckers), new(*health.Checkers)),
)

func NewAppConfig(app lynx.App) (*config.AppConfig, error) {
	var c config.AppConfig
	if err := config.UnmarshalConfig(app.Config(), &c); err != nil {
		return nil, err
	}
	if err := bootkit.ValidateAppConfig(app.Logger(), &c); err != nil {
		return nil, err
	}
	// R4-J2-4：启用页 token 签名（HMAC，purpose 派生自 jwt.secret）。
	if err := bootkit.InitPageTokenSigning(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// NewBuildInfo 返回编译期注入的版本信息（package 级 var，由 ldflags 填充）。
func NewBuildInfo() buildinfo.BuildInfo {
	return buildinfo.BuildInfo{Version: version, Commit: commit, Date: date}
}

// NewComponents 返回服务注册顺序：grpc → gateway → realtime-subscriber
// → metrics。
//
// 关停顺序说明（R09-P2-4，更新至 Lynx v1.3.0）：Lynx v1.3.0 仍经 oklog/run 停止服务——正常关停
// 路径按注册顺序（而非逆序）逐个有界停止，即 grpc → gateway →
// realtime-subscriber → metrics；逆序停止仅用于框架内部 Init/OnStart
// 失败路径的资源清理（stopServices）。依赖方向为 gateway → grpc，理想
// 顺序应先停 gateway 再停 grpc；但关停前已有 30s 排水窗口（readiness
// 摘流 + LB 摘除），且各服务 Stop 均有界，故 grpc 先停仅影响窗口内剩余
// 的少量在途转发请求，可接受。realtime-subscriber 停在 gateway 之后：
// 此时新事件仍会被 worker XADD 进 Stream，重启后由 XGROUP 0-0 + PEL
// 认领续投（at-least-once）。metrics 最后停，Prometheus 采集在关停全程
// 可用。cleanup（DB/Redis 等底层资源）不注册进 Lynx，在 runner.RunE()
// 返回后由 main 统一执行。
func NewComponents(
	grpcServer *lynxgrpc.Server,
	gatewayServer *runtime.GRPCGatewayServer,
	realtimeSubscriber *RealtimeSubscriberService,
	metricsServer *runtime.MetricsServer,
) []lynx.Service {
	return []lynx.Service{
		grpcServer,
		gatewayServer,
		realtimeSubscriber,
		metricsServer,
	}
}

// NewSchemaManager 构造桥接了 documentdb internalIDCache 失效回调的 schema
// 管理器（Round4 J5-3）：项目删除（DROP SCHEMA）后 Invalidate 同时清除
// documentdb 的 internal_id 进程内缓存，否则同 ID 重建项目时旧缓存会以陈旧
// internal_id 打 _tenant 标签，造成静默数据分裂。经结构化接口断言完成桥接，
// 避免 projectschema ↔ documentdb 反向依赖。
func NewSchemaManager(db *clients.Database, docDB databases.DocumentDB) *projectschema.SchemaManager {
	m := projectschema.NewSchemaManager(db)
	if inv, ok := docDB.(documentdb.InternalIDCacheInvalidator); ok {
		m.SetInvalidator(inv.InvalidateInternalIDCache)
	}
	return m
}

// NewProjectsOptions 装配项目用例的生产选项（Round4 J5-2）：注入对象存储
// Purger 与配置，项目删除事务提交后异步清空共享桶 {projectID}/ 前缀。
func NewProjectsOptions(purger domainstorage.Purger, cfg *config.AppConfig) []appserver.ProjectsOption {
	return []appserver.ProjectsOption{appserver.WithObjectPurger(purger, cfg)}
}

// NewStorageOptions 返回生产默认的空选项集（WithClock 等仅供测试注入）。
func NewStorageOptions() []appstorage.StorageOption { return nil }
