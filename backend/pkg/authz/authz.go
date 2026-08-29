// Package authz is the single authorization decision point. Permissions
// are FGA objects granted to role objects, so adding a zone, permission
// or role is a tuple write — the authorization model never changes.
package authz

import "sort"

// Permission names an action, formatted "<module>.<resource>.<action>".
type Permission string

// Public marks a route as declaring no permission requirement. It is a
// real value rather than an empty string so that an unguarded route is
// greppable and can never be created by forgetting an argument.
//
// Public does NOT opt out of tenant membership. A route that declares no
// permission still only serves members of the tenant its caller's token
// names — see NoTenantMembership for the narrower opt-out, and #781 for
// the defect that existed while these two were the same thing: a caller
// whose membership had been revoked resolved to an empty permission set,
// but PermissionSet.Has(Public) answered true unconditionally, so the
// empty set was never actually consulted.
const Public Permission = "public"

// NoTenantMembership marks the handful of routes that must serve a
// caller who is not (or is no longer) a member of the tenant their token
// names: the self-service routes answering "where do I belong?" and
// sign-out. Every one of them gates itself.
//
// Deliberately awkward to type, and pinned by
// TestNoTenantMembershipAllowlist in internal/archtest to an explicit
// list — the safety of every other route depends on this set staying
// small, so growing it must require editing an allowlist a reviewer
// sees.
const NoTenantMembership Permission = "no_tenant_membership"

// Role is a role key. The roles below ship as seeded system roles;
// because roles are data, tenants may define others without a model
// change.
type Role string

const (
	RoleTenantAdmin Role = "tenant_admin"
	RoleDoctor      Role = "doctor"
	RoleNurse       Role = "nurse"
	RolePharmacist  Role = "pharmacist"
	RoleLabTech     Role = "lab_tech"

	// RoleReceptionist is the hospital front desk: registration, and in
	// time appointments and queueing. Named for the person rather than
	// for one feature's view of them, so it stays correct as the front
	// office gains responsibilities. A nurse is not a clerk — conflating
	// the two would put clinical staff in the registration audit trail.
	RoleReceptionist Role = "receptionist"
)

// systemRoles is every seeded system role. It is the single source of
// truth KnownRole validates against, so a role_key read from anywhere
// outside this package (an HTTP request body, a raw-SQL row from
// iam_members) can be checked without hand-copying the role list.
var systemRoles = []Role{RoleTenantAdmin, RoleDoctor, RoleNurse, RolePharmacist, RoleLabTech, RoleReceptionist}

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

// SystemRoles returns every seeded system role, in declaration order.
// It exists so that code which must SHOW an operator the valid roles —
// cmd/bootstrap's rejection message for an unknown -role — reads the
// same list KnownRole validates against, instead of hand-copying it and
// drifting the day a sixth role is added. The slice is a copy, so a
// caller cannot mutate the source of truth.
func SystemRoles() []Role {
	return append([]Role(nil), systemRoles...)
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

// Has reports whether the set carries p.
//
// It used to answer true unconditionally for Public, which meant an
// unguarded route never consulted the set at all — and therefore served
// a caller whose set was empty because their membership had been
// revoked (#781). Public is now handled by Require, which skips the
// permission check explicitly rather than by asking a set a question it
// answers dishonestly.
func (s PermissionSet) Has(p Permission) bool {
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
