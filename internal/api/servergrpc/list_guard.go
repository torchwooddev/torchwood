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
