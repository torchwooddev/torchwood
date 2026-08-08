package shared

import (
	"errors"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapDocumentDBError_DuplicateKey(t *testing.T) {
	// The infra layer re-exports the domain error as an alias; errors.Is must
	// still match so the mapping to AlreadyExists holds for both instances.
	err := errors.New("duplicate key")
	require.Error(t, err)

	plain := errors.New("SQLSTATE 23505 unique constraint")
	mapped := MapDocumentDBError(plain)
	require.Equal(t, plain, mapped)

	dup := databases.ErrDuplicateKey
	require.Equal(t, codes.AlreadyExists, status.Code(MapDocumentDBError(dup)))

	wrapped := errors.Join(dup, errors.New("context"))
	require.Equal(t, codes.AlreadyExists, status.Code(MapDocumentDBError(wrapped)))

	denied := databases.ErrPermissionDenied
	require.Equal(t, codes.PermissionDenied, status.Code(MapDocumentDBError(denied)))
	require.Nil(t, MapDocumentDBError(nil))
}
