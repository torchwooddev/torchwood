package bun

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
)

var ProviderSet = wire.NewSet(
	bunrepo.NewProjectRepository,
	bunrepo.NewOAuthProviderRepository,
	bunrepo.NewAPIKeyRepository,
	bunrepo.NewAdminRepository,
	bunrepo.NewAdminProjectRepository,
	bunrepo.NewAuditRepository,
	bunrepo.NewFunctionRepository,
	bunrepo.NewTransactionRepository,
	bunrepo.NewPaymentOrderRepository,
	bunrepo.NewPaymentCallbackEventRepository,
	bunrepo.NewPaymentFulfillmentRepository,
	bunrepo.NewProviderIndexRepository,
	bunrepo.NewAssetDefRepository,
	bunrepo.NewAssetHoldingRepository,
	bunrepo.NewAssetLedgerRepository,
	bunrepo.NewSubscriptionPlanRepository,
	bunrepo.NewSubscriptionRepository,
	bunrepo.NewUsageRepository,
	bunrepo.NewBillingStatementRepository,
)
