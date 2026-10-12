package patientmaster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// registeredID pulls the created patient's id out of a 201 body.
// Registration returns one shape — {patient, possible_matches} — on
// every band, so no caller (and no test) has to branch on the shape to
// find the id it was given.
func registeredID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Patient struct {
			ID string `json:"id"`
		} `json:"patient"`
		PossibleMatches []possibleMatchBody `json:"possible_matches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Patient.ID)
	require.NotNil(t, body.PossibleMatches,
		"possible_matches must always be present, empty rather than absent")
	return body.Patient.ID
}

// possibleMatchBody is the candidate description returned both alongside
// a Possible-band 201 and inside a blocking 409.
type possibleMatchBody struct {
	PatientID  string  `json:"patient_id"`
	MRN        string  `json:"mrn"`
	GivenName  string  `json:"given_name"`
	FamilyName string  `json:"family_name"`
	Score      float64 `json:"score"`
}

// TestRegisterCreatesPatientAndConsentInOneTransaction — spec D5. The
// two rows are inseparable; a patient without a consent receipt is a
// compliance defect, so this asserts both exist after one call.
func TestRegisterCreatesPatientAndConsentInOneTransaction(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	body := registerBody("Asha", "Rao", "1990-05-10", "9876500001", false, "")
	w := do(r, "POST", "/v1/patients", "tokA", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	respID := registeredID(t, w)
	var patientCount, consentCount int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT count(*) FROM patients WHERE id = ?`, respID).Scan(&patientCount).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM patient_consents WHERE patient_id = ?`, respID).Scan(&consentCount).Error
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
	createdID := registeredID(t, w1)

	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Md Ali", "", "1979-04-02", "9876543210", false, ""))
	require.Equal(t, http.StatusConflict, w2.Code, w2.Body.String())

	// The 409 is the one moment a human adjudicates "same person or
	// not". An id and a score alone give them nothing to decide with, so
	// the body must carry the same candidate description the Possible
	// band returns.
	var conflict struct {
		Match possibleMatchBody `json:"match"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &conflict))
	require.Equal(t, createdID, conflict.Match.PatientID)
	require.NotEmpty(t, conflict.Match.MRN)
	require.Equal(t, "Mohammed Ali", conflict.Match.GivenName)
	require.Greater(t, conflict.Match.Score, 0.0)

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
	createdID := registeredID(t, w1)

	const reason = "clerk confirmed two distinct people at the counter"
	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Md Ali", "", "1979-04-02", "9876543211", true, reason))
	require.Equal(t, http.StatusCreated, w2.Code, w2.Body.String())
	secondID := registeredID(t, w2)
	require.NotEqual(t, createdID, secondID)

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
			secondID, createdID, reason).Scan(&overrideCount).Error
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
// that registration succeeded — a NoMatch pair returns the same 201, the
// same shape and the same row count, differing only in that
// possible_matches is empty. So it asserts the second response names the
// first patient as a candidate: a regression to NoMatch yields an empty
// array and fails here.
func TestPossibleMatchDoesNotBlock(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	w1 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1979-04-02", "", false, ""))
	require.Equal(t, http.StatusCreated, w1.Code, w1.Body.String())
	firstID := registeredID(t, w1)

	w2 := do(r, "POST", "/v1/patients", "tokA", registerBody("Mohammed Ali", "", "1962-01-01", "", false, ""))
	require.Equal(t, http.StatusCreated, w2.Code, w2.Body.String())

	var second struct {
		Patient struct {
			ID string `json:"id"`
		} `json:"patient"`
		PossibleMatches []possibleMatchBody `json:"possible_matches"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))
	require.NotEmpty(t, second.Patient.ID, "a Possible-band registration must still return the created patient")
	require.NotEqual(t, firstID, second.Patient.ID)

	require.Len(t, second.PossibleMatches, 1, "the same-name different-DOB pair must be surfaced as exactly one Possible candidate")
	require.Equal(t, firstID, second.PossibleMatches[0].PatientID)
	require.Greater(t, second.PossibleMatches[0].Score, 0.0)
	require.NotEmpty(t, second.PossibleMatches[0].MRN, "the clerk needs the MRN to recognise the record")
	require.Equal(t, "Mohammed Ali", second.PossibleMatches[0].GivenName)

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
	createdID := registeredID(t, w)

	wGetA := do(r, "GET", "/v1/patients/"+createdID, "tokA", "")
	require.Equal(t, http.StatusOK, wGetA.Code)

	wGetB := do(r, "GET", "/v1/patients/"+createdID, "tokB", "")
	require.Equal(t, http.StatusNotFound, wGetB.Code)
}

// TestRegisteredEventCarriesOnlyTheID is spec D6 asserted on the wire.
// #835 was PHI leaving the RLS boundary in event payloads; this fails
// the day someone adds a name "just for the work queue".
func TestRegisteredEventCarriesOnlyTheID(t *testing.T) {
	r, db, ctx := setupHTTP(t)

	const sentinel = "Zzyzxvortex"
	w := do(r, "POST", "/v1/patients", "tokA", registerBody(sentinel, "Kapoor", "1990-06-15", "9876543214", false, ""))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	createdID := registeredID(t, w)

	var row struct{ Payload []byte }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload FROM outbox_events
			WHERE subject = 'helivanta.in.patientmaster.registered.v1'`).Scan(&row).Error
	}))
	require.NotEmpty(t, row.Payload, "no patient.registered row in the outbox")

	body := string(row.Payload)
	require.Contains(t, body, createdID, "the payload must carry the patient id")
	require.NotContains(t, body, sentinel, "the payload must not carry the patient's given name")
}

// TestPatientWithoutConsentCannotCommit is spec D5 asserted against the
// database rather than against the handler. The handler writing both
// rows in one transaction is a convention; the deferred constraint
// trigger in migration 0002_patientmaster is the control. This inserts a
// patient with no consent receipt through a path the handler does not
// own, and requires the COMMIT to be refused — which is what makes the
// invariant survive a refactor that splits the two writes apart.
func TestPatientWithoutConsentCannotCommit(t *testing.T) {
	_, db, ctx := setupHTTP(t)

	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patients (tenant_id, mrn, given_name, dob)
			VALUES (?::uuid, 'MRN-ORPHAN', 'Orphan', DATE '1990-01-01')`, testutil.TenantA).Error
	})
	require.Error(t, err, "a patient with no consent receipt must not be able to commit")
	require.Contains(t, err.Error(), "consent receipt")

	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM patients`).Scan(&count).Error
	}))
	require.Equal(t, int64(0), count, "the rolled-back patient row must not survive")
}
