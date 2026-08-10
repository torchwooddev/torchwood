package storage

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	NewMinioObjectStore,
	// 直接返回接口（FunctionRepo 先例，不加 wire.Bind）。
	NewRedisUploadSessionStore,
)
