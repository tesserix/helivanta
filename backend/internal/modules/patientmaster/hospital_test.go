package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestRegistrationRequiresExplicitHospital(t *testing.T) {
	r, _, _ := setupHTTP(t)
	w := do(r, "POST", "/v1/patients", "tokA", registerBody("Synthetic", "Person", "1990-05-10", "", false, ""))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestHospitalWithoutClinicalGrantCannotRegister(t *testing.T) {
	r, _, _ := setupHTTP(t)
	w := do(r, "POST", "/v1/patients?hospital_id=22222222-2222-4222-8222-222222222222", "tokA", registerBody("Synthetic", "Person", "1990-05-10", "", false, ""))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestSiblingHospitalCannotReadOrMatchPatients(t *testing.T) {
	r, db, ctx := setupHTTP(t)
	body := registerBody("Synthetic", "Person", "1990-05-10", "", false, "")
	created := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	id := registeredID(t, created)
	const sibling = "22222222-2222-4222-8222-222222222222"
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, 'user-tokA', 'registrar')`, testutil.TenantA, sibling).Error
	}))
	other := do(r, "GET", "/v1/patients/"+id+"?hospital_id="+sibling, "tokA", "")
	require.Equal(t, http.StatusNotFound, other.Code)
	list := do(r, "GET", "/v1/patients?hospital_id="+sibling, "tokA", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, list.Body.String())
	second := do(r, "POST", "/v1/patients?hospital_id="+sibling, "tokA", body)
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.NotEqual(t, id, registeredID(t, second))
	// Even a principal granted both hospitals must select exactly one scope.
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject', 'user-tokA', true), set_config('app.patient_hospital', ?, true)`, sibling).Error; err != nil {
			return err
		}
		var n int
		if err := tx.Raw(`SELECT count(*) FROM patient_consents WHERE patient_id = ?`, id).Scan(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n)
		return nil
	}))
}

func TestMissingScopeListReturnsOneNotFoundEnvelope(t *testing.T) {
	r, _, _ := setupHTTP(t)
	w := do(r, "GET", "/v1/patients", "tokA", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"not_found","message":"hospital not found"}`, w.Body.String())
}

func TestPatientSearchUsesBodyAndHospitalScope(t *testing.T) {
	r, _, _ := setupHTTP(t)
	for _, name := range []string{"SyntheticAlpha", "SyntheticBeta"} {
		w := scopedDo(r, "POST", "/v1/patients", "tokA", registerBody(name, "", "1990-05-10", "", false, ""))
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	}
	w := scopedDo(r, "POST", "/v1/patients/search?limit=1", "tokA", `{"query":"SyntheticA"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "SyntheticAlpha")
	require.NotContains(t, w.Body.String(), "SyntheticBeta")
}

func TestClinicalGrantIsExplicitRevocableAndSeparateFromRoutePermission(t *testing.T) {
	r, _, _ := setupHTTP(t)
	const sibling = "22222222-2222-4222-8222-222222222222"
	path := "/v1/patients/hospitals/" + sibling + "/access/user-tokA"
	denied := do(r, "PUT", path, "tokB", `{"level":"registrar"}`)
	require.Equal(t, http.StatusForbidden, denied.Code)
	granted := do(r, "PUT", path, "tokA", `{"level":"reader"}`)
	require.Equal(t, http.StatusOK, granted.Code, granted.Body.String())
	create := do(r, "POST", "/v1/patients?hospital_id="+sibling, "tokA", registerBody("Synthetic", "Grant", "1980-01-01", "", false, ""))
	require.Equal(t, http.StatusNotFound, create.Code)
	require.Equal(t, http.StatusOK, do(r, "GET", "/v1/patients?hospital_id="+sibling, "tokA", "").Code)
	require.Equal(t, http.StatusOK, do(r, "DELETE", path, "tokA", "").Code)
	revoked := do(r, "GET", "/v1/patients?hospital_id="+sibling, "tokA", "")
	require.Equal(t, http.StatusNotFound, revoked.Code, revoked.Body.String())
}

func TestRegistrationRejectsInvalidDemographicsAndGuardianReceipt(t *testing.T) {
	r, _, _ := setupHTTP(t)
	for _, change := range []map[string]any{
		{"given_name": "   "}, {"dob": "2099-01-01"},
		{"dob": "2020-01-01", "consented_by": "patient"},
		{"consented_by": "guardian", "guardian_name": "", "guardian_relationship": "parent"},
		{"mobile": "123"}, {"tenant_id": testutil.TenantB},
	} {
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(registerBody("Synthetic", "Boundary", "1990-01-01", "", false, "")), &body))
		for k, v := range change {
			body[k] = v
		}
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		w := scopedDo(r, "POST", "/v1/patients", "tokA", string(encoded))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestRegistrationAcceptsInternationalPhoneAndGuardian(t *testing.T) {
	r, _, _ := setupHTTP(t)
	body := `{"request_id":"33333333-3333-4333-8333-333333333333","given_name":"SyntheticChild","family_name":"Example","dob":"2020-01-01","mobile":"+61491570156","notice_version":"synthetic-v1","consented_by":"guardian","guardian_name":"Synthetic Guardian","guardian_relationship":"parent"}`
	w := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "+61491570156")
}

func TestRegistrationRetryReplaysWithoutAnotherPatientOrEvent(t *testing.T) {
	r, db, ctx := setupHTTP(t)
	body := registerBody("SyntheticRetry", "Example", "1990-01-01", "", false, "")
	first := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	second := scopedDo(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.JSONEq(t, first.Body.String(), second.Body.String())
	var changed map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &changed))
	changed["given_name"] = "ChangedSynthetic"
	data, err := json.Marshal(changed)
	require.NoError(t, err)
	conflict := scopedDo(r, "POST", "/v1/patients", "tokA", string(data))
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.NoError(t, withPatientTenant(db, ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var n int
		if err := tx.Raw(`SELECT count(*) FROM patients`).Scan(&n).Error; err != nil {
			return err
		}
		require.Equal(t, 1, n)
		return nil
	}))
}

func TestSameNameAndBirthDateWithoutPhoneRequiresDuplicateReview(t *testing.T) {
	r, _, _ := setupHTTP(t)
	first := scopedDo(r, "POST", "/v1/patients", "tokA", registerBody("SyntheticExact", "Example", "1990-01-01", "", false, ""))
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	second := scopedDo(r, "POST", "/v1/patients", "tokA", registerBody("SyntheticExact", "Example", "1990-01-01", "", false, ""))
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	require.Contains(t, second.Body.String(), registeredID(t, first))
}

func TestConcurrentRegistrationIsIdempotentAndExactDuplicatesAreBlocked(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_request_id_%v", sameKey), func(t *testing.T) {
			r, db, ctx := setupHTTP(t)
			const attempts = 8
			responses := make(chan *httptest.ResponseRecorder, attempts)
			body := registerBody("SyntheticConcurrent", "Example", "1980-01-01", "", false, "")
			for range attempts {
				request := body
				if !sameKey {
					request = registerBody("SyntheticConcurrent", "Example", "1980-01-01", "", false, "")
				}
				go func() { responses <- scopedDo(r, "POST", "/v1/patients", "tokA", request) }()
			}
			ids := map[string]bool{}
			created := 0
			for range attempts {
				response := <-responses
				if response.Code == http.StatusCreated {
					created++
					ids[registeredID(t, response)] = true
				} else {
					require.False(t, sameKey)
					require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
				}
			}
			require.Len(t, ids, 1)
			if sameKey {
				require.Equal(t, attempts, created)
			} else {
				require.Equal(t, 1, created)
			}
			require.NoError(t, withPatientTenant(db, ctx, testutil.TenantA, func(tx *gorm.DB) error {
				for _, query := range []string{`SELECT count(*) FROM patients`, `SELECT count(*) FROM patient_registration_requests`, `SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.patientmaster.registered.v1'`, `SELECT count(*) FROM patient_access_audit WHERE action='register'`} {
					var count int
					if err := tx.Raw(query).Scan(&count).Error; err != nil {
						return err
					}
					require.Equal(t, 1, count, query)
				}
				return nil
			}))
		})
	}
}
