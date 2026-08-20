package databases

import "testing"

func TestExpandPermissionRoles_GuestsDoNotGetUsers(t *testing.T) {
	expanded := ExpandPermissionRoles([]string{"guests"})
	if contains(expanded, "users") {
		t.Fatal("guests should not implicitly receive users role")
	}
	if !contains(expanded, "any") {
		t.Fatal("any should always be present")
	}
}

func TestExpandPermissionRoles_AuthenticatedGetsUsers(t *testing.T) {
	expanded := ExpandPermissionRoles([]string{"users", "user:alice"})
	if !contains(expanded, "users") {
		t.Fatal("authenticated caller should have users role")
	}
}

func TestCollectionAllows_WriteGrantsUpdate(t *testing.T) {
	perms := []Permission{{Type: "write", Role: "users"}}
	if !CollectionAllows(perms, "update", []string{"users"}) {
		t.Fatal("write should grant update")
	}
	if CollectionAllows(perms, "read", []string{"users"}) {
		t.Fatal("write should not grant read")
	}
}

func TestAllowsDocumentAccess_UserCollectionDocumentOverrides(t *testing.T) {
	coll := &Collection{
		DocumentSecurity: true,
		Permissions:      []Permission{{Type: "read", Role: "any"}},
	}
	docPerms := []Permission{{Type: "read", Role: "user:bob"}}
	// 用户集合（IsSystem=false）+ 文档有 _perms：文档权限覆盖集合权限（B1）。
	if AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:alice"}) {
		t.Fatal("user collection: document perms must override collection read:any (alice denied)")
	}
	if AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:carol"}) {
		t.Fatal("user collection: document perms must override collection read:any (carol denied)")
	}
	if !AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:bob"}) {
		t.Fatal("document read:user:bob should allow bob")
	}
	// 无 _perms 行：集合级兜底（docHasPerms=false → collOK）。
	if !AllowsDocumentAccess(coll, nil, false, "read", []string{"user:carol"}) {
		t.Fatal("docHasPerms=false should fall back to collection perms")
	}
}

func TestAllowsDocumentAccess_SystemCollectionOR(t *testing.T) {
	coll := &Collection{
		DocumentSecurity: true,
		IsSystem:         true,
		Permissions:      []Permission{{Type: "read", Role: "any"}},
	}
	docPerms := []Permission{{Type: "read", Role: "user:bob"}}
	// 系统集合（D1 豁免）：保持 OR 语义。
	if !AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:alice"}) {
		t.Fatal("system collection should keep OR semantics (collection read:any allows alice)")
	}
	if !AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:bob"}) {
		t.Fatal("document read:user:bob should allow bob")
	}

	locked := &Collection{
		DocumentSecurity: true,
		Permissions:      []Permission{{Type: "create", Role: "users"}},
	}
	if AllowsDocumentAccess(locked, docPerms, true, "read", []string{"user:carol"}) {
		t.Fatal("carol should not have access without collection or document read")
	}
}

func TestSkipDocumentPermissionFilter(t *testing.T) {
	readAny := []Permission{{Type: "read", Role: "any"}}

	// 系统集合 + 集合级有 read → 跳过（D1 豁免）。
	systemRead := &Collection{IsSystem: true, DocumentSecurity: true, Permissions: readAny}
	if !SkipDocumentPermissionFilter(systemRead, []string{"users"}) {
		t.Fatal("system collection with collection read should skip")
	}
	// 用户集合 documentSecurity=true + 集合级有 read → 一律逐文档过滤（B1）。
	userDocSec := &Collection{DocumentSecurity: true, Permissions: readAny}
	if SkipDocumentPermissionFilter(userDocSec, []string{"users"}) {
		t.Fatal("user collection documentSecurity=true must not skip even with collection read")
	}
	// documentSecurity=false + 集合级有 read → 跳过（纯集合级语义）。
	noDocSec := &Collection{DocumentSecurity: false, Permissions: readAny}
	if !SkipDocumentPermissionFilter(noDocSec, []string{"users"}) {
		t.Fatal("documentSecurity=false with collection read should skip")
	}
	// 集合级无 read → 不跳过。
	noRead := &Collection{DocumentSecurity: true, Permissions: []Permission{{Type: "create", Role: "users"}}}
	if SkipDocumentPermissionFilter(noRead, []string{"users"}) {
		t.Fatal("no collection read should not skip")
	}
}

func TestAllowsDocumentAccess_DocumentSecurityOffIgnoresDocPerms(t *testing.T) {
	coll := &Collection{
		DocumentSecurity: false,
		Permissions:      []Permission{{Type: "read", Role: "any"}},
	}
	docPerms := []Permission{{Type: "read", Role: "user:bob"}}
	if !AllowsDocumentAccess(coll, docPerms, true, "read", []string{"user:carol"}) {
		t.Fatal("document perms ignored when documentSecurity=false")
	}
}

func TestParsePermissionStrings_WriteExpands(t *testing.T) {
	perms, err := ParsePermissionStrings([]string{"write:users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != 3 {
		t.Fatalf("expected 3 perms, got %d", len(perms))
	}
}

func TestExpandPermissionTemplates_UserPlaceholder(t *testing.T) {
	perms := []Permission{{Type: "read", Role: "user:{id}"}}
	roles := []string{"users", "user:alice", "group:t1"}
	out := ExpandPermissionTemplates(perms, roles)
	if len(out) != 1 || out[0].Type != "read" || out[0].Role != "user:alice" {
		t.Fatalf("expected read:user:alice, got %+v", out)
	}
}

func TestExpandPermissionTemplates_GroupPlaceholder(t *testing.T) {
	perms := []Permission{{Type: "update", Role: "group:{id}"}}
	roles := []string{"users", "user:alice", "group:t1"}
	out := ExpandPermissionTemplates(perms, roles)
	if len(out) != 1 || out[0].Type != "update" || out[0].Role != "group:t1" {
		t.Fatalf("expected update:group:t1, got %+v", out)
	}
}

func TestExpandPermissionTemplates_NoMatchKeepsOriginal(t *testing.T) {
	perms := []Permission{{Type: "read", Role: "user:{id}"}, {Type: "read", Role: "any"}}
	roles := []string{"users", "keys"}
	out := ExpandPermissionTemplates(perms, roles)
	if len(out) != 2 {
		t.Fatalf("expected 2 perms, got %d", len(out))
	}
	if out[0].Role != "user:{id}" || out[1].Role != "any" {
		t.Fatalf("expected placeholders preserved, got %+v", out)
	}
}

func TestExpandPermissionTemplates_MultipleTemplates(t *testing.T) {
	perms := []Permission{
		{Type: "read", Role: "user:{id}"},
		{Type: "read", Role: "group:{id}"},
		{Type: "update", Role: "user:{id}"},
		{Type: "delete", Role: "group:{id}"},
	}
	roles := []string{"user:alice", "group:t2"}
	out := ExpandPermissionTemplates(perms, roles)
	want := []Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "read", Role: "group:t2"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "group:t2"},
	}
	if len(out) != len(want) {
		t.Fatalf("expected %d perms, got %d", len(want), len(out))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("perm %d: expected %+v, got %+v", i, want[i], out[i])
		}
	}
}

func TestExpandPermissionTemplates_EmptyPermsUnchanged(t *testing.T) {
	roles := []string{"user:alice", "group:t1"}
	if out := ExpandPermissionTemplates(nil, roles); out != nil {
		t.Fatalf("nil input should stay nil, got %+v", out)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
