package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

func TestActorKind_AgentForbidden(t *testing.T) {
	t.Parallel()
	require.False(t, ActorKind("agent").IsValid())
	require.True(t, ActorKindSystem.IsValid())
	require.True(t, ActorKindService.IsValid())
}

func TestPrincipal_IsAuthenticated_ByKind(t *testing.T) {
	t.Parallel()
	require.False(t, (*Principal)(nil).IsAuthenticated())
	require.False(t, (&Principal{ActorKind: ActorKindEndUser}).IsAuthenticated())
	require.True(t, (&Principal{ActorKind: ActorKindEndUser, UserID: "u1"}).IsAuthenticated())
	require.False(t, (&Principal{ActorKind: ActorKindAdmin, UserID: "admin-1"}).IsAuthenticated(), "admin id 不得塞进 UserID")
	require.True(t, (&Principal{ActorKind: ActorKindAdmin, AdminID: "admin-1"}).IsAuthenticated())
	require.True(t, (&Principal{ActorKind: ActorKindService, APIKeyID: "k1"}).IsAuthenticated())
	require.False(t, (&Principal{ActorKind: ActorKindService}).IsAuthenticated())
	require.True(t, NewSystemPrincipal("p1").IsAuthenticated())
}

func TestPrincipal_IsSystem(t *testing.T) {
	t.Parallel()
	sys := NewSystemPrincipal("p1")
	require.True(t, sys.IsSystem())
	require.Equal(t, ActorKindSystem, sys.ActorKind)
	require.Empty(t, sys.APIKeyID)
	require.NotEqual(t, CredentialTypeAPIKey, sys.CredentialType)
	require.False(t, (&Principal{ActorKind: ActorKindService, APIKeyID: ""}).IsSystem())
	require.Equal(t, databases.SystemPrincipal, sys.DocPrincipal())
}

func TestPrincipal_HasAnyRole_FailClosed(t *testing.T) {
	t.Parallel()
	p := &Principal{Roles: []string{"users"}, Permissions: []string{"storage.read"}}
	require.True(t, p.HasAnyRole([]string{"users"}))
	require.False(t, p.HasAnyRole([]string{"storage.read"}))
	require.False(t, p.HasAnyRole(nil))
	require.True(t, p.HasScope("storage.read"))
	require.False(t, p.HasScope("users"))
}

func TestPrincipal_DocPrincipal_PlatformAdmin(t *testing.T) {
	t.Parallel()
	p := &Principal{Roles: []string{"users"}, IsPlatformAdmin: true}
	got := p.DocPrincipal()
	require.Equal(t, []string{"users"}, got.Roles)
	require.True(t, got.PlatformAdmin)
	require.False(t, got.IsSystem())
	require.True(t, got.BypassesDocumentACL())
}
