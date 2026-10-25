package patientmaster_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSharingNoticeDraftRequiresReviewedHospitalScope(t *testing.T) {
	r, _, _ := consentAuthorityFixture(t)
	path := "/v1/patients/sharing-notices/" + uuid.Must(uuid.NewV7()).String()
	body := syntheticSharingNotice()
	response := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 201, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"status":"draft"`)
	require.Contains(t, response.Body.String(), `"title":"Synthetic notice"`)
	replay := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, response.Body.String(), replay.Body.String())
}

func TestSharingNoticeApprovalIsIndependentAndRetirementIsTerminal(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-ungranted','registrar')`, testutil.TenantA, testHospital).Error
	}))
	path := "/v1/patients/sharing-notices/" + uuid.Must(uuid.NewV7()).String()
	body := syntheticSharingNotice()
	require.Equal(t, 201, scopedDo(r, "PUT", path, "reviewer", body).Code)
	approval := fmt.Sprintf(`{"expected_version":1,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 409, scopedDo(r, "POST", path+"/approve", "reviewer", approval).Code)
	approved := scopedDo(r, "POST", path+"/approve", "ungranted", approval)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	require.Contains(t, approved.Body.String(), `"status":"approved"`)
	require.Contains(t, approved.Body.String(), `"version":2`)
	require.NotContains(t, approved.Body.String(), "user-ungranted")
	replay := scopedDo(r, "POST", path+"/approve", "ungranted", approval)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, approved.Body.String(), replay.Body.String())
	require.Equal(t, 409, scopedDo(r, "POST", path+"/retire", "reviewer", `{"expected_version":1}`).Code)
	retired := scopedDo(r, "POST", path+"/retire", "reviewer", `{"expected_version":2}`)
	require.Equal(t, 200, retired.Code, retired.Body.String())
	require.Contains(t, retired.Body.String(), `"status":"retired"`)
	require.Contains(t, retired.Body.String(), `"version":3`)
	replay = scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, retired.Body.String(), replay.Body.String())
	replay = scopedDo(r, "POST", path+"/approve", "ungranted", approval)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, retired.Body.String(), replay.Body.String())
}

func TestSharingNoticeEnforcesScopeValidationAndImmutableHistory(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	path := "/v1/patients/sharing-notices/" + uuid.Must(uuid.NewV7()).String()
	body := syntheticSharingNotice()
	require.Equal(t, 404, scopedDo(r, "PUT", path, "ungranted", body).Code)
	require.Equal(t, 403, scopedDo(r, "PUT", path, "owner", body).Code)
	require.Equal(t, 201, scopedDo(r, "PUT", path, "reviewer", body).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-foreign','registrar')`, testutil.TenantB, testHospital).Error
	}))
	foreign := scopedDo(r, "GET", path, "foreign", "")
	require.Equal(t, 404, foreign.Code, foreign.Body.String())
	require.NotContains(t, foreign.Body.String(), "Synthetic notice")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-reviewer','registrar')`, testutil.TenantA, "22222222-2222-4222-8222-222222222222").Error
	}))
	other := path + "?hospital_id=22222222-2222-4222-8222-222222222222"
	require.Equal(t, 404, do(r, "GET", other, "reviewer", "").Code)
	require.Equal(t, 404, do(r, "PUT", other, "reviewer", body).Code)
	for _, invalid := range []string{
		strings.Replace(body, `"care_coordination"`, `"marketing"`, 1),
		strings.Replace(body, `"en-IN"`, `"en-in"`, 1),
		strings.Replace(body, `"Synthetic notice"`, `""`, 1),
		strings.TrimSuffix(body, "}") + `,"unknown":true}`,
		body + ` {}`,
		strings.Replace(body, "Synthetic test wording; not approved policy.", strings.Repeat("x", 9000), 1),
	} {
		require.Equal(t, 400, scopedDo(r, "PUT", path, "reviewer", invalid).Code)
	}
	changed := strings.Replace(body, "Synthetic notice", "Changed wording", 1)
	require.Equal(t, 409, scopedDo(r, "PUT", path, "reviewer", changed).Code)
	duplicateVersion := "/v1/patients/sharing-notices/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, scopedDo(r, "PUT", duplicateVersion, "reviewer", body).Code)

	require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true),set_config('app.actor_subject','user-reviewer',true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE patient_sharing_notices SET title='Tampered',version=2 WHERE tenant_id=?`, testutil.TenantA).Error
	}))
	require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true),set_config('app.actor_subject','user-reviewer',true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM patient_sharing_notices WHERE tenant_id=?`, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM patient_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-reviewer'`, testutil.TenantA, testHospital).Error
	}))
	require.Equal(t, 404, scopedDo(r, "GET", path, "reviewer", "").Code)
	require.Equal(t, 404, scopedDo(r, "POST", path+"/retire", "reviewer", `{"expected_version":1}`).Code)
}

func TestSharingNoticeConcurrentRetriesPublishOneRevision(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	r = consentAuthorityGateway(t, r)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/sharing-notices/" + id
	body := syntheticSharingNotice()
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Go(func() { codes <- scopedDo(r, "PUT", path, "reviewer", body).Code })
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		require.Contains(t, []int{200, 201}, code)
		if code == 201 {
			created++
		}
	}
	require.Equal(t, 1, created)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("tenant_id=? AND subject=?", testutil.TenantA, "helivanta.in.patientmaster.sharing_notice_changed.v1").Count(&count).Error
	}))
	require.EqualValues(t, 1, count)
}

func TestSharingNoticeOutboxFailureRollsBackDraftAndAudit(t *testing.T) {
	r, db, _ := consentAuthorityFixture(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_sharing_notice_outbox_failure", SQL: `CREATE FUNCTION reject_notice_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.sharing_notice_changed.v1' THEN RAISE EXCEPTION 'synthetic outbox outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_notice_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_notice_outbox();`}}))
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/sharing-notices/" + id
	body := syntheticSharingNotice()
	failed := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic outbox outage")
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

func syntheticSharingNotice() string {
	return fmt.Sprintf(`{"purpose":"care_coordination","locale":"en-IN","version_label":"synthetic-1","title":"Synthetic notice","text":"Synthetic test wording; not approved policy.","withdrawal_consequence":"Synthetic consequence.","policy_reference":%q}`, uuid.Must(uuid.NewV7()).String())
}
