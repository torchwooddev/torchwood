package conn

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

func TestDialEmptyTarget(t *testing.T) {
	_, err := Dial("")
	require.ErrorContains(t, err, "target is required")
}

func TestDialOK(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	defer lis.Close()
	c, err := Dial("passthrough:///bufconn", grpc.WithContextDialer(
		func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NoError(t, c.Close())
}
