// Package rbac defines the application's permission catalog. Permissions are
// code-defined: a permission string only carries meaning where the code
// enforces it with a requirePermission check, so the catalog here — not the
// database — is the single source of truth for which permissions exist.
package rbac

// Permission is a code-defined capability identifier in "resource:action" form.
type Permission string

const (
	// PermissionAll is a reserved wildcard that grants every permission. It is
	// held only by the built-in admin role and must never be assigned to a
	// custom role.
	PermissionAll Permission = "*"

	PermissionUsersRead        Permission = "users:read"
	PermissionUsersManage      Permission = "users:manage"
	PermissionRolesRead        Permission = "roles:read"
	PermissionRolesManage      Permission = "roles:manage"
	PermissionSystemEventsRead Permission = "system_events:read"
)

// Catalog is every assignable permission in a stable order. PermissionAll is
// deliberately excluded — it is the admin-only wildcard and cannot be granted
// to a custom role. Every entry here is enforced by a requirePermission check;
// register a new module's permissions here as that enforcement is added.
var Catalog = []Permission{
	PermissionUsersRead,
	PermissionUsersManage,
	PermissionRolesRead,
	PermissionRolesManage,
	PermissionSystemEventsRead,
}

// IsAssignable reports whether p is a real catalog permission that may be
// granted to a custom role. The wildcard and unknown strings are not.
func IsAssignable(p Permission) bool {
	for _, c := range Catalog {
		if c == p {
			return true
		}
	}
	return false
}

// Set is a user's resolved permissions — the union across all of their roles.
type Set struct {
	perms map[Permission]struct{}
}

// NewSet builds a permission Set from raw permission strings (as stored in
// role_permissions). Unknown strings are kept as-is; Has only ever returns
// true for a real catalog permission or via the wildcard.
func NewSet(raw []string) Set {
	perms := make(map[Permission]struct{}, len(raw))
	for _, r := range raw {
		perms[Permission(r)] = struct{}{}
	}
	return Set{perms: perms}
}

// Has reports whether the set grants permission p, either directly or through
// the PermissionAll wildcard.
func (s Set) Has(p Permission) bool {
	if _, ok := s.perms[PermissionAll]; ok {
		return true
	}
	_, ok := s.perms[p]
	return ok
}

// List returns the granted permissions as strings in catalog order, with the
// wildcard (if present) first. It is suitable for returning to clients so a
// frontend can render menus.
func (s Set) List() []string {
	out := make([]string, 0, len(s.perms))
	if _, ok := s.perms[PermissionAll]; ok {
		out = append(out, string(PermissionAll))
	}
	for _, c := range Catalog {
		if _, ok := s.perms[c]; ok {
			out = append(out, string(c))
		}
	}
	return out
}
