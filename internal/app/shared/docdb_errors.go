package shared

import (
	"errors"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MapDocumentDBError converts document database errors to gRPC status errors.
func MapDocumentDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, databases.ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return err
}
