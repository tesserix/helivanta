package iam_test

import (
	"encoding/json"
	"maps"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestPartnerGrantDraftRetainsExactTypedIntentWithoutOperatingAuthority(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "foreign": testutil.TenantB, "member": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}, "foreign": {iam.PermAssignmentManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	for _, actor := range []struct{ tenant, subject string }{{testutil.TenantA, "user-admin"}, {testutil.TenantB, "user-foreign"}} {
		require.NoError(t, db.WithTenant(t.Context(), actor.tenant, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,?,'tenant_admin')`, actor.tenant, actor.subject).Error
		}))
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": server.URL}})
	require.NoError(t, err)
	input := map[string]any{"owner_product": "pharmacy", "owner_organization_id": uuid.Must(uuid.NewV7()), "source_location_id": uuid.Must(uuid.NewV7()), "relationship_id": uuid.Must(uuid.NewV7()), "relationship_version": 1, "recipient_kind": "group", "recipient_tenant_id": testutil.TenantB, "recipient_hospital_id": uuid.NewString(), "recipient_group_id": uuid.Must(uuid.NewV7()), "template_key": "partner.shipment_reader.v1", "source_jurisdiction": "IN-MH", "destination_jurisdiction": "IN-OD", "valid_from": time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond), "expires_at": time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), "reason": "Synthetic supplier receiving access proposal"}
	root := "/v1/iam/partner-grant-drafts/" + uuid.Must(uuid.NewV7()).String()
	call := func(method, path, token string, input any, status int) map[string]any {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", map[string]string{"admin": testutil.TenantA, "foreign": testutil.TenantB, "member": testutil.TenantA}[token])
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, response.Body.String())
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		var out map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
		return out
	}
	prepared := call("PUT", root, "admin", input, 200)
	require.Equal(t, "pending_verification", prepared["status"])
	require.Equal(t, false, prepared["operating_access_enabled"])
	require.Equal(t, testutil.TenantA, prepared["owner_tenant_id"])
	require.Equal(t, prepared, call("PUT", root, "admin", input, 200))
	require.Equal(t, prepared, call("GET", root, "admin", nil, 200))
	input["reason"] = "Changed intent"
	call("PUT", root, "admin", input, 409)
	call("GET", root, "foreign", nil, 404)
	call("GET", root, "member", nil, 403)
	require.Empty(t, call("GET", "/v1/iam/partner-grant-drafts?limit=1", "foreign", nil, 200)["data"])

	for _, change := range []map[string]any{
		{"recipient_subject": "synthetic-peer"}, {"recipient_subject": nil}, {"owner_tenant_id": testutil.TenantB}, {"operating_access_enabled": true}, {"recipient_group_id": nil}, {"owner_product": "hospital"}, {"template_key": "pharmacy.admin"}, {"source_jurisdiction": "IN-KA"}, {"destination_jurisdiction": "IN-DL"}, {"relationship_version": 0}, {"owner_organization_id": uuid.NewString()}, {"expires_at": time.Now().Add(-2 * time.Hour)}, {"expires_at": time.Now().Add(367 * 24 * time.Hour)}, {"reason": "\nSynthetic"},
	} {
		invalid := maps.Clone(input)
		maps.Copy(invalid, change)
		call("PUT", "/v1/iam/partner-grant-drafts/"+uuid.Must(uuid.NewV7()).String(), "admin", invalid, 400)
	}
	named := maps.Clone(input)
	delete(named, "recipient_group_id")
	named["recipient_kind"] = "subject"
	named["recipient_subject"] = "synthetic-hospital-account"
	named["owner_product"] = "supply"
	call("PUT", "/v1/iam/partner-grant-drafts/"+uuid.Must(uuid.NewV7()).String(), "admin", named, 200)
	first := call("GET", "/v1/iam/partner-grant-drafts?limit=1", "admin", nil, 200)
	require.Len(t, first["data"], 1)
	require.True(t, first["page"].(map[string]any)["has_more"].(bool))
	cursor := first["page"].(map[string]any)["next_cursor"].(string)
	second := call("GET", "/v1/iam/partner-grant-drafts?limit=1&cursor="+url.QueryEscape(cursor), "admin", nil, 200)
	require.Equal(t, prepared["id"], second["data"].([]any)[0].(map[string]any)["id"])
	call("GET", "/v1/iam/partner-grant-drafts?limit=1&cursor="+url.QueryEscape(cursor), "foreign", nil, 400)
	call("GET", "/v1/iam/partner-grant-drafts?owner_tenant_id="+testutil.TenantB, "admin", nil, 400)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("iam_partner_grant_drafts").Count(&count).Error }))
	require.Zero(t, count, "without the owner purpose, forced RLS hides proposal contents")
}

type partnerGrantDraftAuditFaultModule struct{ *iam.Module }

func (m partnerGrantDraftAuditFaultModule) Migrations() []tenantdb.Migration {
	migrations := partnerGroupAuditFaultModule(m).Migrations()
	for i := range migrations {
		migrations[i].SQL = strings.ReplaceAll(migrations[i].SQL, "NEW.action LIKE 'partner_group_%'", "NEW.action LIKE 'partner_grant_draft_%'")
	}
	return migrations
}

func TestPartnerGrantDraftAuditFailureDoesNotDiscloseAnOwnerList(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}}, Modules: []platform.Module{partnerGrantDraftAuditFaultModule{iam.New(nil)}}})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_partner_group_test_faults(tenant_id,enabled)VALUES(?,true)`, testutil.TenantA).Error
	}))
	request := httptest.NewRequest("GET", "/v1/iam/partner-grant-drafts?limit=1", nil)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "partner_grant_draft_unavailable")
	require.NotContains(t, response.Body.String(), `"data"`)
}

func TestPartnerGrantDraftAuditFailureRollsBackPreparation(t *testing.T) {
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}}, Modules: []platform.Module{partnerGrantDraftAuditFaultModule{iam.New(nil)}}})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_partner_group_test_faults(tenant_id,enabled)VALUES(?,true)`, testutil.TenantA).Error
	}))
	input := iam.PartnerGrantDraftInput{
		OwnerProduct: "supply", OwnerOrganizationID: uuid.Must(uuid.NewV7()), SourceLocationID: uuid.Must(uuid.NewV7()),
		RelationshipID: uuid.Must(uuid.NewV7()), RelationshipVersion: 1,
		RecipientKind: "group", RecipientTenantID: uuid.MustParse(testutil.TenantB), RecipientHospitalID: uuid.New(), RecipientGroupID: new(uuid.Must(uuid.NewV7())),
		TemplateKey: "partner.shipment_reader.v1", SourceJurisdiction: "IN-MH", DestinationJurisdiction: "IN-OD",
		ValidFrom: time.Now().UTC().Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), Reason: "Synthetic supplier audit rollback acceptance",
	}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7())
	call := func(method string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/v1/iam/partner-grant-drafts/"+id.String(), strings.NewReader(string(raw)))
		request.Header.Set("Authorization", "Bearer admin")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	failed := call("PUT")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), `"prepared_by"`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE iam_partner_group_test_faults SET enabled=false WHERE tenant_id=?`, testutil.TenantA).Error
	}))
	missing := call("GET")
	require.Equal(t, 404, missing.Code, "audit failure must leave no recoverable proposal: %s", missing.Body.String())
	saved := call("PUT")
	require.Equal(t, 200, saved.Code, saved.Body.String())
	for _, statement := range []string{
		`UPDATE iam_partner_grant_drafts SET document=jsonb_set(document,'{operating_access_enabled}','true') WHERE tenant_id=? AND id=?`,
		`DELETE FROM iam_partner_grant_drafts WHERE tenant_id=? AND id=?`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_partner_grant_drafts','write',true),set_config('app.actor_subject','user-admin',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(statement, testutil.TenantA, id).Error
		})
		require.ErrorContains(t, err, "immutable")
	}
	recovered := call("GET")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, saved.Body.String(), recovered.Body.String())
}
