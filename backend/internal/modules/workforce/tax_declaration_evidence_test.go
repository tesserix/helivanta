package workforce_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type evidenceFixture struct {
	*identifierFixture
	worker  *tenantdb.DB
	files   *attachments.GCS
	content []byte
}

func newEvidenceFixture(t *testing.T) *evidenceFixture {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	f := newIdentifierFixtureWith(t, vaultClient(t), files)
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	return &evidenceFixture{identifierFixture: f, worker: worker, files: files, content: img.Bytes()}
}

func (f *evidenceFixture) evidencePath(staff, suffix string) string {
	return f.declPath(staff, "/evidence"+suffix)
}

func (f *evidenceFixture) propose(staff string, version int) string {
	f.t.Helper()
	code, out := f.declare(staff, "admin", declBody(version, nil, nil))
	require.Equal(f.t, 200, code, out)
	return out["pending"].(map[string]any)["id"].(string)
}

func (f *evidenceFixture) register(staff, user, revision, file string) (int, map[string]any) {
	digest := sha256.Sum256(f.content)
	return f.call("PUT", f.evidencePath(staff, "/"+file), user, map[string]any{"revision_id": revision, "filename": " synthetic-rent-receipt.png ", "content_type": "image/png", "size": len(f.content), "sha256": hex.EncodeToString(digest[:])})
}

func (f *evidenceFixture) upload(staff, file string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, f.base+"/hr/payroll"+f.evidencePath(staff, "/"+file+"/content"), bytes.NewReader(f.content))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// attach registers and uploads one file, leaving it scan_pending.
func (f *evidenceFixture) attach(staff, revision string) string {
	f.t.Helper()
	file := uuid.NewString()
	code, out := f.register(staff, "admin", revision, file)
	require.Equal(f.t, 201, code, out)
	w := f.upload(staff, file)
	require.Equal(f.t, 202, w.Code, w.Body.String())
	return file
}

func (f *evidenceFixture) scan(staff, file string, attempt int64, verdict string) (string, error) {
	f.t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(f.t, verdict), time.Second, attachments.MaxFileBytes)
	require.NoError(f.t, err)
	processor, err := workforce.NewDocumentProcessor(f.t.Context(), f.worker, f.files, scanner)
	require.NoError(f.t, err)
	return processor.Process(f.t.Context(), workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: f.hospital, StaffID: staff, FileID: file, ScanAttempt: attempt})
}

func (f *evidenceFixture) evidence(staff, user, revision string) (int, map[string]any) {
	return f.call("GET", f.evidencePath(staff, "?revision_id="+revision), user, nil)
}

func (f *evidenceFixture) statusOf(staff, revision, file string) string {
	f.t.Helper()
	code, out := f.evidence(staff, "viewer", revision)
	require.Equal(f.t, 200, code, out)
	for _, row := range out["evidence"].([]any) {
		if d := row.(map[string]any); d["id"] == file {
			return d["status"].(string)
		}
	}
	f.t.Fatalf("evidence %s not listed: %v", file, out)
	return ""
}

// evidenceWrite runs statements past the payroll write gate as an audited actor, as the handlers do.
func (f *evidenceFixture) evidenceWrite(statements ...string) error {
	return f.payrollWrite(append([]string{`SELECT set_config('app.actor_subject','user-admin',true)`}, statements...)...)
}

func (f *evidenceFixture) approve(staff, proposal string, version int) (int, map[string]any) {
	return f.decideDecl(staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": version, "decision": "approve", "note": "Synthetic receipts checked"})
}

func TestTaxEvidenceUploadsScansAndDownloadsUnderPayrollScope(t *testing.T) {
	f := newEvidenceFixture(t)
	proposal := f.propose(f.staff, 0)
	file := uuid.NewString()

	code, out := f.register(f.staff, "reader", proposal, file)
	require.Equal(t, 404, code, "a hospital reader cannot attach: %v", out)
	code, out = f.register(f.staff, "admin", proposal, file)
	require.Equal(t, 201, code, out)
	require.Equal(t, "tax_declaration", out["record_kind"])
	require.Equal(t, "tax_declaration_evidence", out["category"])
	require.Equal(t, proposal, out["record_id"])
	require.Equal(t, "synthetic-rent-receipt.png", out["filename"])
	code, again := f.register(f.staff, "admin", proposal, file)
	require.Equal(t, 200, code, "an identical replay returns the intent: %v", again)
	require.Equal(t, out["id"], again["id"])

	w := f.upload(f.staff, file)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.Equal(t, "scan_pending", f.statusOf(f.staff, proposal, file))
	code, out = f.approve(f.staff, proposal, 0)
	requireSalaryConflict(t, code, out, "evidence_scanning", "", "", "")

	status, err := f.scan(f.staff, file, 1, "stream: OK\x00")
	require.NoError(t, err)
	require.Equal(t, "available", status)
	code, out = f.evidence(f.staff, "viewer", proposal)
	require.Equal(t, 200, code, out)
	require.Equal(t, true, out["uploads_available"])
	require.Len(t, out["evidence"], 1)

	download := testutil.Do(f.router, "GET", f.base+"/hr/payroll"+f.evidencePath(f.staff, "/"+file+"/content"), "viewer", "")
	require.Equal(t, 200, download.Code, download.Body.String())
	require.Equal(t, f.content, download.Body.Bytes())
	require.Equal(t, "private, no-store", download.Header().Get("Cache-Control"))
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("workforce_audit").Where("action=? AND resource_id=? AND changes->>'file_id'=?", "workforce.tax_declaration.evidence_downloaded", f.staff, file).Count(&count).Error)
		require.EqualValues(t, 1, count, "every evidence download is audited")
		return nil
	}))

	code, out = f.approve(f.staff, proposal, 0)
	require.Equal(t, 200, code, out)
	require.Equal(t, "available", f.statusOf(f.staff, proposal, file), "approved evidence stays as the record the approver saw")
	download = testutil.Do(f.router, "GET", f.base+"/hr/payroll"+f.evidencePath(f.staff, "/"+file+"/content"), "viewer", "")
	require.Equal(t, 200, download.Code, download.Body.String())
	code, out = f.register(f.staff, "admin", proposal, uuid.NewString())
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")
}

func TestTaxEvidenceIsInvisibleToStaffDocumentRoutes(t *testing.T) {
	f := newEvidenceFixture(t)
	proposal := f.propose(f.staff, 0)
	file := f.attach(f.staff, proposal)
	_, err := f.scan(f.staff, file, 1, "stream: OK\x00")
	require.NoError(t, err)

	staffDocs := f.base + "/staff/" + f.staff + "/documents"
	listing := testutil.Do(f.router, "GET", staffDocs, "admin", "")
	require.Equal(t, 200, listing.Code, listing.Body.String())
	require.NotContains(t, listing.Body.String(), file, "HR listings never include payroll evidence")
	for _, path := range []string{"/" + file + "/content", "/" + file + "/revocation"} {
		method := "GET"
		if path != "/"+file+"/content" {
			method = "PUT"
		}
		res := testutil.Do(f.router, method, staffDocs+path, "admin", "")
		require.Equal(t, 404, res.Code, "%s %s: %s", method, path, res.Body.String())
	}
	require.Equal(t, "available", f.statusOf(f.staff, proposal, file))

	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_enrollment','authorized',true)`).Error; err != nil {
			return err
		}
		var count int64
		require.NoError(t, tx.Table("workforce_documents").Where("id=?", file).Count(&count).Error)
		require.Zero(t, count, "the enrollment scope cannot see payroll evidence")
		return nil
	}))
}

func TestTaxEvidenceDeniesOtherTenantsHospitalsAndStaff(t *testing.T) {
	f := newEvidenceFixture(t)
	proposal := f.propose(f.staff, 0)
	file := f.attach(f.staff, proposal)
	other := f.enrol("ID-02")

	code, out := f.evidence(f.staff, "foreign", proposal)
	require.Equal(t, 404, code, out)
	code, out = f.evidence(other, "viewer", proposal)
	require.Equal(t, 404, code, "a revision answers only for its own staff member: %v", out)
	code, out = f.register(other, "admin", proposal, uuid.NewString())
	require.Equal(t, 404, code, out)
	code, out = f.register(f.staff, "foreign", proposal, uuid.NewString())
	require.Equal(t, 404, code, out)
	res := testutil.Do(f.router, "GET", f.other+"/hr/payroll"+f.evidencePath(f.staff, "?revision_id="+proposal), "admin", "")
	require.Equal(t, 404, res.Code, res.Body.String())
	res = testutil.Do(f.router, "GET", f.base+"/hr/payroll"+f.evidencePath(other, "/"+file+"/content"), "viewer", "")
	require.Equal(t, 404, res.Code, res.Body.String())
	code, out = f.call("GET", "/staff/"+f.staff+"/tax-declarations/2025-26/evidence?revision_id="+proposal, "viewer", nil)
	require.Equal(t, 404, code, "a revision answers only for its own tax year: %v", out)
}

func TestTaxEvidenceLimitAndDecisionSettlement(t *testing.T) {
	f := newEvidenceFixture(t)
	proposal := f.propose(f.staff, 0)
	files := make([]string, 0, 10)
	for range 10 {
		files = append(files, f.attach(f.staff, proposal))
	}
	code, out := f.register(f.staff, "admin", proposal, uuid.NewString())
	requireSalaryConflict(t, code, out, "evidence_limit", "", "", "")
	status, err := f.scan(f.staff, files[0], 1, "stream: OK\x00")
	require.NoError(t, err)
	require.Equal(t, "available", status)
	status, err = f.scan(f.staff, files[1], 1, "stream: ERROR\x00")
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	for _, file := range files[2:] {
		_, err := f.scan(f.staff, file, 1, "stream: OK\x00")
		require.NoError(t, err)
	}

	code, out = f.decideDecl(f.staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "reject", "note": "Synthetic receipt unreadable"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "revoked", f.statusOf(f.staff, proposal, files[0]), "rejection revokes what was readable")
	require.Equal(t, "scan_error", f.statusOf(f.staff, proposal, files[1]))
	res := testutil.Do(f.router, "GET", f.base+"/hr/payroll"+f.evidencePath(f.staff, "/"+files[0]+"/content"), "viewer", "")
	require.Equal(t, 409, res.Code, res.Body.String())
	code, out = f.call("PUT", f.evidencePath(f.staff, "/"+files[1]+"/retries/2"), "admin", nil)
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=? AND payload::jsonb->'data'->>'status'='revoked'", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 9, count, "one revocation per readable file")
		return nil
	}))

	withdrawn := f.propose(f.staff, 0)
	pending := f.attach(f.staff, withdrawn)
	intent := uuid.NewString()
	code, out = f.register(f.staff, "admin", withdrawn, intent)
	require.Equal(t, 201, code, out)
	code, out = f.call("POST", f.declPath(f.staff, "/withdraw"), "admin", map[string]any{"proposal_id": withdrawn, "note": ""})
	requireSalaryConflict(t, code, out, "evidence_scanning", "", "", "")
	_, err = f.scan(f.staff, pending, 1, "stream: OK\x00")
	require.NoError(t, err)
	code, out = f.call("POST", f.declPath(f.staff, "/withdraw"), "admin", map[string]any{"proposal_id": withdrawn, "note": ""})
	require.Equal(t, 200, code, out)
	require.Equal(t, "revoked", f.statusOf(f.staff, withdrawn, pending))
	w := f.upload(f.staff, intent)
	require.Equal(t, 409, w.Code, "an intent never receives bytes once its proposal is decided: %s", w.Body.String())
}

func TestTaxEvidenceDatabaseGuards(t *testing.T) {
	f := newEvidenceFixture(t)
	proposal := f.propose(f.staff, 0)
	file := f.attach(f.staff, proposal)
	failed := f.attach(f.staff, proposal)
	_, err := f.scan(f.staff, failed, 1, "stream: ERROR\x00")
	require.Error(t, err)
	other := f.enrol("ID-02")
	digest := hex.EncodeToString(make([]byte, 32))
	insert := func(staff, revision string, version int) string {
		return `INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256)
		 VALUES('` + testutil.TenantA + `','` + f.hospital + `','` + uuid.NewString() + `','` + staff + `','` + revision + `','tax_declaration',` + strconv.Itoa(version) + `,'tax_declaration_evidence','synthetic.png','image/png',1,'` + digest + `')`
	}
	requireGuard(t, f.evidenceWrite(insert(other, proposal, 1)), "binding is invalid", "another staff member's revision")
	requireGuard(t, f.evidenceWrite(insert(f.staff, proposal, 99)), "binding is invalid", "a stale revision sequence")
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_staff_tax_declaration_revisions SET status='approved',decided_by_subject='user-approver',decided_at=clock_timestamp() WHERE id='`+proposal+`'`), "workforce_tax_evidence_scanning", "deciding while scanning")
	_, err = f.scan(f.staff, file, 1, "stream: OK\x00")
	require.NoError(t, err)
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_staff_tax_declaration_revisions SET status='rejected',decided_by_subject='user-approver',decided_at=clock_timestamp() WHERE id='`+proposal+`'`), "workforce_tax_evidence_unsettled", "deciding with unfrozen evidence")
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_documents SET frozen=true WHERE record_id='`+proposal+`'`,
		`UPDATE workforce_staff_tax_declaration_revisions SET status='rejected',decided_by_subject='user-approver',decided_at=clock_timestamp() WHERE id='`+proposal+`'`), "workforce_tax_evidence_unsettled", "rejecting with readable evidence")
	require.NoError(t, f.evidenceWrite(`UPDATE workforce_documents SET frozen=true WHERE record_id='`+proposal+`'`), "freezing keeps a scan_error verdict without the processing identity")
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_documents SET frozen=false WHERE id='`+file+`'`), "workforce_tax_evidence_frozen", "unfreezing")
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_documents SET status='scan_pending',scan_attempt=2,failure_code=NULL WHERE id='`+failed+`'`), "workforce_tax_evidence_frozen", "rescanning frozen evidence")
	requireGuard(t, f.evidenceWrite(`UPDATE workforce_documents SET updated_at=clock_timestamp() WHERE id='`+failed+`'`), "processing identity", "only the freezing update itself is exempt")

	require.Error(t, f.asManager(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_enrollment','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(insert(f.staff, proposal, 1)).Error
	}), "the enrollment scope cannot write payroll evidence")
}
