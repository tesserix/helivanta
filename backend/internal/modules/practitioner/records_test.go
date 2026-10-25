package practitioner_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestRecordIntakeIsUnverifiedImmutableAuditedAndTenantScoped(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}, "foreign": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/practitioners/" + id
	body := `{"given_name":"Synthetic","family_name":"Practitioner","professional_type":"physician"}`
	created := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var saved map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &saved))
	require.Equal(t, id, saved["id"])
	require.Equal(t, "unverified", saved["verification_state"])
	require.NotContains(t, saved, "tenant_id")
	require.NotContains(t, saved, "created_by")
	replay := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	read := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
	require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "steward", `{"given_name":"Changed","family_name":"Practitioner","professional_type":"physician"}`).Code)
	require.Equal(t, 403, testutil.Do(r, "GET", path, "peer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	require.Equal(t, 401, testutil.Do(r, "GET", path, "unknown", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var writes, reads int64
		if err := tx.Table("audit_events").Where("resource_type='practitioner_records' AND resource_id=? AND action='insert'", id).Count(&writes).Error; err != nil {
			return err
		}
		if err := tx.Table("audit_events").Where("resource_type='practitioner_records' AND resource_id=? AND action='read' AND actor_subject='user-steward'", id).Count(&reads).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, writes)
		require.EqualValues(t, 2, reads)
		return nil
	}))
}

func TestConcurrentIntakeCreatesOneIntentAndRejectsChangedReplay(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	path := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	body := `{"given_name":"Concurrent","professional_type":"nurse"}`
	results := make(chan int, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { results <- testutil.Do(r, "PUT", path, "steward", body).Code })
	}
	wg.Wait()
	close(results)
	created := 0
	for code := range results {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "steward", `{"given_name":"Changed","professional_type":"nurse"}`).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Table("practitioner_records").Where("tenant_id=?", testutil.TenantA).Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, n)
		return nil
	}))
}

func TestIntakeRejectsVerificationClaimsAndInvalidInputWithoutCreatingRecords(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage}}, Modules: []platform.Module{practitioner.New()}})
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"given_name":"Person","professional_type":"doctor"}`,
		`{"given_name":"Person","professional_type":"physician","verification_state":"hpr_verified"}`,
		`{"given_name":"Person","professional_type":"physician","tenant_id":"` + testutil.TenantB + `"}`,
		`{"given_name":"Person","family_name":"","professional_type":"physician"}`,
		`{"given_name":" Person","professional_type":"physician"}`,
		`{"given_name":"Per\nson","professional_type":"physician"}`,
		`{"given_name":"` + strings.Repeat("x", 151) + `","professional_type":"physician"}`,
		`{"given_name":"Person","professional_type":"physician"} {}`,
		`{"given_name":"` + strings.Repeat("x", 8192) + `","professional_type":"physician"}`,
	} {
		w := testutil.Do(r, "PUT", "/v1/practitioners/"+uuid.Must(uuid.NewV7()).String(), "steward", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Table("practitioner_records").Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n)
		return nil
	}))
}

func TestWorkspaceMismatchAndMissingScopeCannotDiscloseOrCreatePractitioners(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	path := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	body := `{"given_name":"Scoped","professional_type":"physiotherapist"}`
	for _, method := range []string{"PUT", "GET"} {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer steward")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	require.Equal(t, 404, testutil.Do(r, "GET", path, "steward", "").Code)
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", body).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("practitioner_records").Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n, "tenant context alone is not administrative record authority")
		return nil
	}))
}

func TestReadAuditFailurePreventsPractitionerDisclosure(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	path := "/v1/practitioners/" + uuid.Must(uuid.NewV7()).String()
	body := `{"given_name":"Private","professional_type":"paramedic"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", body).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_practitioner_read_audit_failure", SQL: `CREATE FUNCTION reject_practitioner_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='practitioner_records' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_practitioner_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_practitioner_read();`}}))
	for _, method := range []string{"GET", "PUT"} {
		w := testutil.Do(r, method, path, "steward", body)
		require.Equal(t, 500, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "Private")
		require.NotContains(t, w.Body.String(), "synthetic audit outage")
	}
}

func TestDatabaseRetainsOriginalPractitionerIntent(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"steward": testutil.TenantA}, Perms: map[string][]authz.Permission{"steward": {practitioner.PermRecordManage, practitioner.PermRecordRead}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/practitioners/" + id
	body := `{"given_name":"Retained","professional_type":"radiologist"}`
	created := testutil.Do(r, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	for _, sql := range []string{
		"UPDATE practitioner_records SET given_name='Changed' WHERE tenant_id=? AND id=?",
		"UPDATE practitioner_records SET verification_state='hpr_verified' WHERE tenant_id=? AND id=?",
		"DELETE FROM practitioner_records WHERE tenant_id=? AND id=?",
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true),set_config('app.actor_subject','user-steward',true)").Error; e != nil {
				return e
			}
			return tx.Exec(sql, testutil.TenantA, id).Error
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "practitioner identity intent is immutable")
	}
	read := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
}
