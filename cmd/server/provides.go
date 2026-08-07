package main

import (
	"errors"

	"github.com/torchwoodio/torchwood/internal/api"
	"github.com/torchwoodio/torchwood/internal/app"
	"github.com/torchwoodio/torchwood/internal/domain"
	"github.com/torchwoodio/torchwood/internal/infra"
	"github.com/torchwoodio/torchwood/internal/infra/server"
	config "github.com/torchwoodio/torchwood/internal/pkg/config"
	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	lynxgrpc "github.com/lynx-go/lynx/server/grpc"
)

//go:generate wire

var ProviderSet = wire.NewSet(
	boot.New,
	api.ProviderSet,
	app.ProviderSet,
	infra.ProviderSet,
	domain.ProviderSet,

	NewComponents,
	NewComponentBuilders,
	NewOnStarts,
	NewOnStops,
	NewAppConfig,
)

func NewAppConfig(app lynx.App) (*config.AppConfig, error) {
	var c config.AppConfig
	if err := config.UnmarshalConfig(app.Config(), &c); err != nil {
		return nil, err
	}
	if secret := c.GetSecurity().GetJwt().GetSecret(); secret == "" {
		return nil, errors.New("security.jwt.secret must be set (env TORCHWOOD_SECURITY_JWT_SECRET)")
	}
	return &c, nil
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
