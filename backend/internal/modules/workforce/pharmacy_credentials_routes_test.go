package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/pkg/authz"
)

type pharmacyCredentials struct{ *pharmacyDocuments }

// newPharmacyCredentials adds "reviewer", a second pharmacy manager who may decide credential reviews.
func newPharmacyCredentials(t *testing.T) *pharmacyCredentials {
	t.Helper()
	d := newPharmacyDocumentsWith(t, true, map[string][]authz.Permission{
		"reader":   {workforce.PermEnrollmentManage, workforce.PermCredentialReview},
		"outsider": {workforce.PermEnrollmentManage, workforce.PermCredentialReview},
		"reviewer": {workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermCredentialReview},
	})
	grant(t, d.establishmentHarness, d.pharmacy, "user-reviewer", "manager")
	return &pharmacyCredentials{d}
}

func (d *pharmacyCredentials) send(t *testing.T, token, method, rest string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data := ""
	if body != nil {
		data = string(mustJSON(t, body))
	}
	return pharmacyRequest(d.establishmentHarness, token, method, d.path(d.pharmacy, d.staff, rest), data)
}

func (d *pharmacyCredentials) call(t *testing.T, token, method, rest string, body any) (int, map[string]any) {
	t.Helper()
	w := d.send(t, token, method, rest, body)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func pharmacyReviewPath(review, suffix string) string {
	return "/credentials/" + pharmacyCredentialID + "/reviews/" + review + suffix
}

// evidence stores one released credential evidence file for the harness pharmacist's registration.
func (d *pharmacyCredentials) evidence(t *testing.T) string {
	t.Helper()
	file := uuid.Must(uuid.NewV7()).String()
	path := d.path(d.pharmacy, d.staff, "/documents/"+file)
	w := pharmacyRequest(d.establishmentHarness, "admin", "PUT", path, d.intent("credential", pharmacyCredentialID, "credential_evidence"))
	require.Equal(t, 201, w.Code, w.Body.String())
	w = d.upload("admin", path+"/content")
	require.Equal(t, 202, w.Code, w.Body.String())
	status, err := d.scan(t, cleanScan, file, 1)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	return file
}

func (d *pharmacyCredentials) enrollment(t *testing.T) map[string]any {
	t.Helper()
	w := pharmacyRequest(d.establishmentHarness, "admin", "GET", enrollmentPath(d.pharmacy, d.staff), "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func (d *pharmacyCredentials) credential(t *testing.T) (map[string]any, map[string]any) {
	t.Helper()
	w := d.send(t, "admin", "GET", "/credentials", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	rows := out["credentials"].([]any)
	require.Len(t, rows, 1)
	return out, rows[0].(map[string]any)
}

func TestPharmacyCredentialReviewLifecycle(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	listed, cred := d.credential(t)
	require.Nil(t, listed["time_zone"], "no pharmacy time zone yet")
	require.Equal(t, pharmacyCredentialID, cred["id"])
	require.Equal(t, "registration", cred["type"])
	require.Equal(t, "unreviewed", cred["status"])
	require.Nil(t, cred["reviewed"])

	file := d.evidence(t)
	enrollmentEvents := h.count(t, d.pharmacy, "outbox_events", "subject=?", workforce.EnrollmentChanged)
	enrollmentAudit := h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.enrollment.changed'", d.pharmacy, d.staff)
	before := d.enrollment(t)["version"].(float64)
	review := uuid.Must(uuid.NewV7()).String()
	body := reviewBody(0, "2031-12-31", file)
	code, saved := d.call(t, "admin", "PUT", pharmacyReviewPath(review, ""), body)
	require.Equal(t, 200, code, saved)
	require.Equal(t, "pending", saved["status"])
	require.Equal(t, "SYN-RENEWED", saved["number"], "number is trimmed")
	require.Equal(t, "registration", saved["type"], "type comes from the pharmacy enrollment")
	require.Equal(t, "Synthetic pharmacy council", saved["issuer"])
	require.Equal(t, []any{file}, saved["evidence_document_ids"])
	require.Equal(t, d.staff, saved["staff_id"])
	require.NotContains(t, saved, "hospital_id")

	code, out := d.call(t, "reviewer", "POST", pharmacyReviewPath(review, "/decision"), map[string]any{"expected_version": 0, "decision": "approve", "note": "Synthetic certificate checked"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", out["status"])
	require.EqualValues(t, 1, out["approved_version"])
	require.Equal(t, "user-reviewer", out["decided_by_subject"])

	enrolled := d.enrollment(t)
	require.Equal(t, before+1, enrolled["version"], "approval bumps the pharmacy enrollment version so stale forms conflict")
	mirror := enrolled["credentials"].([]any)[0].(map[string]any)
	require.Equal(t, "SYN-RENEWED", mirror["number"], "enrollment mirrors the reviewed record")
	require.Equal(t, "2026-01-01", mirror["valid_from"])
	require.Equal(t, "2031-12-31", mirror["valid_until"])

	listed, cred = d.credential(t)
	require.Nil(t, listed["time_zone"])
	require.Equal(t, "date_unknown", cred["status"], "standing needs the pharmacy time zone")
	require.Equal(t, "SYN-RENEWED", cred["number"])
	reviewed := cred["reviewed"].(map[string]any)
	require.EqualValues(t, 1, reviewed["version"])
	require.Equal(t, review, reviewed["review_id"])
	require.Equal(t, "2031-12-31", reviewed["valid_until"])
	require.Nil(t, cred["pending"])

	code, out = d.call(t, "admin", "GET", "/credentials/"+pharmacyCredentialID+"/reviews?limit=1", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.Equal(t, review, out["data"].([]any)[0].(map[string]any)["id"])

	for action, n := range map[string]int64{"workforce.credential_review.pending": 1, "workforce.credential_review.approved": 1, "workforce.credentials.read": 2} {
		require.EqualValues(t, n, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action=?", d.pharmacy, d.staff, action), action)
	}
	require.Equal(t, enrollmentAudit+1, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.enrollment.changed'", d.pharmacy, d.staff))
	require.Equal(t, []map[string]any{
		{"hospital_id": d.pharmacy, "staff_id": d.staff, "credential_id": pharmacyCredentialID, "review_id": review, "status": "pending", "version": float64(0)},
		{"hospital_id": d.pharmacy, "staff_id": d.staff, "credential_id": pharmacyCredentialID, "review_id": review, "status": "approved", "version": float64(1)},
	}, outboxPayloads(t, h.db, workforce.CredentialReviewChanged), "review events name the owning pharmacy")
	require.Equal(t, []map[string]any{{"hospital_id": d.pharmacy, "staff_id": d.staff, "credential_id": pharmacyCredentialID, "review_id": review, "version": float64(1)}},
		outboxPayloads(t, h.db, workforce.CredentialValidityChanged))
	require.Equal(t, enrollmentEvents, h.count(t, d.pharmacy, "outbox_events", "subject=?", workforce.EnrollmentChanged), "the hospital enrollment event never carries a pharmacy")
	require.Zero(t, h.count(t, d.pharmacy, "workforce_roster_credential_standing", "hospital_id=?", d.pharmacy), "roster standing stays hospital-only")
}
