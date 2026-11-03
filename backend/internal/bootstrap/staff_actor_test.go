package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStaffActorSourceRefusesHistoricalLinkWithoutDigitalGeneration(t *testing.T) {
	graph, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	staff := workforce.New()
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"manager": testutil.TenantA, "reviewer": testutil.TenantA, "candidate": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"manager":  {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermLoginLinkManage, iam.PermAssignmentManage},
			"reviewer": {workforce.PermLoginLinkReview, iam.PermAssignmentReview},
		},
		Modules: []platform.Module{iam.New(nil), staff}, Writer: graph, Roles: graph,
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, graph) },
	})
	for _, actor := range []string{"manager", "reviewer", "candidate"} {
		role := authz.RoleTenantAdmin
		if actor == "candidate" {
			role = authz.RoleStaffMember
		}
		require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, role))
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "user-"+actor, role))
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, testutil.TenantA, "user-"+actor, role).Error
		}))
	}
	call := func(method, path, token, body string, status int) string {
		t.Helper()
		response := testutil.Do(router, method, path, token, body)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}
	hospital, person := uuid.New(), uuid.New()
	base := "/v1/workforce/hospitals/" + hospital.String()
	call("PUT", base, "manager", `{"name":"Synthetic source guard hospital"}`, 200)
	for _, actor := range []string{"manager", "reviewer"} {
		call("PUT", base+"/access/user-"+actor, "manager", `{"level":"manager"}`, 200)
	}
	personPath := base + "/staff/" + person.String()
	call("PUT", personPath, "manager", `{"employee_code":"SYN-HISTORICAL-ACTOR","display_name":"Synthetic historical actor","job_category":"other","employment_status":"active"}`, 200)
	link := personPath + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Synthetic historical Staff link"}`, 200)
	call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independent historical link review"}`, 200)
	assignment := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", assignment, "manager", fmt.Sprintf(`{"hospital_id":%q,"subject":"user-candidate","template_key":"workforce.posting_reader.v1","reason":"Synthetic source guard test","valid_from":%q,"expires_at":%q}`, hospital.String(), time.Now().UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)), 200)
	require.Eventually(t, func() bool {
		var output iam.ProposalOutput
		require.NoError(t, json.Unmarshal([]byte(call("GET", assignment, "manager", "", 200)), &output))
		return output.Status == "pending_review"
	}, 10*time.Second, 20*time.Millisecond)
	call("POST", assignment+"/decision", "reviewer", `{"expected_version":2,"decision":"approve","reason":"Independent assignment approval"}`, 200)
	authority := iam.NewScopedAuthorizer(db, graph)
	request := authz.ScopeRequest{TenantID: testutil.TenantA, Subject: "user-candidate", HospitalID: hospital.String(), Permission: authz.StaffActorUse}
	var grant authz.ScopeGrant
	require.Eventually(t, func() bool {
		grant, err = authority.AuthorizeScope(ctx, request)
		return err == nil && grant.AssignmentID != ""
	}, 10*time.Second, 20*time.Millisecond)
	called := false
	err = authority.(authz.CompoundScopedCommandGuard).WithScopeGrants(ctx, []authz.ScopeCheck{{Request: request, Grant: grant}}, time.Now(), func(commandCtx context.Context) error {
		guard := platform.StaffActors([]platform.Module{staff}, db)
		require.NotNil(t, guard)
		return guard.WithCurrentStaffActor(commandCtx, authz.StaffActorScope{TenantID: request.TenantID, Subject: request.Subject, HospitalID: hospital}, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
	})
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.False(t, called)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workforce_audit").Where("action='workforce.staff_actor.use'").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
	var retained struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(call("GET", personPath+"/login-links", "manager", "", 200)), &retained))
	require.Len(t, retained.Data, 1)
	require.Equal(t, "active", retained.Data[0].Status)
}
