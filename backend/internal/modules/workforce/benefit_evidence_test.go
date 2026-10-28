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

type benefitEvidenceFixture struct {
	*benefitFixture
	worker   *tenantdb.DB
	files    *attachments.GCS
	content  []byte
	employee string
	plans    int
}

// coverageProposal names one proposal by the path that reaches it.
type coverageProposal struct{ staff, enrollment, id string }

func newBenefitEvidenceFixture(t *testing.T) *benefitEvidenceFixture {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	f := newBenefitFixtureWith(t, files)
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	return &benefitEvidenceFixture{benefitFixture: f, worker: worker, files: files, content: img.Bytes(), employee: f.staff("SYN-EV-01")}
}

// propose opens a pending proposal under a plan of its own, so proposals never overlap.
func (f *benefitEvidenceFixture) propose(staff string) coverageProposal {
	f.t.Helper()
	f.plans++
	plan, _ := f.plan("SYN-EV-" + strconv.Itoa(f.plans))
	r := coverageProposal{staff: staff, enrollment: uuid.NewString(), id: uuid.NewString()}
	f.ok("PUT", f.proposalPath(staff, r.enrollment, r.id, ""), "hr", proposalBody(0, plan, nil, "2024-01-01", nil))
	return r
}

func (f *benefitEvidenceFixture) evidencePath(r coverageProposal, suffix string) string {
	return f.proposalPath(r.staff, r.enrollment, r.id, "/evidence"+suffix)
}

func (f *benefitEvidenceFixture) register(user string, r coverageProposal, file string) (int, map[string]any) {
	digest := sha256.Sum256(f.content)
	return f.call("PUT", f.evidencePath(r, "/"+file), user, map[string]any{"filename": " synthetic-marriage-certificate.png ", "content_type": "image/png", "size": len(f.content), "sha256": hex.EncodeToString(digest[:])})
}

func (f *benefitEvidenceFixture) upload(r coverageProposal, file string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, f.evidencePath(r, "/"+file+"/content"), bytes.NewReader(f.content))
	req.Header.Set("Authorization", "Bearer hr")
	req.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// attach registers and uploads one file, leaving it scan_pending.
func (f *benefitEvidenceFixture) attach(r coverageProposal) string {
	f.t.Helper()
	file := uuid.NewString()
	code, out := f.register("hr", r, file)
	require.Equal(f.t, 201, code, out)
	w := f.upload(r, file)
	require.Equal(f.t, 202, w.Code, w.Body.String())
	return file
}

func (f *benefitEvidenceFixture) scan(r coverageProposal, file string, attempt int64, verdict string) (string, error) {
	f.t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(f.t, verdict), time.Second, attachments.MaxFileBytes)
	require.NoError(f.t, err)
	processor, err := workforce.NewDocumentProcessor(f.t.Context(), f.worker, f.files, scanner)
	require.NoError(f.t, err)
	return processor.Process(f.t.Context(), workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: f.hospital, StaffID: r.staff, FileID: file, ScanAttempt: attempt})
}

func (f *benefitEvidenceFixture) evidence(user string, r coverageProposal) (int, map[string]any) {
	return f.call("GET", f.evidencePath(r, ""), user, nil)
}

func (f *benefitEvidenceFixture) statusOf(r coverageProposal, file string) string {
	f.t.Helper()
	out := f.ok("GET", f.evidencePath(r, ""), "reader", nil)
	for _, row := range out["evidence"].([]any) {
		if d := row.(map[string]any); d["id"] == file {
			return d["status"].(string)
		}
	}
	f.t.Fatalf("evidence %s not listed: %v", file, out)
	return ""
}

func (f *benefitEvidenceFixture) download(user string, r coverageProposal, file string) *httptest.ResponseRecorder {
	return testutil.Do(f.r, "GET", f.evidencePath(r, "/"+file+"/content"), user, "")
}

func (f *benefitEvidenceFixture) decide(r coverageProposal, decision, note string) (int, map[string]any) {
	return f.call("POST", f.proposalPath(r.staff, r.enrollment, r.id, "/decision"), "approver", map[string]any{"expected_version": 0, "decision": decision, "note": note})
}

func (f *benefitEvidenceFixture) withdraw(r coverageProposal) (int, map[string]any) {
	return f.call("POST", f.proposalPath(r.staff, r.enrollment, r.id, "/withdraw"), "hr", map[string]any{"note": ""})
}

// scoped runs statements as hr under the given workforce scope flags, past the handlers.
func (f *benefitEvidenceFixture) scoped(flags string, statements ...string) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.actor_subject','user-hr',true)`+flags, f.hospital).Error; err != nil {
			return err
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (f *benefitEvidenceFixture) benefitsWrite(statements ...string) error {
	return f.scoped(`,set_config('app.workforce_benefits','write',true)`, statements...)
}

func (f *benefitEvidenceFixture) revocations() int {
	f.t.Helper()
	revoked := 0
	for _, data := range f.payloads(workforce.DocumentChanged) {
		if data["status"] == "revoked" {
			revoked++
		}
	}
	return revoked
}

func TestBenefitEvidenceUploadsScansAndDownloadsUnderBenefitsScope(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	file := uuid.NewString()

	code, out := f.register("reader", proposal, file)
	require.Equal(t, 404, code, "a hospital reader cannot attach: %v", out)
	code, out = f.register("staff-only", proposal, file)
	require.Equal(t, 403, code, "HR document managers hold no benefits permission: %v", out)
	code, out = f.register("hr", proposal, file)
	require.Equal(t, 201, code, out)
	require.Equal(t, "benefit_proposal", out["record_kind"])
	require.Equal(t, "benefit_eligibility_evidence", out["category"])
	require.Equal(t, proposal.id, out["record_id"])
	require.EqualValues(t, 1, out["record_version"], "evidence records the version the proposal would approve")
	require.Equal(t, "synthetic-marriage-certificate.png", out["filename"])
	code, again := f.register("hr", proposal, file)
	require.Equal(t, 200, code, "an identical replay returns the intent: %v", again)
	require.Equal(t, out["id"], again["id"])

	w := f.upload(proposal, file)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.Equal(t, "scan_pending", f.statusOf(proposal, file))
	f.conflict("evidence_scanning", "POST", f.proposalPath(proposal.staff, proposal.enrollment, proposal.id, "/decision"), "approver", map[string]any{"expected_version": 0, "decision": "approve", "note": ""})

	status, err := f.scan(proposal, file, 1, "stream: OK\x00")
	require.NoError(t, err)
	require.Equal(t, "available", status)
	code, out = f.evidence("reader", proposal)
	require.Equal(t, 200, code, out)
	require.Equal(t, true, out["uploads_available"])
	require.Len(t, out["evidence"], 1)

	download := f.download("reader", proposal, file)
	require.Equal(t, 200, download.Code, download.Body.String())
	require.Equal(t, f.content, download.Body.Bytes())
	require.Equal(t, "private, no-store", download.Header().Get("Cache-Control"))
	require.Equal(t, 1, f.audits("workforce.benefits.evidence_downloaded"), "every evidence download is audited")

	code, out = f.decide(proposal, "approve", "Synthetic certificate checked")
	require.Equal(t, 200, code, out)
	require.Equal(t, "available", f.statusOf(proposal, file), "approved evidence stays as the record the approver saw")
	download = f.download("reader", proposal, file)
	require.Equal(t, 200, download.Code, download.Body.String())
	f.conflict("not_pending", "PUT", f.evidencePath(proposal, "/"+uuid.NewString()), "hr", map[string]any{"filename": "late.png", "content_type": "image/png", "size": len(f.content), "sha256": hex.EncodeToString(make([]byte, 32))})
}

func TestBenefitEvidenceRejectsInvalidFiles(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	digest := hex.EncodeToString(make([]byte, 32))
	for name, body := range map[string]map[string]any{
		"pdf":            {"filename": "synthetic.pdf", "content_type": "application/pdf", "size": 10, "sha256": digest},
		"over 10 MiB":    {"filename": "synthetic.png", "content_type": "image/png", "size": 10*1024*1024 + 1, "sha256": digest},
		"empty":          {"filename": "synthetic.png", "content_type": "image/png", "size": 0, "sha256": digest},
		"path name":      {"filename": "../synthetic.png", "content_type": "image/png", "size": 10, "sha256": digest},
		"short digest":   {"filename": "synthetic.png", "content_type": "image/png", "size": 10, "sha256": "abc"},
		"unknown field":  {"filename": "synthetic.png", "content_type": "image/png", "size": 10, "sha256": digest, "revision_id": proposal.id},
		"blank filename": {"filename": "  ", "content_type": "image/png", "size": 10, "sha256": digest},
	} {
		code, out := f.call("PUT", f.evidencePath(proposal, "/"+uuid.NewString()), "hr", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out := f.evidence("reader", proposal)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["evidence"], "no refused file leaves an intent behind")
}

func TestBenefitEvidenceIsInvisibleOutsideBenefitsScope(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	file := f.attach(proposal)
	_, err := f.scan(proposal, file, 1, "stream: OK\x00")
	require.NoError(t, err)

	staffDocs := f.base + "/staff/" + f.employee + "/documents"
	listing := testutil.Do(f.r, "GET", staffDocs, "staff-only", "")
	require.Equal(t, 200, listing.Code, listing.Body.String())
	require.NotContains(t, listing.Body.String(), file, "HR listings never include benefits evidence")
	for _, route := range [][2]string{{"GET", "/" + file + "/content"}, {"PUT", "/" + file + "/revocation"}, {"PUT", "/" + file + "/retries/2"}} {
		res := testutil.Do(f.r, route[0], staffDocs+route[1], "staff-only", "")
		require.Equal(t, 404, res.Code, "%s %s: %s", route[0], route[1], res.Body.String())
	}
	require.Equal(t, "available", f.statusOf(proposal, file))

	for name, flags := range map[string]string{
		"enrollment":   `,set_config('app.workforce_enrollment','authorized',true)`,
		"payroll":      `,set_config('app.workforce_payroll','write',true),set_config('app.workforce_payroll_read','authorized',true)`,
		"no benefits":  ``,
		"benefit read": `,set_config('app.workforce_benefits','read',true)`,
	} {
		var count int64
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true)`+flags, f.hospital).Error; err != nil {
				return err
			}
			return tx.Table("workforce_documents").Where("id=?", file).Count(&count).Error
		}))
		if name == "benefit read" {
			require.EqualValues(t, 1, count, "the benefits read scope sees its own evidence")
		} else {
			require.Zero(t, count, "the %s scope cannot see benefits evidence", name)
		}
	}
}

func TestBenefitEvidenceDeniesOtherTenantsHospitalsStaffAndProposals(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	file := f.attach(proposal)
	sibling := f.propose(f.employee)
	other := f.staff("SYN-EV-02")

	code, out := f.evidence("foreign", proposal)
	require.Equal(t, 404, code, out)
	code, out = f.evidence("staff-only", proposal)
	require.Equal(t, 403, code, out)
	for name, path := range map[string]coverageProposal{
		"another staff member": {staff: other, enrollment: proposal.enrollment, id: proposal.id},
		"another enrollment":   {staff: proposal.staff, enrollment: sibling.enrollment, id: proposal.id},
	} {
		code, out = f.evidence("reader", path)
		require.Equal(t, 404, code, "%s: %v", name, out)
		code, out = f.register("hr", path, uuid.NewString())
		require.Equal(t, 404, code, "%s: %v", name, out)
	}
	code, out = f.register("foreign", proposal, uuid.NewString())
	require.Equal(t, 404, code, out)
	res := testutil.Do(f.r, "GET", f.other+"/staff/"+proposal.staff+"/benefit-enrollments/"+proposal.enrollment+"/proposals/"+proposal.id+"/evidence", "hr", "")
	require.Equal(t, 404, res.Code, res.Body.String())
	res = f.download("reader", sibling, file)
	require.Equal(t, 404, res.Code, "a file answers only under its own proposal: %s", res.Body.String())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, f.evidencePath(sibling, "/"+file+"/content"), bytes.NewReader(f.content))
	req.Header.Set("Authorization", "Bearer hr")
	req.Header.Set("Content-Type", "image/png")
	f.r.ServeHTTP(w, req)
	require.Equal(t, 404, w.Code, w.Body.String())
	code, out = f.call("PUT", f.evidencePath(sibling, "/"+file+"/retries/2"), "hr", nil)
	require.Equal(t, 404, code, out)
	res = f.download("reader", proposal, "not-a-uuid")
	require.Equal(t, 404, res.Code, res.Body.String())
	code, out = f.evidence("reader", coverageProposal{staff: proposal.staff, enrollment: proposal.enrollment, id: "not-a-uuid"})
	require.Equal(t, 404, code, out)
}

func TestBenefitEvidenceLimitAndDecisionSettlement(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	digest := hex.EncodeToString(make([]byte, 32))
	expired := uuid.NewString()
	require.NoError(t, f.benefitsWrite(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256,created_at,expires_at)
	 VALUES('`+testutil.TenantA+`','`+f.hospital+`','`+expired+`','`+f.employee+`','`+proposal.id+`','benefit_proposal',1,'benefit_eligibility_evidence','synthetic.png','image/png',1,'`+digest+`',now()-interval '1 hour',now()-interval '30 minutes')`))
	files := make([]string, 0, 10)
	for range 10 {
		files = append(files, f.attach(proposal))
	}
	code, out := f.register("hr", proposal, uuid.NewString())
	requireSalaryConflict(t, code, out, "evidence_limit", "", "", "")
	listed := []string{}
	for _, row := range f.ok("GET", f.evidencePath(proposal, ""), "reader", nil)["evidence"].([]any) {
		listed = append(listed, row.(map[string]any)["id"].(string))
	}
	require.ElementsMatch(t, files, listed, "an expired intent is neither counted nor listed")

	status, err := f.scan(proposal, files[0], 1, "stream: OK\x00")
	require.NoError(t, err)
	require.Equal(t, "available", status)
	status, err = f.scan(proposal, files[1], 1, "stream: ERROR\x00")
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	for _, file := range files[2:] {
		_, err := f.scan(proposal, file, 1, "stream: OK\x00")
		require.NoError(t, err)
	}

	code, out = f.decide(proposal, "reject", "Synthetic certificate unreadable")
	require.Equal(t, 200, code, out)
	require.Equal(t, "revoked", f.statusOf(proposal, files[0]), "rejection revokes what was readable")
	require.Equal(t, "scan_error", f.statusOf(proposal, files[1]))
	res := f.download("reader", proposal, files[0])
	require.Equal(t, 409, res.Code, res.Body.String())
	f.conflict("not_pending", "PUT", f.evidencePath(proposal, "/"+files[1]+"/retries/2"), "hr", nil)
	require.Equal(t, 9, f.revocations(), "one revocation per readable file")

	withdrawn := f.propose(f.employee)
	pending := f.attach(withdrawn)
	intent := uuid.NewString()
	code, out = f.register("hr", withdrawn, intent)
	require.Equal(t, 201, code, out)
	code, out = f.withdraw(withdrawn)
	requireSalaryConflict(t, code, out, "evidence_scanning", "", "", "")
	_, err = f.scan(withdrawn, pending, 1, "stream: OK\x00")
	require.NoError(t, err)
	code, out = f.withdraw(withdrawn)
	require.Equal(t, 200, code, out)
	require.Equal(t, "revoked", f.statusOf(withdrawn, pending))
	require.Equal(t, 10, f.revocations())
	w := f.upload(withdrawn, intent)
	require.Equal(t, 409, w.Code, "an intent never receives bytes once its proposal is decided: %s", w.Body.String())
}

func TestBenefitEvidenceDatabaseGuards(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	file := f.attach(proposal)
	failed := f.attach(proposal)
	_, err := f.scan(proposal, failed, 1, "stream: ERROR\x00")
	require.Error(t, err)
	other := f.staff("SYN-EV-02")
	decided := f.propose(f.employee)
	_, out := f.withdraw(decided)
	require.Equal(t, "withdrawn", out["status"], out)
	digest := hex.EncodeToString(make([]byte, 32))
	insert := func(hospital, staff, record string, version int) string {
		return `INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256)
		 VALUES('` + testutil.TenantA + `','` + hospital + `','` + uuid.NewString() + `','` + staff + `','` + record + `','benefit_proposal',` + strconv.Itoa(version) + `,'benefit_eligibility_evidence','synthetic.png','image/png',1,'` + digest + `')`
	}
	requireGuard(t, f.benefitsWrite(insert(f.hospital, other, proposal.id, 1)), "binding is invalid", "another staff member's proposal")
	requireGuard(t, f.benefitsWrite(insert(f.hospital, f.employee, proposal.id, 2)), "binding is invalid", "a version the proposal would not approve")
	requireGuard(t, f.benefitsWrite(insert(f.hospital, f.employee, decided.id, 1)), "workforce_benefit_evidence_pending", "a decided proposal")
	requireGuard(t, f.benefitsWrite(insert(uuid.NewString(), f.employee, proposal.id, 1)), "binding is invalid", "another hospital's proposal")
	requireGuard(t, f.benefitsWrite(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256)
	 VALUES('`+testutil.TenantA+`','`+f.hospital+`','`+uuid.NewString()+`','`+f.employee+`','`+proposal.id+`','benefit_proposal',1,'tax_declaration_evidence','synthetic.png','image/png',1,'`+digest+`')`), "workforce_documents_record_binding", "a category of another record kind")

	reject := `UPDATE workforce_benefit_enrollment_proposals SET status='rejected',decided_by_subject='user-approver',decided_at=clock_timestamp(),decision_note='Synthetic' WHERE id='` + proposal.id + `'`
	requireGuard(t, f.benefitsWrite(reject), "workforce_benefit_evidence_scanning", "deciding while scanning")
	_, err = f.scan(proposal, file, 1, "stream: OK\x00")
	require.NoError(t, err)
	requireGuard(t, f.benefitsWrite(reject), "workforce_benefit_evidence_unsettled", "deciding with unfrozen evidence")
	freeze := `UPDATE workforce_documents SET frozen=true WHERE record_id='` + proposal.id + `'`
	requireGuard(t, f.benefitsWrite(freeze, reject), "workforce_benefit_evidence_unsettled", "rejecting with readable evidence")
	require.NoError(t, f.benefitsWrite(freeze), "freezing keeps a scan_error verdict without the processing identity")
	requireGuard(t, f.benefitsWrite(`UPDATE workforce_documents SET frozen=false WHERE id='`+file+`'`), "workforce_benefit_evidence_frozen", "unfreezing")
	requireGuard(t, f.benefitsWrite(`UPDATE workforce_documents SET status='scan_pending',scan_attempt=2,failure_code=NULL WHERE id='`+failed+`'`), "workforce_benefit_evidence_frozen", "rescanning frozen evidence")

	requireGuard(t, f.scoped(`,set_config('app.workforce_benefits','read',true)`, insert(f.hospital, f.employee, proposal.id, 1)), "row-level security", "the benefits read scope cannot write evidence")
	requireGuard(t, f.scoped(`,set_config('app.workforce_enrollment','authorized',true)`, insert(f.hospital, f.employee, proposal.id, 1)), "binding is invalid", "the enrollment scope cannot see the proposal, so cannot attach to it")
}

func TestFrozenPendingBenefitEvidenceCannotRestartAfterProcessingDeadline(t *testing.T) {
	f := newBenefitEvidenceFixture(t)
	proposal := f.propose(f.employee)
	file := f.attach(proposal)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_frozen_pending_document_clock", SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true),set_config('app.workforce_subject','user-hr',true),set_config('app.actor_subject','user-hr',true),set_config('app.workforce_hospital','` + f.hospital + `',true),set_config('app.workforce_benefits','write',true);
 ALTER TABLE workforce_documents DISABLE TRIGGER workforce_document_identity;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE workforce_documents SET frozen=true,updated_at=clock_timestamp()-interval '46 minutes' WHERE id='` + file + `';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'fixture must freeze exactly one file'; END IF;
 END $$;
 ALTER TABLE workforce_documents ENABLE TRIGGER workforce_document_identity;`}}))
	err := f.benefitsWrite(`UPDATE workforce_documents SET status='scan_pending',scan_attempt=2,updated_at=clock_timestamp() WHERE id='` + file + `'`)
	require.Error(t, err, "even an owner-scoped database connection cannot restart frozen pending evidence")
	code, out := f.call("PUT", f.evidencePath(proposal, "/"+file+"/retries/2"), "hr", nil)
	require.Equal(t, 409, code, out)
}
