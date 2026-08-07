package interceptor

import (
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

func TestPrincipalHasAnyPermission(t *testing.T) {
	t.Parallel()
	p := &shared.Principal{
		Roles:       []string{"users", "user:abc"},
		Permissions: []string{"storage.read"},
	}
	if !p.HasAnyPermission([]string{"users"}) {
		t.Fatal("expected users role to match")
	}
	if !p.HasAnyPermission([]string{"storage.read"}) {
		t.Fatal("expected storage.read scope to match")
	}
	if p.HasAnyPermission([]string{"admin"}) {
		t.Fatal("expected admin permission to fail")
	}
	if !p.HasAnyPermission(nil) {
		t.Fatal("empty required permissions should allow")
	}
}
