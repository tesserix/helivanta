package iam

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
)

// SystemRole is a role seeded into every tenant. Because roles are data
// rather than model relations, a tenant may define additional roles
// without a model change or redeploy; this phase ships no UI for that.
type SystemRole struct {
	Key   authz.Role
	Label string
}

// systemRoleLabels is the presentation label for every seeded system
// role. Keyed by authz.Role rather than restating the role list, so the
// set of grantable roles has exactly one source of truth
// (authz.SystemRoles) and this map only answers "what do we call it"
// for each one that already exists there.
var systemRoleLabels = map[authz.Role]string{
	authz.RoleTenantAdmin:  "Tenant Admin",
	authz.RoleDoctor:       "Doctor",
	authz.RoleNurse:        "Nurse",
	authz.RolePharmacist:   "Pharmacist",
	authz.RoleLabTech:      "Lab Technician",
	authz.RoleReceptionist: "Receptionist",
}

// SystemRoles returns the role catalog the product exposes for granting:
// every role authz.SystemRoles() knows about, labelled for display, in
// the order authz.SystemRoles() returns them. It derives from
// authz.SystemRoles() rather than restating the role list — two
// hand-maintained lists of the same roles is exactly how receptionist
// went missing from this catalog after being added to authz. A role
// authz knows about but this map does not label is a programming error
// (a new role shipped without updating systemRoleLabels), not a runtime
// condition to paper over: panicking turns a silently incomplete role
// picker into an immediate, loud failure instead. TestEverySystemRoleHasALabel
// is meant to catch this in CI before it ever reaches here.
func SystemRoles() []SystemRole {
	roles := authz.SystemRoles()
	out := make([]SystemRole, 0, len(roles))
	for _, key := range roles {
		label, ok := systemRoleLabels[key]
		if !ok {
			panic(fmt.Sprintf("iam: no label registered for system role %q", key))
		}
		out = append(out, SystemRole{Key: key, Label: label})
	}
	return out
}

// listRoles returns the system role catalog. It takes no dependencies —
// the catalog is a fixed, in-process list — so it is a plain function
// rather than a method on a handler struct.
func listRoles(c *gin.Context) {
	respond.OK(c, gin.H{"data": SystemRoles()})
}
