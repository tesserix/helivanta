package workforce_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"google.golang.org/api/option"
	"gorm.io/gorm"
)

func TestStaffDocumentIntentsAreScopedPrivateAndIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("intent metadata must not call GCS while holding authorization locks")
		w.WriteHeader(500)
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	files, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB, "directory": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"manager": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}, "reader": {workforce.PermEnrollmentManage}, "foreign": {workforce.PermEnrollmentManage}},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	hospital, staff, file := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "manager", `{"name":"Synthetic File Hospital"}`).Code)
	for _, level := range []struct{ subject, level string }{{"manager", "manager"}, {"reader", "reader"}, {"directory", "manager"}} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+level.subject, "manager", `{"level":"`+level.level+`"}`).Code)
	}
	staffPath := base + "/staff/" + staff
	require.Equal(t, 200, testutil.Do(r, "PUT", staffPath, "manager", `{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"}`).Code)
	body := `{"record_kind":"enrollment","record_id":"` + staff + `","category":"employment_evidence","filename":"synthetic-evidence.png","content_type":"image/png","size":256,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	path := staffPath + "/documents/" + file
	w := testutil.Do(r, "PUT", path, "manager", body)
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var saved map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, "upload_pending", saved["status"])
	require.Equal(t, "synthetic-evidence.png", saved["filename"])
	require.Equal(t, staff, saved["record_id"])
	for range 3 {
		repeated := testutil.Do(r, "PUT", path, "manager", body)
		require.Equal(t, 200, repeated.Code, repeated.Body.String())
		require.JSONEq(t, w.Body.String(), repeated.Body.String())
	}
	for _, actor := range []struct {
		token  string
		status int
	}{{"reader", 404}, {"foreign", 404}, {"directory", 403}, {"", 401}} {
		for _, method := range []string{"GET", "PUT"} {
			requestPath := path
			if method == "GET" {
				requestPath = staffPath + "/documents"
			}
			denied := testutil.Do(r, method, requestPath, actor.token, body)
			require.Equal(t, actor.status, denied.Code, denied.Body.String())
			require.NotContains(t, denied.Body.String(), "synthetic-evidence.png")
		}
	}
	listing := testutil.Do(r, "GET", staffPath+"/documents", "manager", "")
	require.Equal(t, 200, listing.Code, listing.Body.String())
	require.Contains(t, listing.Body.String(), "synthetic-evidence.png")
	for _, route := range []string{staffPath, base + "/staff"} {
		directory := testutil.Do(r, "GET", route, "manager", "")
		require.NotContains(t, directory.Body.String(), "synthetic-evidence.png")
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var rows int64
		require.NoError(t, tx.Table("workforce_documents").Count(&rows).Error)
		require.Zero(t, rows, "plain tenant scope must not expose confidential file metadata")
		return nil
	}))
	qualificationID := uuid.NewString()
	enrollment := `{"expected_version":1,"staff":{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"},"profile":{"engagement_type":"employee"},"qualifications":[{"id":"` + qualificationID + `","qualification":"Synthetic certificate","institution":"Synthetic school","completion_year":2020}]}`
	enrolled := testutil.Do(r, "PUT", staffPath+"/enrollment", "manager", enrollment)
	require.Equal(t, 200, enrolled.Code, enrolled.Body.String())
	qualifiedBody := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body, `"enrollment"`, `"qualification"`), staff, qualificationID), `"employment_evidence"`, `"qualification_evidence"`)
	qualifiedPath := staffPath + "/documents/" + uuid.NewString()
	qualified := testutil.Do(r, "PUT", qualifiedPath, "manager", qualifiedBody)
	require.Equal(t, 201, qualified.Code, qualified.Body.String())
	removed := testutil.Do(r, "PUT", staffPath+"/enrollment", "manager", `{"expected_version":2,"staff":{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"},"profile":{"engagement_type":"employee"},"qualifications":[]}`)
	require.Equal(t, 200, removed.Code, removed.Body.String())
	replay := testutil.Do(r, "PUT", qualifiedPath, "manager", qualifiedBody)
	require.Equal(t, 200, replay.Code, "historical record removal must not change a completed intent's retry receipt: "+replay.Body.String())
	require.JSONEq(t, qualified.Body.String(), replay.Body.String())
	forged := db.WithTenant(tenantdb.WithActor(t.Context(), "user-manager"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256,status,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at)
		 SELECT tenant_id,hospital_id,?::uuid,staff_id,record_id,record_kind,3,category,filename,content_type,size,sha256,'available',clock_timestamp(),1,2,'1.5.1',28000,clock_timestamp(),clock_timestamp()
		 FROM workforce_documents WHERE id=?`, uuid.NewString(), file).Error
	})
	require.Error(t, forged, "even an authorized application transaction cannot insert a fabricated release")
	require.Equal(t, 204, testutil.Do(r, "DELETE", base+"/access/user-manager", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", staffPath+"/documents", "manager", "").Code)
}
