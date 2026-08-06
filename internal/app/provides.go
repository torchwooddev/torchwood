package app

import (
	"github.com/torchwoodio/torchwood/internal/app/client"
	"github.com/torchwoodio/torchwood/internal/app/console"
	"github.com/torchwoodio/torchwood/internal/app/functions"
	"github.com/torchwoodio/torchwood/internal/app/server"
	"github.com/torchwoodio/torchwood/internal/app/storage"
	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	client.NewUserRoles,
	wire.Bind(new(domainauth.UserRoleResolver), new(*client.UserRoles)),
	client.NewAccount,
	client.NewDatabases,
	client.NewTeams,
	server.NewProjects,
	server.NewUsers,
	server.NewAPIKeys,
	server.NewOAuthProviders,
	server.NewTeams,
	server.NewDatabases,
	console.NewAuth,
	storage.NewStorage,
	functions.NewFunctions,
)
