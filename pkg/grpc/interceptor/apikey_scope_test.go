package interceptor

import "testing"

func TestAPIKeyScopeAllowed(t *testing.T) {
	t.Parallel()
	method := "/torchwood.server.v1.UsersService/ListUsers"

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

	oauthMethod := "/torchwood.server.v1.OAuthProvidersService/ListOAuthProviders"
	if APIKeyScopeAllowed(oauthMethod, nil) {
		t.Fatal("empty scopes should deny oauth providers method")
	}
	if !APIKeyScopeAllowed(oauthMethod, []string{"projects"}) {
		t.Fatal("projects scope should allow oauth providers method")
	}

	unmapped := "/torchwood.server.v1.SomeFutureService/DoSomething"
	if APIKeyScopeAllowed(unmapped, []string{"*"}) {
		t.Fatal("unmapped service must fail closed even for wildcard scope")
	}
}

func TestIsAPIKeysServiceMethod(t *testing.T) {
	t.Parallel()

	if !IsAPIKeysServiceMethod("/torchwood.server.v1.APIKeysService/CreateAPIKey") {
		t.Fatal("APIKeysService method should be detected")
	}
	if IsAPIKeysServiceMethod("/torchwood.server.v1.UsersService/ListUsers") {
		t.Fatal("UsersService method should not be detected")
	}
	if IsAPIKeysServiceMethod("malformed") {
		t.Fatal("malformed method should not be detected")
	}
}
