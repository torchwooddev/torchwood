package api

import (
	"github.com/torchwooddev/torchwood/internal/api/clientgrpc"
	"github.com/torchwooddev/torchwood/internal/api/consolegrpc"
	"github.com/torchwooddev/torchwood/internal/api/servergrpc"
	"github.com/torchwooddev/torchwood/internal/api/serverhttp"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	clientgrpc.NewAccountService,
	clientgrpc.NewDatabasesService,
	clientgrpc.NewTeamsService,
	servergrpc.NewHealthService,
	servergrpc.NewProjectsService,
	servergrpc.NewStorageService,
	servergrpc.NewUsersService,
	servergrpc.NewAPIKeysService,
	servergrpc.NewOAuthProvidersService,
	servergrpc.NewTeamsService,
	servergrpc.NewDatabasesService,
	serverhttp.NewFileHandler,
	serverhttp.NewOAuthHandler,
	consolegrpc.NewAuthService,
	consolegrpc.NewAdminsService,
)
