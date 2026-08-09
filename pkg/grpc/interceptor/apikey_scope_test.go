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
	if !APIKeyScopeAllowed(method, []string{"all"}) {
		t.Fatal("all scope should allow")
	}
	if !APIKeyScopeAllowed(method, []string{"users"}) {
		t.Fatal("matching bare resource scope should allow")
	}
	if !APIKeyScopeAllowed(method, []string{"users.read"}) {
		t.Fatal("read scope should allow read method")
	}
	if APIKeyScopeAllowed(method, []string{"users.write"}) {
		t.Fatal("write scope must NOT allow read method")
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
		t.Fatal("read scope should allow oauth providers method")
	}

	unmapped := "/torchwood.server.v1.SomeFutureService/DoSomething"
	if APIKeyScopeAllowed(unmapped, []string{"*"}) {
		t.Fatal("unmapped service must fail closed even for wildcard scope")
	}
	if APIKeyScopeAllowed(unmapped, []string{"all"}) {
		t.Fatal("unmapped service must fail closed even for all scope")
	}

	createMethod := "/torchwood.server.v1.StorageService/CreateFile"
	getMethod := "/torchwood.server.v1.StorageService/GetFile"
	if !APIKeyScopeAllowed(createMethod, []string{"storage.write"}) {
		t.Fatal("storage.write scope should allow upload")
	}
	if !APIKeyScopeAllowed(getMethod, []string{"storage.read"}) {
		t.Fatal("storage.read scope should allow download")
	}
	// B2 精确匹配：storage.read 不再因前缀匹配放行写方法（原前缀语义在此收紧）。
	if APIKeyScopeAllowed(createMethod, []string{"storage.read"}) {
		t.Fatal("storage.read scope must NOT allow upload")
	}
	if APIKeyScopeAllowed(getMethod, []string{"storage.write"}) {
		t.Fatal("storage.write scope must NOT allow download")
	}
	if APIKeyScopeAllowed(getMethod, []string{"users"}) {
		t.Fatal("unrelated scope should deny download")
	}
	if !APIKeyScopeAllowed(createMethod, []string{"storage"}) {
		t.Fatal("bare resource scope should allow upload")
	}
	if !APIKeyScopeAllowed(getMethod, []string{"storage"}) {
		t.Fatal("bare resource scope should allow download")
	}

	// Databases：方法级 read/write 细分（B2）。
	listDocs := "/torchwood.server.v1.DatabasesService/ListDocuments"
	getDoc := "/torchwood.server.v1.DatabasesService/GetDocument"
	countDocs := "/torchwood.server.v1.DatabasesService/CountDocuments"
	deleteDB := "/torchwood.server.v1.DatabasesService/DeleteDatabase"
	createDoc := "/torchwood.server.v1.DatabasesService/CreateDocument"
	bulkUpdate := "/torchwood.server.v1.DatabasesService/BulkUpdateDocuments"
	if !APIKeyScopeAllowed(listDocs, []string{"databases.read"}) {
		t.Fatal("databases.read should allow ListDocuments")
	}
	if !APIKeyScopeAllowed(getDoc, []string{"databases.read"}) {
		t.Fatal("databases.read should allow GetDocument")
	}
	if !APIKeyScopeAllowed(countDocs, []string{"databases.read"}) {
		t.Fatal("databases.read should allow CountDocuments")
	}
	if APIKeyScopeAllowed(deleteDB, []string{"databases.read"}) {
		t.Fatal("databases.read must NOT allow DeleteDatabase")
	}
	if APIKeyScopeAllowed(createDoc, []string{"databases.read"}) {
		t.Fatal("databases.read must NOT allow CreateDocument")
	}
	if APIKeyScopeAllowed(bulkUpdate, []string{"databases.read"}) {
		t.Fatal("databases.read must NOT allow BulkUpdateDocuments")
	}
	if !APIKeyScopeAllowed(deleteDB, []string{"databases.write"}) {
		t.Fatal("databases.write should allow DeleteDatabase")
	}
	if !APIKeyScopeAllowed(createDoc, []string{"databases.write"}) {
		t.Fatal("databases.write should allow CreateDocument")
	}
	if !APIKeyScopeAllowed(bulkUpdate, []string{"databases.write"}) {
		t.Fatal("databases.write should allow BulkUpdateDocuments")
	}
	if APIKeyScopeAllowed(listDocs, []string{"databases.write"}) {
		t.Fatal("databases.write must NOT allow ListDocuments")
	}
	// 裸 databases scope 全放行。
	if !APIKeyScopeAllowed(listDocs, []string{"databases"}) {
		t.Fatal("bare databases scope should allow read methods")
	}
	if !APIKeyScopeAllowed(deleteDB, []string{"databases"}) {
		t.Fatal("bare databases scope should allow write methods")
	}

	// Projects：UpdateProject 需要 projects.write（B2）。
	updateProject := "/torchwood.server.v1.ProjectsService/UpdateProject"
	if !APIKeyScopeAllowed(updateProject, []string{"projects.write"}) {
		t.Fatal("projects.write scope should allow UpdateProject")
	}
	if !APIKeyScopeAllowed(updateProject, []string{"projects"}) {
		t.Fatal("bare projects scope should allow UpdateProject")
	}
	if !APIKeyScopeAllowed(updateProject, []string{"*"}) {
		t.Fatal("wildcard scope should allow UpdateProject")
	}
	if !APIKeyScopeAllowed(updateProject, []string{"all"}) {
		t.Fatal("all scope should allow UpdateProject")
	}
	if APIKeyScopeAllowed(updateProject, []string{"projects.read"}) {
		t.Fatal("projects.read scope must NOT allow UpdateProject")
	}
	if APIKeyScopeAllowed(updateProject, []string{"users"}) {
		t.Fatal("unrelated scope must NOT allow UpdateProject")
	}
}

func TestValidAPIKeyScope(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"*", "all",
		"databases", "users", "teams", "storage", "projects", "oauthproviders", "apikeys",
		"databases.read", "databases.write",
		"storage.read", "storage.write",
		"users.read", "users.write",
		"teams.read", "teams.write",
		"projects.read", "projects.write",
		"oauthproviders.read", "oauthproviders.write",
		"apikeys.read", "apikeys.write",
	} {
		if !ValidAPIKeyScope(s) {
			t.Fatalf("scope %q should be valid", s)
		}
	}

	for _, s := range []string{"", "foo", "health", "health.read", "databases.delete", "databases.read.extra", "any", "users.read.write"} {
		if ValidAPIKeyScope(s) {
			t.Fatalf("scope %q should be invalid", s)
		}
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
