package workforce_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func employmentEvidencePeriod(t *testing.T, s documentScenario) string {
	t.Helper()
	staffPath := "/v1/workforce/hospitals/" + s.input.HospitalID + "/staff/" + s.input.StaffID
	source := testutil.Do(s.router, "GET", staffPath+"/employment-source", "manager", "")
	require.Equal(t, 200, source.Code, source.Body.String())
	var basis map[string]any
	require.NoError(t, json.Unmarshal(source.Body.Bytes(), &basis))
	period := staffPath + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	body := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": basis["staff_version"], "employment_epoch": basis["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-11-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic appointment", "evidence_reference": "SYN-APPOINTMENT"}
	response := testutil.Do(s.router, "PUT", period, "manager", string(mustJSON(t, body)))
	require.Equal(t, 200, response.Code, response.Body.String())
	return period + "/revisions/1/evidence"
}
func releaseEmploymentEvidence(t *testing.T, s documentScenario) {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	state, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", state)
}
func TestEmploymentEvidencePinsReleasedFileAndKeepsWithdrawalHistory(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic contract evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	response := testutil.Do(s.router, "PUT", path, "manager", body)
	require.Equal(t, 409, response.Code, response.Body.String(), "unscanned files cannot become employment evidence")
	releaseEmploymentEvidence(t, s)
	response = testutil.Do(s.router, "PUT", path, "manager", body)
	require.Equal(t, 200, response.Code, response.Body.String())
	var first map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &first))
	require.Equal(t, "attached", first["status"])
	require.Equal(t, float64(1), first["version"])
	require.Equal(t, s.input.FileID, first["document_id"])
	require.Equal(t, "synthetic-evidence.png", first["filename"])
	require.NotEmpty(t, first["released_generation"])
	require.NotEmpty(t, first["source_generation"])
	require.NotContains(t, first, "object_key")
	require.NotContains(t, first, "bucket")
	replay := testutil.Do(s.router, "PUT", path, "manager", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, response.Body.String(), replay.Body.String())
	withdrawal := fmt.Sprintf(`{"command_id":%q,"expected_version":1,"reason":"Synthetic withdrawal"}`, uuid.Must(uuid.NewV7()).String())
	withdrawn := testutil.Do(s.router, "POST", path+"/withdraw", "manager", withdrawal)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	var last map[string]any
	require.NoError(t, json.Unmarshal(withdrawn.Body.Bytes(), &last))
	require.Equal(t, "withdrawn", last["status"])
	require.Equal(t, float64(2), last["version"])
	replay = testutil.Do(s.router, "PUT", path, "manager", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, response.Body.String(), replay.Body.String(), "receipt replay must not reattach withdrawn evidence")
	current := testutil.Do(s.router, "GET", path, "manager", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, withdrawn.Body.String(), current.Body.String())
	history := testutil.Do(s.router, "GET", path+"/history", "manager", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var page map[string]any
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page["data"], 2)
	content := testutil.Do(s.router, "GET", path+"/content", "manager", "")
	require.NotEqual(t, 200, content.Code)
	require.NotEqual(t, s.content, content.Body.Bytes())
}

func TestEmploymentEvidenceRequiresBothPermissionsAndExactScope(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "manager", body).Code)
	for _, token := range []string{"enrollment-only", "documents-only", "foreign"} {
		for _, endpoint := range []string{root, path, path + "/history", path + "/content"} {
			response := testutil.Do(s.router, "GET", endpoint, token, "")
			require.Equal(t, 403, response.Code, "%s %s: %s", token, endpoint, response.Body.String())
		}
		require.Equal(t, 403, testutil.Do(s.router, "PUT", path, token, body).Code)
	}
	for _, endpoint := range []string{root, path, path + "/history", path + "/content"} {
		response := testutil.Do(s.router, "GET", endpoint, "foreign-hr", "")
		require.Equal(t, 404, response.Code, response.Body.String())
	}
	wrongStaff := strings.Replace(path, s.input.StaffID, uuid.NewString(), 1)
	require.Equal(t, 404, testutil.Do(s.router, "GET", wrongStaff, "manager", "").Code)
	wrongHospital := strings.Replace(path, s.input.HospitalID, uuid.NewString(), 1)
	require.Equal(t, 404, testutil.Do(s.router, "GET", wrongHospital, "manager", "").Code)
}

func TestEmploymentEvidenceDownloadRechecksWithdrawalAfterStorageRead(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "manager", body).Code)
	content := testutil.Do(s.router, "GET", path+"/content", "manager", "")
	require.Equal(t, 200, content.Code, content.Body.String())
	require.Equal(t, s.content, content.Body.Bytes())
	require.Contains(t, content.Header().Get("Cache-Control"), "no-store")
	s.cloud.AfterReleasedRead(func() {
		withdrawal := fmt.Sprintf(`{"command_id":%q,"expected_version":1,"reason":"Synthetic withdrawal during download"}`, uuid.Must(uuid.NewV7()).String())
		response := testutil.Do(s.router, "POST", path+"/withdraw", "manager", withdrawal)
		require.Equal(t, 200, response.Code, response.Body.String())
	})
	content = testutil.Do(s.router, "GET", path+"/content", "manager", "")
	require.Equal(t, 404, content.Code, content.Body.String())
	require.NotEqual(t, s.content, content.Body.Bytes())
}

func TestEmploymentEvidenceRejectsDuplicateFilesAndConflictingReceipts(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	command := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, command, s.input.FileID)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "manager", body).Code)
	duplicate := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Duplicate evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	response := testutil.Do(s.router, "PUT", root+"/"+uuid.Must(uuid.NewV7()).String(), "manager", duplicate)
	require.Equal(t, 409, response.Code, response.Body.String())
	response = testutil.Do(s.router, "PUT", path, "manager", strings.Replace(body, "Synthetic evidence", "Changed reason", 1))
	require.Equal(t, 409, response.Code, response.Body.String())
	forged := strings.TrimSuffix(duplicate, "}") + `,"released_generation":"1"}`
	response = testutil.Do(s.router, "PUT", root+"/"+uuid.Must(uuid.NewV7()).String(), "manager", forged)
	require.Equal(t, 400, response.Code, response.Body.String())
	withdrawal := fmt.Sprintf(`{"command_id":%q,"expected_version":2,"reason":"Stale withdrawal"}`, uuid.Must(uuid.NewV7()).String())
	response = testutil.Do(s.router, "POST", path+"/withdraw", "manager", withdrawal)
	require.Equal(t, 409, response.Code, response.Body.String())
	current := testutil.Do(s.router, "GET", path, "manager", "")
	require.Contains(t, current.Body.String(), `"status":"attached"`)
}

func TestEmploymentEvidenceHistoryRequiresPrivateScopeAndRejectsTampering(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "manager", body).Code)
	for _, table := range []string{"workforce_employment_evidence_bindings", "workforce_employment_evidence_history"} {
		for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
			require.NoError(t, s.db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
				var count int64
				err := tx.Table(table).Count(&count).Error
				require.Zero(t, count, "private evidence must not be exposed by tenant scope alone")
				return err
			}))
		}
		scoped := func(sql string) error {
			return s.db.WithTenantCommand(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_employment_evidence','write',true)`, s.input.HospitalID).Error; err != nil {
					return err
				}
				return tx.Exec(sql).Error
			})
		}
		require.Error(t, scoped("TRUNCATE "+table+" CASCADE"))
		require.Error(t, scoped("UPDATE "+table+" SET reason='tampered'"))
	}
	current := testutil.Do(s.router, "GET", path, "manager", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"reason":"Synthetic evidence"`)
}

func TestEmploymentEvidenceRejectsStaleTermsAndStaffSource(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	period := strings.TrimSuffix(root, "/revisions/1/evidence")
	current := testutil.Do(s.router, "GET", period, "manager", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	var terms map[string]any
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &terms))
	terms["command_id"] = uuid.Must(uuid.NewV7()).String()
	terms["expected_version"] = 1
	input := map[string]any{}
	for _, key := range []string{"command_id", "expected_version", "staff_version", "employment_epoch", "engagement_kind", "duration", "starts_at", "contracting_party", "sponsor_id", "reason", "evidence_reference"} {
		input[key] = terms[key]
	}
	response := testutil.Do(s.router, "PUT", period, "manager", string(mustJSON(t, input)))
	require.Equal(t, 200, response.Code, response.Body.String())
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	response = testutil.Do(s.router, "PUT", root+"/"+uuid.Must(uuid.NewV7()).String(), "manager", body)
	require.Equal(t, 409, response.Code, response.Body.String())
	staffPath := "/v1/workforce/hospitals/" + s.input.HospitalID + "/staff/" + s.input.StaffID
	response = testutil.Do(s.router, "PUT", staffPath, "manager", `{"employee_code":"DOCUMENT-001","display_name":"Synthetic Document Staff Updated","job_category":"nurse","employment_status":"active"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(s.router, "PUT", strings.Replace(root, "/revisions/1/", "/revisions/2/", 1)+"/"+uuid.Must(uuid.NewV7()).String(), "manager", body)
	require.Equal(t, 409, response.Code, response.Body.String())
}

func TestEmploymentEvidenceRevokedFileCannotBeDisclosedOrAttached(t *testing.T) {
	s := newDocumentScenario(t)
	root := employmentEvidencePeriod(t, s)
	releaseEmploymentEvidence(t, s)
	path := root + "/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic evidence"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "manager", body).Code)
	response := testutil.Do(s.router, "PUT", s.path+"/revocation", "manager", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(s.router, "GET", path+"/content", "manager", "")
	require.NotEqual(t, 200, response.Code)
	require.NotEqual(t, s.content, response.Body.Bytes())
	body = fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic revoked file"}`, uuid.Must(uuid.NewV7()).String(), s.input.FileID)
	response = testutil.Do(s.router, "PUT", root+"/"+uuid.Must(uuid.NewV7()).String(), "manager", body)
	require.Equal(t, 409, response.Code, response.Body.String())
	current := testutil.Do(s.router, "GET", path, "manager", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), s.input.FileID, "immutable metadata remains available to current authorized HR")
}
