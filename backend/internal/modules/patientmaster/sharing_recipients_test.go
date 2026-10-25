package patientmaster_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSharingRecipientRequiresIndependentHospitalReviewAndTerminalRevocation(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	path := "/v1/patients/sharing-recipients/" + uuid.Must(uuid.NewV7()).String()
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"organization_reference":%q,"display_name":"Synthetic external facility","grantee_subject":"user-external-facility","policy_reference":%q,"identity_evidence_reference":%q,"valid_until":%q}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), until)
	proposed := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 201, proposed.Code, proposed.Body.String())
	require.Contains(t, proposed.Body.String(), `"status":"draft"`)
	require.Equal(t, 403, scopedDo(r, "GET", path, "owner", "").Code)
	require.Equal(t, 404, scopedDo(r, "GET", path, "foreign", "").Code)
	require.Equal(t, 404, scopedDo(r, "GET", path, "ungranted", "").Code)
	approve := fmt.Sprintf(`{"expected_version":1,"identity_confirmed":true,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 409, scopedDo(r, "POST", path+"/approve", "reviewer", approve).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-ungranted','registrar')`, testutil.TenantA, testHospital).Error
	}))
	approved := scopedDo(r, "POST", path+"/approve", "ungranted", approve)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	require.Contains(t, approved.Body.String(), `"status":"approved"`)
	require.Contains(t, approved.Body.String(), `"version":2`)
	replay := scopedDo(r, "POST", path+"/approve", "ungranted", approve)
	require.JSONEq(t, approved.Body.String(), replay.Body.String())
	require.Equal(t, 409, scopedDo(r, "PUT", path, "reviewer", strings.Replace(body, "Synthetic external facility", "Changed facility", 1)).Code)
	revoke := fmt.Sprintf(`{"expected_version":2,"reason_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	revoked := scopedDo(r, "POST", path+"/revoke", "reviewer", revoke)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.Contains(t, revoked.Body.String(), `"version":3`)
	replay = scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, revoked.Body.String(), replay.Body.String())
	replay = scopedDo(r, "POST", path+"/approve", "ungranted", approve)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, revoked.Body.String(), replay.Body.String())
	replay = scopedDo(r, "POST", path+"/revoke", "reviewer", revoke)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, revoked.Body.String(), replay.Body.String())
}

func TestSharingRecipientValidationAndCurrentHospitalScope(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/sharing-recipients/" + id
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"organization_reference":"synthetic-organization","display_name":"Synthetic facility","grantee_subject":"user-ungranted","policy_reference":%q,"identity_evidence_reference":%q,"valid_until":%q}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), until)
	for _, invalid := range []string{
		strings.Replace(body, "Synthetic facility", "", 1),
		strings.Replace(body, "user-ungranted", " user-ungranted", 1),
		strings.Replace(body, "Synthetic facility", string([]byte{0xff}), 1),
		strings.Replace(body, "synthetic-organization", strings.Repeat("x", 201), 1),
		strings.TrimSuffix(body, "}") + `,"untrusted":true}`,
		body + " {}",
	} {
		require.Equal(t, 400, scopedDo(r, "PUT", path, "reviewer", invalid).Code)
	}
	require.Equal(t, 201, scopedDo(r, "PUT", path, "reviewer", body).Code)
	require.Equal(t, 409, scopedDo(r, "PUT", "/v1/patients/sharing-recipients/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-foreign','registrar')`, testutil.TenantB, testHospital).Error
	}))
	require.Equal(t, 404, scopedDo(r, "GET", path, "foreign", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-ungranted','registrar'),(?,'22222222-2222-4222-8222-222222222222','user-reviewer','registrar')`, testutil.TenantA, testHospital, testutil.TenantA).Error
	}))
	other := path + "?hospital_id=22222222-2222-4222-8222-222222222222"
	require.Equal(t, 404, do(r, "GET", other, "reviewer", "").Code)
	require.Equal(t, 404, do(r, "PUT", other, "reviewer", body).Code)
	approval := fmt.Sprintf(`{"expected_version":1,"identity_confirmed":true,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 409, scopedDo(r, "POST", path+"/approve", "ungranted", approval).Code)
	require.Equal(t, 400, scopedDo(r, "POST", path+"/approve", "ungranted", strings.Replace(approval, `"identity_confirmed":true`, `"identity_confirmed":false`, 1)).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM patient_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-reviewer'`, testutil.TenantA, testHospital).Error
	}))
	require.Equal(t, 404, scopedDo(r, "GET", path, "reviewer", "").Code)
	require.Equal(t, 404, scopedDo(r, "PUT", path, "reviewer", body).Code)
}

func TestSharingRecipientOutboxFailureRollsBackHistoryAndAudit(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_recipient_outbox_failure", SQL: `CREATE FUNCTION reject_recipient_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.sharing_recipient_changed.v1' THEN RAISE EXCEPTION 'synthetic recipient outbox outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_recipient_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_recipient_outbox();`}}))
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/sharing-recipients/" + id
	body := fmt.Sprintf(`{"organization_reference":"synthetic-organization","display_name":"Synthetic facility","grantee_subject":"user-external","policy_reference":%q,"identity_evidence_reference":%q,"valid_until":%q}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano))
	result := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 503, result.Code, result.Body.String())
	require.NotContains(t, result.Body.String(), "synthetic recipient outbox outage")
	require.Equal(t, 404, scopedDo(r, "GET", path, "reviewer", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("audit_events").Where("tenant_id=? AND resource_id=?", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}
