package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New(nil).Migrations()
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
	grants := iam.New(nil).Permissions()
	require.Len(t, grants, 1+1) // iam.member.manage, iam.credential.revoke
	require.Equal(t, iam.PermMemberManage, grants[0].Permission)
	// tenant_admin is implicit, so the grant lists no roles.
	require.Empty(t, grants[0].Roles)
}

// TestModuleDeclaresCredentialRevoke is #781's permission-declaration
// half: PermCredentialRevoke must be declared with NO system roles, so
// tenant_admin — the reconciler's implicit grant — is the only role that
// ever holds it. Listing a role here would let a non-admin end another
// subject's sessions platform-wide.
func TestModuleDeclaresCredentialRevoke(t *testing.T) {
	grants := iam.New(nil).Permissions()
	var found *authz.Grant
	for i := range grants {
		if grants[i].Permission == iam.PermCredentialRevoke {
			found = &grants[i]
		}
	}
	require.NotNil(t, found, "iam.credential.revoke must be declared")
	require.Empty(t, found.Roles, "iam.credential.revoke must list no system roles; only tenant_admin's implicit grant may hold it")
}
