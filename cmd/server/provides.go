package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	lynxgrpc "github.com/lynx-go/lynx/server/grpc"
	"github.com/torchwooddev/torchwood/internal/api"
	"github.com/torchwooddev/torchwood/internal/app"
	"github.com/torchwooddev/torchwood/internal/domain"
	"github.com/torchwooddev/torchwood/internal/infra"
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
	if err := validateJWTSecret(c.GetSecurity().GetJwt().GetSecret(), app.Logger()); err != nil {
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

// validateJWTSecret 拒绝空值、已知弱默认值与过短密钥；命中弱模式但通过
// 长度检查的密钥仅 Warn（不阻断，便于自定义强密钥仍含常见词的情形）。
func validateJWTSecret(secret string, logger *slog.Logger) error {
	s := strings.TrimSpace(secret)
	if s == "" {
		return errors.New("security.jwt.secret must be set (env TORCHWOOD_SECURITY_JWT_SECRET)")
	}
	if len(s) < minJWTSecretLen {
		return fmt.Errorf("security.jwt.secret is too short (%d chars): must be at least %d characters (env TORCHWOOD_SECURITY_JWT_SECRET)", len(s), minJWTSecretLen)
	}
	lower := strings.ToLower(s)
	for _, w := range weakJWTSecretTokens {
		if lower == w {
			return fmt.Errorf("security.jwt.secret is a known weak default value %q; generate a strong random secret (env TORCHWOOD_SECURITY_JWT_SECRET)", w)
		}
		if strings.Contains(lower, w) {
			logger.Warn("security.jwt.secret contains a known weak value; make sure a strong random secret is set",
				"secret_length", len(s))
			break
		}
	}
	return nil
}

// NewBuildInfo 返回编译期注入的版本信息（package 级 var，由 ldflags 填充）。
func NewBuildInfo() buildinfo.BuildInfo {
	return buildinfo.BuildInfo{Version: version, Commit: commit, Date: date}
}

func NewComponents(
	grpcServer *lynxgrpc.Server,
	gatewayServer *server.GRPCGatewayServer,
	metricsServer *server.MetricsServer,
) []lynx.Service {
	return []lynx.Service{
		grpcServer,
		gatewayServer,
		metricsServer,
	}
}

func NewComponentBuilders() []lynx.ServiceFactory {
	return nil
}

func NewOnStarts() boot.OnStartHooks {
	return boot.OnStartHooks{}
}

func NewOnStops() boot.OnStopHooks {
	return boot.OnStopHooks{}
}
