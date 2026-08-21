package databases

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrincipal_IsSystem_NotPlatformAdmin(t *testing.T) {
	t.Parallel()
	require.False(t, Principal{PlatformAdmin: true}.IsSystem())
	require.True(t, Principal{PlatformAdmin: true}.BypassesDocumentACL())
	require.True(t, SystemPrincipal.IsSystem())
	require.True(t, SystemPrincipal.BypassesDocumentACL())
	require.False(t, Principal{Roles: []string{"keys"}}.IsSystem())
}
