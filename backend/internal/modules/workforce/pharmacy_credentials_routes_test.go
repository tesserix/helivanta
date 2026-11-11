package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

type pharmacyCredentials struct {
	*pharmacyDocuments
	reviewed int
}

// newPharmacyCredentials adds pharmacy managers who may decide reviews: "reviewer", "independent" and "linked".
func newPharmacyCredentials(t *testing.T) *pharmacyCredentials {
	t.Helper()
	review := []authz.Permission{workforce.PermEnrollmentManage, workforce.PermCredentialReview}
	d := newPharmacyDocumentsWith(t, true, map[string][]authz.Permission{
		"reader":      review,
		"outsider":    review,
		"independent": review,
		"linked":      review,
		"reviewer":    {workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermCredentialReview},
	})
	for _, subject := range []string{"user-reviewer", "user-independent", "user-linked"} {
		grant(t, d.establishmentHarness, d.pharmacy, subject, "manager")
	}
	return &pharmacyCredentials{pharmacyDocuments: d}
}

func (d *pharmacyCredentials) decide(t *testing.T, token, review string, version int, decision, note string) (int, map[string]any) {
	t.Helper()
	return d.call(t, token, "POST", pharmacyReviewPath(review, "/decision"), map[string]any{"expected_version": version, "decision": decision, "note": note})
}

// link binds subject to the harness pharmacist as a pending independent login, as 4c's association will.
func (d *pharmacyCredentials) link(t *testing.T, subject string) {
	t.Helper()
	version := d.enrollment(t)["version"]
	require.NoError(t, d.tx(t, d.pharmacy, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','manage',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason,independent_staff_version) VALUES(?,?,?,?,?,'pending',1,'user-admin','Synthetic link','user-admin','Synthetic link',?)`,
			testutil.TenantA, d.pharmacy, d.staff, uuid.Must(uuid.NewV7()).String(), subject, version).Error
	}))
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

func (d *pharmacyCredentials) hr(t *testing.T, token, method, rest string, body any) (int, map[string]any) {
	t.Helper()
	data := ""
	if body != nil {
		data = string(mustJSON(t, body))
	}
	w := pharmacyRequest(d.establishmentHarness, token, method, d.pharmacy+"/hr"+rest, data)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"), w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

// approve records one independently reviewed validity for the harness pharmacist's registration.
func (d *pharmacyCredentials) approve(t *testing.T, validFrom, validUntil string) string {
	t.Helper()
	review := uuid.Must(uuid.NewV7()).String()
	body := reviewBody(d.reviewed, validUntil, d.evidence(t))
	body["valid_from"] = validFrom
	code, out := d.call(t, "admin", "PUT", pharmacyReviewPath(review, ""), body)
	require.Equal(t, 200, code, out)
	code, out = d.decide(t, "reviewer", review, d.reviewed, "approve", "")
	require.Equal(t, 200, code, out)
	d.reviewed++
	return review
}

// days counts calendar days in zone from the database clock, which is the clock standing uses.
func (d *pharmacyCredentials) days(t *testing.T, zone string) func(int) string {
	t.Helper()
	var now time.Time
	require.NoError(t, d.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error }))
	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)
	return func(offset int) string { return now.In(loc).AddDate(0, 0, offset).Format(time.DateOnly) }
}

func (d *pharmacyCredentials) zoneAudits(t *testing.T) []map[string]any {
	t.Helper()
	var raw []string
	require.NoError(t, d.tx(t, d.pharmacy, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT changes::text FROM workforce_audit WHERE hospital_id=? AND resource_id=? AND action='workforce.pharmacy_time_zone.changed' ORDER BY created_at,id`, d.pharmacy, d.pharmacy).Scan(&raw).Error
	}))
	out := []map[string]any{}
	for _, text := range raw {
		var changes map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &changes))
		out = append(out, changes)
	}
	return out
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

func TestPharmacyCredentialReviewNeedsAnIndependentSecondPerson(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	file := d.evidence(t)
	review := uuid.Must(uuid.NewV7()).String()
	body := reviewBody(0, "2031-12-31", file)
	code, saved := d.call(t, "reviewer", "PUT", pharmacyReviewPath(review, ""), body)
	require.Equal(t, 200, code, saved)
	code, replay := d.call(t, "reviewer", "PUT", pharmacyReviewPath(review, ""), body)
	require.Equal(t, 200, code, replay)
	require.Equal(t, saved["created_at"], replay["created_at"], "identical replay returns the saved review")
	code, out := d.call(t, "reviewer", "PUT", pharmacyReviewPath(review, ""), reviewBody(0, "2032-12-31", file))
	require.Equal(t, 409, code, out)
	require.Equal(t, "review_conflict", out["reason"])

	for _, decision := range []string{"approve", "reject"} {
		code, out = d.decide(t, "reviewer", review, 0, decision, "Synthetic own decision")
		require.Equal(t, 409, code, out)
		require.Equal(t, "self_review", out["reason"], "the proposer never decides")
	}
	code, out = d.decide(t, "admin", review, 0, "approve", "")
	require.Equal(t, 403, code, out, "a manager without the review permission cannot decide")
	code, out = d.decide(t, "independent", review, 1, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])
	code, out = d.call(t, "admin", "POST", pharmacyReviewPath(review, "/withdraw"), map[string]any{"note": ""})
	require.Equal(t, 409, code, out)
	require.Equal(t, "not_proposer", out["reason"])
	code, out = d.decide(t, "independent", review, 0, "reject", " ")
	require.Equal(t, 400, code, out, "rejecting needs a note")

	d.link(t, "user-linked")
	for _, decision := range []string{"approve", "reject"} {
		code, out = d.decide(t, "linked", review, 0, decision, "Synthetic linked decision")
		require.Equal(t, 409, code, out)
		require.Equal(t, "self_review", out["reason"], "a login linked to the Staff member never decides their credential")
	}
	require.Empty(t, outboxPayloads(t, h.db, workforce.CredentialValidityChanged), "refused decisions publish no validity change")
	require.Zero(t, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND action IN ('workforce.credential_review.approved','workforce.credential_review.rejected')", d.pharmacy))
	_, cred := d.credential(t)
	require.Equal(t, review, cred["pending"].(map[string]any)["id"], "the review is still waiting")

	code, out = d.decide(t, "independent", review, 0, "approve", "Synthetic certificate checked")
	require.Equal(t, 200, code, out)
	require.Equal(t, "user-independent", out["decided_by_subject"])
	code, again := d.decide(t, "independent", review, 0, "approve", "Synthetic certificate checked")
	require.Equal(t, 200, code, again)
	require.Equal(t, out["decided_at"], again["decided_at"], "an identical decision replay changes nothing")
	code, again = d.decide(t, "independent", review, 0, "reject", "Synthetic change of mind")
	require.Equal(t, 409, code, again)
	require.Equal(t, "not_pending", again["reason"])
	require.Len(t, outboxPayloads(t, h.db, workforce.CredentialValidityChanged), 1)
}

func TestPharmacyTimeZoneDrivesCredentialStanding(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	code, out := d.hr(t, "admin", "GET", "/time-zone", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, map[string]any{"version": float64(0)}, out, "no zone is saved yet")

	day := d.days(t, "Asia/Kolkata")
	d.approve(t, day(-400), day(10))
	code, out = d.hr(t, "admin", "GET", "/credentials?expiring_within=30", nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, "time_zone_unset", out["reason"])
	require.Equal(t, "Set this pharmacy's time zone before filtering by expiry.", out["message"])
	_, cred := d.credential(t)
	require.Equal(t, "date_unknown", cred["status"])

	events := h.count(t, d.pharmacy, "outbox_events", "tenant_id=?", testutil.TenantA)
	for name, body := range map[string]any{
		"unsupported zone":    map[string]any{"expected_version": 0, "time_zone": "Mars/Olympus"},
		"local zone":          map[string]any{"expected_version": 0, "time_zone": "Local"},
		"lower-case zone":     map[string]any{"expected_version": 0, "time_zone": "asia/kolkata"},
		"empty zone":          map[string]any{"expected_version": 0, "time_zone": ""},
		"no expected version": map[string]any{"time_zone": "Asia/Kolkata"},
		"unknown field":       map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata", "week_start": 1},
		"over one KiB":        map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata", "pad": strings.Repeat("x", 1100)},
	} {
		code, out = d.hr(t, "admin", "PUT", "/time-zone", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	_, out = d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Mars/Olympus"})
	require.Equal(t, "Use a supported IANA time zone such as Asia/Kolkata.", out["message"], "the handler refuses before the database trigger")
	code, saved := d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata"})
	require.Equal(t, 200, code, saved)
	require.Equal(t, "Asia/Kolkata", saved["time_zone"])
	require.EqualValues(t, 1, saved["version"])
	require.Equal(t, "user-admin", saved["updated_by_subject"])
	require.NotEmpty(t, saved["updated_at"])
	code, out = d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata"})
	require.Equal(t, 200, code, out)
	require.Equal(t, saved, out, "the same zone replays unchanged")
	code, out = d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Dubai"})
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])
	code, out = d.hr(t, "admin", "GET", "/time-zone", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, saved, out)
	require.Equal(t, []map[string]any{{"previous": nil, "time_zone": "Asia/Kolkata", "version": float64(1)}}, d.zoneAudits(t))
	require.Equal(t, events, h.count(t, d.pharmacy, "outbox_events", "tenant_id=?", testutil.TenantA), "a zone save publishes no event")

	listed, cred := d.credential(t)
	require.Equal(t, "Asia/Kolkata", listed["time_zone"])
	require.Equal(t, "valid", cred["status"])
	code, out = d.hr(t, "admin", "GET", "/credentials?expiring_within=30", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	entry := out["data"].([]any)[0].(map[string]any)
	require.Equal(t, "SYN-PH-ENR", entry["employee_code"])
	require.Equal(t, "valid", entry["status"])
	require.EqualValues(t, 10, entry["days_remaining"])
	code, out = d.hr(t, "admin", "GET", "/credentials?expiring_within=5", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"])

	code, out = d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 1, "time_zone": "Asia/Dubai"})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	require.Equal(t, map[string]any{"previous": "Asia/Kolkata", "time_zone": "Asia/Dubai", "version": float64(2)}, d.zoneAudits(t)[1])
	require.Zero(t, h.count(t, d.pharmacy, "workforce_roster_credential_standing", "hospital_id=?", d.pharmacy), "a pharmacy zone never creates roster standing")
}
