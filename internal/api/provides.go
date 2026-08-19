package api

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/api/clientgrpc"
	"github.com/torchwooddev/torchwood/internal/api/consolegrpc"
	apirealtime "github.com/torchwooddev/torchwood/internal/api/realtime"
	"github.com/torchwooddev/torchwood/internal/api/servergrpc"
	"github.com/torchwooddev/torchwood/internal/api/serverhttp"
)

var ProviderSet = wire.NewSet(
	clientgrpc.NewAccountService,
	clientgrpc.NewDatabasesService,
	clientgrpc.NewTeamsService,
	clientgrpc.NewPaymentsService,
	clientgrpc.NewAssetsService,
	servergrpc.NewHealthService,
	servergrpc.NewProjectsService,
	servergrpc.NewStorageService,
	servergrpc.NewUsersService,
	servergrpc.NewAPIKeysService,
	servergrpc.NewOAuthProvidersService,
	servergrpc.NewTeamsService,
	servergrpc.NewDatabasesService,
	servergrpc.NewFunctionsService,
	servergrpc.NewPaymentsService,
	servergrpc.NewAssetsService,
	serverhttp.NewFileHandler,
	serverhttp.NewOAuthHandler,
	serverhttp.NewFunctionsHandler,
	serverhttp.NewPaymentsHandler,
	consolegrpc.NewAuthService,
	consolegrpc.NewAdminsService,
	apirealtime.NewHandler,
)
