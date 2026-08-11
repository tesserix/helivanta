// Package authz is the single authorization decision point. Permissions
// are FGA objects granted to role objects, so adding a zone, permission
// or role is a tuple write — the authorization model never changes.
package authz

import "sort"

// Permission names an action, formatted "<module>.<resource>.<action>".
type Permission string

// Public marks a route as deliberately unguarded. It is a real value
// rather than an empty string so that an unguarded route is greppable
// and can never be created by forgetting an argument.
const Public Permission = "public"

// Role is a role key. The five below ship as seeded system roles;
// because roles are data, tenants may define others without a model
// change.
type Role string

const (
	RoleTenantAdmin Role = "tenant_admin"
	RoleDoctor      Role = "doctor"
	RoleNurse       Role = "nurse"
	RolePharmacist  Role = "pharmacist"
	RoleLabTech     Role = "lab_tech"
)

// systemRoles is every seeded system role. It is the single source of
// truth KnownRole validates against, so a role_key read from anywhere
// outside this package (an HTTP request body, a raw-SQL row from
// iam_members) can be checked without hand-copying the role list.
var systemRoles = []Role{RoleTenantAdmin, RoleDoctor, RoleNurse, RolePharmacist, RoleLabTech}

// KnownRole reports whether r is one of the seeded system roles. Custom
// roles are supported by the data model but not yet creatable, so
// anything else read from an untrusted source (a request, a raw-SQL
// scan) is a value that must never be trusted enough to write an
// authorization tuple for.
func KnownRole(r Role) bool {
	for _, sr := range systemRoles {
		if sr == r {
			return true
		}
	}
	return false
}

// Grant declares that a permission is held by the listed system roles.
// Modules return these from Permissions(); the reconciler turns them
// into tuples. RoleTenantAdmin is implicit — the reconciler grants it
// every declared permission, so modules never list it.
type Grant struct {
	Permission Permission
	Roles      []Role
}

// PermissionSet is a caller's resolved permissions for one tenant.
type PermissionSet map[Permission]struct{}

func NewPermissionSet(perms ...Permission) PermissionSet {
	s := make(PermissionSet, len(perms))
	for _, p := range perms {
		s[p] = struct{}{}
	}
	return s
}

// Has reports whether the set carries p. Public is always allowed, so
// unguarded routes need no special-casing at the call site.
func (s PermissionSet) Has(p Permission) bool {
	if p == Public {
		return true
	}
	_, ok := s[p]
	return ok
}

// Sorted returns the permissions as a stable, non-nil string slice so
// the /iam/me/permissions response is deterministic and marshals to []
// rather than null when empty.
func (s PermissionSet) Sorted() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return out
}
