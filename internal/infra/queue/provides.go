package queue

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	NewRedisQueue,
)
