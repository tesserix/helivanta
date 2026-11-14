package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestGovernanceAssignmentProposalAdmissionUsesExactHospitalWithoutLegacyManager(t *testing.T) {
	native := &governanceNativeIdentity{}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "auditor": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermGovernanceManage, iam.PermGovernanceRead, workforce.PermHospitalManage, iam.PermAssignmentManage, iam.PermAssignmentReview},
			"reviewer": {iam.PermAssignmentManage, iam.PermAssignmentReview},
			"auditor":  {iam.PermAssignmentManage, iam.PermAssignmentReview},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'user-auditor','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	created := governanceRequest(router, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic assignment governance hospital"}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	grant := iam.GovernanceGrantInput{
		Subject: "user-reviewer", ExpectedMembershipEpoch: 1,
		TemplateKey: "iam.scoped_access_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital,
		Reason: "Synthetic exact-hospital access administration", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour),
	}
	raw, err := json.Marshal(grant)
	require.NoError(t, err)
	grantPath := "/v1/iam/governance-grants/" + uuid.Must(uuid.NewV7()).String()
	delegated := governanceRequest(router, http.MethodPut, grantPath, "admin", string(raw))
	require.Equal(t, http.StatusAccepted, delegated.Code, delegated.Body.String())
	var grantReceipt iam.GovernanceGrantReceipt
	require.NoError(t, json.Unmarshal(delegated.Body.Bytes(), &grantReceipt))
	require.Equal(t, "provisional_active", grantReceipt.Status)

	proposal := iam.ProposalInput{
		HospitalID: hospital, Subject: "user-auditor", TemplateKey: "workforce.leave_reviewer.v1",
		Reason: "Synthetic proposed leave review duty", ValidFrom: start, ExpiresAt: start.Add(12 * time.Hour),
	}
	body, err := json.Marshal(proposal)
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7()).String()
	legacy := "/v1/iam/assignment-proposals/" + id
	root := "/v1/iam/hospitals/" + hospital.String() + "/assignment-proposals"
	path := root + "/" + id
	denied := governanceRequest(router, http.MethodPut, legacy, "reviewer", string(body))
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	denied = governanceRequest(router, http.MethodPut, path, "auditor", string(body))
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	admitted := governanceRequest(router, http.MethodPut, path, "reviewer", string(body))
	require.Equal(t, http.StatusOK, admitted.Code, admitted.Body.String())
	var pending iam.ProposalOutput
	require.NoError(t, json.Unmarshal(admitted.Body.Bytes(), &pending))
	require.Equal(t, "pending_scope", pending.Status)
	require.Equal(t, "user-reviewer", pending.ProposedBy)
	require.Equal(t, hospital, pending.Input.HospitalID)
	require.Equal(t, "no-store", admitted.Header().Get("Cache-Control"))
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		read := governanceRequest(router, http.MethodGet, path, "reviewer", "")
		require.Equal(check, http.StatusOK, read.Code, read.Body.String())
		var current iam.ProposalOutput
		require.NoError(check, json.Unmarshal(read.Body.Bytes(), &current))
		require.Equal(check, "pending_review", current.Status, "Workforce must confirm its hospital without granting legacy manager access")
	}, 20*time.Second, 100*time.Millisecond)
	replay := governanceRequest(router, http.MethodPut, path, "reviewer", string(body))
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var retained iam.ProposalOutput
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &retained))
	require.EqualValues(t, 2, retained.Version)
	proposal.Reason = "Different proposal intent"
	changed, err := json.Marshal(proposal)
	require.NoError(t, err)
	conflict := governanceRequest(router, http.MethodPut, path, "reviewer", string(changed))
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	for _, suffix := range []string{"", "/history", "/decisions"} {
		read := governanceRequest(router, http.MethodGet, path+suffix, "reviewer", "")
		require.Equal(t, http.StatusOK, read.Code, read.Body.String())
		require.NotContains(t, read.Body.String(), "governance_authority")
		require.NotContains(t, read.Body.String(), "owner_membership_id")
	}
	wrongRoot := "/v1/iam/hospitals/" + uuid.Must(uuid.NewV7()).String() + "/assignment-proposals/" + id
	invisible := governanceRequest(router, http.MethodGet, wrongRoot, "reviewer", "")
	require.Equal(t, http.StatusNotFound, invisible.Code, invisible.Body.String())

	var effects struct{ Staff, Access, Proposals int64 }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, hospital.String()).Error; err != nil {
			return err
		}
		if err := tx.Table("workforce_staff").Count(&effects.Staff).Error; err != nil {
			return err
		}
		if err := tx.Table("workforce_hospital_access").Where("tenant_id=? AND hospital_id=?", testutil.TenantA, hospital).Count(&effects.Access).Error; err != nil {
			return err
		}
		return tx.Table("iam_assignment_proposals").Where("tenant_id=? AND id=?", testutil.TenantA, id).Count(&effects.Proposals).Error
	}))
	require.Zero(t, effects.Staff, "governance cannot fabricate Staff actors")
	require.Zero(t, effects.Access, "native hospital verification cannot manufacture legacy manager rows")
	require.EqualValues(t, 1, effects.Proposals, "replay retains the canonical proposal")
}

type assignmentScopeGate struct {
	entered, release chan struct{}
	once             sync.Once
}

type assignmentScopeBarrier struct {
	*workforce.Module
	gate atomic.Pointer[assignmentScopeGate]
}

func (m *assignmentScopeBarrier) Consumers(deps platform.Deps) []events.Consumer {
	consumers := m.Module.Consumers(deps)
	for index := range consumers {
		if consumers[index].Name != "workforce-assignment-governance-scope" {
			continue
		}
		handle := consumers[index].Handle
		consumers[index].Handle = func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			if gate := m.gate.Load(); gate != nil {
				gate.once.Do(func() { close(gate.entered) })
				select {
				case <-gate.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return handle(ctx, tx, evt)
		}
	}
	return consumers
}

func TestGovernanceAssignmentDecisionRechecksOriginalSourceAfterOwnerAnswer(t *testing.T) {
	graph, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember, authz.RoleGovernanceDelegate} {
		require.NoError(t, graph.GrantTenantRole(t.Context(), testutil.TenantA, role))
	}
	require.NoError(t, graph.GrantRole(t.Context(), testutil.TenantA, "user-admin", authz.RoleTenantAdmin))
	for _, subject := range []string{"user-reviewer", "synthetic-assignment-recipient"} {
		require.NoError(t, graph.GrantRole(t.Context(), testutil.TenantA, subject, authz.RoleStaffMember))
	}
	native := &governanceNativeIdentity{}
	owner := &assignmentScopeBarrier{Module: workforce.New()}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: graph, Roles: graph, Membership: graph,
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermGovernanceManage, workforce.PermHospitalManage, iam.PermAssignmentManage, iam.PermAssignmentReview},
			"reviewer": {iam.PermAssignmentManage, iam.PermAssignmentReview},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), owner},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'synthetic-assignment-recipient','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	created := governanceRequest(router, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic guarded settlement hospital"}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	grantID := uuid.Must(uuid.NewV7()).String()
	grantPath := "/v1/iam/governance-grants/" + grantID
	grant, err := json.Marshal(iam.GovernanceGrantInput{Subject: "user-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "iam.scoped_access_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital, Reason: "Synthetic independent hospital access review", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour)})
	require.NoError(t, err)
	delegated := governanceRequest(router, http.MethodPut, grantPath, "admin", string(grant))
	require.Equal(t, http.StatusAccepted, delegated.Code, delegated.Body.String())
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "current source settles", true: "revoked source cannot settle"}[revoke], func(t *testing.T) {
			proposalID := uuid.Must(uuid.NewV7())
			path := "/v1/iam/hospitals/" + hospital.String() + "/assignment-proposals/" + proposalID.String()
			body, err := json.Marshal(iam.ProposalInput{HospitalID: hospital, Subject: "synthetic-assignment-recipient", TemplateKey: "workforce.leave_reviewer.v1", Reason: "Synthetic reviewed duty", ValidFrom: start, ExpiresAt: start.Add(12 * time.Hour)})
			require.NoError(t, err)
			proposed := governanceRequest(router, http.MethodPut, path, "admin", string(body))
			require.Equal(t, http.StatusOK, proposed.Code, proposed.Body.String())
			waitStatus := func(status string) {
				require.EventuallyWithT(t, func(check *assert.CollectT) {
					read := governanceRequest(router, http.MethodGet, path, "admin", "")
					require.Equal(check, http.StatusOK, read.Code, read.Body.String())
					var result iam.ProposalOutput
					require.NoError(check, json.Unmarshal(read.Body.Bytes(), &result))
					require.Equal(check, status, result.Status)
				}, 20*time.Second, 100*time.Millisecond)
			}
			waitStatus("pending_review")
			gate := &assignmentScopeGate{entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			unblock := func() { release.Do(func() { close(gate.release) }) }
			t.Cleanup(unblock)
			owner.gate.Store(gate)
			decision := `{"expected_version":2,"decision":"approve","reason":"Synthetic independent examination"}`
			accepted := governanceRequest(router, http.MethodPost, path+"/decision", "reviewer", decision)
			require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
			select {
			case <-gate.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the actual native Workforce owner request did not arrive")
			}
			if revoke {
				withdrawn := governanceRequest(router, http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":1,"decision":"revoke","reason":"Withdraw source before the owner answers"}`)
				require.Equal(t, http.StatusAccepted, withdrawn.Code, withdrawn.Body.String())
			}
			unblock()
			owner.gate.Store(nil)
			status, snapshots := "active", int64(1)
			if revoke {
				status, snapshots = "pending_review", 0
			}
			waitStatus(status)
			var evidence struct{ Requests, Snapshots int64 }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
					return err
				}
				if err := tx.Table("iam_assignment_governance_requests").Where("tenant_id=? AND proposal_id=?", testutil.TenantA, proposalID).Count(&evidence.Requests).Error; err != nil {
					return err
				}
				return tx.Table("iam_assignment_snapshots").Where("tenant_id=? AND id=?", testutil.TenantA, proposalID).Count(&evidence.Snapshots).Error
			}))
			require.EqualValues(t, 2, evidence.Requests, "origin and independent decision retain their own immutable source")
			require.Equal(t, snapshots, evidence.Snapshots)
			if revoke {
				denied := governanceRequest(router, http.MethodPost, path+"/decision", "reviewer", decision)
				require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
			} else {
				replay := governanceRequest(router, http.MethodPost, path+"/decision", "reviewer", decision)
				require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
			}
		})
	}
}
