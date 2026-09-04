package servergrpc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// rejectListQueries 显式拒绝静态表面（storage buckets/files）携带的 queries
// DSL 串（redesign 阶段②包 C / §1-6 契约断裂项）：该面从不消费查询过滤，
// 静默忽略会让调用方以为过滤已生效——携带即 InvalidArgument。users 面的
// ParseUserList 白名单是独立契约，不经此守卫。
func rejectListQueries(queries []string) error {
	if len(queries) > 0 {
		return status.Error(codes.InvalidArgument, "queries are not supported on this surface; this listing does not apply query filters")
	}
	return nil
}
