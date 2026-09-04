package servergrpc

import (
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// rejectListFilterOrderBy returns InvalidArgument if filter/order_by are set
// but the handler does not implement AIP-160/132. This eliminates silent
// no-op (W-K) where clients supply filter/order_by and receive unfiltered
// results without error.
func rejectListFilterOrderBy(req *sharedv1.ListRequest) error {
	if req == nil {
		return nil
	}
	if req.GetFilter() != "" {
		return status.Error(codes.InvalidArgument, "filter is not supported for this resource")
	}
	if req.GetOrderBy() != "" {
		return status.Error(codes.InvalidArgument, "order_by is not supported for this resource")
	}
	return nil
}

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
