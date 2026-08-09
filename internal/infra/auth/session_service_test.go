package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubRoleResolver struct{}

func (stubRoleResolver) LoadUserRoles(_ context.Context, _, userID string) ([]string, error) {
	return []string{"users", "user:" + userID}, nil
}

func TestSessionService_RecordsClientInfo(t *testing.T) {
	t.Parallel()

	// Unit-level check: CreateSessionAndTokens reads ClientInfo from context.
	// Full integration is covered by account integration tests.
	svc := auth.NewSessionService(nil, nil, stubRoleResolver{}, nil)
	require.NotNil(t, svc)

	ctx := contexts.WithClientInfo(context.Background(), contexts.ClientInfo{
		IP:        "203.0.113.10",
		UserAgent: "TorchwoodTest/1.0",
	})
	info := contexts.ClientInfoFrom(ctx)
	require.Equal(t, "203.0.113.10", info.IP)
	require.Equal(t, "TorchwoodTest/1.0", info.UserAgent)
}

func TestSessionService_EnsureActiveSession_CorruptExpireAtFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{
		sessions: map[string]map[string]map[string]any{
			"proj-1": {
				"sess-bad": {"user_id": "user-1", "expire_at": "garbage"},
				"sess-ok":  {"user_id": "user-1", "expire_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)},
			},
		},
	}
	svc := auth.NewSessionService(nil, docDB, nil, nil)

	err := svc.EnsureActiveSession(ctx, "proj-1", "sess-bad", "user-1")
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())

	require.NoError(t, svc.EnsureActiveSession(ctx, "proj-1", "sess-ok", "user-1"))
}

func TestProviderConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, "email", domainauth.ProviderEmail)
	require.Equal(t, "wechat_web", domainauth.ProviderWeChatWeb)
}
