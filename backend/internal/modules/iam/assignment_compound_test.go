package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises the public IAM owner.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestCompoundAssignmentCommand(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga,
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "target": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, iam.PermAssignmentManage},
			"reviewer": {iam.PermAssignmentReview},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(membershipIdentity{}), assignmentWorkforceModule()},
	})
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	call("PUT", base, "admin", `{"name":"Synthetic compound authority QA"}`, 200)
	for _, actor := range []string{"admin", "reviewer"} {
		call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"tenant_admin"}`, "user-"+actor), 202)
		call("PUT", base+"/access/user-"+actor, "admin", `{"level":"manager"}`, 200)
	}
	call("POST", "/v1/iam/members", "admin", `{"subject":"user-target","role_key":"staff_member"}`, 202)
	authority := iam.NewScopedAuthorizer(db, fga)
	guard, ok := authority.(authz.CompoundScopedCommandGuard)
	require.True(t, ok)
	propose := func(template string, expires time.Time) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7()).String()
		path := "/v1/iam/assignment-proposals/" + id
		body := fmt.Sprintf(`{"hospital_id":%q,"subject":"user-target","template_key":%q,"reason":"Synthetic compound command verification","valid_from":%q,"expires_at":%q}`, hospital, template, time.Now().UTC().Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
		call("PUT", path, "admin", body, 200)
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var proposal iam.ProposalOutput
			w := call("GET", path, "admin", "", 200)
			require.NoError(c, json.Unmarshal(w.Body.Bytes(), &proposal))
			require.Equal(c, "pending_review", proposal.Status)
		}, 20*time.Second, 50*time.Millisecond)
		call("POST", path+"/decision", "reviewer", `{"expected_version":2,"decision":"approve","reason":"Independent synthetic review"}`, 200)
		return id
	}
	checks := make([]authz.ScopeCheck, 0, 2)
	for _, role := range []struct {
		template   string
		permission authz.Permission
	}{
		{"workforce.identity_manager.v1", "workforce.identity_checks.manage"},
		{"workforce.identity_reviewer.v1", "workforce.identity_checks.review"},
	} {
		propose(role.template, time.Now().UTC().Add(time.Hour))
		checks = append(checks, authz.ScopeCheck{Request: authz.ScopeRequest{TenantID: testutil.TenantA, Subject: "user-target", HospitalID: hospital, Permission: role.permission}})
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		for i := range checks {
			grant, err := authority.AuthorizeScope(t.Context(), checks[i].Request)
			require.NoError(c, err)
			require.NotEmpty(c, grant.AssignmentID)
			checks[i].Grant = grant
		}
		require.Equal(c, checks[0].Grant.ScopeEpoch, checks[1].Grant.ScopeEpoch)
		require.Equal(c, checks[0].Grant.MembershipEpoch, checks[1].Grant.MembershipEpoch)
	}, 20*time.Second, 50*time.Millisecond)
	ctx := tenantdb.WithActor(t.Context(), "user-target")
	authTime := time.Now().UTC()
	slices.Reverse(checks)
	original := slices.Clone(checks)
	write := func(ctx context.Context) error {
		return db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'user-target','compound_scope_write','iam_assignment_snapshots',?)`, testutil.TenantA, checks[0].Grant.AssignmentID).Error
		})
	}
	require.NoError(t, guard.WithScopeGrants(ctx, checks, authTime, write))
	require.Equal(t, original, checks, "stable lock ordering must not mutate caller evidence")
	countWrites := func() int64 {
		t.Helper()
		var count int64
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("audit_events").Where("action='compound_scope_write'").Count(&count).Error
		}))
		return count
	}
	require.EqualValues(t, 1, countWrites())
	failure := errors.New("synthetic owner failure")
	err = guard.WithScopeGrants(ctx, checks, authTime, func(ctx context.Context) error {
		if err := write(ctx); err != nil {
			return err
		}
		return failure
	})
	require.ErrorIs(t, err, failure)
	require.EqualValues(t, 1, countWrites(), "both permissions and the owner write share rollback")
	for _, tc := range []struct {
		name  string
		alter func([]authz.ScopeCheck) []authz.ScopeCheck
	}{
		{"empty", func(v []authz.ScopeCheck) []authz.ScopeCheck { return nil }},
		{"too_many", func(v []authz.ScopeCheck) []authz.ScopeCheck { return append(v, v[0]) }},
		{"duplicate_permission", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1] = v[0]; return v }},
		{"foreign_tenant", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Request.TenantID = testutil.TenantB; return v }},
		{"foreign_hospital", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Request.HospitalID = uuid.NewString(); return v }},
		{"foreign_subject", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Request.Subject = "user-other"; return v }},
		{"different_membership", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Grant.MembershipEpoch++; return v }},
		{"different_scope_epoch", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Grant.ScopeEpoch++; return v }},
		{"missing_receipt", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Grant = authz.ScopeGrant{}; return v }},
		{"conflicting_snapshot", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Grant = v[0].Grant; v[1].Grant.Version++; return v }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := guard.WithScopeGrants(ctx, tc.alter(slices.Clone(checks)), authTime, func(context.Context) error { called = true; return nil })
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.False(t, called)
		})
	}
	missingPermission := slices.Clone(checks)
	missingPermission[1].Grant = checks[0].Grant
	called := false
	err = guard.WithScopeGrants(ctx, missingPermission, authTime, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.False(t, called, "one valid assignment cannot replace a missing required permission")
	for _, unavailable := range []authz.ScopedAuthorizer{iam.NewScopedAuthorizer(nil, fga), iam.NewScopedAuthorizer(db, nil)} {
		called := false
		err := unavailable.(authz.CompoundScopedCommandGuard).WithScopeGrants(ctx, checks, authTime, func(context.Context) error { called = true; return nil })
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		require.False(t, called)
	}
	revokedAt := time.Now().UTC()
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return iam.NewRevocationChecker(db).RevokeTx(tx, "user-target", revokedAt, "admin_revoke", "user-admin")
	}))
	err = guard.WithScopeGrants(ctx, checks, authTime, write)
	require.ErrorIs(t, err, authz.ErrScopedCredentialRevoked)
	require.EqualValues(t, 1, countWrites(), "two live grants cannot revive revoked credentials")
	authTime = time.Now().UTC()
	require.NoError(t, guard.WithScopeGrants(ctx, checks, authTime, func(context.Context) error { return nil }))
	held := make(chan int, 1)
	release := make(chan struct{})
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	guardDone := make(chan error, 1)
	go func() {
		guardDone <- guard.WithScopeGrants(ctx, checks, authTime, func(commandCtx context.Context) error {
			var pid int
			if err := db.WithTenantCommand(commandCtx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error
			}); err != nil {
				return err
			}
			held <- pid
			select {
			case <-release:
				return write(commandCtx)
			case <-commandCtx.Done():
				return commandCtx.Err()
			}
		})
	}()
	var holderPID int
	select {
	case holderPID = <-held:
	case err := <-guardDone:
		t.Fatalf("compound guard did not acquire source locks: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("compound command did not begin")
	}
	withdrawal := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		withdrawal <- membershipCommandRequest(router, "POST", "/v1/iam/assignment-proposals/"+checks[1].Grant.AssignmentID+"/decision", "admin", `{"expected_version":5,"decision":"withdraw","reason":"Synthetic authority withdrawal"}`)
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&blocked).Error
		})
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond, "withdrawal must actually wait for the compound owner commit")
	unlock()
	require.NoError(t, <-guardDone)
	select {
	case response := <-withdrawal:
		require.Equal(t, 200, response.Code, response.Body.String())
	case <-time.After(3 * time.Second):
		t.Fatal("withdrawal did not complete after owner commit")
	}
	require.EqualValues(t, 2, countWrites())
	err = guard.WithScopeGrants(ctx, checks, authTime, write)
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.EqualValues(t, 2, countWrites(), "withdrawn evidence cannot commit another owner write")

	expires := time.Now().UTC().Add(15 * time.Second)
	expiring := propose("workforce.identity_manager.v1", expires)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		for i := range checks {
			grant, err := authority.AuthorizeScope(t.Context(), checks[i].Request)
			require.NoError(c, err)
			require.NotEmpty(c, grant.AssignmentID)
			checks[i].Grant = grant
		}
		require.Equal(c, expiring, checks[1].Grant.AssignmentID)
		require.Equal(c, checks[0].Grant.ScopeEpoch, checks[1].Grant.ScopeEpoch)
	}, 10*time.Second, 50*time.Millisecond)
	beforeExpiry := time.NewTimer(time.Until(expires.Add(-time.Second)))
	defer beforeExpiry.Stop()
	select {
	case <-beforeExpiry.C:
	case <-t.Context().Done():
		t.Fatal("fixture canceled before source expiry boundary")
	}
	startedWrite := false
	err = guard.WithScopeGrants(ctx, checks, authTime, func(commandCtx context.Context) error {
		if err := write(commandCtx); err != nil {
			return err
		}
		startedWrite = true
		<-commandCtx.Done()
		return commandCtx.Err()
	})
	require.True(t, startedWrite, "both grants must be current when the product command begins")
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied, "the earlier source expiry must cancel before the five-second command budget")
	require.EqualValues(t, 2, countWrites(), "expiry must roll back the product write")
	longLived, err := authority.AuthorizeScope(t.Context(), checks[0].Request)
	require.NoError(t, err)
	require.NotEmpty(t, longLived.AssignmentID, "the other required grant remains valid")
}
