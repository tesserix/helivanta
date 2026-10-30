package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestMembershipCommandRemovesExactRoleAndRetainsReplayReceipt(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	target := uuid.New()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-command-target','staff_member'),(gen_random_uuid(),?,'synthetic-command-target','doctor'),(gen_random_uuid(),?,'user-admin','tenant_admin')`, target, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	command := uuid.Must(uuid.NewV7())
	body := fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":"synthetic-command-target","role_key":"staff_member","expected_membership_epoch":1,"reason":"Remove ordinary membership role after review"}`, target.String())
	path := "/v1/iam/membership-commands/" + command.String()
	first := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"result_membership_epoch":1`)
	replay := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	listed := membershipCommandRequest(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Contains(t, listed.Body.String(), `"role_key":"doctor"`)
	require.NotContains(t, listed.Body.String(), target.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_membership_commands").Where("id=?", command).Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, tx.Table("audit_events").Where("resource_id=? AND action='membership_receipt_read'", command).Count(&count).Error)
		require.EqualValues(t, 1, count, "receipt replay remains an attributable protected read")
		return nil
	}))
	fabricatedID := uuid.Must(uuid.NewV7())
	fabricatedMembership := uuid.New()
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-admin',true),set_config('app.membership_command',?,true)`, fabricatedID.String()).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_membership_commands(tenant_id,id,operation,basis_id,subject,role_key,expected_membership_epoch,reason,result_membership_id,result_membership_epoch,actor_subject) VALUES(?,?,'remove',?,'synthetic-command-target','staff_member',1,'Fabricated removal without transition',?,1,'user-admin')`, testutil.TenantA, fabricatedID, fabricatedMembership, fabricatedMembership).Error
	})
	require.Error(t, err, "immutable command evidence must originate from an actual membership transition")
}

func TestMembershipCommandRejectsUntrustedScopeStateAndAuthority(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "stale-admin": testutil.TenantA, "restricted": testutil.TenantA, "foreign-admin": testutil.TenantB},
		Perms:  map[string][]authz.Permission{"admin": {iam.PermMemberManage}, "stale-admin": {iam.PermMemberManage}, "foreign-admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil)},
	})
	target, owner := uuid.New(), uuid.New()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-denial-target','staff_member'),(?,?,'user-admin','tenant_admin')`, target, testutil.TenantA, owner, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-foreign-admin','tenant_admin')`, testutil.TenantB).Error
	}))
	base := iam.MembershipCommandInput{Operation: "remove", BasisID: target, Subject: "synthetic-denial-target", RoleKey: "staff_member", ExpectedMembershipEpoch: 1, Reason: "Synthetic removal denial check"}
	for _, tc := range []struct {
		name, token                   string
		change                        func(*iam.MembershipCommandInput)
		status                        int
		tenant                        string
		subject                       string
		omitSubject, duplicateSubject bool
	}{
		{name: "blank_reason", token: "admin", change: func(in *iam.MembershipCommandInput) { in.Reason = " " }, status: 400},
		{name: "wrong_incarnation", token: "admin", change: func(in *iam.MembershipCommandInput) { in.BasisID = uuid.New() }, status: 404},
		{name: "wrong_role", token: "admin", change: func(in *iam.MembershipCommandInput) { in.RoleKey = "doctor" }, status: 404},
		{name: "stale_epoch", token: "admin", change: func(in *iam.MembershipCommandInput) { in.ExpectedMembershipEpoch = 2 }, status: 409},
		{name: "removed_administrator", token: "stale-admin", status: 403},
		{name: "restricted_actor", token: "restricted", status: 403},
		{name: "other_tenant", token: "foreign-admin", status: 404},
		{name: "stale_workspace", token: "admin", tenant: testutil.TenantB, status: 409},
		{name: "stale_account", token: "admin", subject: "previous-admin", status: 409},
		{name: "missing_account", token: "admin", omitSubject: true, status: 409},
		{name: "duplicate_account", token: "admin", duplicateSubject: true, status: 409},
		{name: "governance_recovery_required", token: "admin", change: func(in *iam.MembershipCommandInput) {
			in.Subject = "user-admin"
			in.RoleKey = "tenant_admin"
			in.BasisID = owner
		}, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			if tc.change != nil {
				tc.change(&in)
			}
			body, err := json.Marshal(in)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tc.token)
			expectedTenant := testutil.TenantA
			if tc.token == "foreign-admin" {
				expectedTenant = testutil.TenantB
			}
			req.Header.Set("X-Helivanta-Expected-Tenant", expectedTenant)
			if !tc.omitSubject {
				expectedSubject := "user-" + tc.token
				if tc.subject != "" {
					expectedSubject = tc.subject
				}
				req.Header.Set("X-Helivanta-Expected-Subject", expectedSubject)
			}
			if tc.duplicateSubject {
				req.Header.Add("X-Helivanta-Expected-Subject", "user-"+tc.token)
			}
			if tc.tenant != "" {
				req.Header.Set("X-Helivanta-Expected-Tenant", tc.tenant)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, tc.status, res.Code, res.Body.String())
		})
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("id=?", target).Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, tx.Table("iam_membership_commands").Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
}

type membershipIdentity struct{}

func (membershipIdentity) UserState(context.Context, string) (loginclient.UserState, error) {
	return loginclient.UserStateActive, nil
}

type controlledMembershipIdentity struct {
	state       loginclient.UserState
	unavailable bool
}

func (p *controlledMembershipIdentity) UserState(context.Context, string) (loginclient.UserState, error) {
	if p.unavailable {
		return 0, errors.New("synthetic identity outage")
	}
	return p.state, nil
}

func TestMembershipCommandRecoveryRequiresActiveIdentityAndReplaysReceiptAfterLaterDisable(t *testing.T) {
	provider := &controlledMembershipIdentity{state: loginclient.UserStateInactive}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil).SetUserStateChecker(provider)}})
	target := uuid.New()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-inactive-target','staff_member'),(gen_random_uuid(),?,'user-admin','tenant_admin')`, target, testutil.TenantA, testutil.TenantA).Error
	}))
	removeID := uuid.Must(uuid.NewV7())
	input := iam.MembershipCommandInput{Operation: "remove", BasisID: target, Subject: "synthetic-inactive-target", RoleKey: "staff_member", ExpectedMembershipEpoch: 1, Reason: "Remove ordinary membership before recovery checks"}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	removed := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removeID.String(), "admin", string(body))
	require.Equal(t, 202, removed.Code, removed.Body.String())
	input.Operation = "recover"
	input.BasisID = removeID
	input.ExpectedMembershipEpoch = 2
	input.Reason = "Recover ordinary membership after identity-owner validation"
	body, err = json.Marshal(input)
	require.NoError(t, err)
	path := "/v1/iam/membership-commands/" + uuid.Must(uuid.NewV7()).String()
	for _, tc := range []struct {
		name        string
		unavailable bool
		status      int
	}{{"inactive", false, 409}, {"provider_outage", true, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			provider.unavailable = tc.unavailable
			res := membershipCommandRequest(r, http.MethodPut, path, "admin", string(body))
			require.Equal(t, tc.status, res.Code, res.Body.String())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				require.NoError(t, tx.Table("iam_members").Where("subject=?", input.Subject).Count(&count).Error)
				require.Zero(t, count)
				require.NoError(t, tx.Table("iam_membership_commands").Count(&count).Error)
				require.EqualValues(t, 1, count)
				return nil
			}))
		})
	}
	provider.unavailable = false
	provider.state = loginclient.UserStateActive
	success := membershipCommandRequest(r, http.MethodPut, path, "admin", string(body))
	require.Equal(t, 202, success.Code, success.Body.String())
	provider.state = loginclient.UserStateInactive
	provider.unavailable = true
	replay := membershipCommandRequest(r, http.MethodPut, path, "admin", string(body))
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, success.Body.String(), replay.Body.String())
}

func TestMembershipCommandRecoveryCreatesFreshOrdinaryMembershipAndOldReceiptCannotRemoveIt(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil).SetUserStateChecker(membershipIdentity{})},
	})
	target := uuid.New()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-recovery-target','doctor'),(gen_random_uuid(),?,'user-admin','tenant_admin')`, target, testutil.TenantA, testutil.TenantA).Error
	}))
	removeID := uuid.Must(uuid.NewV7())
	removeBody := fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":"synthetic-recovery-target","role_key":"doctor","expected_membership_epoch":1,"reason":"End this role following review"}`, target.String())
	removePath := "/v1/iam/membership-commands/" + removeID.String()
	removed := membershipCommandRequest(r, http.MethodPut, removePath, "admin", removeBody)
	require.Equal(t, http.StatusAccepted, removed.Code, removed.Body.String())
	require.Contains(t, removed.Body.String(), `"result_membership_epoch":2`)
	recoverBody := fmt.Sprintf(`{"operation":"recover","basis_id":%q,"subject":"synthetic-recovery-target","role_key":"staff_member","expected_membership_epoch":2,"reason":"Recover ordinary membership after verifying active account"}`, removeID.String())
	recoverPath := "/v1/iam/membership-commands/" + uuid.Must(uuid.NewV7()).String()
	recovered := membershipCommandRequest(r, http.MethodPut, recoverPath, "admin", recoverBody)
	require.Equal(t, http.StatusAccepted, recovered.Code, recovered.Body.String())
	var result iam.MembershipCommand
	require.NoError(t, json.Unmarshal(recovered.Body.Bytes(), &result))
	require.NotEqual(t, target, result.ResultMembershipID)
	require.Equal(t, "staff_member", result.RoleKey)
	replay := membershipCommandRequest(r, http.MethodPut, removePath, "admin", removeBody)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, removed.Body.String(), replay.Body.String())
	recoveryReplay := membershipCommandRequest(r, http.MethodPut, recoverPath, "admin", recoverBody)
	require.Equal(t, http.StatusAccepted, recoveryReplay.Code, recoveryReplay.Body.String())
	require.JSONEq(t, recovered.Body.String(), recoveryReplay.Body.String())
	listed := membershipCommandRequest(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Contains(t, listed.Body.String(), result.ResultMembershipID.String())
	require.NotContains(t, listed.Body.String(), `"role_key":"doctor"`)
	candidates := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-candidates?limit=1", "admin", "")
	require.Equal(t, http.StatusOK, candidates.Code, candidates.Body.String())
	var firstPage struct {
		Data []iam.MembershipCandidate
		Page struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(candidates.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Data, 1)
	require.True(t, firstPage.Page.HasMore)
	next := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-candidates?limit=1&cursor="+url.QueryEscape(firstPage.Page.NextCursor), "admin", "")
	require.Equal(t, http.StatusOK, next.Code, next.Body.String())
	var nextPage struct {
		Data []iam.MembershipCandidate
		Page struct {
			HasMore bool `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &nextPage))
	require.Len(t, nextPage.Data, 1)
	require.False(t, nextPage.Page.HasMore)
	require.NotEqual(t, firstPage.Data[0].ID, nextPage.Data[0].ID)
	var recoveredCandidate *iam.MembershipCandidate
	for _, candidate := range append(firstPage.Data, nextPage.Data...) {
		if candidate.ID == result.ResultMembershipID {
			copy := candidate
			recoveredCandidate = &copy
		}
	}
	require.NotNil(t, recoveredCandidate)
	require.EqualValues(t, 2, recoveredCandidate.MembershipEpoch)
	history := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-commands", "admin", "")
	require.Equal(t, http.StatusOK, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), removeID.String())
	require.Contains(t, history.Body.String(), result.ID.String())
}

func membershipCommandRequest(r http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	tenant := testutil.TenantA
	if token == "foreign-admin" {
		tenant = testutil.TenantB
	}
	req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

type membershipOutboxFailureModule struct{ *iam.Module }

func (m membershipOutboxFailureModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9000_iam_test_membership_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT synthetic_membership_outbox_failure CHECK(false) NOT VALID;`})
}

func TestMembershipCommandOutboxFailureRollsBackMembershipReceiptEpochAndAudit(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{membershipOutboxFailureModule{iam.New(nil)}}})
	target := uuid.New()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-rollback-target','staff_member'),(gen_random_uuid(),?,'user-admin','tenant_admin')`, target, testutil.TenantA, testutil.TenantA).Error
	}))
	command := uuid.Must(uuid.NewV7())
	input := iam.MembershipCommandInput{Operation: "remove", BasisID: target, Subject: "synthetic-rollback-target", RoleKey: "staff_member", ExpectedMembershipEpoch: 1, Reason: "Synthetic rollback verification"}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	res := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+command.String(), "admin", string(body))
	require.Equal(t, 500, res.Code, res.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("id=?", target).Count(&count).Error)
		require.EqualValues(t, 1, count)
		for _, table := range []string{"iam_membership_commands", "iam_membership_revocations"} {
			require.NoError(t, tx.Table(table).Where("subject=?", input.Subject).Count(&count).Error)
			require.Zero(t, count, table)
		}
		var epoch struct {
			Epoch       int64
			MemberCount int64
		}
		require.NoError(t, tx.Table("iam_subject_membership_epochs").Where("subject=?", input.Subject).Take(&epoch).Error)
		require.EqualValues(t, 1, epoch.Epoch)
		require.EqualValues(t, 1, epoch.MemberCount)
		require.NoError(t, tx.Table("audit_events").Where("resource_id IN ?", []uuid.UUID{target, command}).Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
}
