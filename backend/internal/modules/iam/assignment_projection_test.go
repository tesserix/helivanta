package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pausedAssignmentWriter struct {
	*authz.Client
	gates     chan assignmentWriteGate
	projected sync.Map
}

type scopedProbeIAM struct {
	*iam.Module
	permission authz.Permission
}

func (m scopedProbeIAM) Routes(r *platform.Router, deps platform.Deps) {
	m.Module.Routes(r, deps)
	r.Group("/iam").POSTScopedHospital("/qa/hospitals/:hospital/review", m.permission, "hospital", func(c *gin.Context) {
		grant, ok := authz.HospitalScopeGrant(c, m.permission, c.Param("hospital"))
		if ok {
			_, otherAction := authz.HospitalScopeGrant(c, iam.PermMemberManage, c.Param("hospital"))
			_, otherHospital := authz.HospitalScopeGrant(c, m.permission, "00000000-0000-0000-0000-000000000001")
			respond.OK(c, gin.H{"assignment_id": grant.AssignmentID, "other_action": otherAction, "other_hospital": otherHospital})
			return
		}
		respond.OK(c, gin.H{"legacy": true, "scope_unavailable": authz.HospitalScopeUnavailable(c)})
	})
}

type assignmentWriteGate struct {
	started chan struct{}
	release chan struct{}
	fail    bool
}

type interceptedScopedChecker struct {
	*authz.Client
	after   func()
	failure bool
}

func (c *interceptedScopedChecker) CheckScopedAssignment(ctx context.Context, a authz.ScopedAssignment, actor string, permission authz.Permission) (bool, error) {
	if c.failure {
		return false, errors.New("synthetic private graph failure")
	}
	allowed, err := c.Client.CheckScopedAssignment(ctx, a, actor, permission)
	if c.after != nil {
		c.after()
	}
	return allowed, err
}

func (w *pausedAssignmentWriter) ProjectScopedAssignment(ctx context.Context, a authz.ScopedAssignment) error {
	select {
	case gate := <-w.gates:
		close(gate.started)
		select {
		case <-gate.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if gate.fail {
			return errors.New("synthetic OpenFGA outage")
		}
	default:
	}
	if err := w.Client.ProjectScopedAssignment(ctx, a); err != nil {
		return err
	}
	w.projected.Store(a.ID, a)
	return nil
}

func TestAssignmentProjectionAcknowledgesOnlyCurrentScope(t *testing.T) {
	exerciseAssignmentProjection(t, "workforce.leave_reviewer.v1", assignmentLeaveApprove)
}

func TestIdentityTemplateScope(t *testing.T) {
	for _, template := range []struct {
		key        string
		permission authz.Permission
	}{
		{"workforce.identity_manager.v1", "workforce.identity_checks.manage"},
		{"workforce.identity_reviewer.v1", "workforce.identity_checks.review"},
	} {
		t.Run(strings.TrimSuffix(strings.TrimPrefix(template.key, "workforce."), ".v1"), func(t *testing.T) { exerciseAssignmentProjection(t, template.key, template.permission) })
	}
}

func exerciseAssignmentProjection(t *testing.T, templateKey string, permission authz.Permission) {
	t.Helper()
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	writer := &pausedAssignmentWriter{Client: fga, gates: make(chan assignmentWriteGate, 1)}
	routeGraph := &interceptedScopedChecker{Client: fga}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: writer, Roles: fga,
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "target": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, iam.PermAssignmentManage, permission},
			"reviewer": {iam.PermAssignmentReview},
		}, Modules: []platform.Module{scopedProbeIAM{Module: iam.New(nil), permission: permission}, assignmentWorkforceModule()},
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, routeGraph) },
	})
	upstream := httptest.NewServer(r)
	authority := iam.NewScopedAuthorizer(db, fga)
	request := authz.ScopeRequest{TenantID: testutil.TenantA, Subject: "user-target", Permission: permission}
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		body = strings.ReplaceAll(body, "workforce.leave_reviewer.v1", templateKey)
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	hospital := uuid.NewString()
	request.HospitalID = hospital
	probe := "/v1/iam/qa/hospitals/" + hospital + "/review"
	base := "/v1/workforce/hospitals/" + hospital
	call("PUT", base, "admin", `{"name":"Synthetic projection QA"}`, 200)
	for _, actor := range []string{"admin", "reviewer"} {
		call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"tenant_admin"}`, "user-"+actor), 202)
		call("PUT", base+"/access/user-"+actor, "admin", `{"level":"manager"}`, 200)
	}
	call("POST", "/v1/iam/members", "admin", `{"subject":"user-target","role_key":"staff_member"}`, 202)
	body := fmt.Sprintf(`{"hospital_id":%q,"subject":"user-target","template_key":"workforce.leave_reviewer.v1","reason":"Synthetic projection verification","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	propose := func() string {
		t.Helper()
		id := uuid.Must(uuid.NewV7()).String()
		path := "/v1/iam/assignment-proposals/" + id
		call("PUT", path, "admin", body, 200)
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var p iam.ProposalOutput
			w := call("GET", path, "admin", "", 200)
			require.NoError(c, json.Unmarshal(w.Body.Bytes(), &p))
			require.Equal(c, "pending_review", p.Status)
		}, 20*time.Second, 100*time.Millisecond)
		return id
	}
	approve := func() string {
		t.Helper()
		id := propose()
		call("POST", "/v1/iam/assignment-proposals/"+id+"/decision", "reviewer", `{"expected_version":2,"decision":"approve","reason":"Independent synthetic projection review"}`, 200)
		return id
	}
	firstGate := assignmentWriteGate{started: make(chan struct{}), release: make(chan struct{}), fail: true}
	writer.gates <- firstGate
	first := approve()
	familyPath := "/v1/iam/assignment-proposals?limit=50"
	otherPath := "/v1/iam/identity-assignment-proposals?limit=50"
	if templateKey != "workforce.leave_reviewer.v1" {
		familyPath, otherPath = otherPath, familyPath
	}
	currentList := call("GET", familyPath, "admin", "", 200)
	require.Contains(t, currentList.Body.String(), first)
	otherList := call("GET", otherPath, "admin", "", 200)
	require.NotContains(t, otherList.Body.String(), first, "legacy leave consumers must not receive identity assignments")
	catalogue := call("GET", "/v1/iam/staff-access-templates", "admin", "", 200)
	require.Contains(t, catalogue.Body.String(), templateKey)

	epoch := func(c *assert.CollectT, wantEpoch, wantAck int64) {
		require.NoError(c, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			var state struct {
				Epoch             int64
				AcknowledgedEpoch int64
			}
			err := tx.Table("iam_assignment_scope_epochs").Where("hospital_id=? AND subject=?", hospital, "user-target").Take(&state).Error
			require.NoError(c, err)
			require.Equal(c, wantEpoch, state.Epoch)
			require.Equal(c, wantAck, state.AcknowledgedEpoch)
			return err
		}))
	}
	select {
	case <-firstGate.started:
	case <-time.After(20 * time.Second):
		t.Fatal("projection did not begin")
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 1, 0) }, 5*time.Second, 100*time.Millisecond)
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE iam_assignment_proposals SET status='active',version=version+1 WHERE id=?`, first).Error
	})
	require.ErrorContains(t, err, "projected snapshot required")
	grant, err := authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID, "unacknowledged projection must not authorize")
	call("POST", probe, "target", "", 403)
	legacy := call("POST", probe, "admin", "", 200)
	require.Contains(t, legacy.Body.String(), `"legacy":true`)
	close(firstGate.release)
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 1, 1) }, 20*time.Second, 100*time.Millisecond)
	wActive := call("GET", "/v1/iam/assignment-proposals/"+first, "admin", "", 200)
	var activeProposal iam.ProposalOutput
	require.NoError(t, json.Unmarshal(wActive.Body.Bytes(), &activeProposal))
	require.Equal(t, "active", activeProposal.Status)
	require.Equal(t, int64(5), activeProposal.Version)
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, first, grant.AssignmentID)
	require.Equal(t, int64(4), grant.Version, "authority binds the reviewed immutable snapshot, not the activation history version")
	require.Equal(t, int64(1), grant.ScopeEpoch)
	for _, candidate := range []authz.Permission{"workforce.leave.approve", "workforce.identity_checks.manage", "workforce.identity_checks.review"} {
		if candidate == request.Permission {
			continue
		}
		other := request
		other.Permission = candidate
		denied, e := authority.AuthorizeScope(t.Context(), other)
		require.NoError(t, e)
		require.Empty(t, denied.AssignmentID, "templates must not confer another family's permission")
	}

	guard, supportsCommandGuard := authority.(authz.ScopedCommandGuard)
	require.True(t, supportsCommandGuard, "the IAM provider must guard the real product transaction")
	guardCtx := tenantdb.WithActor(t.Context(), request.Subject)
	guardFailure := errors.New("synthetic failure after guarded product write")
	writeGuardAudit := func(ctx context.Context) error {
		return db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'scope_guard_test_write','iam_assignment_snapshots',?)`, testutil.TenantA, request.Subject, first).Error
		})
	}
	err = guard.WithScopeGrant(guardCtx, request, grant, time.Now().UTC(), func(ctx context.Context) error {
		if err := writeGuardAudit(ctx); err != nil {
			return err
		}
		return guardFailure
	})
	require.ErrorIs(t, err, guardFailure)
	var guardedWrites int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("action='scope_guard_test_write'").Count(&guardedWrites).Error
	}))
	require.Zero(t, guardedWrites, "an unsuccessful guard must roll back the product write")
	require.NoError(t, guard.WithScopeGrant(guardCtx, request, grant, time.Now().UTC(), writeGuardAudit))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("action='scope_guard_test_write'").Count(&guardedWrites).Error
	}))
	require.EqualValues(t, 1, guardedWrites)
	currentReceipt := grant
	require.NoError(t, guard.WithScopeGrant(guardCtx, request, currentReceipt, time.Now().UTC(), func(ctx context.Context) error {
		revocationCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		err := db.WithSystem(revocationCtx, func(tx *gorm.DB) error {
			return iam.NewRevocationChecker(db).RevokeTx(tx, request.Subject, time.Now().UTC(), "admin_revoke", "user-admin")
		})
		require.ErrorIs(t, err, context.DeadlineExceeded, "security revocation must serialize with an in-flight guarded command")
		return nil
	}))
	authenticatedAt := time.Now().Add(-time.Minute).UTC()
	revokedAt := time.Now().UTC()
	revocations := iam.NewRevocationChecker(db)
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return revocations.RevokeTx(tx, request.Subject, revokedAt, "admin_revoke", "user-admin")
	}))
	calledAfterRevocation := false
	err = guard.WithScopeGrant(guardCtx, request, grant, authenticatedAt, func(context.Context) error {
		calledAfterRevocation = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedCredentialRevoked)
	require.False(t, calledAfterRevocation, "a current assignment cannot authorize an old revoked credential")
	require.NoError(t, guard.WithScopeGrant(guardCtx, request, grant, revokedAt.Add(time.Second), func(context.Context) error { return nil }))
	verified := call("POST", probe, "target", "", 200)
	require.Contains(t, verified.Body.String(), first)
	require.Contains(t, verified.Body.String(), `"other_action":false`)
	require.Contains(t, verified.Body.String(), `"other_hospital":false`)
	forgedRequest := httptest.NewRequest("POST", "/v1/iam/qa/hospitals/"+uuid.NewString()+"/review", nil)
	forgedRequest.Header.Set("Authorization", "Bearer target")
	forgedRequest.Header.Set("X-Helivanta-Scoped-Assignment", first)
	forgedResponse := httptest.NewRecorder()
	gateway.ServeHTTP(forgedResponse, forgedRequest)
	require.Equal(t, 403, forgedResponse.Code, "browser assignment headers cannot create route proof")
	call("POST", "/v1/iam/qa/hospitals/"+uuid.NewString()+"/review", "target", "", 403)
	call("POST", "/v1/iam/qa/hospitals/invalid/review", "target", "", 400)
	routeGraph.failure = true
	call("POST", probe, "target", "", 503)
	routeGraph.failure = false
	for _, altered := range []authz.ScopeRequest{
		{TenantID: testutil.TenantB, Subject: request.Subject, HospitalID: hospital, Permission: request.Permission},
		{TenantID: request.TenantID, Subject: "user-other", HospitalID: hospital, Permission: request.Permission},
		{TenantID: request.TenantID, Subject: request.Subject, HospitalID: uuid.NewString(), Permission: request.Permission},
		{TenantID: request.TenantID, Subject: request.Subject, HospitalID: hospital, Permission: "iam.member.manage"},
		{TenantID: "invalid", Subject: request.Subject, HospitalID: hospital, Permission: request.Permission},
	} {
		denied, e := authority.AuthorizeScope(t.Context(), altered)
		require.NoError(t, e)
		require.Empty(t, denied.AssignmentID)
	}
	for _, unavailable := range []authz.ScopedAuthorizer{
		iam.NewScopedAuthorizer(nil, fga),
		iam.NewScopedAuthorizer(db, nil),
		iam.NewScopedAuthorizer(db, &interceptedScopedChecker{Client: fga, failure: true}),
	} {
		denied, e := unavailable.AuthorizeScope(t.Context(), request)
		require.ErrorIs(t, e, authz.ErrScopedAuthorizationUnavailable)
		require.NotContains(t, e.Error(), "private")
		require.Empty(t, denied.AssignmentID)
	}
	snapshot := authz.ScopedAssignment{TenantID: testutil.TenantA, HospitalID: hospital, ScopeKind: "hospital", ScopeID: hospital, ID: first, Version: 4, Subject: "user-target", Permissions: []authz.Permission{permission}}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		allowed, e := fga.CheckScopedAssignment(t.Context(), snapshot, "user-target", permission)
		require.NoError(c, e)
		require.True(c, allowed)
	}, 20*time.Second, 100*time.Millisecond)
	resolved, err := fga.Resolve(t.Context(), "user-target", testutil.TenantA)
	require.NoError(t, err)
	require.Empty(t, resolved)
	secondGate := assignmentWriteGate{started: make(chan struct{}), release: make(chan struct{})}
	writer.gates <- secondGate
	second := approve()
	select {
	case <-secondGate.started:
	case <-time.After(20 * time.Second):
		t.Fatal("projection did not begin")
	}
	call("POST", "/v1/iam/assignment-proposals/"+second+"/decision", "admin", `{"expected_version":4,"decision":"withdraw","reason":"Synthetic withdrawal during projection"}`, 200)
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 3, 1) }, 5*time.Second, 100*time.Millisecond)
	close(secondGate.release)
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 3, 3) }, 30*time.Second, 100*time.Millisecond)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		var count int64
		err := tx.Table("iam_assignment_snapshots").Count(&count).Error
		require.Equal(t, int64(2), count)
		return err
	}))
	w := call("GET", "/v1/iam/assignment-proposals/"+second, "admin", "", 200)
	require.Contains(t, w.Body.String(), `"status":"withdrawn"`)
	for _, table := range []string{"iam_assignment_snapshots", "iam_assignment_scope_epochs"} {
		for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
			require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
				if tenant == testutil.TenantB {
					if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
						return err
					}
				}
				var count int64
				err := tx.Table(table).Count(&count).Error
				require.Zero(t, count, "scope evidence requires matching tenant and the access guard")
				return err
			}))
		}
	}
	for _, mutation := range []struct{ statement, message string }{
		{`UPDATE iam_assignment_snapshots SET permissions='["iam.member.manage"]'`, "proposal history is immutable"},
		{`DELETE FROM iam_assignment_snapshots`, "proposal history is immutable"},
		{`UPDATE iam_assignment_scope_epochs SET epoch=epoch+1`, "invalid scope epoch transition"},
		{`UPDATE iam_assignment_scope_epochs SET acknowledged_epoch=0`, "invalid scope epoch transition"},
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(mutation.statement).Error
		})
		require.ErrorContains(t, err, mutation.message)
	}
	assertMembership := func(wantEpoch, wantCount int64) {
		t.Helper()
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			var state struct {
				Epoch       int64
				MemberCount int64
			}
			err := tx.Table("iam_subject_membership_epochs").Where("subject=?", "user-target").Take(&state).Error
			require.NoError(t, err)
			require.Equal(t, wantEpoch, state.Epoch)
			require.Equal(t, wantCount, state.MemberCount)
			return err
		}))
	}
	assertMembership(1, 1)
	call("POST", "/v1/iam/members", "admin", `{"subject":"user-target","role_key":"doctor"}`, 202)
	assertMembership(1, 2)
	removeOrdinaryPath, removeOrdinaryBody := currentMembershipRemoval(t, gateway, "user-target", "staff_member")
	removedOrdinary := membershipCommandRequest(gateway, http.MethodPut, removeOrdinaryPath, "admin", removeOrdinaryBody)
	require.Equal(t, 202, removedOrdinary.Code, removedOrdinary.Body.String())
	assertMembership(1, 1)
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 3, 3) }, 5*time.Second, 100*time.Millisecond)
	pendingBeforeRemoval := propose()
	removeDoctorPath, removeDoctorBody := currentMembershipRemoval(t, gateway, "user-target", "doctor")
	removedDoctor := membershipCommandRequest(gateway, http.MethodPut, removeDoctorPath, "admin", removeDoctorBody)
	require.Equal(t, 202, removedDoctor.Code, removedDoctor.Body.String())
	assertMembership(2, 0)
	calledAfterRemoval := false
	err = guard.WithScopeGrant(guardCtx, request, currentReceipt, time.Now().UTC(), func(context.Context) error {
		calledAfterRemoval = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.False(t, calledAfterRemoval, "membership removal invalidates the old receipt before any product work")
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var evidence struct {
			Subject      string
			Epoch        int64
			ActorSubject string
			RoleKey      string
		}
		if err := tx.Table("iam_membership_revocations").Where("subject=?", "user-target").Take(&evidence).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), evidence.Epoch)
		require.Equal(t, "user-admin", evidence.ActorSubject)
		require.Equal(t, "doctor", evidence.RoleKey)
		return nil
	}))
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 4, 3) }, 5*time.Second, 100*time.Millisecond)
	removalReplay := membershipCommandRequest(gateway, http.MethodPut, removeDoctorPath, "admin", removeDoctorBody)
	require.Equal(t, 202, removalReplay.Code, removalReplay.Body.String())
	require.JSONEq(t, removedDoctor.Body.String(), removalReplay.Body.String())
	assertMembership(2, 0)
	call("POST", "/v1/iam/members", "admin", `{"subject":"user-target","role_key":"staff_member"}`, 202)
	assertMembership(2, 1)
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID, "re-enrollment cannot revive an old graph decision")
	call("PUT", "/v1/iam/assignment-proposals/"+first, "admin", body, 409)
	call("POST", "/v1/iam/assignment-proposals/"+pendingBeforeRemoval+"/decision", "reviewer", `{"expected_version":2,"decision":"approve","reason":"Old pending proposal cannot survive re-enrollment"}`, 404)
	call("POST", "/v1/iam/assignment-proposals/"+first+"/decision", "reviewer", `{"expected_version":2,"decision":"approve","reason":"Independent synthetic projection review"}`, 404)
	// Old graph tuples can become true again after re-enrollment. They are not authority.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		allowed, e := fga.CheckScopedAssignment(t.Context(), snapshot, "user-target", permission)
		require.NoError(c, e)
		require.True(c, allowed)
	}, 20*time.Second, 100*time.Millisecond)
	writer.projected.Clear()
	third := approve()
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 5, 5) }, 20*time.Second, 100*time.Millisecond)
	writer.projected.Range(func(key, _ any) bool { require.Equal(t, third, key); return true })
	_, projected := writer.projected.Load(third)
	require.True(t, projected)
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, third, grant.AssignmentID)
	require.Equal(t, int64(2), grant.MembershipEpoch)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		var rows []struct {
			ID              string
			MembershipEpoch int64
		}
		if err := tx.Table("iam_assignment_snapshots").Select("id,membership_epoch").Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 3)
		for _, row := range rows {
			if row.ID == third {
				require.Equal(t, int64(2), row.MembershipEpoch)
			} else {
				require.Equal(t, int64(1), row.MembershipEpoch)
			}
		}
		return nil
	}))
	for _, role := range []string{"staff_member", "doctor"} {
		call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":"user-concurrent-removal","role_key":%q}`, role), 202)
	}
	statuses := make(chan int, 2)
	start := make(chan struct{})
	for _, role := range []string{"staff_member", "doctor"} {
		path, input := currentMembershipRemoval(t, gateway, "user-concurrent-removal", role)
		go func() {
			<-start
			w := membershipCommandRequest(gateway, http.MethodPut, path, "admin", input)
			statuses <- w.Code
		}()
	}
	close(start)
	for range 2 {
		require.Equal(t, 202, <-statuses)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var row struct {
			Epoch       int64
			MemberCount int64
		}
		err := tx.Table("iam_subject_membership_epochs").Where("subject=?", "user-concurrent-removal").Take(&row).Error
		require.Equal(t, int64(2), row.Epoch)
		require.Zero(t, row.MemberCount)
		return err
	}))
	for _, mutation := range []string{`UPDATE iam_subject_membership_epochs SET epoch=1`, `DELETE FROM iam_subject_membership_epochs`, `UPDATE iam_membership_revocations SET actor_subject='tampered'`, `DELETE FROM iam_membership_revocations`} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(mutation).Error })
		require.Error(t, err)
	}
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM iam_members WHERE subject='user-target'`).Error
	})
	require.ErrorContains(t, err, "membership removal actor required")
	assertMembership(2, 1)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		for _, table := range []string{"iam_subject_membership_epochs", "iam_membership_revocations"} {
			var count int64
			if err := tx.Table(table).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count)
		}
		return nil
	}))
	racing := iam.NewScopedAuthorizer(db, &interceptedScopedChecker{Client: fga, after: func() {
		call("POST", "/v1/iam/assignment-proposals/"+third+"/decision", "admin", `{"expected_version":5,"decision":"withdraw","reason":"Synthetic withdrawal during authority check"}`, 200)
	}})
	grant, err = racing.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID, "a graph allow must be discarded after concurrent withdrawal")
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 6, 6) }, 20*time.Second, 100*time.Millisecond)
	body = fmt.Sprintf(`{"hospital_id":%q,"subject":"user-target","template_key":"workforce.leave_reviewer.v1","reason":"Synthetic future validity","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Add(time.Hour).Format(time.RFC3339), time.Now().UTC().Add(2*time.Hour).Format(time.RFC3339))
	_ = approve()
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 7, 7) }, 20*time.Second, 100*time.Millisecond)
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID, "future validity must deny even after graph projection")
	expires := time.Now().UTC().Add(15 * time.Second).Truncate(time.Second)
	body = fmt.Sprintf(`{"hospital_id":%q,"subject":"user-target","template_key":"workforce.leave_reviewer.v1","reason":"Synthetic expiry during authority check","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Format(time.RFC3339), expires.Format(time.RFC3339))
	expiring := approve()
	require.EventuallyWithT(t, func(c *assert.CollectT) { epoch(c, 8, 8) }, 20*time.Second, 100*time.Millisecond)
	grant, err = authority.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, expiring, grant.AssignmentID)
	time.Sleep(time.Until(expires.Add(-100 * time.Millisecond)))
	racing = iam.NewScopedAuthorizer(db, &interceptedScopedChecker{Client: fga, after: func() { time.Sleep(200 * time.Millisecond) }})
	grant, err = racing.AuthorizeScope(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, grant.AssignmentID, "validity must be rechecked after the graph call")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var activated int64
		if err := tx.Table("audit_events").Where("resource_type=? AND resource_id=? AND action=? AND actor_subject=?", "iam_assignment_proposals", first, "proposal_active", "system:iam-assignment-projector").Count(&activated).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), activated, "projection retries must not invent repeated activation")
		for _, outcome := range []string{"allowed", "denied"} {
			var count int64
			if err := tx.Table("audit_events").Where("actor_subject=? AND action=?", request.Subject, "scope:"+outcome+":"+string(request.Permission)).Count(&count).Error; err != nil {
				return err
			}
			require.Positive(t, count)
		}
		return nil
	}))
}
