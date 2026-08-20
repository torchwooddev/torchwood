package app

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/app/assets"
	"github.com/torchwooddev/torchwood/internal/app/billing"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/app/console"
	"github.com/torchwooddev/torchwood/internal/app/functions"
	"github.com/torchwooddev/torchwood/internal/app/payments"
	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/app/subscriptions"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
)

var ProviderSet = wire.NewSet(
	client.NewUserRoles,
	wire.Bind(new(domainauth.UserRoleResolver), new(*client.UserRoles)),
	client.NewAccount,
	client.NewDatabases,
	client.NewTransactions,
	client.NewGroups,
	server.NewProjects,
	server.NewUsers,
	server.NewAPIKeys,
	server.NewOAuthProviders,
	server.NewGroups,
	server.NewDatabases,
	server.NewTransactions,
	shared.NewTransactions,
	console.NewAuth,
	console.NewAdmins,
	console.NewSetup,
	storage.NewStorage,
	functions.NewFunctionsWithUsage,
	payments.NewPayments,
	assets.NewAssets,
	subscriptions.NewSubscriptions,
	subscriptions.NewOrderFulfiller,
	wire.Bind(new(domainpayments.SubscriptionCallbackHandler), new(*subscriptions.Subscriptions)),
	billing.NewBilling,
)
