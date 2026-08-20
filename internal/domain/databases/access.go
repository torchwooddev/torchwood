package databases

// Principal is the access context for document operations.
// It is constructed by the app layer from shared.Principal and passed to
// DocumentDB implementations. Keeping it in domain/databases avoids a
// dependency on internal/domain/shared from the document port.
type Principal struct {
	// Roles carries the caller's role strings (e.g. "users", "user:<id>",
	// "group:<id>", "keys", "any"). The "__system__" role bypasses all
	// document-level permission checks.
	Roles []string

	// PlatformAdmin indicates the caller is a console admin with full
	// access (bypasses document-level permission checks).
	PlatformAdmin bool
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

// IsSystem reports whether the principal should bypass all document-level
// permission checks.
func (p Principal) IsSystem() bool {
	return p.PlatformAdmin || p.HasRole("__system__")
}
