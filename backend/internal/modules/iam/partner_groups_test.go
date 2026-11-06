package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestPartnerGroupPreparationIsTenantOwnedPendingAndReplayable(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "foreign": testutil.TenantB, "member": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}, "foreign": {iam.PermAssignmentManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-foreign','tenant_admin')`, testutil.TenantB).Error
	}))
	upstream := httptest.NewServer(router)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	path := "/v1/iam/partner-groups/" + uuid.Must(uuid.NewV7()).String()
	hospital := uuid.NewString()
	body := fmt.Sprintf(`{"hospital_id":%q,"name":"Supplier P receiving team","purpose":"receiving","reason":"Synthetic partner access preparation","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339))
	call := func(method, token, content string, status int) map[string]any {
		req := httptest.NewRequest(method, path, strings.NewReader(content))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", map[string]string{"admin": testutil.TenantA, "member": testutil.TenantA, "foreign": testutil.TenantB}[token])
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	prepared := call("PUT", "admin", body, 200)
	require.Equal(t, "pending_scope", prepared["status"])
	require.Equal(t, false, prepared["external_access_enabled"])
	require.Equal(t, hospital, prepared["hospital_id"])
	require.Equal(t, prepared, call("PUT", "admin", body, 200))
	require.Equal(t, prepared, call("GET", "admin", "", 200))
	call("PUT", "admin", strings.Replace(body, "receiving team", "finance team", 1), 409)
	call("GET", "foreign", "", 404)
	call("GET", "member", "", 403)
	originalPath := path
	path = "/v1/iam/partner-groups/" + uuid.Must(uuid.NewV7()).String()
	newer := call("PUT", "admin", body, 200)
	path = "/v1/iam/partner-groups?limit=1"
	firstPage := call("GET", "admin", "", 200)
	data := firstPage["data"].([]any)
	require.Len(t, data, 1)
	require.Equal(t, newer["id"], data[0].(map[string]any)["id"])
	page := firstPage["page"].(map[string]any)
	require.Equal(t, true, page["has_more"])
	path = "/v1/iam/partner-groups?limit=1&cursor=" + url.QueryEscape(page["next_cursor"].(string))
	secondPage := call("GET", "admin", "", 200)
	require.Equal(t, prepared["id"], secondPage["data"].([]any)[0].(map[string]any)["id"])
	call("GET", "foreign", "", 400)
	path = "/v1/iam/partner-groups?limit=1"
	require.Empty(t, call("GET", "foreign", "", 200)["data"])
	path = originalPath
}

type partnerGroupAuditFaultModule struct{ *iam.Module }

func (m partnerGroupAuditFaultModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9000_iam_partner_group_audit_fault", SQL: `
 CREATE TABLE iam_partner_group_test_faults(tenant_id uuid PRIMARY KEY,enabled boolean NOT NULL);
 ALTER TABLE iam_partner_group_test_faults ENABLE ROW LEVEL SECURITY;
 ALTER TABLE iam_partner_group_test_faults FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON iam_partner_group_test_faults USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
 CREATE FUNCTION iam_partner_group_test_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NEW.action LIKE 'partner_group_%' AND EXISTS(SELECT 1 FROM iam_partner_group_test_faults WHERE tenant_id=NEW.tenant_id AND enabled) THEN
  RAISE EXCEPTION 'synthetic group audit failure' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
 END $$;
 CREATE TRIGGER iam_partner_group_test_audit_fault BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION iam_partner_group_test_audit_fault();`})
}

func TestPartnerGroupAuditFailureRollsBackAndWithholdsDisclosure(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}},
		Modules: []platform.Module{partnerGroupAuditFaultModule{iam.New(nil)}},
	})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_partner_group_test_faults(tenant_id,enabled) VALUES(?,true)`, testutil.TenantA).Error
	}))
	upstream := httptest.NewServer(router)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7()).String()
	input, err := json.Marshal(map[string]any{"hospital_id": uuid.NewString(), "name": "Synthetic partner procurement group", "purpose": "procurement", "reason": "Attributable group preparation", "valid_from": time.Now().UTC().Add(-time.Hour), "expires_at": time.Now().UTC().Add(time.Hour)})
	require.NoError(t, err)
	call := func(method string, status int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/iam/partner-groups/"+id, strings.NewReader(string(input)))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	call("PUT", 503)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.iam_partner_groups','read',true)`).Error)
		var count int64
		require.NoError(t, tx.Table("iam_partner_groups").Where("id=?", id).Count(&count).Error)
		require.Zero(t, count)
		return tx.Exec(`UPDATE iam_partner_group_test_faults SET enabled=false WHERE tenant_id=?`, testutil.TenantA).Error
	}))
	require.Contains(t, call("PUT", 200).Body.String(), `"external_access_enabled":false`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE iam_partner_group_test_faults SET enabled=true WHERE tenant_id=?`, testutil.TenantA).Error
	}))
	hidden := call("GET", 503)
	require.NotContains(t, hidden.Body.String(), "Synthetic partner procurement group")
	require.NotContains(t, hidden.Body.String(), id)
}

func TestPartnerGroupHospitalScopeComesFromWorkforceOwner(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermAssignmentManage, iam.PermAssignmentReview, assignmentHospitalManage}, "reviewer": {iam.PermAssignmentManage, iam.PermAssignmentReview}},
		Modules: []platform.Module{iam.New(nil), assignmentWorkforceModule()},
	})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','tenant_admin')`, testutil.TenantA, testutil.TenantA).Error
	}))
	upstream := httptest.NewServer(router)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	actor := "admin"
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		return w
	}
	hospital := uuid.NewString()
	created := request("PUT", "/v1/workforce/hospitals/"+hospital, `{"name":"Synthetic partner recipient hospital"}`)
	require.Equal(t, 200, created.Code, created.Body.String())
	for _, scope := range []struct{ hospital, status string }{{hospital, "pending_review"}, {uuid.NewString(), "scope_rejected"}} {
		path := "/v1/iam/partner-groups/" + uuid.Must(uuid.NewV7()).String()
		body, err := json.Marshal(map[string]any{"hospital_id": scope.hospital, "name": "Synthetic governed recipient team", "purpose": "receiving", "reason": "Hospital scope must come from its actual owner", "valid_from": time.Now().UTC().Add(-time.Hour), "expires_at": time.Now().UTC().Add(time.Hour)})
		require.NoError(t, err)
		initial := request("PUT", path, string(body))
		require.Equal(t, 200, initial.Code, initial.Body.String())
		require.Contains(t, initial.Body.String(), `"status":"pending_scope"`)
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			actual := request("GET", path, "")
			require.Equal(c, 200, actual.Code, actual.Body.String())
			require.Contains(c, actual.Body.String(), `"status":"`+scope.status+`"`)
			require.Contains(c, actual.Body.String(), `"external_access_enabled":false`)
		}, 10*time.Second, 100*time.Millisecond)
		if scope.status == "pending_review" {
			commandPath := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
			commandBody := `{"operation":"approve","expected_version":2,"reason":"Independent publication review"}`
			self := request("PUT", commandPath, commandBody)
			require.Equal(t, 403, self.Code, self.Body.String())
			actor = "reviewer"
			approved := request("PUT", commandPath, commandBody)
			require.Equal(t, 200, approved.Code, approved.Body.String())
			require.Contains(t, approved.Body.String(), `"status":"approved"`)
			require.Contains(t, approved.Body.String(), `"external_access_enabled":false`)
			require.JSONEq(t, approved.Body.String(), request("PUT", commandPath, commandBody).Body.String())
			require.JSONEq(t, approved.Body.String(), request("GET", commandPath, "").Body.String())
			changed := request("PUT", commandPath, strings.Replace(commandBody, "Independent publication review", "Changed reviewed intent", 1))
			require.Equal(t, 409, changed.Code, changed.Body.String())
			actor = "admin"
		}
	}
}
