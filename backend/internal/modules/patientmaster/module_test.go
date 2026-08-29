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
