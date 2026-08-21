package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCreateAttribute_RejectsArray (D-5)：array=true 不得写入 catalog（物理列是标量）。
// CreateCollection 属性列表同样拒绝，防止绕过 CreateAttribute。
func TestCreateAttribute_RejectsArray(t *testing.T) {
	uc := NewDatabases(fakeProjectRepo{}, newFakeDocDB())
	ctx := contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "key-1", ActorKind: shared.ActorKindService, Roles: []string{"keys"},
		Permissions: []string{"databases.write"},
	})

	err := uc.CreateAttribute(ctx, "proj-1", "app", "coll", databases.Attribute{
		Key:   "tags",
		Type:  "string",
		Array: true,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), `attribute "tags": array is not supported`)

	err = uc.CreateCollection(ctx, "proj-1", "app", "coll", "Coll", []databases.Attribute{
		{Key: "title", Type: "string"},
		{Key: "tags", Type: "string", Array: true},
	}, nil, nil, false)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), `attribute "tags": array is not supported`)

	require.NoError(t, uc.CreateAttribute(ctx, "proj-1", "app", "coll", databases.Attribute{
		Key:  "title",
		Type: "string",
	}))
	require.NoError(t, uc.CreateCollection(ctx, "proj-1", "app", "coll", "Coll", []databases.Attribute{
		{Key: "title", Type: "string"},
	}, nil, nil, false))
}
