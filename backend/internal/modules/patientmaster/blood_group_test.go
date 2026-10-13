package patientmaster_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestRegistrationPersistsReportedBloodGroupOnlyInPatientDetail(t *testing.T) {
	r, _, _ := setupHTTP(t)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(registerBody("SyntheticBloodGroup", "Example", "1990-01-01", "", false, "")), &body))
	body["reported_blood_group"] = map[string]string{"abo": "AB", "rh": "negative", "source": "patient"}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	created := scopedDo(r, "POST", "/v1/patients", "tokA", string(encoded))
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	id := registeredID(t, created)
	detail := scopedDo(r, "GET", "/v1/patients/"+id, "tokA", "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	var result struct {
		ReportedBloodGroup struct {
			ABO, Rh, Source string
			RecordedAt      string `json:"recorded_at"`
		} `json:"reported_blood_group"`
	}
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &result))
	require.Equal(t, "AB", result.ReportedBloodGroup.ABO)
	require.Equal(t, "negative", result.ReportedBloodGroup.Rh)
	require.Equal(t, "patient", result.ReportedBloodGroup.Source)
	require.NotEmpty(t, result.ReportedBloodGroup.RecordedAt)
	list := scopedDo(r, "GET", "/v1/patients", "tokA", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.NotContains(t, list.Body.String(), "reported_blood_group")
	retry := scopedDo(r, "POST", "/v1/patients", "tokA", string(encoded))
	require.Equal(t, http.StatusCreated, retry.Code, retry.Body.String())
	require.JSONEq(t, created.Body.String(), retry.Body.String())
	body["reported_blood_group"] = map[string]string{"abo": "O", "rh": "negative", "source": "patient"}
	changed, err := json.Marshal(body)
	require.NoError(t, err)
	conflict := scopedDo(r, "POST", "/v1/patients", "tokA", string(changed))
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
}

func TestReportedBloodGroupRejectsUntrustedClinicalClaims(t *testing.T) {
	r, _, _ := setupHTTP(t)
	for _, group := range []string{
		`{}`, `{"abo":"C","rh":"positive","source":"patient"}`,
		`{"abo":"A","rh":"invalid","source":"patient"}`,
		`{"abo":"A","rh":"positive","source":"lab"}`,
		`{"abo":"A","rh":"positive","source":"patient","verified":true}`,
		`{"abo":"A","rh":"positive","source":"patient","recorded_at":"2000-01-01T00:00:00Z"}`,
	} {
		t.Run(group, func(t *testing.T) {
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(registerBody("SyntheticInvalid", "Group", "1990-01-01", "", false, "")), &body))
			body["reported_blood_group"] = json.RawMessage(group)
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			response := scopedDo(r, "POST", "/v1/patients", "tokA", string(encoded))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

func TestReportedBloodGroupUsesParentTenantHospitalAndWriteGrant(t *testing.T) {
	r, db, ctx := setupHTTP(t)
	body := `{"request_id":"33333333-3333-4333-8333-333333333333","given_name":"SyntheticScope","dob":"1990-01-01","notice_version":"v1","consented_by":"patient","reported_blood_group":{"abo":"O","rh":"unknown","source":"document"}}`
	created := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	id := registeredID(t, created)
	foreign := scopedDo(r, "GET", "/v1/patients/"+id, "tokB", "")
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	const sibling = "22222222-2222-4222-8222-222222222222"
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,'user-tokA','registrar')`, testutil.TenantA, sibling).Error
	}))
	denied := do(r, "GET", "/v1/patients/"+id+"?hospital_id="+sibling, "tokA", "")
	require.Equal(t, http.StatusNotFound, denied.Code, denied.Body.String())
	for _, scope := range []struct {
		tenant, hospital, subject string
		visible                   int
	}{
		{testutil.TenantB, testHospital, "user-tokB", 0},
		{testutil.TenantA, sibling, "user-tokA", 0},
		{testutil.TenantA, testHospital, "ungranted", 0},
		{testutil.TenantA, testHospital, "user-tokA", 1},
	} {
		require.NoError(t, db.WithTenant(ctx, scope.tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.patient_subject',?,true),set_config('app.patient_hospital',?,true)`, scope.subject, scope.hospital).Error; err != nil {
				return err
			}
			var count int
			if err := tx.Raw(`SELECT count(*) FROM patient_reported_blood_groups WHERE patient_id=?`, id).Scan(&count).Error; err != nil {
				return err
			}
			require.Equal(t, scope.visible, count)
			return nil
		}))
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE patient_hospital_access SET level='reader' WHERE subject='user-tokA'`).Error
	}))
	detail := scopedDo(r, "GET", "/v1/patients/"+id, "tokA", "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	require.Contains(t, detail.Body.String(), `"abo":"O"`)
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokA',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		result := tx.Exec(`UPDATE patient_reported_blood_groups SET abo='A' WHERE patient_id=?`, id)
		require.Zero(t, result.RowsAffected)
		return result.Error
	})
	require.ErrorContains(t, err, "reported blood group is append only")
}

func TestReportedBloodGroupCannotBeDeletedThroughReaderPolicy(t *testing.T) {
	r, db, ctx := setupHTTP(t)
	body := `{"request_id":"44444444-4444-4444-8444-444444444444","given_name":"SyntheticImmutable","dob":"1990-01-01","notice_version":"v1","consented_by":"patient","reported_blood_group":{"abo":"A","rh":"positive","source":"guardian"}}`
	created := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	id := registeredID(t, created)
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokA',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE patient_hospital_access SET level='reader' WHERE subject='user-tokA'`).Error; err != nil {
			return err
		}
		result := tx.Exec(`DELETE FROM patient_reported_blood_groups WHERE patient_id=?`, id)
		return result.Error
	})
	require.ErrorContains(t, err, "reported blood group is append only")
	detail := scopedDo(r, "GET", "/v1/patients/"+id, "tokA", "")
	require.Contains(t, detail.Body.String(), `"abo":"A"`)
}
