package globalapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type scopedLeaveIAMModule struct{ *iam.Module }

func (m *scopedLeaveIAMModule) Routes(r *platform.Router, deps platform.Deps) {
	m.Module = iam.New(iam.NewRevocationChecker(deps.DB))
	m.Module.Routes(r, deps)
}

func TestScopedLeaveThroughGlobalAPI(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	modules := []platform.Module{&scopedLeaveIAMModule{iam.New(nil)}, workforce.New()}
	for _, module := range modules {
		require.NoError(t, registry.Register(module))
	}
	require.NoError(t, platform.ReconcileTenant(t.Context(), registry, fga, testutil.TenantA))
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
		require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, role))
	}
	for _, subject := range []string{"synthetic-admin", "synthetic-access-reviewer"} {
		require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, subject, authz.RoleTenantAdmin))
	}
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga, Membership: fga, Resolver: fga,
		Verifier:         authn.NewSessionVerifier(verifier),
		Modules:          modules,
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, fga) },
	})
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	tokens := map[string]string{}
	for _, subject := range []string{"synthetic-admin", "synthetic-access-reviewer", "synthetic-leave-reviewer"} {
		tokens[subject], err = signer.Mint(subject, testutil.TenantA, time.Now(), time.Now().Add(time.Minute))
		require.NoError(t, err)
	}
	call := func(method, path, subject string, body any, status int) map[string]any {
		t.Helper()
		var encoded []byte
		if body != nil {
			var marshalErr error
			encoded, marshalErr = json.Marshal(body)
			require.NoError(t, marshalErr)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		req.Header.Set("Authorization", "Bearer "+tokens[subject])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	const admin, accessReviewer, reviewer = "synthetic-admin", "synthetic-access-reviewer", "synthetic-leave-reviewer"
	for _, member := range []struct{ subject, role string }{{admin, "tenant_admin"}, {accessReviewer, "tenant_admin"}, {reviewer, "staff_member"}} {
		call("POST", "/v1/iam/members", admin, map[string]any{"subject": member.subject, "role_key": member.role}, 202)
	}
	require.Eventually(t, func() bool {
		member, e := fga.IsMember(t.Context(), reviewer, testutil.TenantA)
		return e == nil && member
	}, 10*time.Second, 50*time.Millisecond)
	permissions, err := fga.Resolve(t.Context(), reviewer, testutil.TenantA)
	require.NoError(t, err)
	require.Empty(t, permissions, "reviewer membership must not confer tenant-wide business authority")
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	call("PUT", base, admin, map[string]any{"name": "Synthetic Scoped Leave Gateway Hospital"}, 200)
	for _, subject := range []string{admin, accessReviewer} {
		call("PUT", base+"/access/"+subject, admin, map[string]any{"level": "manager"}, 200)
	}
	staff := func(code string) string {
		t.Helper()
		id := uuid.NewString()
		call("PUT", base+"/staff/"+id+"/enrollment", admin, map[string]any{
			"expected_version": 0,
			"staff":            map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "nurse", "employment_status": "active"},
			"profile":          map[string]any{"personal_email": strings.ToLower(code) + "@example.invalid", "engagement_type": "employee", "start_date": "2026-01-01"},
			"credentials":      []any{}, "qualifications": []any{}, "experiences": []any{},
		}, 200)
		return id
	}
	actor, target := staff("SYN-GW-REVIEWER"), staff("SYN-GW-TARGET")
	link := base + "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", link, admin, map[string]any{"subject": reviewer, "reason": "Synthetic reviewed identity binding"}, 200)
	call("POST", link+"/decision", accessReviewer, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic identity review"}, 200)
	call("PUT", base+"/hr/leave-settings", admin, map[string]any{"year_start_month": 1, "reviewed": true}, 200)
	call("PUT", base+"/hr/leave-years/2026", admin, nil, 200)
	call("PUT", base+"/hr/leave-configs/"+uuid.NewString(), admin, map[string]any{
		"expected_version": 0, "kind": "holiday_calendar", "code": "SYN-GW-CALENDAR", "name": "Synthetic Calendar", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"calendar_year": 2026, "holidays": []any{}, "entries": []any{},
	}, 200)
	leaveType := uuid.NewString()
	kind := call("PUT", base+"/hr/leave-configs/"+leaveType, admin, map[string]any{
		"expected_version": 0, "kind": "leave_type", "code": "SYN-GW-LEAVE", "name": "Synthetic Gateway Leave", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"rules": map[string]any{"paid": true, "balance_tracked": true, "half_day_allowed": true, "short_leave_minutes": 120, "clubbable": true}, "holidays": []any{}, "entries": []any{},
	}, 200)
	structure := call("PUT", base+"/hr/leave-configs/"+uuid.NewString(), admin, map[string]any{
		"expected_version": 0, "kind": "structure", "code": "SYN-GW-STRUCTURE", "name": "Synthetic Gateway Structure", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"holidays": []any{}, "entries": []any{map[string]any{"leave_type_revision_id": kind["revision_id"], "annual_half_days": 12, "accrual": "upfront", "carry_forward_half_days": 0, "proration": "join_month", "probation_eligible": true}},
	}, 200)
	for _, id := range []string{actor, target} {
		call("PUT", base+"/staff/"+id+"/leave-assignment", admin, map[string]any{"expected_version": 0, "structure_revision_id": structure["revision_id"], "active": true, "effective_on": "2026-01-01", "reviewed": true}, 200)
	}
	call("POST", base+"/hr/leave-years/2026/postings", admin, map[string]any{"month": 1}, 200)
	file := func(staffID, date string) string {
		t.Helper()
		path := base + "/staff/" + staffID + "/leave-requests/" + uuid.NewString()
		call("PUT", path, admin, map[string]any{"leave_type_id": leaveType, "unit": "full_day", "start_on": date, "end_on": date, "reason": "Synthetic private leave reason"}, 200)
		return path
	}
	request := file(target, "2026-10-05")
	require.Empty(t, call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 200)["data"], "a reviewed link without an assignment is not authority")
	decision := map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent synthetic scoped decision"}
	call("POST", request+"/decision", reviewer, decision, 403)
	proposal := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", proposal, admin, map[string]any{"hospital_id": hospital, "subject": reviewer, "template_key": "workforce.leave_reviewer.v1", "reason": "Synthetic scoped gateway review", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
	require.Eventually(t, func() bool { return call("GET", proposal, admin, nil, 200)["status"] == "pending_review" }, 20*time.Second, 50*time.Millisecond)
	call("POST", proposal+"/decision", accessReviewer, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic assignment approval"}, 200)
	authority := iam.NewScopedAuthorizer(db, fga)
	require.Eventually(t, func() bool {
		grant, e := authority.AuthorizeScope(t.Context(), authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermLeaveApprove})
		return e == nil && grant.AssignmentID != ""
	}, 20*time.Second, 50*time.Millisecond)
	discovered := call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 200)["data"].([]any)
	require.Len(t, discovered, 1)
	require.Equal(t, hospital, discovered[0].(map[string]any)["hospital_id"])
	require.Equal(t, "Synthetic Scoped Leave Gateway Hospital", discovered[0].(map[string]any)["name"])
	queue := call("GET", base+"/leave-review-requests", reviewer, nil, 200)["data"].([]any)
	require.Len(t, queue, 1)
	require.Equal(t, target, queue[0].(map[string]any)["staff_id"])
	require.NotContains(t, queue[0].(map[string]any), "reason")
	reviewDetail := base + "/staff/" + target + "/leave-review-requests/" + request[strings.LastIndex(request, "/")+1:]
	reviewed := call("GET", reviewDetail, reviewer, nil, 200)
	require.Equal(t, "Synthetic private leave reason", reviewed["request"].(map[string]any)["reason"])
	call("GET", strings.Replace(reviewDetail, target, uuid.NewString(), 1), reviewer, nil, 404)
	call("GET", strings.Replace(reviewDetail, hospital, uuid.NewString(), 1), reviewer, nil, 403)
	approved := call("POST", request+"/decision", reviewer, decision, 200)
	require.Equal(t, "approved", approved["request"].(map[string]any)["status"])
	require.Equal(t, reviewer, approved["request"].(map[string]any)["decided_by_subject"])
	require.EqualValues(t, 2, approved["request"].(map[string]any)["version"])
	call("POST", request+"/decision", reviewer, decision, 200)
	call("POST", strings.Replace(request, hospital, uuid.NewString(), 1)+"/decision", reviewer, decision, 403)
	call("POST", strings.Replace(request, target, uuid.NewString(), 1)+"/decision", reviewer, decision, 404)
	self := file(actor, "2026-10-06")
	call("POST", self+"/decision", reviewer, decision, 403)
	require.Empty(t, call("GET", base+"/leave-review-requests", reviewer, nil, 200)["data"])
	call("GET", base+"/staff/"+actor+"/leave-review-requests/"+self[strings.LastIndex(self, "/")+1:], reviewer, nil, 404)
	call("GET", reviewDetail, reviewer, nil, 404)
	directory := call("GET", base+"/staff", reviewer, nil, 200)
	require.Empty(t, directory["data"], "the directory contract returns an empty page for a caller without hospital access")
	for _, path := range []string{base + "/staff/" + target + "/enrollment", base + "/hr/payroll/salary-heads"} {
		call("GET", path, reviewer, nil, 403)
	}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), admin), testutil.TenantA, func(tx *gorm.DB) error {
		var grants int64
		if e := tx.Table("workforce_hospital_access").Where("subject=?", reviewer).Count(&grants).Error; e != nil {
			return e
		}
		require.Zero(t, grants)
		requestID := request[strings.LastIndex(request, "/")+1:]
		if e := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, admin, hospital).Error; e != nil {
			return e
		}
		var consumption struct{ Count, HalfDays int64 }
		if e := tx.Table("workforce_leave_ledger").Select("count(*) AS count, sum(half_days) AS half_days").Where("request_id=? AND kind='consumption'", requestID).Scan(&consumption).Error; e != nil {
			return e
		}
		require.EqualValues(t, 1, consumption.Count)
		require.EqualValues(t, -2, consumption.HalfDays)
		var audits []struct {
			ActorSubject string
			Changes      json.RawMessage
		}
		if e := tx.Table("workforce_audit").Select("actor_subject,changes").Where("resource_id=? AND action='workforce.leave_request.approved'", requestID).Find(&audits).Error; e != nil {
			return e
		}
		require.Len(t, audits, 1)
		require.Equal(t, reviewer, audits[0].ActorSubject)
		require.Contains(t, string(audits[0].Changes), strings.TrimPrefix(proposal, "/v1/iam/assignment-proposals/"))
		require.NotContains(t, string(audits[0].Changes), "Synthetic private leave reason")
		return nil
	}))
	call("POST", link+"/decision", admin, map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Synthetic access cutoff"}, 200)
	require.Empty(t, call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 200)["data"])
	call("POST", request+"/decision", reviewer, decision, 404)
	call("GET", base+"/leave-review-requests", reviewer, nil, 404)
	newLink := base + "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", newLink, admin, map[string]any{"subject": reviewer, "reason": "Synthetic fresh reviewed binding"}, 200)
	call("POST", newLink+"/decision", accessReviewer, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic rebind review"}, 200)
	call("POST", request+"/decision", reviewer, decision, 200)
	call("POST", "/v1/iam/subjects/"+reviewer+"/revoke", admin, nil, 200)
	// The harness middleware deliberately misses revocation; the command guard must not.
	call("POST", request+"/decision", reviewer, decision, 401)
	call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 401)
	call("GET", base+"/leave-review-requests", reviewer, nil, 401)
	freshAuthentication := time.NewTimer(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	defer freshAuthentication.Stop()
	select {
	case <-freshAuthentication.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	tokens[reviewer], err = signer.Mint(reviewer, testutil.TenantA, time.Now(), time.Now().Add(time.Minute))
	require.NoError(t, err)
	call("POST", request+"/decision", reviewer, decision, 200)

	hold := func(query string, args ...any) (int, func()) {
		t.Helper()
		ready := make(chan int, 1)
		release := make(chan struct{})
		done := make(chan struct{})
		var lockErr error
		go func() {
			lockErr = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if e := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; e != nil {
					return e
				}
				var pid int
				if e := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; e != nil {
					return e
				}
				if e := tx.Exec(query, args...).Error; e != nil {
					return e
				}
				ready <- pid
				select {
				case <-release:
					return nil
				case <-t.Context().Done():
					return t.Context().Err()
				}
			})
			close(done)
		}()
		var once sync.Once
		unlock := func() { once.Do(func() { close(release); <-done; require.NoError(t, lockErr) }) }
		t.Cleanup(unlock)
		select {
		case pid := <-ready:
			return pid, unlock
		case <-done:
			t.Fatalf("lock holder failed: %v", lockErr)
		case <-time.After(10 * time.Second):
			t.Fatal("lock holder did not become ready")
		}
		return 0, unlock
	}
	blockedBy := func(blocker int, queryFragment string) int {
		t.Helper()
		var pid int
		require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pid FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)) AND query LIKE ? LIMIT 1`, blocker, "%"+queryFragment+"%").Scan(&pid).Error
		}))
		return pid
	}
	start := func(path, subject string, body any) <-chan *httptest.ResponseRecorder {
		t.Helper()
		encoded, e := json.Marshal(body)
		require.NoError(t, e)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(encoded)))
		req.Header.Set("Authorization", "Bearer "+tokens[subject])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { w := httptest.NewRecorder(); gateway.ServeHTTP(w, req); result <- w }()
		return result
	}
	pending := file(target, "2026-10-07")
	blocker, unlock := hold(`SELECT epoch FROM iam_assignment_scope_epochs WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, testutil.TenantA, hospital, reviewer)
	withdrawal := start(proposal+"/decision", admin, map[string]any{"expected_version": 5, "decision": "withdraw", "reason": "Synthetic assignment cutoff during command"})
	var withdrawingPID int
	require.Eventually(t, func() bool {
		withdrawingPID = blockedBy(blocker, "iam_assignment_proposals")
		return withdrawingPID != 0
	}, 2*time.Second, 20*time.Millisecond)
	waitingDecision := start(pending+"/decision", reviewer, decision)
	require.Eventually(t, func() bool { return blockedBy(withdrawingPID, "FOR SHARE OF p,m,q") != 0 }, 2*time.Second, 20*time.Millisecond)
	unlock()
	withdrawn := <-withdrawal
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	denied := <-waitingDecision
	require.Equal(t, 403, denied.Code, denied.Body.String())
	require.Empty(t, call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 200)["data"])
	stillPending := call("GET", pending, admin, nil, 200)
	require.Equal(t, "pending", stillPending["request"].(map[string]any)["status"])
	require.EqualValues(t, 1, stillPending["request"].(map[string]any)["version"])
	call("POST", self+"/decision", reviewer, decision, 403)

	expires := time.Now().UTC().Add(15 * time.Second).Truncate(time.Second)
	expiringProposal := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", expiringProposal, admin, map[string]any{"hospital_id": hospital, "subject": reviewer, "template_key": "workforce.leave_reviewer.v1", "reason": "Synthetic command expiry", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": expires.Format(time.RFC3339)}, 200)
	require.Eventually(t, func() bool { return call("GET", expiringProposal, admin, nil, 200)["status"] == "pending_review" }, 10*time.Second, 50*time.Millisecond)
	call("POST", expiringProposal+"/decision", accessReviewer, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic expiring assignment review"}, 200)
	require.Eventually(t, func() bool {
		grant, e := authority.AuthorizeScope(t.Context(), authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermLeaveApprove})
		return e == nil && grant.AssignmentID == strings.TrimPrefix(expiringProposal, "/v1/iam/assignment-proposals/")
	}, 10*time.Second, 50*time.Millisecond)
	// PostgreSQL owns this validity clock; enter the real request just before expiry.
	nearExpiry := time.NewTimer(time.Until(expires.Add(-2 * time.Second)))
	defer nearExpiry.Stop()
	select {
	case <-nearExpiry.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	ledgerBlocker, unlockLedger := hold(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, testutil.TenantA+":"+hospital+":leave-ledger")
	expiringDecision := start(pending+"/decision", reviewer, decision)
	require.Eventually(t, func() bool { return blockedBy(ledgerBlocker, "pg_advisory_xact_lock(") != 0 }, time.Second, 20*time.Millisecond)
	expiredResponse := <-expiringDecision
	unlockLedger()
	require.Equal(t, 403, expiredResponse.Code, expiredResponse.Body.String())
	call("GET", base+"/leave-review-requests", reviewer, nil, 403)
	require.Empty(t, call("GET", "/v1/workforce/leave-review-scopes", reviewer, nil, 200)["data"])
	stillPending = call("GET", pending, admin, nil, 200)
	require.Equal(t, "pending", stillPending["request"].(map[string]any)["status"])
	require.EqualValues(t, 1, stillPending["request"].(map[string]any)["version"])
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, admin, hospital).Error; e != nil {
			return e
		}
		requestID := pending[strings.LastIndex(pending, "/")+1:]
		var count int64
		if e := tx.Table("workforce_leave_ledger").Where("request_id=? AND kind='consumption'", requestID).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		if e := tx.Table("workforce_audit").Where("resource_id=? AND action='workforce.leave_request.approved'", requestID).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		return nil
	}))
}
