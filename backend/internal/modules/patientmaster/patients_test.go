package patientmaster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var do = testutil.Do

// setupHTTP builds the harness exactly as lab/module_test.go's setup
// does, but keeps the *gin.Engine this file actually needs to drive
// registration and lookup over real HTTP.
func setupHTTP(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
			"tokB": {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	return r, db, ctx
}

// registerBody builds a registration request that is otherwise valid,
// varying only the fields each test cares about.
func registerBody(givenName, familyName, dob, mobile string, override bool, overrideReason string) string {
	req := map[string]any{
		"given_name":      givenName,
		"family_name":     familyName,
		"dob":             dob,
		"mobile":          mobile,
		"notice_version":  "v1",
		"consented_by":    "patient",
		"override":        override,
		"override_reason": overrideReason,
	}
	b, err := json.Marshal(req)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestRegisterCreatesPatientAndConsentInOneTransaction — spec D5. The
// two rows are inseparable; a patient without a consent receipt is a
// compliance defect, so this asserts both exist after one call.
func TestRegisterCreatesPatientAndConsentInOneTransaction(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	body := registerBody("Asha", "Rao", "1990-05-10", "9876500001", false, "")
	w := do(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	var patientCount, consentCount int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT count(*) FROM patients WHERE id = ?`, resp.ID).Scan(&patientCount).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM patient_consents WHERE patient_id = ?`, resp.ID).Scan(&consentCount).Error
	}))
	require.Equal(t, int64(1), patientCount)
	require.Equal(t, int64(1), consentCount)
}

// TestConfidentDuplicateIsBlocked: registering the same human twice with
// the name spelled differently must be refused with 409 and the existing
// candidate returned — not silently created.
func TestConfidentDuplicateIsBlocked(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	w1 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1979-04-02", "9876543210", false, ""))
	require.Equal(t, http.StatusCreated, w1.Code, w1.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &created))

	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Md Ali", "", "1979-04-02", "9876543210", false, ""))
	require.Equal(t, http.StatusConflict, w2.Code, w2.Body.String())
	require.Contains(t, w2.Body.String(), created.ID)

	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM patients`).Scan(&count).Error
	}))
	require.Equal(t, int64(1), count)
}

// TestOverrideCreatesTheDuplicateAndRecordsWhy: the escape hatch, and
// the audit row that makes deferring merge honest.
func TestOverrideCreatesTheDuplicateAndRecordsWhy(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	w1 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1979-04-02", "9876543211", false, ""))
	require.Equal(t, http.StatusCreated, w1.Code, w1.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &created))

	const reason = "clerk confirmed two distinct people at the counter"
	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Md Ali", "", "1979-04-02", "9876543211", true, reason))
	require.Equal(t, http.StatusCreated, w2.Code, w2.Body.String())
	var second struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))
	require.NotEqual(t, created.ID, second.ID)

	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var patientCount int64
		if err := tx.Raw(`SELECT count(*) FROM patients`).Scan(&patientCount).Error; err != nil {
			return err
		}
		require.Equal(t, int64(2), patientCount)

		var overrideCount int64
		err := tx.Raw(`SELECT count(*) FROM patient_duplicate_overrides
			WHERE created_patient_id = ? AND matched_patient_id = ?
			  AND reason = ? AND actor_subject <> ''`,
			second.ID, created.ID, reason).Scan(&overrideCount).Error
		if err != nil {
			return err
		}
		require.Equal(t, int64(1), overrideCount)
		return nil
	}))
}

// TestOverrideWithoutAReasonIsRefused: the reason IS the control. An
// override with an empty reason produces an audit row that proves
// nothing.
func TestOverrideWithoutAReasonIsRefused(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	w := do(r, "POST", "/v1/patients", "tokA", registerBody("Ramesh", "Iyer", "1985-03-03", "9876543212", true, ""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM patients`).Scan(&count).Error
	}))
	require.Equal(t, int64(0), count)
}

// TestPossibleMatchDoesNotBlock: a same-name different-DOB pair must
// register freely. Blocking here stops unrelated people with common
// names from being registered at all.
//
// This also has to prove the Possible band is actually reached, not just
// that registration succeeded — a NoMatch pair would return the same 201
// and row count. So it asserts the second response's body names the
// first patient as a possible match: a response shape that degrades
// silently to NoMatch's (bare patient, no possible_matches) cannot pass
// this assertion, only band==Possible can.
func TestPossibleMatchDoesNotBlock(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	w1 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1979-04-02", "", false, ""))
	require.Equal(t, http.StatusCreated, w1.Code, w1.Body.String())
	var first struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &first))

	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1962-01-01", "", false, ""))
	require.Equal(t, http.StatusCreated, w2.Code, w2.Body.String())

	var second struct {
		Patient struct {
			ID string `json:"id"`
		} `json:"patient"`
		PossibleMatches []struct {
			PatientID string  `json:"patient_id"`
			Score     float64 `json:"score"`
		} `json:"possible_matches"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))
	require.NotEmpty(t, second.Patient.ID, "a Possible-band registration must still return the created patient")
	require.NotEqual(t, first.ID, second.Patient.ID)

	require.Len(t, second.PossibleMatches, 1, "the same-name different-DOB pair must be surfaced as exactly one Possible candidate")
	require.Equal(t, first.ID, second.PossibleMatches[0].PatientID)
	require.Greater(t, second.PossibleMatches[0].Score, 0.0)

	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM patients`).Scan(&count).Error
	}))
	require.Equal(t, int64(2), count)
}

// TestGetPatientFromAnotherTenantIs404: cross-tenant probes must not
// distinguish "not yours" from "does not exist".
func TestGetPatientFromAnotherTenantIs404(t *testing.T) {
	r, _, _ := setupHTTP(t)

	w := do(r, "POST", "/v1/patients", "tokA", registerBody("Sunita", "Verma", "1988-11-20", "9876543213", false, ""))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	wGetA := do(r, "GET", "/v1/patients/"+created.ID, "tokA", "")
	require.Equal(t, http.StatusOK, wGetA.Code)

	wGetB := do(r, "GET", "/v1/patients/"+created.ID, "tokB", "")
	require.Equal(t, http.StatusNotFound, wGetB.Code)
}
