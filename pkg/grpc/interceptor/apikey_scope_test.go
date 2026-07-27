package interceptor

import "testing"

func TestAPIKeyScopeAllowed(t *testing.T) {
	t.Parallel()
	method := "/graviton.server.v1.UsersService/ListUsers"

	if APIKeyScopeAllowed(method, nil) {
		t.Fatal("empty scopes should deny resource-scoped methods")
	}
	if !APIKeyScopeAllowed(method, []string{"*"}) {
		t.Fatal("wildcard scope should allow")
	}
	if !APIKeyScopeAllowed(method, []string{"users"}) {
		t.Fatal("matching resource scope should allow")
	}
	if !APIKeyScopeAllowed(method, []string{"users.read"}) {
		t.Fatal("prefixed resource scope should allow")
	}
	if APIKeyScopeAllowed(method, []string{"storage"}) {
		t.Fatal("unrelated scope should deny")
	}

	oauthMethod := "/graviton.server.v1.OAuthProvidersService/ListOAuthProviders"
	if APIKeyScopeAllowed(oauthMethod, nil) {
		t.Fatal("empty scopes should deny oauth providers method")
	}
	if !APIKeyScopeAllowed(oauthMethod, []string{"projects"}) {
		t.Fatal("projects scope should allow oauth providers method")
	}

	unmapped := "/graviton.server.v1.SomeFutureService/DoSomething"
	if APIKeyScopeAllowed(unmapped, []string{"*"}) {
		t.Fatal("unmapped service must fail closed even for wildcard scope")
	}
}
