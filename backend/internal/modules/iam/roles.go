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
// went missing from this catalog after being added to authz.
//
// A role authz knows about but systemRoleLabels does not label is a
// programming error (a new role shipped without updating the label
// map), not a condition this package papers over: it returns an error
// rather than an incomplete or panicking catalog, so the caller — a
// live HTTP handler — can fail the request through the normal error
// path instead of taking the whole process down or serving a silently
// short list.
func SystemRoles() ([]SystemRole, error) {
	roles := authz.SystemRoles()
	out := make([]SystemRole, 0, len(roles))
	for _, key := range roles {
		label, ok := systemRoleLabels[key]
		if !ok {
			return nil, fmt.Errorf("iam: no label registered for system role %q", key)
		}
		out = append(out, SystemRole{Key: key, Label: label})
	}
	return out, nil
}

// listRoles returns the system role catalog. It takes no dependencies —
// the catalog is a fixed, in-process list — so it is a plain function
// rather than a method on a handler struct.
func listRoles(c *gin.Context) {
	roles, err := SystemRoles()
	if err != nil {
		respond.InternalErr(c, err, "could not list system roles")
		return
	}
	respond.OK(c, gin.H{"data": roles})
}
