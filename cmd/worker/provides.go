package main

import (
	"errors"

	"github.com/google/wire"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/boot"
	"github.com/torchwooddev/torchwood/internal/app"
	"github.com/torchwooddev/torchwood/internal/domain"
	"github.com/torchwooddev/torchwood/internal/infra"
	config "github.com/torchwooddev/torchwood/internal/pkg/config"
)

//go:generate wire

var ProviderSet = wire.NewSet(
	boot.New,
	app.ProviderSet,
	infra.ProviderSet,
	domain.ProviderSet,

	NewComponents,
	NewComponentBuilders,
	NewOnStarts,
	NewOnStops,
	NewAppConfig,
	NewWorker,
)

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

func NewComponents(worker *Worker) []lynx.Service {
	return []lynx.Service{worker}
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
