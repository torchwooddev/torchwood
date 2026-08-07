package bun

import (
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	bunrepo.NewProjectRepository,
	bunrepo.NewOAuthProviderRepository,
	bunrepo.NewAPIKeyRepository,
	bunrepo.NewConsoleAdminRepository,
	bunrepo.NewConsoleAdminProjectRepository,
	bunrepo.NewAuditRepository,
)
