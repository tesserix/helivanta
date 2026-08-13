package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New().Migrations()
	require.Len(t, migs, 3)
	require.Equal(t, "0001_iam", migs[0].ID)
	require.Equal(t, "0002_iam", migs[1].ID)
	require.Equal(t, "0003_iam", migs[2].ID)
}

func TestSystemRolesAreTheFiveShippedRoles(t *testing.T) {
	keys := make([]authz.Role, 0, len(iam.SystemRoles()))
	for _, r := range iam.SystemRoles() {
		keys = append(keys, r.Key)
	}
	require.ElementsMatch(t, []authz.Role{
		authz.RoleTenantAdmin, authz.RoleDoctor, authz.RoleNurse,
		authz.RolePharmacist, authz.RoleLabTech,
	}, keys)
}

func TestModuleDeclaresMemberManage(t *testing.T) {
	grants := iam.New().Permissions()
	require.Len(t, grants, 1)
	require.Equal(t, iam.PermMemberManage, grants[0].Permission)
	// tenant_admin is implicit, so the grant lists no roles.
	require.Empty(t, grants[0].Roles)
}
