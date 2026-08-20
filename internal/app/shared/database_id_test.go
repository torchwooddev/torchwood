package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRejectExternalDatabaseID(t *testing.T) {
	require.NoError(t, RejectExternalDatabaseID("default"))
	require.NoError(t, RejectExternalDatabaseID("app"))
	require.Equal(t, codes.InvalidArgument, status.Code(RejectExternalDatabaseID("")))
	require.Equal(t, codes.InvalidArgument, status.Code(RejectExternalDatabaseID(ident.ProjectDataPlaneID)))
	require.Equal(t, codes.InvalidArgument, status.Code(RejectExternalDatabaseID("Bad-ID")))
}
