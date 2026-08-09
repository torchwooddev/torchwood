package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
)

func TestNormalizeServerOAuthProvider(t *testing.T) {
	t.Parallel()
	require.Equal(t, domainauth.ProviderGoogle, normalizeServerOAuthProvider("google"))
	require.Equal(t, domainauth.ProviderWeChatWeb, normalizeServerOAuthProvider("wechat_web"))
	require.Equal(t, "", normalizeServerOAuthProvider("unknown"))
}
