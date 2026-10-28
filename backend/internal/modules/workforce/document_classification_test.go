package workforce_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestStaffDocumentClassificationPreservesEvidencePurposeAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("classification metadata must not call file storage")
		w.WriteHeader(500)
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	files, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB, "directory": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":  {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermMasterManage},
			"reader": {workforce.PermDocumentManage}, "foreign": {workforce.PermDocumentManage},
		}, Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	hospital, staff, file := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Classification Hospital"}`).Code)
	for _, grant := range []struct{ subject, level string }{{"admin", "manager"}, {"reader", "reader"}, {"directory", "manager"}} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+grant.subject, "admin", `{"level":"`+grant.level+`"}`).Code)
	}
	staffPath := base + "/staff/" + staff
	require.Equal(t, 200, testutil.Do(r, "PUT", staffPath, "admin", `{"employee_code":"SYN-CLASS-001","display_name":"Synthetic Classification Staff","job_category":"other","employment_status":"active"}`).Code)
	filePath := staffPath + "/documents/" + file
	intent := `{"record_kind":"enrollment","record_id":"` + staff + `","category":"identity_evidence","filename":"synthetic-identity.png","content_type":"image/png","size":256,"sha256":"` + strings.Repeat("a", 64) + `"}`
	w := testutil.Do(r, "PUT", filePath, "admin", intent)
	require.Equal(t, 201, w.Code, w.Body.String())
	originalIntent := w.Body.String()
	path := filePath + "/classification"
	w = testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var current struct {
		FileID     string `json:"file_id"`
		HospitalID string `json:"hospital_id"`
		StaffID    string `json:"staff_id"`
		Version    int64  `json:"version"`
		Reference  string `json:"classification_revision_id"`
		Name       string `json:"classification_name"`
		UpdatedAt  string `json:"updated_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	require.Zero(t, current.Version)
	require.Empty(t, current.Reference)
	masterPath := base + "/hr/masters/" + uuid.NewString()
	masterBody := `{"expected_version":0,"kind":"document_category","code":"SYN-IDENTITY","name":"Synthetic reviewed identity category","description":"Synthetic only","active":true}`
	w = testutil.Do(r, "PUT", masterPath, "admin", masterBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	var master struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
	require.NotEmpty(t, master.RevisionID)
	assignment := `{"expected_version":0,"classification_revision_id":"` + master.RevisionID + `"}`
	w = testutil.Do(r, "PUT", path, "admin", assignment)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	require.EqualValues(t, 1, current.Version)
	require.Equal(t, file, current.FileID)
	require.Equal(t, hospital, current.HospitalID)
	require.Equal(t, staff, current.StaffID)
	require.Equal(t, master.RevisionID, current.Reference)
	require.Equal(t, "Synthetic reviewed identity category", current.Name)
	require.True(t, strings.HasSuffix(current.UpdatedAt, "Z"), "wire timestamps use UTC even when the API process uses another timezone")
	replay := testutil.Do(r, "PUT", path, "admin", assignment)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", `{"expected_version":0,"classification_revision_id":""}`).Code)
	for _, body := range []string{strings.Repeat(" ", 1100) + `{"expected_version":1,"classification_revision_id":""}`, `{"expected_version":1,"classification_revision_id":null}`, `{"expected_version":1}`, `{"expected_version":1,"classification_revision_id":"invalid"}`, `{"expected_version":1,"classification_revision_id":"","category":"employment_evidence"}`} {
		invalid := testutil.Do(r, "PUT", path, "admin", body)
		require.Equal(t, 400, invalid.Code, invalid.Body.String())
	}
	for _, actor := range []struct {
		token string
		code  int
	}{{"reader", 404}, {"foreign", 404}, {"directory", 403}, {"", 401}} {
		for _, method := range []string{"GET", "PUT"} {
			denied := testutil.Do(r, method, path, actor.token, assignment)
			require.Equal(t, actor.code, denied.Code, denied.Body.String())
			require.NotContains(t, denied.Body.String(), "Synthetic reviewed identity")
		}
	}
	for _, deniedPath := range []string{
		strings.Replace(path, staff, uuid.NewString(), 1), strings.Replace(path, hospital, uuid.NewString(), 1), strings.Replace(path, file, uuid.NewString(), 1),
	} {
		require.Equal(t, 404, testutil.Do(r, "GET", deniedPath, "admin", "").Code)
	}
	retired := strings.Replace(masterBody, `"expected_version":0`, `"expected_version":1`, 1)
	retired = strings.Replace(retired, `"active":true`, `"active":false`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", masterPath, "admin", retired).Code)
	retained := testutil.Do(r, "PUT", path, "admin", strings.Replace(assignment, `"expected_version":0`, `"expected_version":1`, 1))
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.JSONEq(t, w.Body.String(), retained.Body.String())
	cleared := testutil.Do(r, "PUT", path, "admin", `{"expected_version":1,"classification_revision_id":""}`)
	require.Equal(t, 200, cleared.Code, cleared.Body.String())
	require.NoError(t, json.Unmarshal(cleared.Body.Bytes(), &current))
	require.EqualValues(t, 2, current.Version)
	require.Empty(t, current.Reference)
	require.Empty(t, current.Name)
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "admin", strings.Replace(assignment, `"expected_version":0`, `"expected_version":2`, 1)).Code, "retired revision is not a new assignment")
	history := testutil.Do(r, "GET", path+"/history?limit=1", "admin", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), `"version":2`)
	require.NotContains(t, history.Body.String(), `"version":1`)
	var firstPage struct {
		Data []struct {
			Version   int64  `json:"version"`
			CreatedAt string `json:"created_at"`
		}
		Page struct {
			HasMore bool   `json:"has_more"`
			Cursor  string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &firstPage))
	require.True(t, firstPage.Page.HasMore)
	require.NotEmpty(t, firstPage.Page.Cursor)
	require.True(t, strings.HasSuffix(firstPage.Data[0].CreatedAt, "Z"))
	nextHistory := testutil.Do(r, "GET", path+"/history?limit=1&cursor="+url.QueryEscape(firstPage.Page.Cursor), "admin", "")
	require.Equal(t, 200, nextHistory.Code, nextHistory.Body.String())
	require.NoError(t, json.Unmarshal(nextHistory.Body.Bytes(), &firstPage))
	require.EqualValues(t, 1, firstPage.Data[0].Version)
	require.False(t, firstPage.Page.HasMore)

	activeBody := strings.Replace(strings.Replace(masterBody, "SYN-IDENTITY", "SYN-OTHER", 1), "Synthetic reviewed identity category", "Synthetic second identity category", 1)
	activePath := base + "/hr/masters/" + uuid.NewString()
	active := testutil.Do(r, "PUT", activePath, "admin", activeBody)
	require.Equal(t, 200, active.Code, active.Body.String())
	var nextMaster struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(active.Body.Bytes(), &nextMaster))
	concurrentBody := `{"expected_version":2,"classification_revision_id":"` + nextMaster.RevisionID + `"}`
	responses := make(chan *httptest.ResponseRecorder, 8)
	for range 8 {
		go func() { responses <- testutil.Do(r, "PUT", path, "admin", concurrentBody) }()
	}
	for range 8 {
		saved := <-responses
		require.Equal(t, 200, saved.Code, saved.Body.String())
		var value struct {
			Version int64 `json:"version"`
		}
		require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &value))
		require.EqualValues(t, 3, value.Version)
	}
	wrongKind := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), "admin", strings.Replace(strings.Replace(activeBody, "SYN-OTHER", "SYN-WRONG", 1), `"kind":"document_category"`, `"kind":"language"`, 1))
	require.Equal(t, 200, wrongKind.Code, wrongKind.Body.String())
	require.NoError(t, json.Unmarshal(wrongKind.Body.Bytes(), &nextMaster))
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "admin", `{"expected_version":3,"classification_revision_id":"`+nextMaster.RevisionID+`"}`).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("workforce_document_classification_revisions").Count(&count).Error)
		require.Zero(t, count, "a tenant connection without the protected enrollment context cannot read classification history")
		require.NoError(t, tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error)
		require.NoError(t, tx.Table("workforce_document_classification_revisions").Where("tenant_id=? AND hospital_id=? AND file_id=?", testutil.TenantA, hospital, file).Count(&count).Error)
		require.EqualValues(t, 3, count, "concurrent retries create one immutable revision")
		var payloads []string
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", "helivanta.in.workforce.document_classification_changed.v1").Pluck("payload::text", &payloads).Error)
		require.Len(t, payloads, 3, "concurrent retries publish one change")
		for _, payload := range payloads {
			for _, private := range []string{"Synthetic", "identity_evidence", "classification_revision_id", "filename"} {
				require.NotContains(t, payload, private)
			}
		}
		return nil
	}))

	otherHospital := uuid.NewString()
	otherBase := "/v1/workforce/hospitals/" + otherHospital
	require.Equal(t, 200, testutil.Do(r, "PUT", otherBase, "admin", `{"name":"Synthetic second classification hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", otherBase+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	foreignMaster := testutil.Do(r, "PUT", otherBase+"/hr/masters/"+uuid.NewString(), "admin", activeBody)
	require.Equal(t, 200, foreignMaster.Code, foreignMaster.Body.String())
	require.NoError(t, json.Unmarshal(foreignMaster.Body.Bytes(), &nextMaster))
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "admin", `{"expected_version":3,"classification_revision_id":"`+nextMaster.RevisionID+`"}`).Code, "a manager of both hospitals cannot assign a foreign master")
	newFile := uuid.NewString()
	require.Equal(t, 201, testutil.Do(r, "PUT", staffPath+"/documents/"+newFile, "admin", intent).Code)
	invalidFirst := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error; err != nil {
			return err
		}
		revision := uuid.NewString()
		if err := tx.Exec(`INSERT INTO workforce_document_classification_revisions(tenant_id,hospital_id,file_id,id,version,actor_subject) VALUES(?,?,?,?,2,'user-admin')`, testutil.TenantA, hospital, newFile, revision).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_document_classifications(tenant_id,hospital_id,id,version,revision_id) VALUES(?,?,?,2,?)`, testutil.TenantA, hospital, newFile, revision).Error
	})
	require.Error(t, invalidFirst, "the first classification cannot skip version one")
	for _, statement := range []string{
		`UPDATE workforce_document_classification_revisions SET classification_revision_id=NULL WHERE tenant_id=? AND hospital_id=?`,
		`DELETE FROM workforce_document_classification_revisions WHERE tenant_id=? AND hospital_id=?`,
		`UPDATE workforce_document_classifications SET version=version+2 WHERE tenant_id=? AND hospital_id=?`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement, testutil.TenantA, hospital).Error
		})
		require.Error(t, err, "direct database writes cannot rewrite classification history or skip current versions")
	}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_workforce_classification_outbox_failure", SQL: `CREATE FUNCTION classification_test_fail_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.workforce.document_classification_changed.v1' THEN RAISE EXCEPTION 'synthetic publication failure' USING ERRCODE='57014'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER classification_test_fail_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION classification_test_fail_outbox();`}}))
	failedCommand := `{"expected_version":3,"classification_revision_id":""}`
	failed := testutil.Do(r, "PUT", path, "admin", failedCommand)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	afterFailure := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, afterFailure.Code, afterFailure.Body.String())
	require.NoError(t, json.Unmarshal(afterFailure.Body.Bytes(), &current))
	require.EqualValues(t, 3, current.Version)
	require.NotEmpty(t, current.Reference)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error)
		for _, query := range []string{`SELECT count(*) FROM workforce_document_classification_revisions WHERE tenant_id=? AND hospital_id=? AND file_id=?`, `SELECT count(*) FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.document_classification.changed'`} {
			var count int64
			require.NoError(t, tx.Raw(query, testutil.TenantA, hospital, file).Scan(&count).Error)
			require.EqualValues(t, 3, count, "history and attributed writes roll back with failed outbox publication")
		}
		return nil
	}))
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_workforce_classification_outbox_recovery", SQL: `DROP TRIGGER classification_test_fail_outbox ON outbox_events; DROP FUNCTION classification_test_fail_outbox();`}}))
	successfulRetry := testutil.Do(r, "PUT", path, "admin", failedCommand)
	require.Equal(t, 200, successfulRetry.Code, successfulRetry.Body.String())
	require.NoError(t, json.Unmarshal(successfulRetry.Body.Bytes(), &current))
	require.EqualValues(t, 4, current.Version)
	require.Empty(t, current.Reference)
	evidence := testutil.Do(r, "PUT", filePath, "admin", intent)
	require.Equal(t, 200, evidence.Code, evidence.Body.String())
	require.JSONEq(t, originalIntent, evidence.Body.String(), "classification cannot alter the existing strict file response or evidence purpose")
	require.Equal(t, 409, testutil.Do(r, "GET", filePath+"/content", "admin", "").Code, "classification does not release unscanned content")
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "admin", `{"level":"manager"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "reader", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "admin", `{"level":"reader"}`).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "reader", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/history", "reader", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "reader", `{"expected_version":4,"classification_revision_id":""}`).Code, "even a no-op requires current hospital manager authority")

}

func TestStaffDocumentClassificationDoesNotCrossEvidencePurposeOwners(t *testing.T) {
	t.Run("benefit", func(t *testing.T) {
		f := newBenefitEvidenceFixture(t)
		proposal := f.propose(f.employee)
		file := uuid.NewString()
		code, out := f.register("hr", proposal, file)
		require.Equal(t, 201, code, out)
		ordinary := uuid.NewString()
		ordinaryPath := f.base + "/staff/" + f.employee + "/documents/" + ordinary
		intent := `{"record_kind":"enrollment","record_id":"` + f.employee + `","category":"employment_evidence","filename":"synthetic-owner.png","content_type":"image/png","size":256,"sha256":"` + strings.Repeat("a", 64) + `"}`
		require.Equal(t, 201, testutil.Do(f.r, "PUT", ordinaryPath, "staff-only", intent).Code)
		require.Equal(t, 200, testutil.Do(f.r, "GET", ordinaryPath+"/classification", "staff-only", "").Code)
		path := f.base + "/staff/" + f.employee + "/documents/" + file + "/classification"
		for _, action := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/history", ""}, {"PUT", "", `{"expected_version":0,"classification_revision_id":""}`}} {
			denied := testutil.Do(f.r, action.method, path+action.suffix, "staff-only", action.body)
			require.Equal(t, 404, denied.Code, denied.Body.String())
		}
	})
	t.Run("tax", func(t *testing.T) {
		f := newEvidenceFixture(t)
		proposal := f.propose(f.staff, 0)
		file := uuid.NewString()
		code, out := f.register(f.staff, "admin", proposal, file)
		require.Equal(t, 201, code, out)
		ordinary := uuid.NewString()
		ordinaryPath := f.base + "/staff/" + f.staff + "/documents/" + ordinary
		intent := `{"record_kind":"enrollment","record_id":"` + f.staff + `","category":"employment_evidence","filename":"synthetic-owner.png","content_type":"image/png","size":256,"sha256":"` + strings.Repeat("a", 64) + `"}`
		require.Equal(t, 201, testutil.Do(f.router, "PUT", ordinaryPath, "admin", intent).Code)
		require.Equal(t, 200, testutil.Do(f.router, "GET", ordinaryPath+"/classification", "admin", "").Code)
		path := f.base + "/staff/" + f.staff + "/documents/" + file + "/classification"
		for _, action := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/history", ""}, {"PUT", "", `{"expected_version":0,"classification_revision_id":""}`}} {
			denied := testutil.Do(f.router, action.method, path+action.suffix, "admin", action.body)
			require.Equal(t, 404, denied.Code, denied.Body.String())
		}
	})
}
