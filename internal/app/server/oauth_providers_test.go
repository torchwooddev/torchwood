package server

import (
	"testing"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/stretchr/testify/require"
)

func TestNormalizeServerOAuthProvider(t *testing.T) {
	t.Parallel()
	require.Equal(t, domainauth.ProviderGoogle, normalizeServerOAuthProvider("google"))
	require.Equal(t, domainauth.ProviderWeChatWeb, normalizeServerOAuthProvider("wechat_web"))
	require.Equal(t, "", normalizeServerOAuthProvider("unknown"))
}
