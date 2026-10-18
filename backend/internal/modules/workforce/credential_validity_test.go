package workforce_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type credentialFixture struct {
	t                     *testing.T
	r                     *gin.Engine
	db, worker            *tenantdb.DB
	files                 *attachments.GCS
	content               []byte
	hospital, base, other string
}

type credentialStaff struct{ id, credential string }

func newCredentialFixture(t *testing.T) *credentialFixture {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	hr := []authz.Permission{workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermRosterManage}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"hr": testutil.TenantA, "reviewer": testutil.TenantA, "enrol-only": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"hr": append(hr, workforce.PermCredentialReview), "reviewer": {workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermCredentialReview},
			"enrol-only": {workforce.PermEnrollmentManage}, "reader": {workforce.PermEnrollmentManage, workforce.PermCredentialReview}, "foreign": append(hr, workforce.PermCredentialReview)},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	f := &credentialFixture{t: t, r: r, db: db, worker: worker, files: files, content: img.Bytes(), hospital: uuid.NewString()}
	f.base = "/v1/workforce/hospitals/" + f.hospital
	f.other = "/v1/workforce/hospitals/" + uuid.NewString()
	for _, base := range []string{f.base, f.other} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base, "hr", `{"name":"Synthetic Credential Hospital"}`).Code)
	}
	for user, level := range map[string]string{"hr": "manager", "reviewer": "manager", "enrol-only": "manager", "reader": "reader"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", f.base+"/access/user-"+user, "hr", `{"level":"`+level+`"}`).Code)
	}
	return f
}

func (f *credentialFixture) call(method, path, user string, body any) (int, map[string]any) {
	f.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(f.t, err)
		data = string(raw)
	}
	w := testutil.Do(f.r, method, path, user, data)
	var out map[string]any
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func enrollmentBody(version int, code string, credentials ...map[string]any) map[string]any {
	if credentials == nil {
		credentials = []map[string]any{}
	}
	return map[string]any{"expected_version": version, "staff": map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "nurse", "employment_status": "active"},
		"profile": map[string]any{}, "addresses": []any{}, "emergency_contacts": []any{}, "credentials": credentials, "qualifications": []any{}, "experiences": []any{}}
}

func credentialRow(id, number, from, until string) map[string]any {
	return map[string]any{"id": id, "type": "Nursing registration", "number": number, "issuer": "Synthetic Nursing Council", "country": "IN", "valid_from": from, "valid_until": until}
}

func (f *credentialFixture) enrol(code string) credentialStaff {
	f.t.Helper()
	s := credentialStaff{id: uuid.NewString(), credential: uuid.NewString()}
	code2, out := f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", enrollmentBody(0, code, credentialRow(s.credential, "SYN-"+code, "2024-01-01", "2026-12-31")))
	require.Equal(f.t, 200, code2, out)
	return s
}

func (f *credentialFixture) staffVersion(s credentialStaff) int {
	f.t.Helper()
	code, out := f.call("GET", f.base+"/staff/"+s.id+"/enrollment", "hr", nil)
	require.Equal(f.t, 200, code, out)
	return int(out["version"].(float64))
}

// evidence registers, uploads and scans one credential evidence image, returning its id.
func (f *credentialFixture) evidence(s credentialStaff, verdict string) string {
	f.t.Helper()
	file := uuid.NewString()
	digest := sha256.Sum256(f.content)
	path := f.base + "/staff/" + s.id + "/documents/" + file
	code, out := f.call("PUT", path, "hr", map[string]any{"record_kind": "credential", "record_id": s.credential, "category": "credential_evidence", "filename": "synthetic-licence.png", "content_type": "image/png", "size": len(f.content), "sha256": hex.EncodeToString(digest[:])})
	require.Equal(f.t, 201, code, out)
	req := httptest.NewRequest(http.MethodPut, path+"/content", bytes.NewReader(f.content))
	req.Header.Set("Authorization", "Bearer hr")
	req.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	require.Equal(f.t, 202, w.Code, w.Body.String())
	if verdict != "" {
		scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(f.t, verdict), time.Second, attachments.MaxFileBytes)
		require.NoError(f.t, err)
		processor, err := workforce.NewDocumentProcessor(f.t.Context(), f.worker, f.files, scanner)
		require.NoError(f.t, err)
		_, err = processor.Process(f.t.Context(), workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: f.hospital, StaffID: s.id, FileID: file, ScanAttempt: 1})
		require.NoError(f.t, err)
	}
	return file
}

const cleanScan = "stream: OK\x00"

func (f *credentialFixture) reviewPath(s credentialStaff, review, suffix string) string {
	return f.base + "/staff/" + s.id + "/credentials/" + s.credential + "/reviews/" + review + suffix
}

func reviewBody(version int, until string, evidence ...string) map[string]any {
	return map[string]any{"expected_version": version, "number": " SYN-RENEWED ", "valid_from": "2026-01-01", "valid_until": until, "evidence_document_ids": evidence, "reason": "Synthetic renewal certificate"}
}

func (f *credentialFixture) propose(s credentialStaff, user string, body map[string]any) (string, int, map[string]any) {
	f.t.Helper()
	review := uuid.NewString()
	code, out := f.call("PUT", f.reviewPath(s, review, ""), user, body)
	return review, code, out
}

func (f *credentialFixture) decide(s credentialStaff, review, user string, version int, decision, note string) (int, map[string]any) {
	return f.call("POST", f.reviewPath(s, review, "/decision"), user, map[string]any{"expected_version": version, "decision": decision, "note": note})
}

func (f *credentialFixture) setZone(zone string) {
	f.t.Helper()
	body := `{"expected_version":0,"timezone":"` + zone + `","week_start":0,"auto_population":"fixed_shift","effective_on":"2026-10-19","reviewed":true}`
	w := testutil.Do(f.r, "PUT", f.base+"/hr/roster-settings", "hr", body)
	require.Equal(f.t, 200, w.Code, w.Body.String())
}

func (f *credentialFixture) credentials(s credentialStaff) map[string]any {
	f.t.Helper()
	code, out := f.call("GET", f.base+"/staff/"+s.id+"/credentials", "hr", nil)
	require.Equal(f.t, 200, code, out)
	return out
}

func (f *credentialFixture) payloads(subject string) []map[string]any {
	f.t.Helper()
	var raw []string
	require.NoError(f.t, f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", subject).Order("created_at").Pluck("payload", &raw).Error
	}))
	result := make([]map[string]any, len(raw))
	for i, p := range raw {
		var event map[string]any
		require.NoError(f.t, json.Unmarshal([]byte(p), &event))
		data := event["data"]
		if s, ok := data.(string); ok {
			require.NoError(f.t, json.Unmarshal([]byte(s), &data))
		}
		result[i] = data.(map[string]any)
	}
	return result
}

func TestCredentialReviewNeedsASecondPersonAndUpdatesEnrollment(t *testing.T) {
	f := newCredentialFixture(t)
	s := f.enrol("CRED-01")
	out := f.credentials(s)
	require.Nil(t, out["time_zone"], "no roster zone yet")
	cred := out["credentials"].([]any)[0].(map[string]any)
	require.Equal(t, "unreviewed", cred["status"])
	require.Nil(t, cred["reviewed"])

	file := f.evidence(s, cleanScan)
	body := reviewBody(0, "2031-12-31", file)
	review, code, saved := f.propose(s, "hr", body)
	require.Equal(t, 200, code, saved)
	require.Equal(t, "pending", saved["status"])
	require.Equal(t, "SYN-RENEWED", saved["number"], "number is trimmed")
	require.Equal(t, "Nursing registration", saved["type"], "type comes from enrollment")
	require.Equal(t, []any{file}, saved["evidence_document_ids"])

	code, replay := f.call("PUT", f.reviewPath(s, review, ""), "hr", body)
	require.Equal(t, 200, code, replay)
	require.Equal(t, saved["created_at"], replay["created_at"], "identical replay returns the saved review")
	changed := reviewBody(0, "2032-12-31", file)
	code, out = f.call("PUT", f.reviewPath(s, review, ""), "hr", changed)
	require.Equal(t, 409, code, out)
	require.Equal(t, "review_conflict", out["reason"])
	_, code, out = f.propose(s, "reviewer", reviewBody(0, "2032-12-31", file))
	require.Equal(t, 409, code, out)
	require.Equal(t, "review_pending", out["reason"])

	code, out = f.decide(s, review, "hr", 0, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "self_review", out["reason"])
	code, out = f.decide(s, review, "enrol-only", 0, "approve", "")
	require.Equal(t, 403, code, out)
	code, out = f.decide(s, review, "reviewer", 1, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])
	code, out = f.call("POST", f.reviewPath(s, review, "/withdraw"), "reviewer", map[string]any{"note": ""})
	require.Equal(t, 409, code, out)
	require.Equal(t, "not_proposer", out["reason"])

	before := f.staffVersion(s)
	code, out = f.decide(s, review, "reviewer", 0, "approve", "Synthetic certificate checked")
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", out["status"])
	require.EqualValues(t, 1, out["approved_version"])
	code, again := f.decide(s, review, "reviewer", 0, "approve", "Synthetic certificate checked")
	require.Equal(t, 200, code, again)
	require.Equal(t, out["decided_at"], again["decided_at"], "an identical decision replay changes nothing")
	require.Equal(t, before+1, f.staffVersion(s), "approval bumps the enrollment version so stale forms conflict")

	f.setZone("Asia/Kolkata")
	out = f.credentials(s)
	require.Equal(t, "Asia/Kolkata", out["time_zone"])
	cred = out["credentials"].([]any)[0].(map[string]any)
	require.Equal(t, "valid", cred["status"])
	require.Equal(t, "SYN-RENEWED", cred["number"], "enrollment mirrors the reviewed record")
	require.Equal(t, "2031-12-31", cred["valid_until"])
	reviewed := cred["reviewed"].(map[string]any)
	require.EqualValues(t, 1, reviewed["version"])
	require.Equal(t, review, reviewed["review_id"])
	require.Nil(t, cred["pending"])

	code, out = f.call("GET", f.base+"/staff/"+s.id+"/credentials/"+s.credential+"/reviews?limit=1", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)

	reviews := f.payloads(workforce.CredentialReviewChanged)
	require.Len(t, reviews, 2)
	require.Equal(t, []string{"pending", "approved"}, []string{reviews[0]["status"].(string), reviews[1]["status"].(string)})
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "staff_id": s.id, "credential_id": s.credential, "review_id": review, "status": "approved", "version": float64(1)}, reviews[1], "events carry identifiers only")
	validity := f.payloads(workforce.CredentialValidityChanged)
	require.Equal(t, []map[string]any{{"hospital_id": f.hospital, "staff_id": s.id, "credential_id": s.credential, "review_id": review, "version": float64(1)}}, validity)
	for _, event := range append(reviews, validity...) {
		raw, err := json.Marshal(event)
		require.NoError(t, err)
		for _, secret := range []string{"SYN-RENEWED", "2031-12-31", "Synthetic Nursing Council", "Nursing registration"} {
			require.NotContains(t, string(raw), secret, "events carry identifiers only")
		}
	}
}

func TestEnrollmentCannotBypassACredentialReview(t *testing.T) {
	f := newCredentialFixture(t)
	s := f.enrol("CRED-02")
	review, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", f.evidence(s, cleanScan)))
	require.Equal(t, 200, code, out)
	code, out = f.decide(s, review, "reviewer", 0, "approve", "")
	require.Equal(t, 200, code, out)
	version := f.staffVersion(s)

	edited := enrollmentBody(version, "CRED-02", credentialRow(s.credential, "SYN-RENEWED", "2026-01-01", "2035-12-31"))
	code, out = f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", edited)
	require.Equal(t, 409, code, out)
	require.Equal(t, "credential_reviewed", out["reason"], "changing a reviewed date is refused")
	code, out = f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", enrollmentBody(version, "CRED-02"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "credential_reviewed", out["reason"], "removing a reviewed credential is refused")
	kept := enrollmentBody(version, "CRED-02", credentialRow(s.credential, "SYN-RENEWED", "2026-01-01", "2031-12-31"))
	kept["staff"].(map[string]any)["display_name"] = "Synthetic Renamed"
	code, out = f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", kept)
	require.Equal(t, 200, code, out, "other enrollment edits still save")
	code, out = f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", enrollmentBody(version-1, "CRED-02", credentialRow(s.credential, "SYN-RENEWED", "2026-01-01", "2031-12-31")))
	require.Equal(t, 409, code, out, "a form opened before approval is stale")
}

func TestCredentialReviewEvidenceMustBeThisCredentialsScannedFiles(t *testing.T) {
	f := newCredentialFixture(t)
	s, other := f.enrol("CRED-03"), f.enrol("CRED-04")
	foreign := f.evidence(other, cleanScan)
	infected := f.evidence(s, "stream: Eicar-Test-Signature FOUND\x00")
	revoked := f.evidence(s, cleanScan)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.base+"/staff/"+s.id+"/documents/"+revoked+"/revocation", "hr", "").Code)
	for name, file := range map[string]string{"another staff member's file": foreign, "an infected file": infected, "a revoked file": revoked, "an unknown file": uuid.NewString()} {
		_, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", file))
		require.Equal(t, 409, code, name)
		require.Equal(t, "evidence_invalid", out["reason"], name)
	}

	scanning := f.evidence(s, "")
	review, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", scanning))
	require.Equal(t, 200, code, out, "a file still scanning may be cited")
	code, out = f.decide(s, review, "reviewer", 0, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "evidence_unavailable", out["reason"], "approval waits for the scan")
	code, out = f.decide(s, review, "reviewer", 0, "reject", " ")
	require.Equal(t, 400, code, out, "rejecting needs a note")
	code, out = f.decide(s, review, "reviewer", 0, "reject", "Synthetic file unreadable")
	require.Equal(t, 200, code, out)
	require.Equal(t, "rejected", out["status"])
	code, out = f.decide(s, review, "reviewer", 0, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "not_pending", out["reason"])
	require.Empty(t, f.payloads(workforce.CredentialValidityChanged), "a rejection writes no reviewed validity")

	withdrawn, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", f.evidence(s, cleanScan)))
	require.Equal(t, 200, code, out)
	code, out = f.call("POST", f.reviewPath(s, withdrawn, "/withdraw"), "hr", map[string]any{"note": "Synthetic wrong certificate"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "withdrawn", out["status"])
}

func TestCredentialReviewsValidateAndStayScoped(t *testing.T) {
	f := newCredentialFixture(t)
	s := f.enrol("CRED-05")
	file := f.evidence(s, cleanScan)
	for name, body := range map[string]map[string]any{
		"end before start":   reviewBody(0, "2025-12-31", file),
		"no evidence":        reviewBody(0, "2031-12-31"),
		"duplicate evidence": reviewBody(0, "2031-12-31", file, file),
		"impossible date":    reviewBody(0, "2031-02-30", file),
		"blank number":       func() map[string]any { b := reviewBody(0, "2031-12-31", file); b["number"] = "  "; return b }(),
		"blank reason":       func() map[string]any { b := reviewBody(0, "2031-12-31", file); b["reason"] = ""; return b }(),
		"six files":          reviewBody(0, "2031-12-31", file, uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()),
	} {
		_, code, out := f.propose(s, "hr", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	_, code, out := f.propose(s, "hr", reviewBody(1, "2031-12-31", file))
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])

	review, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", file))
	require.Equal(t, 200, code, out)
	foreignPath := strings.Replace(f.reviewPath(s, review, "/decision"), f.base, f.other, 1)
	stranger := credentialStaff{id: uuid.NewString(), credential: s.credential}
	for name, path := range map[string]string{
		"other hospital":   foreignPath,
		"other staff":      f.reviewPath(stranger, review, "/decision"),
		"other credential": f.base + "/staff/" + s.id + "/credentials/" + uuid.NewString() + "/reviews/" + review + "/decision",
	} {
		code, out := f.call("POST", path, "reviewer", map[string]any{"expected_version": 0, "decision": "approve", "note": ""})
		require.Equal(t, 404, code, "%s: %v", name, out)
	}
	for _, user := range []string{"foreign", "reader"} {
		code, out := f.decide(s, review, user, 0, "approve", "")
		require.Equal(t, 404, code, "%s: %v", user, out)
		code, out = f.call("GET", f.base+"/staff/"+s.id+"/credentials", user, nil)
		require.Equal(t, 404, code, "%s: %v", user, out)
	}
	code, out = f.call("GET", f.base+"/staff/"+uuid.NewString()+"/credentials", "hr", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", f.base+"/staff/"+s.id+"/credentials/"+uuid.NewString()+"/reviews", "hr", nil)
	require.Equal(t, 404, code, out)
	w := testutil.Do(f.r, "GET", f.base+"/staff/"+s.id+"/credentials", "hr", "")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestCredentialRegisterListsSoonestExpiryInTheHospitalCalendar(t *testing.T) {
	f := newCredentialFixture(t)
	var now time.Time
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error }))
	loc, err := time.LoadLocation("Pacific/Auckland")
	require.NoError(t, err)
	today := now.In(loc)
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format(time.DateOnly) }
	expiries := map[string]string{"REG-01": day(200), "REG-02": day(-3), "REG-03": day(10)}
	for code, until := range expiries {
		s := f.enrol(code)
		body := reviewBody(0, until, f.evidence(s, cleanScan))
		body["valid_from"] = day(-400)
		review, status, out := f.propose(s, "hr", body)
		require.Equal(t, 200, status, out)
		status, out = f.decide(s, review, "reviewer", 0, "approve", "")
		require.Equal(t, 200, status, out)
	}
	f.enrol("REG-04")

	code, out := f.call("GET", f.base+"/hr/credentials?expiring_within=30", "hr", nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, "time_zone_unset", out["reason"], "an expiry filter needs the hospital calendar")
	code, out = f.call("GET", f.base+"/hr/credentials", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "date_unknown", out["data"].([]any)[0].(map[string]any)["status"])

	f.setZone("Pacific/Auckland")
	codes := func(out map[string]any) []string {
		var result []string
		for _, item := range out["data"].([]any) {
			result = append(result, item.(map[string]any)["employee_code"].(string))
		}
		return result
	}
	code, out = f.call("GET", f.base+"/hr/credentials?expiring_within=30", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"REG-02", "REG-03"}, codes(out), "expired and expiring soon, soonest first; unreviewed staff are absent")
	first := out["data"].([]any)[0].(map[string]any)
	require.Equal(t, "expired", first["status"])
	require.EqualValues(t, -3, first["days_remaining"])

	code, out = f.call("GET", f.base+"/hr/credentials?limit=2", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"REG-02", "REG-03"}, codes(out))
	cursor, ok := out["page"].(map[string]any)["next_cursor"].(string)
	require.True(t, ok, out)
	code, out = f.call("GET", f.base+"/hr/credentials?limit=2&cursor="+cursor, "hr", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"REG-01"}, codes(out))

	for _, bad := range []string{"-1", "366", "07", "x"} {
		code, out := f.call("GET", f.base+"/hr/credentials?expiring_within="+bad, "hr", nil)
		require.Equal(t, 400, code, "%s: %v", bad, out)
	}
	code, out = f.call("GET", strings.Replace(f.base, f.hospital, uuid.NewString(), 1)+"/hr/credentials", "hr", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", f.base+"/hr/credentials", "foreign", nil)
	require.Equal(t, 404, code, out)
}

func TestCredentialDatabaseGuards(t *testing.T) {
	f := newCredentialFixture(t)
	s := f.enrol("CRED-06")
	review, code, out := f.propose(s, "hr", reviewBody(0, "2031-12-31", f.evidence(s, cleanScan)))
	require.Equal(t, 200, code, out)
	write := func(flagged bool, statements ...string) error {
		return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, f.hospital).Error; err != nil {
				return err
			}
			if flagged {
				if err := tx.Exec(`SELECT set_config('app.workforce_credential_review','authorized',true)`).Error; err != nil {
					return err
				}
			}
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	where := ` WHERE tenant_id='` + testutil.TenantA + `' AND hospital_id='` + f.hospital + `' AND id='` + review + `'`
	requireGuard(t, write(false, `UPDATE workforce_credential_reviews SET status='approved',approved_version=1,decided_by_subject=proposed_by_subject,decided_at=now()`+where), "workforce_credential_review_separation", "self decision")
	requireGuard(t, write(false, `UPDATE workforce_credential_reviews SET reason='Synthetic rewrite'`+where), "decided once", "pending content is immutable")
	insert := `INSERT INTO workforce_credential_reviews(tenant_id,hospital_id,id,staff_id,credential_id,base_version,status,type,number,issuer,valid_from,valid_until,evidence_document_ids,reason,proposed_by_subject,decision_note)
 SELECT tenant_id,hospital_id,'` + uuid.NewString() + `',staff_id,credential_id,0,'pending',type,number,issuer,valid_from,valid_until,evidence_document_ids,reason,'user-other','' FROM workforce_credential_reviews` + where
	requireGuard(t, write(false, insert), "workforce_credential_review_one_pending", "one pending review")
	validity := `INSERT INTO workforce_credential_validity(tenant_id,hospital_id,staff_id,credential_id,type,number,issuer,valid_from,valid_until,version,review_id)
 SELECT tenant_id,hospital_id,staff_id,credential_id,type,number,issuer,valid_from,valid_until,1,id FROM workforce_credential_reviews` + where
	requireGuard(t, write(false, validity), "only by approving a review", "unflagged validity write")
	code, out = f.decide(s, review, "reviewer", 0, "approve", "")
	require.Equal(t, 200, code, out)
	requireGuard(t, write(true, `UPDATE workforce_enrollment_credentials SET valid_until='2040-01-01' WHERE staff_id='`+s.id+`'`), "workforce_credential_reviewed", "enrollment row follows the reviewed record")
	requireGuard(t, write(true, `UPDATE workforce_credential_validity SET valid_until='2040-01-01',version=2 WHERE staff_id='`+s.id+`'`), "only by approving a review", "validity must match an approved review")
	requireGuard(t, write(true, `DELETE FROM workforce_credential_reviews WHERE staff_id='`+s.id+`'`), "", "reviews are never deleted")
}
