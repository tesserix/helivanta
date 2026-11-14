package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/organization"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGovernedAssignmentDepartmentChoicesUseCurrentOwnerScopeAndDenyMasterWrites(t *testing.T) {
	modules := []platform.Module{iam.New(nil).SetMembershipIdentityReader(&governanceNativeIdentity{}), workforce.New(), organization.New()}
	ownerPerms := []authz.Permission{workforce.PermHospitalManage, organization.PermStructureManage, organization.PermStructureRead, iam.PermGovernanceManage, iam.PermGovernanceReview, iam.PermGovernanceAssignmentManage}
	backend, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "owner2": testutil.TenantA, "reviewer": testutil.TenantA, "auditor": testutil.TenantA, "foreignadmin": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": ownerPerms, "owner2": ownerPerms, "reviewer": {iam.PermGovernanceAssignmentManage}, "auditor": {organization.PermStructureRead, iam.PermGovernanceAssignmentManage}, "foreignadmin": ownerPerms},
		Modules: modules,
	})
	ctx := t.Context()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-owner2','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'user-auditor','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-foreignadmin','tenant_admin')`, testutil.TenantB).Error
	}))
	upstream := httptest.NewServer(backend)
	defer upstream.Close()
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	request := func(method, path, actor string, input any) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if input != nil {
			var encodeErr error
			body, encodeErr = json.Marshal(input)
			require.NoError(t, encodeErr)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+actor)
		r.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if actor == "foreignadmin" {
			tenant = testutil.TenantB
		}
		r.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		r.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		return w
	}
	call := func(method, path, actor string, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := request(method, path, actor, input)
		require.Equal(t, status, r.Code, r.Body.String())
		return r
	}
	createRoot := func(actor string, register bool) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		call("PUT", "/v1/workforce/hospitals/"+id.String(), actor, map[string]any{"name": "Synthetic selection hospital"}, 200)
		if register {
			call("PUT", "/v1/organization/hospitals/"+id.String()+"/registrations/"+uuid.Must(uuid.NewV7()).String(), actor, map[string]any{"reason": "Synthetic structural reference"}, 200)
		}
		return id
	}
	hospital, sibling, foreign := createRoot("admin", true), createRoot("admin", true), createRoot("foreignadmin", true)
	departmentRoot := func(h uuid.UUID) string { return "/v1/organization/hospitals/" + h.String() }
	createDepartment := func(h uuid.UUID, actor, code, name string) uuid.UUID {
		id, revision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		body := organization.DepartmentCommand{ExpectedVersion: new(int64), Code: code, Name: name, Kind: "department", Status: "active", Reason: "Synthetic qualified scope"}
		require.EventuallyWithT(t, func(check *assert.CollectT) {
			r := request("PUT", departmentRoot(h)+"/departments/"+id.String()+"/revisions/"+revision.String(), actor, body)
			require.Equal(check, 200, r.Code, r.Body.String())
		}, 20*time.Second, 100*time.Millisecond)
		return id
	}
	first := createDepartment(hospital, "admin", "SYN-FIRST", "Synthetic visible department")
	createDepartment(hospital, "admin", "SYN-NEXT", "Synthetic continuation department")
	retired := createDepartment(hospital, "admin", "SYN-RETIRED", "Synthetic retired department")
	createDepartment(sibling, "admin", "SYN-SIBLING", "Synthetic sibling department")
	createDepartment(foreign, "foreignadmin", "SYN-FOREIGN", "Synthetic foreign department")
	version := int64(1)
	call("PUT", departmentRoot(hospital)+"/departments/"+retired.String()+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "admin", organization.DepartmentCommand{ExpectedVersion: &version, Code: "SYN-RETIRED", Name: "Synthetic retired department", Kind: "department", Status: "retired", Reason: "Retained historical department"}, 200)
	grantPath := "/v1/iam/governance-grants/" + uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	call("PUT", grantPath, "admin", iam.GovernanceGrantInput{Subject: "user-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "iam.scoped_access_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Reason: "Synthetic current department preparation"}, 202)
	call("PUT", grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.GovernanceGrantDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "Independent owner review"}, 202)
	choices := departmentRoot(hospital) + "/assignment-department-choices"
	type choicePage struct {
		Data []organization.DepartmentOutput `json:"data"`
		Page struct {
			HasMore bool    `json:"has_more"`
			Next    *string `json:"next_cursor"`
		} `json:"page"`
	}
	response := call("GET", choices+"?limit=1", "reviewer", nil, 200)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var page choicePage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.Next)
	require.Equal(t, hospital, page.Data[0].HospitalID)
	require.Equal(t, "active", page.Data[0].Status)
	second := call("GET", choices+"?limit=1&cursor="+url.QueryEscape(*page.Page.Next), "reviewer", nil, 200)
	var next choicePage
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &next))
	require.Len(t, next.Data, 1)
	require.NotEqual(t, page.Data[0].ID, next.Data[0].ID)
	require.False(t, next.Page.HasMore)
	for _, private := range []string{"actor_subject", "reason", "owner_membership_id", "source_grant_id", "Synthetic retired department", "Synthetic sibling department", "Synthetic foreign department"} {
		require.NotContains(t, response.Body.String()+second.Body.String(), private)
	}
	call("GET", choices, "admin", nil, 200)
	call("GET", choices, "auditor", nil, 404)
	call("GET", departmentRoot(sibling)+"/assignment-department-choices", "reviewer", nil, 404)
	call("GET", departmentRoot(foreign)+"/assignment-department-choices", "reviewer", nil, 404)
	call("GET", choices+"?limit=21", "reviewer", nil, 400)
	call("GET", choices+"?subject=user-admin", "reviewer", nil, 400)
	call("GET", choices+"?limit=1&cursor="+url.QueryEscape(*page.Page.Next), "admin", nil, 400)
	call("GET", departmentRoot(sibling)+"/assignment-department-choices?cursor="+url.QueryEscape(*page.Page.Next), "reviewer", nil, 400)
	call("GET", departmentRoot(foreign)+"/assignment-department-choices?cursor="+url.QueryEscape(*page.Page.Next), "foreignadmin", nil, 400)

	auditCount := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("audit_events").Where("tenant_id=? AND action='organization.assignment_department_choices.read'", testutil.TenantA).Count(&count).Error
		}))
		return count
	}
	before := auditCount()
	apply := func(sql string) {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_assignment_choices_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	apply(`ALTER TABLE audit_events ADD CONSTRAINT synthetic_assignment_choice_audit CHECK(action<>'organization.assignment_department_choices.read')NOT VALID;`)
	hidden := call("GET", choices, "reviewer", nil, 503)
	require.NotContains(t, hidden.Body.String(), "Synthetic visible department")
	require.Equal(t, before, auditCount())
	apply(`ALTER TABLE audit_events DROP CONSTRAINT synthetic_assignment_choice_audit;`)
	source := platform.GovernanceSources(modules, db)
	principal := authn.Principal{TenantID: testutil.TenantA, Subject: "user-reviewer", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}
	var denied *pgconn.PgError
	err = source.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: principal, Capability: authz.GovernanceAssignmentManage, Scope: authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "hospital", ID: &hospital}}, func(held context.Context, evidence authz.GovernanceSourceEvidence) error {
		proof, e := authz.AssignmentDepartmentChoiceProof(authz.GovernedAssignmentSelectionRequest{Principal: principal, HospitalID: hospital, Capability: authz.GovernanceAssignmentManage, Source: evidence}, time.Now())
		if e != nil {
			return e
		}
		raw, e := json.Marshal(proof)
		if e != nil {
			return e
		}
		return db.WithTenantCommand(held, testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec(`SELECT set_config('app.organization_assignment_selection',?,true),set_config('app.organization_department_write','authorized',true)`, string(raw)).Error; e != nil {
				return e
			}
			return tx.Exec(`UPDATE organization_departments SET name='Denied source write',version=version+1,effective_at=clock_timestamp(),updated_at=clock_timestamp()WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, first).Error
		})
	})
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	call("GET", choices, "reviewer", nil, 200)
	apply(`ALTER TABLE organization_departments NO FORCE ROW LEVEL SECURITY;`)
	call("GET", choices, "reviewer", nil, 503)
	apply(`ALTER TABLE organization_departments FORCE ROW LEVEL SECURITY;`)
	call("PUT", grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", iam.GovernanceGrantDecisionInput{ExpectedVersion: 2, Decision: "revoke", Reason: "Current delegate cutoff"}, 202)
	call("GET", choices, "reviewer", nil, 404)
	call("GET", choices, "admin", nil, 200)
	missing := createRoot("admin", false)
	call("GET", departmentRoot(missing)+"/assignment-department-choices", "admin", nil, 503)
	apply(`DROP POLICY assignment_selection_purpose ON organization_departments;`)
	call("GET", choices, "admin", nil, 503)
}
