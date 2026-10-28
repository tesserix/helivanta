package practitioner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestRegistrationIntakeRetainsUnverifiedCouncilClaimAndRejectsDuplicateIdentity(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA, "foreign": testutil.TenantB, "peer": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}, "foreign": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	p := uuid.Must(uuid.NewV7()).String()
	base := "/v1/practitioners/" + p
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Council claimant","professional_type":"physician"}`).Code)
	reg := uuid.Must(uuid.NewV7()).String()
	path := base + "/registrations/" + reg
	body := `{"council_system":"synthetic.state-council","registration_number":"SYN-001","issued_on":"2020-02-29","expires_on":"2030-02-28"}`
	created := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &row))
	require.Equal(t, "unverified", row["verification_state"])
	require.EqualValues(t, 1, row["version"])
	require.Equal(t, "2020-02-29", row["issued_on"])
	require.Equal(t, "2030-02-28", row["expires_on"])
	require.NotContains(t, row, "created_by")
	require.NotContains(t, row, "tenant_id")
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	replay := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	read := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
	changed := fmt.Sprintf(`{"council_system":"synthetic.state-council","registration_number":"%s"}`, "SYN-002")
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "steward", changed).Code)
	other := base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, testutil.Do(r, "PUT", other, "steward", body).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	require.Equal(t, 403, testutil.Do(r, "GET", path, "peer", "").Code)
	require.Equal(t, 401, testutil.Do(r, "GET", path, "unknown", "").Code)
}

func TestRegistrationRenewalPreservesPriorClaimAndReplaysOriginalReceipt(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Renewal claimant","professional_type":"nurse"}`).Code)
	path := base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	initial := `{"council_system":"synthetic.nursing-council","registration_number":"SYN-N-1","issued_on":"2020-01-01","expires_on":"2025-12-31"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", initial).Code)
	command := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	body := `{"expected_version":1,"issued_on":"2025-01-01","expires_on":"2030-12-31"}`
	renewed := testutil.Do(r, "PUT", command, "steward", body)
	require.Equal(t, 201, renewed.Code, renewed.Body.String())
	require.Contains(t, renewed.Body.String(), `"version":2`)
	require.Contains(t, renewed.Body.String(), `"verification_state":"unverified"`)
	next := testutil.Do(r, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":2,"issued_on":"2030-01-01","expires_on":null}`)
	require.Equal(t, 201, next.Code, next.Body.String())
	replay := testutil.Do(r, "PUT", command, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, renewed.Body.String(), replay.Body.String())
	retained := testutil.Do(r, "GET", command, "steward", "")
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.JSONEq(t, renewed.Body.String(), retained.Body.String())
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"version":3`)
	require.Contains(t, current.Body.String(), `"expires_on":null`)
	initialReplay := testutil.Do(r, "PUT", path, "steward", initial)
	require.Equal(t, 200, initialReplay.Code, initialReplay.Body.String())
	require.JSONEq(t, current.Body.String(), initialReplay.Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", command, "steward", `{"expected_version":1,"issued_on":"2025-01-01","expires_on":"2031-12-31"}`).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	first := testutil.Do(r, "GET", path+"/revisions/"+path[strings.LastIndex(path, "/")+1:], "steward", "")
	require.Equal(t, 200, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"expires_on":"2025-12-31"`)
	require.Contains(t, first.Body.String(), `"version":1`)
}

func TestRegistrationAndRevisionCollectionsAreBoundedAndRecoverAllRetainedClaims(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}, "foreign": {practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Two councils","professional_type":"physician"}`).Code)
	var path string
	for i := range 2 {
		path = base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
		require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", fmt.Sprintf(`{"council_system":"synthetic.council-%d","registration_number":"SYN-1"}`, i)).Code)
	}
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":1,"issued_on":"2026-01-01","expires_on":"2030-12-31"}`).Code)
	for _, collection := range []string{base + "/registrations", path + "/revisions"} {
		var page struct {
			Data []map[string]any `json:"data"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		first := testutil.Do(r, "GET", collection+"?limit=1", "steward", "")
		require.Equal(t, 200, first.Code, first.Body.String())
		require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.True(t, page.Page.HasMore)
		require.NotNil(t, page.Page.NextCursor)
		firstID := page.Data[0]["id"]
		next := testutil.Do(r, "GET", collection+"?limit=1&cursor="+url.QueryEscape(*page.Page.NextCursor), "steward", "")
		require.Equal(t, 200, next.Code, next.Body.String())
		require.NoError(t, json.Unmarshal(next.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.NotEqual(t, firstID, page.Data[0]["id"])
		require.False(t, page.Page.HasMore)
		require.Equal(t, 400, testutil.Do(r, "GET", collection+"?limit=201", "steward", "").Code)
		require.Equal(t, 404, testutil.Do(r, "GET", collection, "foreign", "").Code)
	}
}

func TestRegistrationRenewalsSerializeConflictsAndDeduplicateConcurrentReplay(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Concurrent renewals","professional_type":"dentist"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := base + "/registrations/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", `{"council_system":"synthetic.dental-council","registration_number":"SYN-D-1"}`).Code)
	for _, sameCommand := range []bool{false, true} {
		version := 1
		if sameCommand {
			version = 2
		}
		command := uuid.Must(uuid.NewV7()).String()
		codes := make(chan int, 12)
		var wg sync.WaitGroup
		for range 12 {
			c := command
			if !sameCommand {
				c = uuid.Must(uuid.NewV7()).String()
			}
			wg.Go(func() {
				codes <- testutil.Do(r, "PUT", path+"/revisions/"+c, "steward", fmt.Sprintf(`{"expected_version":%d,"expires_on":"2035-01-01"}`, version)).Code
			})
		}
		wg.Wait()
		close(codes)
		created := 0
		for code := range codes {
			if code == 201 {
				created++
			} else if sameCommand {
				require.Equal(t, 200, code)
			} else {
				require.Equal(t, 409, code)
			}
		}
		require.Equal(t, 1, created)
	}
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"version":3`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Table("practitioner_registration_revisions").Where("registration_id=?", id).Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, n)
		return nil
	}))
}

func TestInvalidRegistrationDatesAndAuthorityClaimsCannotCreateRevisions(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Validation claimant","professional_type":"pharmacist"}`).Code)
	path := base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	prefix := `"council_system":"synthetic.pharmacy-council","registration_number":"SYN-P-1"`
	for _, suffix := range []string{
		`,"issued_on":"2023-02-29"`, `,"expires_on":"0000-01-01"`, `,"issued_on":""`, `,"expires_on":"2026-1-01"`, `,"issued_on":"2030-01-01","expires_on":"2029-12-31"`, `,"verification_state":"hpr_verified"`, `,"created_by":"forged"`, `,"expires_on":false`,
	} {
		w := testutil.Do(r, "PUT", path, "steward", "{"+prefix+suffix+"}")
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Equal(t, 404, testutil.Do(r, "GET", path, "steward", "").Code)
	}
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", "{"+prefix+"}").Code)
	for _, body := range []string{
		`null`, `{}`, `{"expected_version":0}`, `{"expected_version":9007199254740991}`, `{"expected_version":1.5}`, `{"expected_version":1,"expires_on":"2025-02-29"}`, `{"expected_version":1,"issued_on":"2030-01-01","expires_on":"2029-01-01"}`, `{"expected_version":1,"verification_state":"hpr_verified"}`, `{"expected_version":1} {}`,
	} {
		w := testutil.Do(r, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"version":1`)
	require.Contains(t, current.Body.String(), `"expires_on":null`)
}

func TestRegistrationHeadAndRevisionRollBackWhenUpdateAuditFails(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Audit rollback claimant","professional_type":"radiologist"}`).Code)
	path := base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	initial := testutil.Do(r, "PUT", path, "steward", `{"council_system":"synthetic.medical-council","registration_number":"PRIVATE-SYN-1"}`)
	require.Equal(t, 201, initial.Code, initial.Body.String())
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registration_update_audit_outage", SQL: `CREATE FUNCTION reject_registration_update_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='practitioner_registrations' AND NEW.action='update' THEN RAISE EXCEPTION 'synthetic registration audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_registration_update_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_registration_update_audit();`}}))
	command := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	failed := testutil.Do(r, "PUT", command, "steward", `{"expected_version":1,"expires_on":"2030-01-01"}`)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "PRIVATE-SYN-1")
	require.NotContains(t, failed.Body.String(), "synthetic registration audit failure")
	require.Equal(t, 404, testutil.Do(r, "GET", command, "steward", "").Code)
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, initial.Body.String(), current.Body.String())
}

func TestEmptyRegistrationCollectionRequiresCommittedReadAudit(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Empty collection claimant","professional_type":"paramedic"}`).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registration_list_read_audit_outage", SQL: `CREATE FUNCTION reject_registration_list_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' THEN RAISE EXCEPTION 'synthetic read audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_registration_list_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_registration_list_read();`}}))
	read := testutil.Do(r, "GET", base+"/registrations", "steward", "")
	require.Equal(t, 500, read.Code, read.Body.String())
	require.NotContains(t, read.Body.String(), `"data"`)
	require.NotContains(t, read.Body.String(), "synthetic read audit failure")
}

func TestRegistrationDatabaseGuardsRejectOrphanHistoryAndDestructiveChanges(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	practitionerID := uuid.Must(uuid.NewV7()).String()
	base := "/v1/practitioners/" + practitionerID
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Retained registration claimant","professional_type":"pathologist"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := base + "/registrations/" + id
	original := testutil.Do(r, "PUT", path, "steward", `{"council_system":"synthetic.medical-council","registration_number":"SYN-PATH-1"}`)
	require.Equal(t, 201, original.Code, original.Body.String())
	for _, sql := range []string{
		"UPDATE practitioner_registrations SET version=2 WHERE tenant_id=? AND id=?",
		"DELETE FROM practitioner_registrations WHERE tenant_id=? AND id=?",
		"UPDATE practitioner_registration_revisions SET expires_on='2030-01-01' WHERE tenant_id=? AND id=?",
		"DELETE FROM practitioner_registration_revisions WHERE tenant_id=? AND id=?",
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true)").Error; e != nil {
				return e
			}
			return tx.Exec(sql, testutil.TenantA, id).Error
		})
		require.Error(t, err)
	}
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true)").Error; e != nil {
			return e
		}
		return tx.Exec("INSERT INTO practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id,expected_version,version,created_by) VALUES(?,?,?,?,1,2,'user-steward')", testutil.TenantA, practitionerID, id, uuid.Must(uuid.NewV7()).String()).Error
	})
	require.ErrorContains(t, err, "registration revision requires advanced head")
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, original.Body.String(), current.Body.String())
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if tenant == testutil.TenantB {
				if e := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; e != nil {
					return e
				}
			}
			for _, table := range []string{"practitioner_registrations", "practitioner_registration_revisions"} {
				var n int64
				if e := tx.Table(table).Count(&n).Error; e != nil {
					return e
				}
				require.Zero(t, n, "unscoped same tenant and scoped foreign tenant cannot read records")
			}
			return nil
		}))
	}
}

func TestCancelledRegistrationRenewalFailsClosedWithoutAdvancingHead(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Cancellation claimant","professional_type":"nurse"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := base + "/registrations/" + id
	initial := testutil.Do(r, "PUT", path, "steward", `{"council_system":"synthetic.nursing-council","registration_number":"SYN-CANCEL-1"}`)
	require.Equal(t, 201, initial.Code, initial.Body.String())
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
				return err
			}
			if err := tx.Exec("SELECT id FROM practitioner_registrations WHERE tenant_id=? AND id=? FOR UPDATE", testutil.TenantA, id).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("lock setup failed: %v", err)
	case <-t.Context().Done():
		t.Fatal("lock setup cancelled")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	command := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	req := httptest.NewRequest("PUT", command, strings.NewReader(`{"expected_version":1,"expires_on":"2030-01-01"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer steward")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 503, w.Code, w.Body.String())
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Contains(t, w.Body.String(), `"error":"practitioner_unavailable"`)
	require.NotContains(t, w.Body.String(), "SYN-CANCEL-1")
	close(release)
	released = true
	require.NoError(t, <-done)
	require.Equal(t, 404, testutil.Do(r, "GET", command, "steward", "").Code)
	current := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, initial.Body.String(), current.Body.String())
}

func TestUnverifiedClaimCannotReserveAnotherPractitionersCouncilIdentity(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	body := `{"council_system":"synthetic.medical-council","registration_number":"SYN-DISPUTED-1"}`
	var paths []string
	for _, name := range []string{"First reported claimant", "Second reported claimant"} {
		base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
		require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", fmt.Sprintf(`{"given_name":%q,"professional_type":"physician"}`, name)).Code)
		path := base + "/registrations/" + uuid.Must(uuid.NewV7()).String()
		saved := testutil.Do(r, "PUT", path, "steward", body)
		require.Equal(t, 201, saved.Code, saved.Body.String())
		require.Contains(t, saved.Body.String(), `"verification_state":"unverified"`)
		paths = append(paths, path)
	}
	wrong := paths[0][:strings.LastIndex(paths[0], "/")+1] + paths[1][strings.LastIndex(paths[1], "/")+1:]
	require.Equal(t, 404, testutil.Do(r, "GET", wrong, "steward", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", wrong+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":1}`).Code)
	for _, path := range paths {
		retained := testutil.Do(r, "GET", path, "steward", "")
		require.Equal(t, 200, retained.Code, retained.Body.String())
		require.Contains(t, retained.Body.String(), `"verification_state":"unverified"`)
	}
}

func TestRegistrationReadAuditFailureBlocksDetailsHistoryAndReplayDisclosures(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	base := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", base, "steward", `{"given_name":"Read audit claimant","professional_type":"nurse"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := base + "/registrations/" + id
	body := `{"council_system":"synthetic.nursing-council","registration_number":"PRIVATE-CLAIM-1"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", body).Code)
	command := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	renewal := `{"expected_version":1,"issued_on":"2026-01-01","expires_on":"2030-12-31"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", command, "steward", renewal).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registration_read_audit_outage", SQL: `CREATE FUNCTION reject_registration_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' THEN RAISE EXCEPTION 'synthetic registration read audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_registration_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_registration_read();`}}))
	for _, op := range []struct{ method, path, body string }{
		{"GET", path, ""}, {"GET", command, ""}, {"GET", base + "/registrations", ""}, {"GET", path + "/revisions", ""}, {"PUT", path, body}, {"PUT", command, renewal},
	} {
		w := testutil.Do(r, op.method, op.path, "steward", op.body)
		require.Equal(t, 500, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "PRIVATE-CLAIM-1")
		require.NotContains(t, w.Body.String(), "2030-12-31")
		require.NotContains(t, w.Body.String(), "synthetic registration read audit failure")
		require.NotContains(t, w.Body.String(), `"data"`)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; e != nil {
			return e
		}
		var head struct{ Version int64 }
		if e := tx.Table("practitioner_registrations").Select("version").Where("id=?", id).Take(&head).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, head.Version)
		var count int64
		if e := tx.Table("practitioner_registration_revisions").Where("registration_id=?", id).Count(&count).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
}
