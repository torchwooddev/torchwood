package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	lynxgrpc "github.com/lynx-go/lynx/server/grpc"
	"github.com/torchwooddev/torchwood/internal/api"
	apirealtime "github.com/torchwooddev/torchwood/internal/api/realtime"
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
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/infra/server"
	"github.com/torchwooddev/torchwood/internal/pkg/buildinfo"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/crud"
)

//go:generate wire

var ProviderSet = wire.NewSet(
	boot.New,
	api.ProviderSet,
	app.ProviderSet,
	infra.ProviderSet,
	domain.ProviderSet,

	NewLogger,
	NewComponents,
	NewComponentBuilders,
	NewOnStarts,
	NewOnStops,
	NewAppConfig,
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
	if err := validateAppConfig(app.Logger(), &c); err != nil {
		return nil, err
	}
	// R4-J2-4：启用页 token 签名（HMAC，purpose 派生自 jwt.secret）。
	// 未配置主密钥即拒绝启动，与 JWT 校验同一 fail-closed 口径。
	if err := crud.InitPageTokenSigning(c.GetSecurity().GetJwt().GetSecret()); err != nil {
		return nil, fmt.Errorf("init page token signing: %w", err)
	}
	return &c, nil
}

// validateAppConfig 校验安全相关配置并按需告警：显式 security.encryption_key
// 套用与 jwt.secret 相同的强度规则（不合规拒绝启动）；未配置时回退
// jwt.secret 仅告警（历史行为，W-I）。
func validateAppConfig(logger *slog.Logger, c *config.AppConfig) error {
	if key, fallback := config.EncryptionSecret(c); fallback {
		logger.Warn("security.encryption_key is not set: static encryption (OAuth/TOTP secrets) falls back to security.jwt.secret; configure a dedicated key (env TORCHWOOD_SECURITY_ENCRYPTION_KEY)")
	} else if err := validateSecret("security.encryption_key", "TORCHWOOD_SECURITY_ENCRYPTION_KEY", key); err != nil {
		return err
	}
	return validateJWTSecret(c.GetSecurity().GetJwt().GetSecret())
}

// weakJWTSecretTokens 是已知弱默认值/常见占位密钥的子串黑名单。
var weakJWTSecretTokens = []string{
	"change-me",
	"changeme",
	"minioadmin",
	"secret",
	"password",
	"torchwood",
}

// minJWTSecretLen 是主密钥的最小长度（HS256 / secretbox 密钥熵下界）。
const minJWTSecretLen = 32

// validateSecret 拒绝空值、过短密钥与任何含已知弱子串的密钥：命中弱
// 子串即整体拒绝（而不只是 Warn），否则 "change-me" 之类的占位默认值只要
// 拼够长度就能绕过长度检查，弱密钥子串是实际绕过手法中最常见的一类。
func validateSecret(fieldPath, envName, secret string) error {
	s := strings.TrimSpace(secret)
	if s == "" {
		return fmt.Errorf("%s must be set (env %s)", fieldPath, envName)
	}
	if len(s) < minJWTSecretLen {
		return fmt.Errorf("%s is too short (%d chars): must be at least %d characters (env %s)", fieldPath, len(s), minJWTSecretLen, envName)
	}
	lower := strings.ToLower(s)
	for _, w := range weakJWTSecretTokens {
		if strings.Contains(lower, w) {
			return fmt.Errorf("%s contains known weak value %q; generate a strong random secret (env %s)", fieldPath, w, envName)
		}
	}
	return nil
}

// validateJWTSecret 按主密钥强度规则校验 JWT 主密钥。
func validateJWTSecret(secret string) error {
	return validateSecret("security.jwt.secret", "TORCHWOOD_SECURITY_JWT_SECRET", secret)
}

// NewBuildInfo 返回编译期注入的版本信息（package 级 var，由 ldflags 填充）。
func NewBuildInfo() buildinfo.BuildInfo {
	return buildinfo.BuildInfo{Version: version, Commit: commit, Date: date}
}

// NewComponents 返回服务注册顺序：grpc → gateway → realtime-subscriber
// → metrics。
//
// 关停顺序说明（R09-P2-4）：Lynx v1.2.0 经 oklog/run 停止服务——正常关停
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
	gatewayServer *server.GRPCGatewayServer,
	realtimeSubscriber *RealtimeSubscriberService,
	metricsServer *server.MetricsServer,
) []lynx.Service {
	return []lynx.Service{
		grpcServer,
		gatewayServer,
		realtimeSubscriber,
		metricsServer,
	}
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
