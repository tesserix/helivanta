package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New(nil).Migrations()
	require.Len(t, migs, 5)
	require.Equal(t, "0001_iam", migs[0].ID)
	require.Equal(t, "0002_iam", migs[1].ID)
	require.Equal(t, "0003_iam", migs[2].ID)
	require.Equal(t, "0004_iam", migs[3].ID)
	require.Equal(t, "0005_iam", migs[4].ID)
}

// TestSystemRolesMatchesAuthzRegistry pins iam.SystemRoles() to
// authz.SystemRoles(): same keys, same order. iam.SystemRoles() derives
// from the authz registry rather than restating it, so this is really a
// test that the derivation didn't drop or reorder anything, not a test
// of a second hand-maintained list.
func TestSystemRolesMatchesAuthzRegistry(t *testing.T) {
	keys := make([]authz.Role, 0, len(iam.SystemRoles()))
	for _, r := range iam.SystemRoles() {
		keys = append(keys, r.Key)
	}
	require.Equal(t, authz.SystemRoles(), keys)
}

// TestEverySystemRoleHasALabel guards the map iam.SystemRoles() panics
// on a miss against: every role authz.SystemRoles() knows about must
// have a display label here, or a new authz role ships with no way to
// grant it through the product (#70 fix round 1 — receptionist shipped
// in authz but stayed absent from this catalog until this test existed).
func TestEverySystemRoleHasALabel(t *testing.T) {
	labels := make(map[authz.Role]string, len(iam.SystemRoles()))
	for _, r := range iam.SystemRoles() {
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
