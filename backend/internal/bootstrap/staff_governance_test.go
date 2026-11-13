package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

type governanceNativeIdentity struct {
	calls       atomic.Int64
	before      func(context.Context) error
	unavailable bool
}

func (native *governanceNativeIdentity) MembershipEligible(ctx context.Context, tenant, subject string) (bool, error) {
	native.calls.Add(1)
	if native.before != nil {
		if err := native.before(ctx); err != nil {
			return false, err
		}
	}
	if native.unavailable {
		return false, errors.New("synthetic identity owner unavailable")
	}
	return tenant == testutil.TenantA && (subject == "user-reviewer" || subject == "user-owner2" || subject == "user-auditor"), nil
}

func governanceRequest(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}

func TestGovernanceSingleOwnerGrantRetainsIntentAndNotifiesEveryOwner(t *testing.T) {
	native := &governanceNativeIdentity{}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {"iam.governance.manage", "iam.governance.read", workforce.PermHospitalManage}},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member')`, testutil.TenantA, testutil.TenantA).Error
	}))
	native.before = func(nativeCtx context.Context) error {
		return db.WithTenant(nativeCtx, testutil.TenantA, func(tx *gorm.DB) error {
			var available bool
			if err := tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0))`, "iam.governance_owners:"+testutil.TenantA).Scan(&available).Error; err != nil {
				return err
			}
			require.True(t, available, "the external identity call runs after owner/source database locks are released")
			return nil
		})
	}
	hospital := uuid.Must(uuid.NewV7())
	created := governanceRequest(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic governance hospital"}`)
	require.Equal(t, 200, created.Code, created.Body.String())
	id := uuid.Must(uuid.NewV7())
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	input := iam.GovernanceGrantInput{Subject: "user-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital, Reason: "Delegate independent Staff link review", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour)}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	path := "/v1/iam/governance-grants/" + id.String()
	first := governanceRequest(r, http.MethodPut, path, "admin", string(raw))
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var receipt iam.GovernanceGrantReceipt
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &receipt))
	require.Equal(t, id, receipt.ID)
	require.Equal(t, testutil.TenantA, receipt.TenantID.String())
	require.Equal(t, "provisional_active", receipt.Status)
	require.Equal(t, "single_owner_provisional", receipt.AuthorizationMode)
	require.EqualValues(t, 1, receipt.Version)
	require.EqualValues(t, 1, receipt.OwnerCount)
	require.Equal(t, "user-admin", receipt.IssuedBy)
	require.Equal(t, input.Subject, receipt.Input.Subject)
	replay := governanceRequest(r, http.MethodPut, path, "admin", string(raw))
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.EqualValues(t, 1, native.calls.Load(), "retained command replay must not depend on another native observation")
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	changed := strings.Replace(string(raw), input.Reason, "A different delegation intent", 1)
	conflict := governanceRequest(r, http.MethodPut, path, "admin", changed)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	notices := governanceRequest(r, http.MethodGet, "/v1/iam/governance-notices", "admin", "")
	require.Equal(t, http.StatusOK, notices.Code, notices.Body.String())
	var inbox struct {
		Data []iam.GovernanceOwnerNotice `json:"data"`
	}
	require.NoError(t, json.Unmarshal(notices.Body.Bytes(), &inbox))
	require.Len(t, inbox.Data, 1)
	require.Equal(t, id, inbox.Data[0].GrantID)
	require.Equal(t, "user-admin", inbox.Data[0].RecipientSubject)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", "helivanta.in.iam.member_granted.v1").Count(&count).Error)
		require.EqualValues(t, 1, count, "routing projection intent commits with source, notice and audit")
		require.NoError(t, tx.Table("audit_events").Where("resource_type='iam_governance_grants' AND resource_id=? AND action=?", id, "iam.governance.granted").Count(&count).Error)
		require.EqualValues(t, 1, count, "replay cannot produce a second grant or grant audit")
		return nil
	}))
	t.Logf("Retained grant %s and owner notice %s; recipient login and operating access are separate acceptance gates", id, inbox.Data[0].ID)
}

func TestGovernanceOwnerGrantRefusesStaleAuthorityAndSessionChanges(t *testing.T) {
	native := &governanceNativeIdentity{}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "stale": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin": {"iam.governance.manage", "iam.governance.read", iam.PermMemberManage, workforce.PermHospitalManage},
			"stale": {"iam.governance.manage", "iam.governance.read"},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'user-stale','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	root := governanceRequest(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic grant boundary hospital"}`)
	require.Equal(t, 200, root.Code, root.Body.String())
	input := iam.GovernanceGrantInput{Subject: "user-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital, Reason: "Separate HR link review", ValidFrom: time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	encode := func(value iam.GovernanceGrantInput) string {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return string(raw)
	}
	path := "/v1/iam/governance-grants/" + uuid.Must(uuid.NewV7()).String()
	stale := governanceRequest(r, http.MethodPut, path, "stale", encode(input))
	require.Equal(t, 403, stale.Code, stale.Body.String())
	require.EqualValues(t, 0, native.calls.Load(), "stale FGA authority must be rejected before native identity I/O")
	self := input
	self.Subject = "user-admin"
	rejected := governanceRequest(r, http.MethodPut, path, "admin", encode(self))
	require.Equal(t, 403, rejected.Code, rejected.Body.String())
	for _, headers := range []map[string][]string{
		{"X-Helivanta-Expected-Subject": {"user-stale"}},
		{"X-Helivanta-Expected-Subject": {"user-admin", "user-admin"}},
		{"X-Helivanta-Expected-Tenant": {testutil.TenantB}},
	} {
		request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(encode(input)))
		request.Header.Set("Authorization", "Bearer admin")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		for key, values := range headers {
			request.Header[key] = values
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		require.Equal(t, 409, response.Code, response.Body.String())
	}
	bypass := governanceRequest(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"user-reviewer","role_key":"governance_delegate"}`)
	require.Equal(t, 409, bypass.Code, bypass.Body.String())
	require.Contains(t, bypass.Body.String(), "governance_grant_required")
	require.EqualValues(t, 0, native.calls.Load())
	deniedInbox := governanceRequest(r, http.MethodGet, "/v1/iam/governance-notices", "stale", "")
	require.Equal(t, 403, deniedInbox.Code, deniedInbox.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("role_key='governance_delegate'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Zero(t, count, "refused commands cannot emit a routing grant")
		return nil
	}))
}

func TestGovernanceIndependentAuditorReviewsAnotherGrantAndRetainsBothReceipts(t *testing.T) {
	native := &governanceNativeIdentity{}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "auditor": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":   {"iam.governance.manage", "iam.governance.read", "iam.governance.review", workforce.PermHospitalManage},
			"auditor": {"iam.governance.read", "iam.governance.review"},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'user-auditor','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	created := governanceRequest(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic independent review hospital"}`)
	require.Equal(t, 200, created.Code, created.Body.String())
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	makeGrant := func(subject, key, kind string, scope *uuid.UUID) (uuid.UUID, string, *httptest.ResponseRecorder) {
		id := uuid.Must(uuid.NewV7())
		raw, err := json.Marshal(iam.GovernanceGrantInput{Subject: subject, ExpectedMembershipEpoch: 1, TemplateKey: key, ScopeKind: kind, ScopeID: scope, Reason: "Synthetic independent nonclinical review", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour)})
		require.NoError(t, err)
		response := governanceRequest(r, http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", string(raw))
		require.Equal(t, 202, response.Code, response.Body.String())
		return id, string(raw), response
	}
	grant, grantBody, original := makeGrant("user-reviewer", "workforce.account_link_reviewer.v1", "hospital", &hospital)
	auditor, _, _ := makeGrant("user-auditor", "iam.governance_auditor.v1", "tenant", nil)
	decision := `{"expected_version":1,"decision":"approve","reason":"Independent synthetic source examined"}`
	path := func(parent, command uuid.UUID) string {
		return "/v1/iam/governance-grants/" + parent.String() + "/decisions/" + command.String()
	}
	issuer := governanceRequest(r, http.MethodPut, path(grant, uuid.Must(uuid.NewV7())), "admin", decision)
	require.Equal(t, 403, issuer.Code, issuer.Body.String(), "the issuer cannot call their own decision independent")
	self := governanceRequest(r, http.MethodPut, path(auditor, uuid.Must(uuid.NewV7())), "auditor", decision)
	require.Equal(t, 403, self.Code, self.Body.String(), "the recipient cannot approve their own auditor grant")
	command := uuid.Must(uuid.NewV7())
	approved := governanceRequest(r, http.MethodPut, path(grant, command), "auditor", decision)
	require.Equal(t, 202, approved.Code, approved.Body.String())
	var receipt iam.GovernanceGrantDecisionReceipt
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &receipt))
	require.Equal(t, command, receipt.ID)
	require.Equal(t, grant, receipt.GrantID)
	require.Equal(t, "user-auditor", receipt.ActorSubject)
	require.EqualValues(t, 2, receipt.ResultVersion)
	head := governanceRequest(r, http.MethodGet, "/v1/iam/governance-grants/"+grant.String(), "auditor", "")
	require.Equal(t, 200, head.Code, head.Body.String())
	var current iam.GovernanceGrantReceipt
	require.NoError(t, json.Unmarshal(head.Body.Bytes(), &current))
	require.Equal(t, "active", current.Status)
	require.EqualValues(t, 2, current.Version)
	revoked := governanceRequest(r, http.MethodPut, path(grant, uuid.Must(uuid.NewV7())), "admin", `{"expected_version":2,"decision":"revoke","reason":"Withdraw synthetic review authority"}`)
	require.Equal(t, 202, revoked.Code, revoked.Body.String())
	replay := governanceRequest(r, http.MethodPut, path(grant, command), "auditor", decision)
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, approved.Body.String(), replay.Body.String(), "decision replay returns the retained decision after later revocation")
	initial := governanceRequest(r, http.MethodPut, "/v1/iam/governance-grants/"+grant.String(), "admin", grantBody)
	require.Equal(t, 202, initial.Code, initial.Body.String())
	require.JSONEq(t, original.Body.String(), initial.Body.String(), "creation replay cannot masquerade as the later current head")

	changed := governanceRequest(r, http.MethodPut, path(grant, command), "auditor", strings.Replace(decision, "Independent synthetic source examined", "Changed command reason", 1))
	require.Equal(t, 409, changed.Code, changed.Body.String())
	stale := governanceRequest(r, http.MethodPut, path(grant, uuid.Must(uuid.NewV7())), "auditor", decision)
	require.Equal(t, 409, stale.Code, stale.Body.String())
	inbox := governanceRequest(r, http.MethodGet, "/v1/iam/governance-notices", "admin", "")
	require.Equal(t, 200, inbox.Code, inbox.Body.String())
	var notices struct {
		Data []iam.GovernanceOwnerNotice `json:"data"`
	}
	require.NoError(t, json.Unmarshal(inbox.Body.Bytes(), &notices))
	require.Len(t, notices.Data, 4, "two grants, approval and revocation each notify the current owner exactly once")
	privateInbox := governanceRequest(r, http.MethodGet, "/v1/iam/governance-notices", "auditor", "")
	require.Equal(t, 403, privateInbox.Code, privateInbox.Body.String())

	pending, _, _ := makeGrant("user-reviewer", "workforce.account_link_reviewer.v1", "hospital", &hospital)
	native.unavailable = true
	unavailable := governanceRequest(r, http.MethodPut, path(pending, uuid.Must(uuid.NewV7())), "auditor", decision)
	require.Equal(t, 503, unavailable.Code, unavailable.Body.String())
	native.unavailable = false
	unchanged := governanceRequest(r, http.MethodGet, "/v1/iam/governance-grants/"+pending.String(), "admin", "")
	require.Equal(t, 200, unchanged.Code, unchanged.Body.String())
	require.NoError(t, json.Unmarshal(unchanged.Body.Bytes(), &current))
	require.EqualValues(t, 1, current.Version)
	require.Equal(t, "provisional_active", current.Status)

	// A different retained grant keeps coarse routing membership after auditor revocation.
	makeGrant("user-auditor", "workforce.account_link_reviewer.v1", "hospital", &hospital)
	var revokedSource atomic.Bool
	native.before = func(context.Context) error {
		if revokedSource.CompareAndSwap(false, true) {
			withdrawn := governanceRequest(r, http.MethodPut, path(auditor, uuid.Must(uuid.NewV7())), "admin", `{"expected_version":1,"decision":"revoke","reason":"Withdraw auditor between preflight and commit"}`)
			require.Equal(t, 202, withdrawn.Code, withdrawn.Body.String())
		}
		return nil
	}
	lostAuthority := governanceRequest(r, http.MethodPut, path(pending, uuid.Must(uuid.NewV7())), "auditor", decision)
	require.True(t, revokedSource.Load(), "the independent authority changes while native eligibility runs outside locks")
	require.Equal(t, 403, lostAuthority.Code, lostAuthority.Body.String())
	native.before = nil
	unchanged = governanceRequest(r, http.MethodGet, "/v1/iam/governance-grants/"+pending.String(), "admin", "")
	require.Equal(t, 200, unchanged.Code, unchanged.Body.String())
	require.NoError(t, json.Unmarshal(unchanged.Body.Bytes(), &current))
	require.EqualValues(t, 1, current.Version, "stale route permission cannot commit the pending approval")
	require.Equal(t, "provisional_active", current.Status)
}
