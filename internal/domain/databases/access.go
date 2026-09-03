package databases

// Principal is the access context for document operations.
// It is constructed by the app layer from shared.Principal and passed to
// DocumentDB implementations. Keeping it in domain/databases avoids a
// dependency on internal/domain/shared from the document port.
type Principal struct {
	// Roles carries the caller's role strings (e.g. "users", "user:<id>",
	// "group:<id>", "keys", "any"). The "__system__" role is the document
	// projection of ActorKind System and bypasses document-level checks.
	Roles []string

	// PlatformAdmin indicates the caller is a console admin with full
	// access (bypasses document-level permission checks). 这不是 System actor.
	PlatformAdmin bool

	// KeyID 是 API key 主体（ActorKind=Service）的 key ID，用于写入归因：
	// _created_by/_updated_by 落 "key:<id>"（redesign §10.2-1——一等 Agent
	// 的最低要求是行为可归因）。非 key 主体为空。
	KeyID string
}

// GuestPrincipal is used for unauthenticated Client API read requests.
var GuestPrincipal = Principal{Roles: []string{"guests"}}

// SystemPrincipal is the principal used by internal infrastructure paths
// (session validation, post-create reads, email lookup). It bypasses all
// document-level permission checks.
var SystemPrincipal = Principal{
	Roles: []string{"__system__"},
}

// HasRole reports whether the principal holds the given role.
func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsSystem reports whether this is the internal System actor projection
// (__system__ 角色)，不是 PlatformAdmin。
func (p Principal) IsSystem() bool {
	return p.HasRole("__system__")
}

// BypassesDocumentACL reports whether document permission checks should be
// skipped. System（内部）与 PlatformAdmin（console）都旁路，但它们不是同一 Actor.
func (p Principal) BypassesDocumentACL() bool {
	return p.IsSystem() || p.PlatformAdmin
}
