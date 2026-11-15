package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/organization"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
)

type governanceAssignmentIdentityReader func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error)

func (f governanceAssignmentIdentityReader) AssignmentCandidateIdentity(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
	return f(ctx, tenant, subject)
}

func TestGovernanceAssignmentProposalAdmissionUsesExactHospitalWithoutLegacyManager(t *testing.T) {
	native := &governanceNativeIdentity{}
	var identityRead governanceAssignmentIdentityReader
	identity := governanceAssignmentIdentityReader(func(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		return identityRead(ctx, tenant, subject)
	})
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "auditor": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, iam.PermAssignmentManage, iam.PermAssignmentReview, iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview},
			"reviewer": {iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview},
			"auditor":  {iam.PermAssignmentManage, iam.PermAssignmentReview, iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native).SetAssignmentIdentityReader(identity), workforce.New()},
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

	discovered := governanceRequest(router, http.MethodGet, "/v1/workforce/assignment-authority-scopes?limit=20", "reviewer", "")
	require.Equal(t, http.StatusOK, discovered.Code, discovered.Body.String())
	var choices struct {
		Data []struct {
			HospitalID uuid.UUID `json:"hospital_id"`
			Name       string    `json:"name"`
			CanManage  bool      `json:"can_manage"`
			CanReview  bool      `json:"can_review"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(discovered.Body.Bytes(), &choices))
	require.Len(t, choices.Data, 1)
	require.Equal(t, hospital, choices.Data[0].HospitalID)
	require.Equal(t, "Synthetic assignment governance hospital", choices.Data[0].Name)
	require.True(t, choices.Data[0].CanManage)
	require.True(t, choices.Data[0].CanReview)
	hidden := governanceRequest(router, http.MethodGet, "/v1/workforce/assignment-authority-scopes?limit=20", "auditor", "")
	require.Equal(t, http.StatusOK, hidden.Code, hidden.Body.String())
	choices.Data = nil
	require.NoError(t, json.Unmarshal(hidden.Body.Bytes(), &choices))
	require.Empty(t, choices.Data, "coarse permission alone cannot disclose a hospital choice")

	catalogRoot := "/v1/iam/hospitals/" + hospital.String() + "/assignment-templates"
	catalog := governanceRequest(router, http.MethodGet, catalogRoot, "reviewer", "")
	require.Equal(t, http.StatusOK, catalog.Code, catalog.Body.String())
	require.Equal(t, "no-store", catalog.Header().Get("Cache-Control"))
	var definitions []iam.TemplateDefinition
	require.NoError(t, json.Unmarshal(catalog.Body.Bytes(), &definitions))
	require.NotEmpty(t, definitions)
	foundIntake := false
	for _, definition := range definitions {
		require.Contains(t, definition.EstablishmentKinds, "hospital")
		if definition.Key == "workforce.hr_intake.v1" {
			foundIntake = true
			require.Equal(t, "HR intake administrator", definition.Name)
			require.ElementsMatch(t, []string{"workforce.enrollment.manage", "workforce.documents.manage"}, definition.Permissions)
		}
	}
	require.True(t, foundIntake)
	forbiddenCatalog := governanceRequest(router, http.MethodGet, catalogRoot, "auditor", "")
	require.Equal(t, http.StatusNotFound, forbiddenCatalog.Code, forbiddenCatalog.Body.String())
	wrongCatalog := governanceRequest(router, http.MethodGet, "/v1/iam/hospitals/"+uuid.Must(uuid.NewV7()).String()+"/assignment-templates", "reviewer", "")
	require.Equal(t, http.StatusNotFound, wrongCatalog.Code, wrongCatalog.Body.String())
	legacyCatalog := governanceRequest(router, http.MethodGet, "/v1/iam/staff-access-templates", "reviewer", "")
	require.Equal(t, http.StatusForbidden, legacyCatalog.Code, legacyCatalog.Body.String())
	badQuery := governanceRequest(router, http.MethodGet, catalogRoot+"?catalog=legacy-v1", "reviewer", "")
	require.Equal(t, http.StatusBadRequest, badQuery.Code, badQuery.Body.String())
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
	denied = governanceRequest(router, http.MethodPut, legacy, "auditor", string(body))
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
	reads := 0
	identityRead = func(_ context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		reads++
		require.Equal(t, testutil.TenantA, tenant)
		require.Equal(t, retained.Input.Subject, subject)
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Synthetic proposal recipient", Email: "synthetic-recipient@example.invalid"}, nil
	}
	accountPath := path + "/account"
	account := governanceRequest(router, http.MethodGet, accountPath, "reviewer", "")
	require.Equal(t, http.StatusOK, account.Code, account.Body.String())
	require.Equal(t, "no-store", account.Header().Get("Cache-Control"))
	var presentation iam.AssignmentAccountPresentation
	require.NoError(t, json.Unmarshal(account.Body.Bytes(), &presentation))
	require.True(t, presentation.Available)
	require.Equal(t, retained.Version, presentation.ProposalVersion)
	require.Equal(t, retained.Input.Subject, presentation.Subject)
	require.Equal(t, "Synthetic proposal recipient", *presentation.Name)
	require.Equal(t, "synthetic-recipient@example.invalid", *presentation.Email)
	require.Equal(t, 1, reads)
	outside := governanceRequest(router, http.MethodGet, accountPath, "auditor", "")
	require.Equal(t, http.StatusNotFound, outside.Code, outside.Body.String())
	outside = governanceRequest(router, http.MethodGet, wrongRoot+"/account", "reviewer", "")
	require.Equal(t, http.StatusNotFound, outside.Code, outside.Body.String())
	outside = governanceRequest(router, http.MethodGet, accountPath+"?subject=user-auditor", "reviewer", "")
	require.Equal(t, http.StatusBadRequest, outside.Code, outside.Body.String())
	require.Equal(t, 1, reads, "unauthorized, wrong-resource and query-forged reads cannot query native identity")
	independentGrant := grant
	independentGrant.Subject = "user-auditor"
	independentBody, err := json.Marshal(independentGrant)
	require.NoError(t, err)
	independent := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+uuid.Must(uuid.NewV7()).String(), "admin", string(independentBody))
	require.Equal(t, http.StatusAccepted, independent.Code, independent.Body.String())
	independentRead := governanceRequest(router, http.MethodGet, accountPath, "auditor", "")
	require.Equal(t, http.StatusOK, independentRead.Code, independentRead.Body.String())
	require.Contains(t, independentRead.Body.String(), "Synthetic proposal recipient")
	require.Equal(t, 2, reads, "an independently sourced reviewer can inspect the same exact proposal account")

	identityRead = func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		return nil, nil
	}
	absent := governanceRequest(router, http.MethodGet, accountPath, "reviewer", "")
	require.Equal(t, http.StatusOK, absent.Code, absent.Body.String())
	require.NotContains(t, absent.Body.String(), `"name"`)
	require.NotContains(t, absent.Body.String(), `"email"`)
	require.Contains(t, absent.Body.String(), `"available":false`)
	identityRead = func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		return nil, errors.New("synthetic-recipient@example.invalid private provider failure")
	}
	failed := governanceRequest(router, http.MethodGet, accountPath, "reviewer", "")
	require.Equal(t, http.StatusServiceUnavailable, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic-recipient@example.invalid")

	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'synthetic-target','staff_member')`, testutil.TenantA).Error
	}))
	targetIntent := retained.Input
	targetIntent.Subject = "synthetic-target"
	targetBody, err := json.Marshal(targetIntent)
	require.NoError(t, err)
	targetPath := root + "/" + uuid.Must(uuid.NewV7()).String()
	targetCreated := governanceRequest(router, http.MethodPut, targetPath, "reviewer", string(targetBody))
	require.Equal(t, http.StatusOK, targetCreated.Code, targetCreated.Body.String())
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		response := governanceRequest(router, http.MethodGet, targetPath, "reviewer", "")
		require.Equal(check, http.StatusOK, response.Code, response.Body.String())
		var row iam.ProposalOutput
		require.NoError(check, json.Unmarshal(response.Body.Bytes(), &row))
		require.Equal(check, "pending_review", row.Status)
	}, 20*time.Second, 100*time.Millisecond)
	identityRead = func(ctx context.Context, _, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		require.Equal(t, "synthetic-target", subject)
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=?`, testutil.TenantA, subject).Error
		}))
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Removed target must not disclose", Email: "removed-target@example.invalid"}, nil
	}
	removed := governanceRequest(router, http.MethodGet, targetPath+"/account", "reviewer", "")
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	require.Contains(t, removed.Body.String(), `"available":false`)
	require.NotContains(t, removed.Body.String(), "Removed target")
	require.NotContains(t, removed.Body.String(), "removed-target@example.invalid")
	history := governanceRequest(router, http.MethodGet, targetPath+"/history", "reviewer", "")
	require.Equal(t, http.StatusOK, history.Code, history.Body.String())

	identityRead = func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		changed := governanceRequest(router, http.MethodPost, path+"/decision", "reviewer", `{"expected_version":2,"decision":"withdraw","reason":"Withdraw while account presentation is in flight"}`)
		require.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Changed proposal must not disclose", Email: "changed-proposal@example.invalid"}, nil
	}
	changedAccount := governanceRequest(router, http.MethodGet, accountPath, "reviewer", "")
	require.Equal(t, http.StatusConflict, changedAccount.Code, changedAccount.Body.String())
	require.NotContains(t, changedAccount.Body.String(), "Changed proposal")
	require.NotContains(t, changedAccount.Body.String(), "changed-proposal@example.invalid")
	identityRead = func(ctx context.Context, _, _ string) (*customercontract.AssignmentCandidateIdentity, error) {
		withdrawn := governanceRequest(router, http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":1,"decision":"revoke","reason":"Withdraw authority while native presentation is in flight"}`)
		require.Equal(t, http.StatusAccepted, withdrawn.Code, withdrawn.Body.String())
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Must not disclose", Email: "must-not-disclose@example.invalid"}, nil
	}
	revoked := governanceRequest(router, http.MethodGet, accountPath, "reviewer", "")
	require.Equal(t, http.StatusNotFound, revoked.Code, revoked.Body.String())
	require.NotContains(t, revoked.Body.String(), "Must not disclose")
	require.NotContains(t, revoked.Body.String(), "must-not-disclose@example.invalid")

}

func TestGovernanceAssignmentDepartmentRetirementCannotUseRetainedOwnerApproval(t *testing.T) {
	owner := &assignmentScopeBarrier{Module: workforce.New()}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms: map[string][]authz.Permission{"admin": {
			workforce.PermHospitalManage, iam.PermAssignmentManage, iam.PermAssignmentReview, iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview, organization.PermStructureManage,
		}},
		Modules: []platform.Module{iam.New(nil), owner, organization.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'user-admin','tenant_admin'),(?,'synthetic-department-recipient','staff_member')`, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital, department := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	created := governanceRequest(router, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic department source hospital"}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	organizationRoot := "/v1/organization/hospitals/" + hospital.String()
	registered := governanceRequest(router, http.MethodPut, organizationRoot+"/registrations/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"reason":"Synthetic structural source reference"}`)
	require.Equal(t, http.StatusOK, registered.Code, registered.Body.String())
	departmentRoot := organizationRoot + "/departments/" + department.String() + "/revisions/"
	createRevision := departmentRoot + uuid.Must(uuid.NewV7()).String()
	active := `{"expected_version":0,"code":"SYN-GOV-ACCESS","name":"Synthetic access review department","kind":"department","parent_id":null,"status":"active","reason":"Synthetic structural setup"}`
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		response := governanceRequest(router, http.MethodPut, createRevision, "admin", active)
		require.Equal(check, http.StatusOK, response.Code, response.Body.String())
	}, 20*time.Second, 100*time.Millisecond)

	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	intent := iam.ProposalInput{HospitalID: hospital, Subject: "synthetic-department-recipient", TemplateKey: "workforce.department_posting_reviewer.v1", DepartmentID: &department, Reason: "Synthetic department assignment review", ValidFrom: start, ExpiresAt: start.Add(12 * time.Hour)}
	activeBody, err := json.Marshal(intent)
	require.NoError(t, err)
	activePath := "/v1/iam/hospitals/" + hospital.String() + "/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	activeScope := governanceRequest(router, http.MethodPut, activePath, "admin", string(activeBody))
	require.Equal(t, http.StatusOK, activeScope.Code, activeScope.Body.String())
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		read := governanceRequest(router, http.MethodGet, activePath, "admin", "")
		require.Equal(check, http.StatusOK, read.Code, read.Body.String())
		var result iam.ProposalOutput
		require.NoError(check, json.Unmarshal(read.Body.Bytes(), &result))
		require.Equal(check, "pending_review", result.Status, "current owned department paths remain admissible")
	}, 20*time.Second, 100*time.Millisecond)
	gate := &assignmentScopeGate{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	t.Cleanup(unblock)
	owner.gate.Store(gate)
	proposalID := uuid.Must(uuid.NewV7())
	path := "/v1/iam/hospitals/" + hospital.String() + "/assignment-proposals/" + proposalID.String()
	body, err := json.Marshal(intent)
	require.NoError(t, err)
	proposed := governanceRequest(router, http.MethodPut, path, "admin", string(body))
	require.Equal(t, http.StatusOK, proposed.Code, proposed.Body.String())
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the native Workforce request did not reach its controlled boundary")
	}
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		var count int64
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_assignment_department_verifications").Where("tenant_id=? AND proposal_id=? AND owner='organization' AND eligible", testutil.TenantA, proposalID).Count(&count).Error
		})
		require.NoError(check, err)
		require.EqualValues(check, 1, count, "Organization must have actually confirmed the then-active department")
	}, 20*time.Second, 100*time.Millisecond)
	retired := governanceRequest(router, http.MethodPut, departmentRoot+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":1,"code":"SYN-GOV-ACCESS","name":"Synthetic access review department","kind":"department","parent_id":null,"status":"retired","reason":"Retire after the first owner answer"}`)
	require.Equal(t, http.StatusOK, retired.Code, retired.Body.String())
	before := governanceRequest(router, http.MethodGet, path, "admin", "")
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	var pending iam.ProposalOutput
	require.NoError(t, json.Unmarshal(before.Body.Bytes(), &pending))
	require.Equal(t, "pending_scope", pending.Status, "the remaining owner answer is still held")
	unblock()
	owner.gate.Store(nil)
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		read := governanceRequest(router, http.MethodGet, path, "admin", "")
		require.Equal(check, http.StatusOK, read.Code, read.Body.String())
		var result iam.ProposalOutput
		require.NoError(check, json.Unmarshal(read.Body.Bytes(), &result))
		require.Equal(check, "scope_rejected", result.Status, "retained owner approval cannot replace a current structural source")
	}, 20*time.Second, 100*time.Millisecond)
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
			"admin":    {iam.PermGovernanceManage, iam.PermGovernanceReview, workforce.PermHospitalManage, iam.PermAssignmentManage, iam.PermAssignmentReview, iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview},
			"reviewer": {iam.PermGovernanceAssignmentManage, iam.PermGovernanceAssignmentReview},
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
