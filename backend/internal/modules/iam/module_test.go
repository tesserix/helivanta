package iam_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New(nil).Migrations()
	ids := make([]string, len(migs))
	for i, migration := range migs {
		ids[i] = migration.ID
	}
	require.Len(t, ids, 50)
	require.Equal(t, "0050_iam_operating_governance", migs[49].ID)
	require.NotEmpty(t, migs[49].SQL)
	require.Equal(t, "0049_iam_assignment_governance", migs[48].ID)
	require.NotEmpty(t, migs[48].SQL)
	require.Equal(t, "0048_iam_governance_employment_bases", migs[47].ID)
	require.NotEmpty(t, migs[47].SQL)
	require.Equal(t, "0047_iam_governance", migs[46].ID)
	require.NotEmpty(t, migs[46].SQL)
	require.Equal(t, "0046_iam_supplier_acceptance_templates", migs[45].ID)
	require.Equal(t, "0042_iam_purchase_order_templates", migs[41].ID)
	require.Equal(t, "0043_iam_order_operating_templates", migs[42].ID)
	require.Equal(t, "0044_iam_fulfilment_operating_templates", migs[43].ID)
	require.Equal(t, "0045_iam_supplier_receiving_templates", migs[44].ID)
	require.Equal(t, "0432bdd5d479ab23cdaccbe033fe76d5f896e91be1c433f423e92da0d33e4d1c", fmt.Sprintf("%x", sha256.Sum256([]byte(migs[42].SQL))), "the published operating-template migration must remain byte-for-byte immutable")
	require.Equal(t, "0041_iam_stock_quality_template", migs[40].ID)
	require.NotEmpty(t, migs[40].SQL)
	require.Equal(t, "0038_iam_operating_duties", migs[37].ID)
	require.NotEmpty(t, migs[37].SQL)
	require.Equal(t, "0039_iam_supplier_grants", migs[38].ID)
	require.NotEmpty(t, migs[38].SQL)
	require.Equal(t, "0040_iam_supplier_access_templates", migs[39].ID)
	require.NotEmpty(t, migs[39].SQL)
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
	require.Equal(t, "0018_iam_membership_enrollments", migs[17].ID)
	require.Equal(t, "0019_iam_membership_recovery_admission", migs[18].ID)
	require.Equal(t, "0020_iam_hr_intake_templates", migs[19].ID)
	require.NotEmpty(t, migs[19].SQL)
	require.Equal(t, "0021_iam_employment_templates", migs[20].ID)
	require.NotEmpty(t, migs[20].SQL)
	require.Equal(t, "0022_iam_employment_activation_templates", migs[21].ID)
	require.NotEmpty(t, migs[21].SQL)
	require.Equal(t, "0023_iam_employment_bases", migs[22].ID)
	require.NotEmpty(t, migs[22].SQL)
	require.Equal(t, "0024_iam_staff_posting_templates", migs[23].ID)
	require.NotEmpty(t, migs[23].SQL)
	require.Equal(t, "0025_iam_staff_posting_review_templates", migs[24].ID)
	require.NotEmpty(t, migs[24].SQL)
	require.Equal(t, "0026_iam_staff_posting_management_templates", migs[25].ID)
	require.NotEmpty(t, migs[25].SQL)
	require.Equal(t, "0027_iam_staff_posting_cutoff_templates", migs[26].ID)
	require.Equal(t, "0028_iam_staff_posting_source_templates", migs[27].ID)
	require.NotEmpty(t, migs[26].SQL)
	require.Equal(t, "0029_iam_pharmacy_department_binding_templates", migs[28].ID)
	require.Equal(t, "0030_iam_department_posting_review_templates", migs[29].ID)
	require.NotEmpty(t, migs[29].SQL)
	require.Equal(t, "0031_iam_department_assignment_scope", migs[30].ID)
	require.NotEmpty(t, migs[30].SQL)
	require.Equal(t, "0032_iam_department_leave_review_templates", migs[31].ID)
	require.Contains(t, migs[31].SQL, "'workforce.department_leave_reviewer.v1' THEN ARRAY['department']")
	require.Equal(t, "0033_iam_establishment_root", migs[32].ID)
	require.Contains(t, migs[32].SQL, "CREATE POLICY establishment_root ON iam_assignment_proposals AS RESTRICTIVE")
	for i, id := range []string{"0034_iam_partner_groups", "0035_iam_partner_membership_templates", "0036_iam_partner_members"} {
		require.Equal(t, id, migs[33+i].ID)
		require.NotEmpty(t, migs[33+i].SQL)
	}

}

func TestPharmacyDepartmentBindingTemplatesArePersisted(t *testing.T) {
	for _, migration := range iam.New(nil).Migrations() {
		if migration.ID == "0029_iam_pharmacy_department_binding_templates" {
			require.Contains(t, migration.SQL, "pharmacy.department_binding_preparer.v1")
			require.Contains(t, migration.SQL, "pharmacy.department_binding_reviewer.v1")
			require.Contains(t, migration.SQL, "workforce.staff_actor.use")
			return
		}
	}
	t.Fatal("the canonical Pharmacy relationship templates must have a persisted IAM migration")
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
	grants := map[authz.Permission][]authz.Role{}
	for _, grant := range iam.New(nil).Permissions() {
		require.NotContains(t, grants, grant.Permission, "permission declarations must be unique")
		grants[grant.Permission] = grant.Roles
	}
	for _, permission := range []authz.Permission{iam.PermMemberManage, iam.PermAssignmentManage, iam.PermAssignmentReview, iam.PermPartnerGroupMemberRead, iam.PermPartnerGroupMemberPrepare, iam.PermPartnerGroupMemberReview, iam.PermCredentialRevoke} {
		require.Contains(t, grants, permission)
		require.Empty(t, grants[permission], "%s remains tenant-admin-only by default", permission)
	}
}

func TestOperatingAdministrationKeepsSourceRolesSeparate(t *testing.T) {
	expected := map[authz.Permission][]authz.Role{
		iam.PermOperatingRead:    {authz.RoleSupplyOwner, authz.RoleSupplyAdmin, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor, authz.RolePharmacyOwner},
		iam.PermOperatingPrepare: {authz.RoleSupplyOwner, authz.RoleSupplyAdmin, authz.RolePharmacyOwner},
		iam.PermOperatingReview:  {authz.RoleSupplyOwner, authz.RoleSupplyAdmin, authz.RoleSupplyCompliance, authz.RolePharmacyOwner},
		iam.PermOperatingRevoke:  {authz.RoleSupplyOwner, authz.RoleSupplyAdmin, authz.RolePharmacyOwner},
	}
	for _, grant := range iam.New(nil).Permissions() {
		if roles, ok := expected[grant.Permission]; ok {
			require.ElementsMatch(t, roles, grant.Roles, "%s must not grant broader source administration", grant.Permission)
			delete(expected, grant.Permission)
		}
	}
	require.Empty(t, expected, "all reviewed operating permissions must be registered")
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

func TestStockQualityTemplateExpansionPreservesReceivingPermissions(t *testing.T) {
	migrations := iam.New(nil).Migrations()
	require.NotContains(t, migrations[37].SQL, "supply.stock.review")
	require.Contains(t, migrations[40].SQL, "CREATE OR REPLACE FUNCTION iam_operating_template_permissions")
	require.Contains(t, migrations[40].SQL, "supply.stock_quality_reviewer.v1")
	require.Contains(t, migrations[40].SQL, "supply.inventory_receiver.v1")
}
