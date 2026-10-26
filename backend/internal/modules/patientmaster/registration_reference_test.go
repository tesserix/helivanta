package patientmaster_test

import (
	"fmt"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestRegistrationReferenceThroughGlobalAPIExposesOnlyAuditedScope(t *testing.T) {
	owner, db, ctx := setupHTTP(t)
	created := scopedDo(owner, "POST", "/v1/patients", "tokA", registerBody("PrivateReference", "Synthetic", "1990-01-01", "", false, ""))
	require.Equal(t, 201, created.Code, created.Body.String())
	id := registeredID(t, created)
	gateway := consentAuthorityGateway(t, owner)
	path := "/v1/patients/" + id + "/registration-reference?hospital_id=" + testHospital
	response := patientExpectedTenant(gateway, "GET", path, "tokA", "", []string{testutil.TenantA})
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"patient_id":"%s","hospital_id":"%s","record_access_authorized":false}`, id, testHospital), response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("patient_access_audit").Where("actor_subject=? AND hospital_id=? AND action='read' AND resource_ids @> ?::jsonb", "user-tokA", testHospital, fmt.Sprintf(`["%s"]`, id)).Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
	foreign := patientExpectedTenant(gateway, "GET", path, "tokB", "", []string{testutil.TenantB})
	require.Equal(t, 404, foreign.Code, foreign.Body.String())
	mismatch := patientExpectedTenant(gateway, "GET", path, "tokA", "", []string{testutil.TenantB})
	require.Equal(t, 409, mismatch.Code, mismatch.Body.String())
	for _, invalid := range []string{
		"/v1/patients/" + uuid.NewString() + "/registration-reference?hospital_id=" + testHospital,
		"/v1/patients/" + id + "/registration-reference?hospital_id=" + uuid.NewString(),
		"/v1/patients/" + id + "/registration-reference?hospital_id=" + testHospital + "&hospital_id=" + testHospital,
	} {
		denied := patientExpectedTenant(gateway, "GET", invalid, "tokA", "", nil)
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	revoke := do(owner, "DELETE", "/v1/patients/hospitals/"+testHospital+"/access/user-tokA", "tokA", "")
	require.Equal(t, 200, revoke.Code, revoke.Body.String())
	revoked := patientExpectedTenant(gateway, "GET", path, "tokA", "", nil)
	require.Equal(t, 404, revoked.Code, revoked.Body.String())
}

func TestRegistrationReferenceDependencyOutageDoesNotReleaseIdentifiers(t *testing.T) {
	for _, dependency := range []string{"audit", "hospital_authority"} {
		t.Run(dependency, func(t *testing.T) {
			owner, db, _ := setupHTTP(t)
			created := scopedDo(owner, "POST", "/v1/patients", "tokA", registerBody("PrivateReference", "Synthetic", "1990-01-01", "", false, ""))
			require.Equal(t, 201, created.Code, created.Body.String())
			id := registeredID(t, created)
			gateway := consentAuthorityGateway(t, owner)
			path := "/v1/patients/" + id + "/registration-reference?hospital_id=" + testHospital
			outage := `CREATE FUNCTION reject_reference_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' THEN RAISE EXCEPTION 'synthetic private audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_reference_audit BEFORE INSERT ON patient_access_audit FOR EACH ROW EXECUTE FUNCTION reject_reference_audit();`
			recovery := `DROP TRIGGER reject_reference_audit ON patient_access_audit;`
			if dependency == "hospital_authority" {
				outage = `ALTER TABLE patient_hospital_access RENAME TO reference_access_unavailable;`
				recovery = `ALTER TABLE reference_access_unavailable RENAME TO patient_hospital_access;`
			}
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_reference_outage", SQL: outage}}))
			denied := patientExpectedTenant(gateway, "GET", path, "tokA", "", nil)
			require.Equal(t, 503, denied.Code, denied.Body.String())
			require.Contains(t, denied.Body.String(), "registration_reference_unavailable")
			for _, private := range []string{id, testHospital, "PrivateReference", "synthetic", "patient_hospital_access"} {
				require.NotContains(t, denied.Body.String(), private)
			}
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_reference_recovery", SQL: recovery}}))
			recovered := patientExpectedTenant(gateway, "GET", path, "tokA", "", nil)
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, fmt.Sprintf(`{"patient_id":"%s","hospital_id":"%s","record_access_authorized":false}`, id, testHospital), recovered.Body.String())
		})
	}
}

func TestRegistrationReferenceRequiresReadPermission(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"peer": testutil.TenantA}, Perms: map[string][]authz.Permission{"peer": {}}, Modules: []platform.Module{patientmaster.New()}})
	path := "/v1/patients/" + uuid.NewString() + "/registration-reference?hospital_id=" + testHospital
	denied := do(r, "GET", path, "peer", "")
	require.Equal(t, 403, denied.Code, denied.Body.String())
}
