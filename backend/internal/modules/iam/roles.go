package iam

import "github.com/tesserix/hms/pkg/authz"

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
