package events

import (
	"github.com/google/wire"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

var ProviderSet = wire.NewSet(
	NewEventOutbox,
	wire.Bind(new(shared.EventPublisher), new(*eventOutbox)),
)
