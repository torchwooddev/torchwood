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
	if APIKeyScopeAllowed(oauthMethod, []string{"projects"}) {
		t.Fatal("projects scope must NOT allow oauth providers method (client_secret isolation)")
	}
	if !APIKeyScopeAllowed(oauthMethod, []string{"oauthproviders"}) {
		t.Fatal("oauthproviders scope should allow oauth providers method")
	}
	if !APIKeyScopeAllowed(oauthMethod, []string{"oauthproviders.read"}) {
		t.Fatal("prefixed oauthproviders scope should allow oauth providers method")
	}

	unmapped := "/torchwood.server.v1.SomeFutureService/DoSomething"
	if APIKeyScopeAllowed(unmapped, []string{"*"}) {
		t.Fatal("unmapped service must fail closed even for wildcard scope")
	}

	createMethod := "/torchwood.server.v1.StorageService/CreateFile"
	getMethod := "/torchwood.server.v1.StorageService/GetFile"
	if !APIKeyScopeAllowed(createMethod, []string{"storage.write"}) {
		t.Fatal("storage.write scope should allow upload")
	}
	if !APIKeyScopeAllowed(getMethod, []string{"storage.read"}) {
		t.Fatal("storage.read scope should allow download")
	}
	// 注意前缀匹配语义：storage.read 也命中 storage. 前缀，对 CreateFile 同样放行；
	// 因此 HTTP 层需要按请求方法区分 GetFile/CreateFile，供未来细粒度 scope 落地方便。
	if !APIKeyScopeAllowed(createMethod, []string{"storage.read"}) {
		t.Fatal("storage.read scope matches storage prefix and should allow upload per prefix rule")
	}
	if APIKeyScopeAllowed(getMethod, []string{"users"}) {
		t.Fatal("unrelated scope should deny download")
	}
	if !APIKeyScopeAllowed(createMethod, []string{"storage"}) {
		t.Fatal("resource scope should allow upload")
	}
	if !APIKeyScopeAllowed(getMethod, []string{"storage"}) {
		t.Fatal("resource scope should allow download")
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
