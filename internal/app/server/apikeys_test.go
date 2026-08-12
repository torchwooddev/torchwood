package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAPIKeyRepository struct{}

func (f *fakeAPIKeyRepository) CreateAPIKey(ctx context.Context, key *projects.APIKey) error {
	return nil
}
func (f *fakeAPIKeyRepository) GetAPIKey(ctx context.Context, id string) (*projects.APIKey, error) {
	return nil, nil
}
func (f *fakeAPIKeyRepository) GetAPIKeyBySecretHash(ctx context.Context, hash string) (*projects.APIKey, error) {
	return nil, nil
}
func (f *fakeAPIKeyRepository) ListAPIKeys(ctx context.Context, projectID string) ([]projects.APIKey, error) {
	return nil, nil
}
func (f *fakeAPIKeyRepository) DeleteAPIKey(ctx context.Context, id string) error { return nil }

// TestAPIKeys_Create_ScopeValidation (B2): Create 时校验 scope 格式
// ∈ {*, all, 裸资源名, <resource>.read, <resource>.write}，上限 32 项/64 字符。
func TestAPIKeys_Create_ScopeValidation(t *testing.T) {
	uc := NewAPIKeys(&fakeAPIKeyRepository{})
	ctx := platformAdminCtx(context.Background())

	for _, scopes := range [][]string{
		{"*"},
		{"all"},
		{"databases"},
		{"databases.read", "databases.write"},
		{"users.read", "storage.write", "oauthproviders", "teams"},
	} {
		_, _, err := uc.Create(ctx, CreateAPIKeyCommand{Name: "k", Scopes: scopes})
		require.NoError(t, err, "scopes %v should be accepted", scopes)
	}

	for _, scopes := range [][]string{
		{"foo"},
		{"health"},
		{"health.read"},
		{"databases.delete"},
		{"databases.read.extra"},
		{"any"},
		{""},
	} {
		_, _, err := uc.Create(ctx, CreateAPIKeyCommand{Name: "k", Scopes: scopes})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "scopes %v should be rejected", scopes)
	}

	// 超过 32 项拒绝。
	overCount := make([]string, 33)
	for i := range overCount {
		overCount[i] = "databases.read"
	}
	_, _, err := uc.Create(ctx, CreateAPIKeyCommand{Name: "k", Scopes: overCount})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 单项超过 64 字符拒绝。
	scope := "databases.read"
	for len(scope) <= 64 {
		scope += "x"
	}
	_, _, err = uc.Create(ctx, CreateAPIKeyCommand{Name: "k", Scopes: []string{scope}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 空 scope 拒绝。
	_, _, err = uc.Create(ctx, CreateAPIKeyCommand{Name: "k"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestAPIKeys_Create_RequiresPlatformAdmin（F2-2 纵深防御）：受限 admin
// （viewer/member）与 API key 主体调用 Create 必须 PermissionDenied。
func TestAPIKeys_Create_RequiresPlatformAdmin(t *testing.T) {
	uc := NewAPIKeys(&fakeAPIKeyRepository{})

	for _, principal := range []*shared.Principal{
		{ActorID: "admin-2", ActorKind: shared.ActorKindAdmin, Roles: []string{"viewer"}},
		{ActorID: "admin-3", ActorKind: shared.ActorKindAdmin, Roles: []string{"member"}},
		{ActorID: "key-1", ActorKind: shared.ActorKindService, Roles: []string{"keys"}, Permissions: []string{"*"}},
	} {
		ctx := contexts.WithPrincipal(context.Background(), principal)
		_, _, err := uc.Create(ctx, CreateAPIKeyCommand{Name: "k", Scopes: []string{"*"}})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "principal %+v should be denied", principal)
	}

	// 平台 admin 放行。
	_, _, err := uc.Create(platformAdminCtx(context.Background()), CreateAPIKeyCommand{Name: "k", Scopes: []string{"*"}})
	require.NoError(t, err)
}
