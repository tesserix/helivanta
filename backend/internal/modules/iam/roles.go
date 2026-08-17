package iam

import (
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

func SystemRoles() []SystemRole {
	return []SystemRole{
		{Key: authz.RoleTenantAdmin, Label: "Tenant Admin"},
		{Key: authz.RoleDoctor, Label: "Doctor"},
		{Key: authz.RoleNurse, Label: "Nurse"},
		{Key: authz.RolePharmacist, Label: "Pharmacist"},
		{Key: authz.RoleLabTech, Label: "Lab Technician"},
	}
}

// listRoles returns the system role catalog. It takes no dependencies —
// the catalog is a fixed, in-process list — so it is a plain function
// rather than a method on a handler struct.
func listRoles(c *gin.Context) {
	respond.OK(c, gin.H{"data": SystemRoles()})
}
