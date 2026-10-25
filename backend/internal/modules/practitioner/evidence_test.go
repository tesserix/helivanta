package practitioner_test

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestRegistrationEvidenceIntentBindsExactRevisionAndRetainsPrivateUnverifiedState(t *testing.T) {
	perms := []authz.Permission{practitioner.PermRecordManage, practitioner.PermRecordRead, practitioner.PermEvidenceManage, practitioner.PermEvidenceRead}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA, "foreign": testutil.TenantB, "peer": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": perms, "foreign": perms, "peer": {practitioner.PermRecordRead, practitioner.PermRecordManage}}, Modules: []platform.Module{practitioner.New()}})
	owner := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", owner, "steward", `{"given_name":"Evidence","professional_type":"physician"}`).Code)
	registration := uuid.Must(uuid.NewV7()).String()
	registrationPath := owner + "/registrations/" + registration
	require.Equal(t, 201, testutil.Do(r, "PUT", registrationPath, "steward", `{"council_system":"synthetic.council","registration_number":"QA-1"}`).Code)
	path := registrationPath + "/revisions/" + registration + "/evidence/" + uuid.Must(uuid.NewV7()).String()
	body := `{"content_type":"image/png","size":123,"sha256":"` + strings.Repeat("a", 64) + `"}`
	created := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var saved map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &saved))
	bad, err := db.LintRLS(t.Context())
	require.NoError(t, err)
	require.Empty(t, bad)
	require.Equal(t, registration, saved["revision_id"])
	require.Equal(t, "upload_pending", saved["status"])
	createdAt, err := time.Parse(time.RFC3339Nano, saved["created_at"].(string))
	require.NoError(t, err)
	expiresAt, err := time.Parse(time.RFC3339Nano, saved["expires_at"].(string))
	require.NoError(t, err)
	require.Equal(t, time.Hour, expiresAt.Sub(createdAt))
	for _, key := range []string{"tenant_id", "created_by", "object_key", "source_generation", "released_generation", "verified_at"} {
		require.NotContains(t, saved, key)
	}
	replay := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	get := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, get.Code, get.Body.String())
	require.Equal(t, "no-store", get.Header().Get("Cache-Control"))
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "steward", strings.Replace(body, `"size":123`, `"size":124`, 1)).Code)
	require.Equal(t, 403, testutil.Do(r, "GET", path, "peer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	require.Equal(t, 401, testutil.Do(r, "GET", path, "unknown", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("practitioner_registration_evidence").Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n, "tenant context alone must not disclose evidence")
		return nil
	}))
}

func evidenceFixture(t *testing.T) (*gin.Engine, *tenantdb.DB, string, string) {
	t.Helper()
	return evidenceFixtureWithFiles(t, nil)
}
func evidenceFixtureWithFiles(t *testing.T, files *attachments.GCS) (*gin.Engine, *tenantdb.DB, string, string) {
	t.Helper()
	return evidenceFixtureWithAccess(t, files, nil)
}
func evidenceFixtureWithAccess(t *testing.T, files *attachments.GCS, membership authz.MembershipChecker) (*gin.Engine, *tenantdb.DB, string, string) {
	t.Helper()
	perms := []authz.Permission{practitioner.PermRecordManage, practitioner.PermRecordRead, practitioner.PermEvidenceManage, practitioner.PermEvidenceRead}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Documents: files, Membership: membership, Tokens: map[string]string{"steward": testutil.TenantA, "foreign": testutil.TenantB, "evidence-only": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": perms, "foreign": perms, "evidence-only": {practitioner.PermEvidenceManage, practitioner.PermEvidenceRead}}, Modules: []platform.Module{practitioner.New()}})
	owner := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", owner, "steward", `{"given_name":"Evidence","professional_type":"nurse"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := owner + "/registrations/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", `{"council_system":"synthetic.council","registration_number":"QA-1"}`).Code)
	return r, db, path + "/revisions/" + id + "/evidence", path
}

func TestEvidenceIntakeRejectsUntrustedMetadataAndWrongRevisionWithoutPersisting(t *testing.T) {
	r, db, collection, _ := evidenceFixture(t)
	body := `{"content_type":"image/png","size":123,"sha256":"` + strings.Repeat("a", 64) + `"}`
	for _, input := range []string{`null`, `[]`, `{}`, body + `{}`, strings.Replace(body, "image/png", "application/pdf", 1), strings.Replace(body, `"size":123`, `"size":0`, 1), strings.Replace(body, `"size":123`, `"size":10485761`, 1), strings.Replace(body, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), strings.TrimSuffix(body, "}") + `,"status":"available"}`, strings.TrimSuffix(body, "}") + `,"tenant_id":"` + testutil.TenantB + `"}`, strings.TrimSuffix(body, "}") + `,"filename":"/private/file.png"}`} {
		got := testutil.Do(r, "PUT", collection+"/"+uuid.Must(uuid.NewV7()).String(), "steward", input)
		require.Equal(t, 400, got.Code, got.Body.String())
	}
	var wrong string
	// A real syntactically valid but unrelated revision is also not a parent.
	parts := strings.Split(collection, "/")
	parts[len(parts)-2] = uuid.Must(uuid.NewV7()).String()
	wrong = strings.Join(parts, "/")
	require.Equal(t, 404, testutil.Do(r, "PUT", wrong+"/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true)").Error)
		var n int64
		err := tx.Table("practitioner_registration_evidence").Count(&n).Error
		require.Zero(t, n)
		return err
	}))
}

func TestEvidenceConcurrentQuotaRetainsReplayAndBoundedAuditedCollection(t *testing.T) {
	r, db, collection, _ := evidenceFixture(t)
	body := `{"content_type":"image/jpeg","size":123,"sha256":"` + strings.Repeat("b", 64) + `"}`
	results := make(chan int, 20)
	paths := make(chan string, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			path := collection + "/" + uuid.Must(uuid.NewV7()).String()
			response := testutil.Do(r, "PUT", path, "steward", body)
			results <- response.Code
			if response.Code == 201 {
				paths <- path
			}
		})
	}
	wg.Wait()
	close(results)
	close(paths)
	successes := 0
	for code := range results {
		if code == 201 {
			successes++
		} else {
			require.Equal(t, 409, code)
		}
	}
	require.Equal(t, 10, successes)
	for path := range paths {
		require.Equal(t, 200, testutil.Do(r, "PUT", path, "steward", body).Code)
	}
	seen := map[string]bool{}
	next := ""
	for {
		path := collection + "?limit=3"
		if next != "" {
			path += "&cursor=" + next
		}
		response := testutil.Do(r, "GET", path, "steward", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		var page struct {
			Data []struct {
				ID string `json:"id"`
			}
			Page struct {
				NextCursor string `json:"next_cursor"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		require.LessOrEqual(t, len(page.Data), 3)
		for _, row := range page.Data {
			require.False(t, seen[row.ID])
			seen[row.ID] = true
		}
		next = page.Page.NextCursor
		if next == "" {
			break
		}
	}
	require.Len(t, seen, 10)
	require.Equal(t, 404, testutil.Do(r, "GET", collection, "foreign", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("audit_events").Where("resource_type='practitioner_registration_evidence' AND action='insert'").Count(&n).Error
		require.EqualValues(t, 10, n)
		return err
	}))
}

func TestEvidenceAuditFailureBlocksPrivateReadsReplayAndEmptyCollections(t *testing.T) {
	r, db, collection, registration := evidenceFixture(t)
	body := `{"content_type":"image/png","size":123,"sha256":"` + strings.Repeat("c", 64) + `"}`
	path := collection + "/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", body).Code)
	revision := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", registration+"/revisions/"+revision, "steward", `{"expected_version":1}`).Code)
	emptyCollection := registration + "/revisions/" + revision + "/evidence"
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_evidence_audit_outage", SQL: `CREATE FUNCTION reject_evidence_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type IN ('practitioner_registration_evidence','practitioner_registration_revisions') THEN RAISE EXCEPTION 'synthetic evidence audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_evidence_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_evidence_read();`}}))
	for _, request := range []struct{ method, path, body string }{{"GET", path, ""}, {"PUT", path, body}, {"GET", collection, ""}, {"GET", emptyCollection, ""}} {
		got := testutil.Do(r, request.method, request.path, "steward", request.body)
		require.Equal(t, 500, got.Code, got.Body.String())
		require.NotContains(t, got.Body.String(), strings.Repeat("c", 64))
		require.NotContains(t, got.Body.String(), "synthetic evidence audit")
	}
}

func TestEvidenceDatabasePreventsReparentingVerdictsAndHistoryDeletion(t *testing.T) {
	r, db, collection, _ := evidenceFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := collection + "/" + id
	body := `{"content_type":"image/png","size":123,"sha256":"` + strings.Repeat("d", 64) + `"}`
	created := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	for _, sql := range []string{"UPDATE practitioner_registration_evidence SET size=size+1 WHERE id=?", "UPDATE practitioner_registration_evidence SET status='available' WHERE id=?", "UPDATE practitioner_registration_evidence SET expires_at=expires_at+interval '1 hour' WHERE id=?", "DELETE FROM practitioner_registration_evidence WHERE id=?"} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; e != nil {
				return e
			}
			return tx.Exec(sql, id).Error
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "evidence")
	}
	got := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, got.Code)
	require.JSONEq(t, created.Body.String(), got.Body.String())
}

func TestEvidenceReplaySerializesOneIntentAndRenewalCannotInheritIt(t *testing.T) {
	r, db, collection, registration := evidenceFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := collection + "/" + id
	body := `{"content_type":"image/png","size":123,"sha256":"` + strings.Repeat("e", 64) + `"}`
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(r, "PUT", path, "steward", body).Code })
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	original := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, original.Code)
	revision := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", registration+"/revisions/"+revision, "steward", `{"expected_version":1,"expires_on":"2030-12-31"}`).Code)
	other := registration + "/revisions/" + revision + "/evidence/" + id
	require.Equal(t, 404, testutil.Do(r, "GET", other, "steward", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", other, "steward", body).Code)
	retained := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 200, retained.Code)
	require.JSONEq(t, original.Body.String(), retained.Body.String())
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_evidence_write_audit_outage", SQL: `CREATE FUNCTION reject_evidence_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='insert' AND NEW.resource_type='practitioner_registration_evidence' THEN RAISE EXCEPTION 'synthetic evidence write audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_evidence_write BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_evidence_write();`}}))
	missing := collection + "/" + uuid.Must(uuid.NewV7()).String()
	failed := testutil.Do(r, "PUT", missing, "steward", body)
	require.Equal(t, 500, failed.Code)
	require.NotContains(t, failed.Body.String(), "synthetic")
	require.Equal(t, 404, testutil.Do(r, "GET", missing, "steward", "").Code)
}
