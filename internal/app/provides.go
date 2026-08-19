package app

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/app/console"
	"github.com/torchwooddev/torchwood/internal/app/functions"
	"github.com/torchwooddev/torchwood/internal/app/payments"
	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/app/storage"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
)

var ProviderSet = wire.NewSet(
	client.NewUserRoles,
	wire.Bind(new(domainauth.UserRoleResolver), new(*client.UserRoles)),
	client.NewAccount,
	client.NewDatabases,
	client.NewTransactions,
	client.NewTeams,
	server.NewProjects,
	server.NewUsers,
	server.NewAPIKeys,
	server.NewOAuthProviders,
	server.NewTeams,
	server.NewDatabases,
	server.NewTransactions,
	shared.NewTransactions,
	console.NewAuth,
	console.NewAdmins,
	console.NewSetup,
	storage.NewStorage,
	functions.NewFunctions,
	payments.NewPayments,
	payments.NewRecordOnlyFulfiller,
)
