package storage

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	NewMinioObjectStore,
	// 直接返回接口（FunctionRepo 先例，不加 wire.Bind）。
	NewRedisUploadSessionStore,
	// Purger 与 ObjectStore 共享同一 MinIO client（Round4 J5-2：项目删除
	// 后异步清空共享桶 {projectID}/ 前缀）。
	NewObjectPurger,
)
