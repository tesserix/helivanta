package workforce_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
)

type pharmacyCredentials struct {
	*pharmacyDocuments
	reviewed int
}

// newPharmacyCredentials adds pharmacy managers who may decide reviews: "reviewer", "independent" and "linked"; "documents" manages without enrollment; "roster" sets a hospital's roster zone.
func newPharmacyCredentials(t *testing.T) *pharmacyCredentials {
	t.Helper()
	review := []authz.Permission{workforce.PermEnrollmentManage, workforce.PermCredentialReview}
	d := newPharmacyDocumentsWith(t, true, map[string][]authz.Permission{
		"reader":      review,
		"outsider":    review,
		"independent": review,
		"linked":      review,
		"documents":   {workforce.PermDocumentManage},
		"foreign":     {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermCredentialReview},
		"reviewer":    {workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermCredentialReview},
		"roster":      {workforce.PermRosterManage},
	})
	for _, subject := range []string{"user-reviewer", "user-independent", "user-linked", "user-documents"} {
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

// advance runs one reminder cycle for the harness pharmacist's registration as the document worker.
func (d *pharmacyCredentials) advance(t *testing.T, version int) workforcecontract.CredentialReminderStep {
	t.Helper()
	advancer, err := workforce.NewCredentialReminderAdvancer(t.Context(), d.worker)
	require.NoError(t, err)
	step, err := advancer.Advance(t.Context(), workforcecontract.CredentialReminderInput{TenantID: testutil.TenantA, HospitalID: d.pharmacy, StaffID: d.staff, CredentialID: pharmacyCredentialID, Version: int64(version)})
	require.NoError(t, err)
	return step
}

func (d *pharmacyCredentials) reminders(t *testing.T, status string) []map[string]any {
	t.Helper()
	code, out := d.hr(t, "admin", "GET", "/credential-reminders?status="+status, nil)
	require.Equal(t, 200, code, out)
	rows := []map[string]any{}
	for _, item := range out["data"].([]any) {
		rows = append(rows, item.(map[string]any))
	}
	return rows
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

func TestPharmacyCredentialRemindersFollowThePharmacyZone(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	day := d.days(t, "Asia/Kolkata")
	d.approve(t, day(-400), day(10))
	first := d.reviewed
	code, out := d.hr(t, "admin", "PUT", "/credential-reminder-policy", map[string]any{"expected_version": 0, "thresholds": []int{30, 7, 0}})
	require.Equal(t, 200, code, out)
	require.False(t, d.advance(t, first).Done)
	require.Zero(t, h.count(t, d.pharmacy, "workforce_credential_reminders", "hospital_id=?", d.pharmacy), "without a pharmacy zone the worker writes nothing")
	require.Empty(t, outboxPayloads(t, h.db, workforce.CredentialReminderChanged))

	code, out = d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata"})
	require.Equal(t, 200, code, out)
	step := d.advance(t, first)
	require.False(t, step.Done)
	require.False(t, d.advance(t, first).Done, "a replayed cycle changes nothing")
	open := d.reminders(t, "open")
	require.Len(t, open, 1, "only the due threshold opens")
	require.EqualValues(t, 30, open[0]["threshold_days"])
	require.Equal(t, day(-20), open[0]["due_on"], "due dates are pharmacy-local")
	require.Equal(t, day(10), open[0]["valid_until"])
	require.Equal(t, "SYN-PH-ENR", open[0]["employee_code"])
	require.Equal(t, "registration", open[0]["type"])
	require.NotContains(t, open[0], "number", "the inbox never carries the licence number")
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	next, err := time.ParseInLocation(time.DateOnly, day(3), loc)
	require.NoError(t, err)
	require.False(t, step.WakeAt.After(next), "wakes no later than the next threshold's pharmacy-local midnight")

	ack := "/credential-reminders/" + open[0]["id"].(string) + "/acknowledgement"
	code, out = d.hr(t, "admin", "POST", ack, nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "acknowledged", out["status"])
	require.Equal(t, "user-admin", out["acknowledged_by_subject"])
	code, out = d.hr(t, "reviewer", "POST", ack, nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "user-admin", out["acknowledged_by_subject"], "a second acknowledgement keeps the first")

	code, out = d.hr(t, "admin", "PUT", "/credential-reminder-policy", map[string]any{"expected_version": 1, "thresholds": []int{30, 10, 0}})
	require.Equal(t, 200, code, out)
	require.False(t, d.advance(t, first).Done)
	open = d.reminders(t, "open")
	require.Len(t, open, 1)
	require.EqualValues(t, 10, open[0]["threshold_days"])

	d.approve(t, day(-400), day(200))
	require.Empty(t, d.reminders(t, "open"), "the next approval supersedes the open reminder")
	superseded := d.reminders(t, "superseded")
	require.Len(t, superseded, 1)
	require.Equal(t, open[0]["id"], superseded[0]["id"])
	require.True(t, d.advance(t, first).Done, "the old revision's schedule ends")
	require.False(t, d.advance(t, d.reviewed).Done)
	require.Empty(t, d.reminders(t, "open"), "nothing is due for the renewed registration yet")

	statuses := map[string]int{}
	for _, p := range outboxPayloads(t, h.db, workforce.CredentialReminderChanged) {
		require.Equal(t, d.pharmacy, p["hospital_id"], "reminder events name the owning pharmacy")
		statuses[p["status"].(string)]++
	}
	require.Equal(t, map[string]int{"open": 2, "acknowledged": 1, "superseded": 1}, statuses)
}

func TestPharmacyCredentialRoutesRefuseCallersOutsideTheirAuthority(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	day := d.days(t, "Asia/Kolkata")
	d.approve(t, day(-400), day(10))
	code, out := d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata"})
	require.Equal(t, 200, code, out)
	code, out = d.hr(t, "admin", "PUT", "/credential-reminder-policy", map[string]any{"expected_version": 0, "thresholds": []int{30, 7, 0}})
	require.Equal(t, 200, code, out)
	d.advance(t, d.reviewed)
	open := d.reminders(t, "open")
	require.Len(t, open, 1)
	review := uuid.Must(uuid.NewV7()).String()
	proposal := reviewBody(d.reviewed, day(300), d.evidence(t))
	code, out = d.call(t, "admin", "PUT", pharmacyReviewPath(review, ""), proposal)
	require.Equal(t, 200, code, out)

	other := h.pharmacy(t, "independent")
	grant(t, h, other, "user-admin", "manager")
	grant(t, h, other, "user-reviewer", "manager")
	elsewhere := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", enrollmentPath(other, elsewhere), string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	require.Equal(t, 200, testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/access/user-reviewer", "admin", `{"level":"manager"}`).Code)
	hospitalStaff := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/staff/"+hospitalStaff+"/enrollment", "admin", `{"expected_version":0,"staff":{"employee_code":"SYN-HOSPITAL-CRED","display_name":"Synthetic nurse","job_category":"nurse","employment_status":"active"},"credentials":[{"id":"`+pharmacyCredentialID+`","type":"registration","number":"SYN-NURSE-1","issuer":"Synthetic nursing council"}]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	supplier, supplierStaff := h.supplierStaff(t)
	require.NoError(t, h.tx(t, supplier, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-reviewer','manager')`, testutil.TenantA, supplier).Error
	}))

	decision := map[string]any{"expected_version": d.reviewed, "decision": "approve", "note": ""}
	// control is a permitted caller's answer on the real records, so a mistyped route cannot pass as a refusal; the decision and withdrawal controls conflict instead of writing.
	policy, zone := map[string]any{"expected_version": 1, "thresholds": []int{30, 7, 0}}, map[string]any{"expected_version": 1, "time_zone": "Asia/Kolkata"}
	routes := []struct {
		name, method, path string
		staff, review      bool
		body, controlBody  any
		controller         string
		control            int
	}{
		{"credentials", "GET", "/credentials", true, false, nil, nil, "admin", 200},
		{"review history", "GET", "/credentials/{credential}/reviews", true, false, nil, nil, "admin", 200},
		{"proposal", "PUT", "/credentials/{credential}/reviews/{review}", true, false, proposal, proposal, "admin", 200},
		{"decision", "POST", "/credentials/{credential}/reviews/{review}/decision", true, true, decision, map[string]any{"expected_version": d.reviewed + 5, "decision": "approve", "note": ""}, "reviewer", 409},
		{"withdrawal", "POST", "/credentials/{credential}/reviews/{review}/withdraw", true, false, map[string]any{"note": ""}, map[string]any{"note": ""}, "reviewer", 409},
		{"register", "GET", "/hr/credentials", false, false, nil, nil, "admin", 200},
		{"policy", "GET", "/hr/credential-reminder-policy", false, false, nil, nil, "admin", 200},
		{"policy save", "PUT", "/hr/credential-reminder-policy", false, false, policy, policy, "admin", 200},
		{"reminders", "GET", "/hr/credential-reminders", false, false, nil, nil, "admin", 200},
		{"acknowledgement", "POST", "/hr/credential-reminders/{reminder}/acknowledgement", false, false, nil, nil, "admin", 200},
		{"time zone", "GET", "/hr/time-zone", false, false, nil, nil, "admin", 200},
		{"time zone save", "PUT", "/hr/time-zone", false, false, zone, zone, "admin", 200},
	}
	ids := strings.NewReplacer("{credential}", pharmacyCredentialID, "{review}", review, "{reminder}", open[0]["id"].(string))
	target := func(path string, staffRoute bool, pharmacy, staff string) string {
		if staffRoute {
			return d.path(pharmacy, staff, ids.Replace(path))
		}
		return pharmacy + ids.Replace(path)
	}
	encode := func(body any) string {
		if body == nil {
			return ""
		}
		return string(mustJSON(t, body))
	}
	writes := func() []int64 {
		counts := []int64{}
		for _, establishment := range []string{d.pharmacy, other, h.hospital, supplier} {
			counts = append(counts, h.count(t, establishment, "workforce_audit", "hospital_id=?", establishment))
		}
		return append(counts, h.count(t, d.pharmacy, "outbox_events", "tenant_id=?", testutil.TenantA),
			h.count(t, d.pharmacy, "workforce_credential_reviews", "hospital_id=? AND status='pending'", d.pharmacy),
			h.count(t, d.pharmacy, "workforce_credential_reminders", "hospital_id=? AND status='open'", d.pharmacy))
	}
	before := writes()

	cases := map[string]struct {
		token, reviewToken, pharmacy, staff string
		staffOnly                           bool
		status                              int
	}{
		"a reader":                 {"reader", "reader", d.pharmacy, d.staff, false, 404},
		"no access row":            {"outsider", "outsider", d.pharmacy, d.staff, false, 404},
		"no permission":            {"documents", "admin", d.pharmacy, d.staff, false, 403},
		"a hospital ID":            {"admin", "reviewer", h.hospital, hospitalStaff, false, 404},
		"a supplier ID":            {"admin", "reviewer", supplier, supplierStaff, false, 404},
		"an unregistered pharmacy": {"admin", "reviewer", uuid.Must(uuid.NewV7()).String(), d.staff, false, 404},
		"a foreign tenant":         {"foreign", "foreign", d.pharmacy, d.staff, false, 404},
		"a missing Staff record":   {"admin", "reviewer", d.pharmacy, uuid.Must(uuid.NewV7()).String(), true, 404},
		"another pharmacy's Staff": {"admin", "reviewer", d.pharmacy, elsewhere, true, 404},
		"a hospital Staff record":  {"admin", "reviewer", d.pharmacy, hospitalStaff, true, 404},
	}
	for name, call := range cases {
		for _, route := range routes {
			if call.staffOnly && !route.staff {
				continue
			}
			token := call.token
			if route.review {
				token = call.reviewToken
			}
			t.Run(route.name+" "+name, func(t *testing.T) {
				w := pharmacyRequest(h, token, route.method, target(route.path, route.staff, call.pharmacy, call.staff), encode(route.body))
				require.Equal(t, call.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "SYN-RENEWED")
				require.NotContains(t, w.Body.String(), "SYN-PH-ENR")
			})
		}
	}
	for _, route := range routes {
		token := "admin"
		if route.review {
			token = "reviewer"
		}
		path := target(route.path, route.staff, d.pharmacy, d.staff)
		t.Run(route.name+" the HR intake view", func(t *testing.T) {
			separator := "?"
			if strings.Contains(path, "?") {
				separator = "&"
			}
			w := pharmacyRequest(h, token, route.method, path+separator+"hr_intake=v1", encode(route.body))
			require.Equal(t, 400, w.Code, w.Body.String())
		})
		for name, headers := range map[string]map[string][]string{
			"a changed subject":   {"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-other"}},
			"a missing subject":   {"X-Helivanta-Expected-Tenant": {testutil.TenantA}},
			"a missing tenant":    {"X-Helivanta-Expected-Subject": {"user-" + token}},
			"a repeated subject":  {"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-" + token, "user-" + token}},
			"no expected headers": {},
		} {
			t.Run(route.name+" "+name, func(t *testing.T) {
				req := httptest.NewRequest(route.method, "/v1/workforce/pharmacies/"+path, strings.NewReader(encode(route.body)))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				for header, values := range headers {
					for _, value := range values {
						req.Header.Add(header, value)
					}
				}
				w := httptest.NewRecorder()
				h.router.ServeHTTP(w, req)
				require.Equal(t, 409, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			})
		}
	}
	require.Equal(t, before, writes(), "no refused call reads or writes anything")

	for _, route := range routes {
		w := pharmacyRequest(h, route.controller, route.method, target(route.path, route.staff, d.pharmacy, d.staff), encode(route.controlBody))
		require.Equal(t, route.control, w.Code, "%s: %s", route.name, w.Body.String())
	}
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_credential_reviews", "hospital_id=? AND id=? AND status='pending'", d.pharmacy, review), "the conflicting controls leave the review pending")
}

// hospitalCredential gives a hospital nurse an approved validity with one open reminder, then a pending review, through the hospital family.
func (d *pharmacyCredentials) hospitalCredential(t *testing.T, day func(int) string) (staff, credential, pending, reminder string) {
	t.Helper()
	h := d.establishmentHarness
	base := "/v1/workforce/hospitals/" + h.hospital
	do := func(token, method, path, body string, want int) map[string]any {
		t.Helper()
		w := testutil.Do(h.router, method, base+path, token, body)
		require.Equal(t, want, w.Code, "%s %s: %s", method, path, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
		return out
	}
	for _, subject := range []string{"user-reviewer", "user-roster"} {
		do("admin", "PUT", "/access/"+subject, `{"level":"manager"}`, 200)
	}
	staff, credential = uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	do("admin", "PUT", "/staff/"+staff+"/enrollment", `{"expected_version":0,"staff":{"employee_code":"SYN-HOSPITAL-CRED","display_name":"Synthetic nurse","job_category":"nurse","employment_status":"active"},"credentials":[{"id":"`+credential+`","type":"registration","number":"SYN-NURSE-1","issuer":"Synthetic nursing council"}]}`, 200)
	do("roster", "PUT", "/hr/roster-settings", `{"expected_version":0,"timezone":"Asia/Kolkata","week_start":0,"auto_population":"fixed_shift","effective_on":"2026-10-19","reviewed":true}`, 200)
	do("admin", "PUT", "/hr/credential-reminder-policy", `{"expected_version":0,"thresholds":[30,7,0]}`, 200)
	evidence := func() string {
		file := uuid.Must(uuid.NewV7()).String()
		path := base + "/staff/" + staff + "/documents/" + file
		w := testutil.Do(h.router, "PUT", path, "admin", d.intent("credential", credential, "credential_evidence"))
		require.Equal(t, 201, w.Code, w.Body.String())
		req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(d.content))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set("Content-Type", "image/png")
		w = httptest.NewRecorder()
		h.router.ServeHTTP(w, req)
		require.Equal(t, 202, w.Code, w.Body.String())
		scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, cleanScan), time.Second, attachments.MaxFileBytes)
		require.NoError(t, err)
		processor, err := workforce.NewDocumentProcessor(t.Context(), d.worker, d.files, scanner)
		require.NoError(t, err)
		status, err := processor.Process(t.Context(), workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: h.hospital, StaffID: staff, FileID: file, ScanAttempt: 1})
		require.NoError(t, err)
		require.Equal(t, "available", status)
		return file
	}
	reviews := "/staff/" + staff + "/credentials/" + credential + "/reviews/"
	approved, body := uuid.Must(uuid.NewV7()).String(), reviewBody(0, day(10), evidence())
	body["valid_from"] = day(-400)
	do("admin", "PUT", reviews+approved, string(mustJSON(t, body)), 200)
	do("reviewer", "POST", reviews+approved+"/decision", `{"expected_version":0,"decision":"approve","note":""}`, 200)
	advancer, err := workforce.NewCredentialReminderAdvancer(t.Context(), d.worker)
	require.NoError(t, err)
	_, err = advancer.Advance(t.Context(), workforcecontract.CredentialReminderInput{TenantID: testutil.TenantA, HospitalID: h.hospital, StaffID: staff, CredentialID: credential, Version: 1})
	require.NoError(t, err)
	open := do("admin", "GET", "/hr/credential-reminders?status=open", "", 200)["data"].([]any)
	require.Len(t, open, 1)
	pending = uuid.Must(uuid.NewV7()).String()
	do("admin", "PUT", reviews+pending, string(mustJSON(t, reviewBody(1, day(300), evidence()))), 200)
	return staff, credential, pending, open[0].(map[string]any)["id"].(string)
}

// A record ID is found only under the family and establishment that keep it, whichever family names it.
func TestPharmacyCredentialRecordsStayInTheirFamily(t *testing.T) {
	d := newPharmacyCredentials(t)
	h := d.establishmentHarness
	day := d.days(t, "Asia/Kolkata")
	d.approve(t, day(-400), day(10))
	code, out := d.hr(t, "admin", "PUT", "/time-zone", map[string]any{"expected_version": 0, "time_zone": "Asia/Kolkata"})
	require.Equal(t, 200, code, out)
	code, out = d.hr(t, "admin", "PUT", "/credential-reminder-policy", map[string]any{"expected_version": 0, "thresholds": []int{30, 7, 0}})
	require.Equal(t, 200, code, out)
	d.advance(t, d.reviewed)
	open := d.reminders(t, "open")
	require.Len(t, open, 1)
	pharmacyReminder, pharmacyPending := open[0]["id"].(string), uuid.Must(uuid.NewV7()).String()
	code, out = d.call(t, "admin", "PUT", pharmacyReviewPath(pharmacyPending, ""), reviewBody(d.reviewed, day(300), d.evidence(t)))
	require.Equal(t, 200, code, out)
	hospitalStaff, hospitalCredential, hospitalPending, hospitalReminder := d.hospitalCredential(t, day)

	proposal := string(mustJSON(t, reviewBody(d.reviewed, day(300), d.evidence(t))))
	decision, withdrawal := `{"expected_version":1,"decision":"approve","note":""}`, `{"note":""}`
	type call struct{ token, method, path, body string }
	staffCalls := func(staff, credential, review string) []call {
		path := staff + "/credentials/" + credential + "/reviews"
		return []call{
			{"admin", "GET", path, ""},
			{"admin", "PUT", path + "/" + uuid.Must(uuid.NewV7()).String(), proposal},
			{"reviewer", "POST", path + "/" + review + "/decision", decision},
			{"admin", "POST", path + "/" + review + "/withdraw", withdrawal},
		}
	}
	// Each family's own ID with the other's Staff, credential, review or reminder, then each family's routes on the other's ID.
	families := map[string]struct {
		send  func(token, method, path, body string) *httptest.ResponseRecorder
		calls []call
	}{
		"the pharmacy family": {func(token, method, path, body string) *httptest.ResponseRecorder {
			return pharmacyRequest(h, token, method, path, body)
		}, slices.Concat(
			staffCalls(d.pharmacy+"/staff/"+d.staff, hospitalCredential, hospitalPending),
			staffCalls(d.pharmacy+"/staff/"+d.staff, pharmacyCredentialID, hospitalPending)[2:],
			staffCalls(d.pharmacy+"/staff/"+hospitalStaff, hospitalCredential, hospitalPending),
			[]call{{"admin", "POST", d.pharmacy + "/hr/credential-reminders/" + hospitalReminder + "/acknowledgement", ""}},
		)},
		"the hospital family": {func(token, method, path, body string) *httptest.ResponseRecorder {
			return testutil.Do(h.router, method, "/v1/workforce/hospitals/"+path, token, body)
		}, slices.Concat(
			staffCalls(h.hospital+"/staff/"+hospitalStaff, pharmacyCredentialID, pharmacyPending),
			staffCalls(h.hospital+"/staff/"+hospitalStaff, hospitalCredential, pharmacyPending)[2:],
			staffCalls(h.hospital+"/staff/"+d.staff, pharmacyCredentialID, pharmacyPending),
			staffCalls(d.pharmacy+"/staff/"+d.staff, pharmacyCredentialID, pharmacyPending),
			[]call{
				{"admin", "GET", h.hospital + "/staff/" + d.staff + "/credentials", ""},
				{"admin", "GET", d.pharmacy + "/staff/" + d.staff + "/credentials", ""},
				{"admin", "POST", h.hospital + "/hr/credential-reminders/" + pharmacyReminder + "/acknowledgement", ""},
				{"admin", "POST", d.pharmacy + "/hr/credential-reminders/" + pharmacyReminder + "/acknowledgement", ""},
				{"admin", "GET", d.pharmacy + "/hr/credentials", ""},
				{"admin", "GET", d.pharmacy + "/hr/credential-reminders", ""},
				{"admin", "GET", d.pharmacy + "/hr/credential-reminder-policy", ""},
				{"admin", "PUT", d.pharmacy + "/hr/credential-reminder-policy", `{"expected_version":1,"thresholds":[60,0]}`},
			},
		)},
	}
	writes := func() []int64 {
		counts := []int64{}
		for _, establishment := range []string{d.pharmacy, h.hospital} {
			counts = append(counts, h.count(t, establishment, "workforce_audit", "hospital_id=?", establishment),
				h.count(t, establishment, "workforce_credential_reviews", "hospital_id=? AND status='pending'", establishment),
				h.count(t, establishment, "workforce_credential_reminders", "hospital_id=? AND status='open'", establishment))
		}
		return append(counts, h.count(t, d.pharmacy, "outbox_events", "tenant_id=?", testutil.TenantA))
	}
	before := writes()
	for name, family := range families {
		for _, c := range family.calls {
			t.Run(name+" "+c.method+" "+c.path, func(t *testing.T) {
				w := family.send(c.token, c.method, c.path, c.body)
				require.Equal(t, 404, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "SYN-RENEWED")
			})
		}
	}
	require.Equal(t, before, writes(), "no cross-family call reads or writes anything")

	// The same IDs answer under their own family, so every refusal above is the family boundary.
	for _, control := range []struct {
		send              func(token, method, path, body string) *httptest.ResponseRecorder
		reviews, reminder string
	}{
		{families["the pharmacy family"].send, d.pharmacy + "/staff/" + d.staff + "/credentials/" + pharmacyCredentialID + "/reviews/" + pharmacyPending, d.pharmacy + "/hr/credential-reminders/" + pharmacyReminder},
		{families["the hospital family"].send, h.hospital + "/staff/" + hospitalStaff + "/credentials/" + hospitalCredential + "/reviews/" + hospitalPending, h.hospital + "/hr/credential-reminders/" + hospitalReminder},
	} {
		w := control.send("admin", "POST", control.reviews+"/withdraw", withdrawal)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"withdrawn"`)
		w = control.send("admin", "POST", control.reminder+"/acknowledgement", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"acknowledged"`)
	}
}
