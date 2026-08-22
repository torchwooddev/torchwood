package main

import (
	"context"
	"errors"
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
	"github.com/torchwooddev/torchwood/internal/domain"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/infra/server"
	"github.com/torchwooddev/torchwood/internal/pkg/buildinfo"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
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
	if _, fallback := config.EncryptionSecret(&c); fallback {
		app.Logger().Warn("security.encryption_key is not set: static encryption (OAuth/TOTP secrets) falls back to security.jwt.secret; configure a dedicated key (env TORCHWOOD_SECURITY_ENCRYPTION_KEY)")
	}
	if err := validateJWTSecret(c.GetSecurity().GetJwt().GetSecret()); err != nil {
		return nil, err
	}
	return &c, nil
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

// minJWTSecretLen 是 JWT 主密钥的最小长度（HS256 密钥熵下界）。
const minJWTSecretLen = 32

// validateJWTSecret 拒绝空值、过短密钥与任何含已知弱子串的密钥：命中弱
// 子串即整体拒绝（而不只是 Warn），否则 "change-me" 之类的占位默认值只要
// 拼够长度就能绕过长度检查，弱密钥子串是实际绕过手法中最常见的一类。
func validateJWTSecret(secret string) error {
	s := strings.TrimSpace(secret)
	if s == "" {
		return errors.New("security.jwt.secret must be set (env TORCHWOOD_SECURITY_JWT_SECRET)")
	}
	if len(s) < minJWTSecretLen {
		return fmt.Errorf("security.jwt.secret is too short (%d chars): must be at least %d characters (env TORCHWOOD_SECURITY_JWT_SECRET)", len(s), minJWTSecretLen)
	}
	lower := strings.ToLower(s)
	for _, w := range weakJWTSecretTokens {
		if strings.Contains(lower, w) {
			return fmt.Errorf("security.jwt.secret contains known weak value %q; generate a strong random secret (env TORCHWOOD_SECURITY_JWT_SECRET)", w)
		}
	}
	return nil
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
