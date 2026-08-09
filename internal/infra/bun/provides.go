package bun

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
)

var ProviderSet = wire.NewSet(
	bunrepo.NewProjectRepository,
	bunrepo.NewOAuthProviderRepository,
	bunrepo.NewAPIKeyRepository,
	bunrepo.NewConsoleAdminRepository,
	bunrepo.NewConsoleAdminProjectRepository,
	bunrepo.NewAuditRepository,
)
