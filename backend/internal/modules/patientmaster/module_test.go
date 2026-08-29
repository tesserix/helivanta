package patientmaster_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// setup builds the harness exactly as lab/module_test.go does. This
// module has no routes yet (Task 5 adds them), so only the *tenantdb.DB
// and context matter here — the *gin.Engine is discarded.
func setup(t *testing.T) (*tenantdb.DB, context.Context) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
			"tokB": {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	return db, ctx
}

// insertPatient creates a minimal patient row under tenant and returns
// its id, so tests can construct cross-tenant references deliberately.
func insertPatient(t *testing.T, db *tenantdb.DB, ctx context.Context, tenant, mrn string) uuid.UUID {
	t.Helper()
	var idStr string
	require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		return tx.Raw(`INSERT INTO patients (tenant_id, mrn, given_name, dob, dob_estimated)
			VALUES (?, ?, 'seed', '1990-01-01', false) RETURNING id`, tenant, mrn).Scan(&idStr).Error
	}))
	return uuid.MustParse(idStr)
}

// TestPatientsAreTenantIsolated is the adversarial RLS check: a row
// written by one tenant must be invisible to another, enforced by the
// database rather than by a WHERE clause a handler might forget.
func TestPatientsAreTenantIsolated(t *testing.T) {
	db, ctx := setup(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patients (tenant_id, mrn, given_name, dob, dob_estimated, mobile)
			VALUES (?, 'MRN-1', 'suresh', '1979-04-02', false, '9876543210')`, tenantA).Error
	}))

	countAs := func(tenant string) int {
		var n int
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM patients`).Scan(&n).Error
		}))
		return n
	}
	require.Equal(t, 1, countAs(tenantA))
	require.Equal(t, 0, countAs(tenantB), "tenant B can read tenant A's patients")
}

// TestPatientWriteIsPinnedToOneTenant proves WITH CHECK rejects a row
// stamped for a different tenant than the transaction's.
func TestPatientWriteIsPinnedToOneTenant(t *testing.T) {
	db, ctx := setup(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	err := db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patients (tenant_id, mrn, given_name, dob, dob_estimated)
			VALUES (?, 'MRN-2', 'priya', '1996-08-14', false)`, tenantB).Error
	})
	require.Error(t, err)
}

// TestConsentCannotLinkToAnotherTenantsPatient is the adversarial FK
// check for the hole PostgreSQL's row-security does NOT close: FK
// constraint checks are not subject to RLS, so a plain
// "REFERENCES patients(id)" would let tenant A attach a consent receipt
// to a patient_id it merely knows (guessed, leaked, or logged) but does
// not own. The composite FK — (tenant_id, patient_id) REFERENCES
// patients(tenant_id, id) — is what makes that impossible: no row in
// patients carries the pair (tenantA, patientB's id), so the INSERT is
// rejected at the constraint, before RLS even enters the picture.
func TestConsentCannotLinkToAnotherTenantsPatient(t *testing.T) {
	db, ctx := setup(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	patientB := insertPatient(t, db, ctx, tenantB, "MRN-B1")

	err := db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_consents
			(tenant_id, patient_id, notice_version, consented_by, recorded_by_subject)
			VALUES (?, ?, 'v1', 'patient', 'user-tokA')`, tenantA, patientB).Error
	})
	require.Error(t, err, "tenant A must not be able to attach a consent to tenant B's patient")
}

// TestDuplicateOverrideCannotLinkToAnotherTenantsPatient is the same
// adversarial FK check as TestConsentCannotLinkToAnotherTenantsPatient,
// covering patient_duplicate_overrides.matched_patient_id — the second
// of the table's two patient-referencing columns.
func TestDuplicateOverrideCannotLinkToAnotherTenantsPatient(t *testing.T) {
	db, ctx := setup(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	createdA := insertPatient(t, db, ctx, tenantA, "MRN-A1")
	matchedB := insertPatient(t, db, ctx, tenantB, "MRN-B2")

	err := db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_duplicate_overrides
			(tenant_id, created_patient_id, matched_patient_id, score, reason, actor_subject)
			VALUES (?, ?, ?, 0.900, 'phonetic match', 'user-tokA')`,
			tenantA, createdA, matchedB).Error
	})
	require.Error(t, err, "tenant A must not be able to record an override matched against tenant B's patient")
}
