package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New(nil).Migrations()
	require.Len(t, migs, 17)
	require.Equal(t, "0001_iam", migs[0].ID)
	require.Equal(t, "0002_iam", migs[1].ID)
	require.Equal(t, "0003_iam", migs[2].ID)
	require.Equal(t, "0004_iam", migs[3].ID)
	require.Equal(t, "0005_iam", migs[4].ID)
	require.Equal(t, "0006_iam", migs[5].ID)
	require.Equal(t, "0007_iam", migs[6].ID)
	require.Equal(t, "0008_iam_assignment_proposals", migs[7].ID)
	require.Equal(t, "0009_iam_assignment_decisions", migs[8].ID)
	require.Equal(t, "0010_iam_assignment_projection", migs[9].ID)
	require.Equal(t, "0011_iam_assignment_membership", migs[10].ID)
	require.Equal(t, "0012_iam_assignment_activation", migs[11].ID)
	require.Equal(t, "0013_iam_identity_guard", migs[12].ID)
	require.Equal(t, "0014_iam_staff_templates", migs[13].ID)
	require.Equal(t, "0015_iam_membership_commands", migs[14].ID)
	require.Equal(t, "0016_iam_membership_candidate_index", migs[15].ID)
	require.Equal(t, "iam_members_candidate_history", migs[15].ConcurrentIndexes[0].Name)
	require.Equal(t, "0017_iam_membership_projection_reader", migs[16].ID)
}

// TestSystemRolesMatchesAuthzRegistry pins iam.SystemRoles() to
// authz.SystemRoles(): same keys, same order. iam.SystemRoles() derives
// from the authz registry rather than restating it, so this is really a
// test that the derivation didn't drop or reorder anything, not a test
// of a second hand-maintained list.
func TestSystemRolesMatchesAuthzRegistry(t *testing.T) {
	roles, err := iam.SystemRoles()
	require.NoError(t, err)

	keys := make([]authz.Role, 0, len(roles))
	for _, r := range roles {
		keys = append(keys, r.Key)
	}
	require.Equal(t, authz.SystemRoles(), keys)
}

// TestEverySystemRoleHasALabel guards the label map iam.SystemRoles()
// reads against: every role authz.SystemRoles() knows about must have a
// display label, or a new authz role ships with no way to grant it
// through the product (#70 fix round 1 — receptionist shipped in authz
// but stayed absent from this catalog until this test existed).
func TestEverySystemRoleHasALabel(t *testing.T) {
	roles, err := iam.SystemRoles()
	require.NoError(t, err)

	labels := make(map[authz.Role]string, len(roles))
	for _, r := range roles {
		labels[r.Key] = r.Label
	}
	for _, role := range authz.SystemRoles() {
		label, ok := labels[role]
		require.True(t, ok, "system role %q has no label in iam.SystemRoles()", role)
		require.NotEmpty(t, label, "system role %q has a blank label", role)
	}
}

func TestModuleDeclaresMemberManage(t *testing.T) {
	grants := iam.New(nil).Permissions()
	require.Len(t, grants, 4)
	require.Equal(t, iam.PermAssignmentReview, grants[2].Permission)
	require.Empty(t, grants[2].Roles)
	require.Equal(t, iam.PermAssignmentManage, grants[1].Permission)
	require.Empty(t, grants[1].Roles, "proposals are administered through the explicit tenant-admin baseline")
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
