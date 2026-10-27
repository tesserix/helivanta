package workforce_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test package importing the module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type leaveScopeAuthority struct {
	failure            error
	db                 *tenantdb.DB
	failureAfterAction error
	request            authz.ScopeRequest
	grant              authz.ScopeGrant
	calls              int
	withdrawOnCall     int
}

func (a *leaveScopeAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, _ authz.ScopeGrant, _ time.Time, action func(context.Context) error) error {
	return a.db.WithGuardedTenantCommand(ctx, r.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		if err := action(ctx); err != nil {
			return err
		}
		return a.failureAfterAction
	})
}

func (a *leaveScopeAuthority) AuthorizeScope(_ context.Context, request authz.ScopeRequest) (authz.ScopeGrant, error) {
	if a.failure != nil {
		return authz.ScopeGrant{}, a.failure
	}
	a.request = request
	a.calls++
	if a.calls == a.withdrawOnCall {
		return authz.ScopeGrant{}, nil
	}
	if request.TenantID != testutil.TenantA || request.Subject != "user-scoped" || request.Permission != workforce.PermLeaveApprove {
		return authz.ScopeGrant{}, nil
	}
	return a.grant, nil
}

func TestScopedLeaveReviewerNeedsNoLegacyGrant(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-scoped", authz.RoleStaffMember))
	authority := &leaveScopeAuthority{grant: authz.ScopeGrant{
		AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 3, ScopeEpoch: 1, MembershipEpoch: 1,
	}}
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Roles = fga
		opts.Tokens["scoped"] = testutil.TenantA
		opts.Perms["scoped"] = []authz.Permission{}
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermLoginLinkManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermLoginLinkReview)
		opts.ScopedAuthorizer = func(db *tenantdb.DB) authz.ScopedAuthorizer { authority.db = db; return authority }
	})
	code, out := h.call("PUT", "/access/user-profiler", "admin", map[string]any{"level": "manager"})
	require.Equal(t, 200, code, out)
	types := configureLeave(t, h.call, 2026)
	actor := h.enrolled("SYN-SCOPED-ACTOR", types, "2026-01-01")
	target := h.enrolled("SYN-SCOPED-TARGET", types, "2026-01-01")
	unrelated := h.enrolled("SYN-SCOPED-UNRELATED", types, "2026-01-01")
	code, currentEnrollment := h.call("GET", "/staff/"+target+"/enrollment", "admin", nil)
	require.Equal(t, 200, code, currentEnrollment)
	code, out = h.call("PUT", "/staff/"+target+"/enrollment", "admin", map[string]any{
		"expected_version": currentEnrollment["version"], "staff": map[string]any{"employee_code": "SYN-SCOPED-TARGET", "display_name": "Synthetic SYN-SCOPED-TARGET", "job_category": "nurse", "employment_status": "active"},
		"profile": map[string]any{"personal_email": "private-review-target@example.invalid", "engagement_type": "employee", "start_date": "2026-01-01"}, "credentials": []any{}, "qualifications": []any{}, "experiences": []any{},
	})
	require.Equal(t, 200, code, out)
	link := "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	code, out = h.call("PUT", link, "admin", map[string]any{"subject": "user-scoped", "reason": "Synthetic verified reviewer"})
	require.Equal(t, 200, code, out)
	code, out = h.call("POST", link+"/decision", "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic verification"})
	require.Equal(t, 200, code, out)
	scopeStatus, scopeBody := h.do("GET", "/v1/workforce/leave-review-scopes", "scoped", "")
	require.Equal(t, 200, scopeStatus, scopeBody)
	var discovered struct {
		Data []struct {
			HospitalID string `json:"hospital_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(scopeBody), &discovered))
	require.Len(t, discovered.Data, 1)
	require.Equal(t, h.hospital, discovered.Data[0].HospitalID)
	authority.failure = errors.New("synthetic scope dependency unavailable")
	scopeStatus, scopeBody = h.do("GET", "/v1/workforce/leave-review-scopes", "scoped", "")
	require.Equal(t, 503, scopeStatus, scopeBody)
	require.NotContains(t, scopeBody, h.hospital)
	authority.failure = nil
	authority.failureAfterAction = authz.ErrScopedCommandDenied
	scopeStatus, scopeBody = h.do("GET", "/v1/workforce/leave-review-scopes", "scoped", "")
	require.Equal(t, 200, scopeStatus, scopeBody)
	require.NoError(t, json.Unmarshal([]byte(scopeBody), &discovered))
	require.Empty(t, discovered.Data, "scope disclosure must not survive a failed guard")
	authority.failureAfterAction = nil
	h.post("2026", 1)
	id := uuid.NewString()
	code, out = h.file(target, id, "clerk", full(types.earned, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	code, out = h.call("GET", "/leave-review-requests", "scoped", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.NotContains(t, out["data"].([]any)[0].(map[string]any), "reason")
	code, out = h.call("GET", "/staff/"+target+"/leave-review-requests/"+id, "scoped", nil)
	require.Equal(t, 200, code, out)
	authority.failureAfterAction = authz.ErrScopedCommandDenied
	code, out = h.call("GET", "/leave-review-requests", "scoped", nil)
	require.Equal(t, 403, code, out)
	require.NotContains(t, out, "data")
	require.EqualValues(t, 1, h.count("workforce_audit", "resource_id=? AND action='workforce.leave_review.queue_read'", id), "failed read guard must roll back its audit")
	authority.failureAfterAction = nil
	readContext := func(fn func(*gorm.DB) error) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec(`SELECT set_config('app.workforce_subject','user-scoped',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_actor_check','authorized',true),set_config('app.workforce_leave_review_actor',?,true),set_config('app.workforce_leave_review_read','authorized',true),set_config('app.workforce_leave_review_read_kind','queue',true)`, h.hospital, actor).Error; e != nil {
				return e
			}
			return fn(tx)
		})
	}
	require.NoError(t, readContext(func(tx *gorm.DB) error {
		var count int64
		for _, table := range []string{"workforce_enrollment_profile", "workforce_leave_ledger"} {
			if e := tx.Table(table).Count(&count).Error; e != nil {
				return e
			}
			require.Zero(t, count, table)
		}
		if e := tx.Table("workforce_staff").Where("id=?", unrelated).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count, "queue purpose cannot expose unrelated staff")
		var visible bool
		if e := tx.Raw(`SELECT workforce_leave_review_readable_request(?::uuid,?::uuid,?::uuid,?::uuid,'user-scoped','pending')`, testutil.TenantA, h.hospital, id, target).Scan(&visible).Error; e != nil {
			return e
		}
		require.False(t, visible, "original filer cannot review their own request")
		return nil
	}))
	readWriteErr := readContext(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_leave_requests SET status='approved',version=version+1,decided_by_subject='user-scoped',decided_at=clock_timestamp() WHERE id=?`, id).Error
	})
	var readWriteDenied *pgconn.PgError
	require.True(t, errors.As(readWriteErr, &readWriteDenied), "read context must not authorize a decision: %v", readWriteErr)
	require.Equal(t, "42501", readWriteDenied.Code)
	authority.failureAfterAction = authz.ErrScopedCommandDenied
	code, out = h.act(target, id, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent scoped review"})
	require.Equal(t, 403, code, out)
	require.Zero(t, h.count("workforce_leave_ledger", "request_id=? AND kind='consumption'", id), "failed command must not consume leave")
	require.Zero(t, h.count("workforce_audit", "resource_id=? AND action='workforce.leave_request.approved'", id), "failed command must not record a successful decision")
	authority.failureAfterAction = nil
	code, out = h.act(target, id, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent scoped review"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", request(out)["status"])
	require.Equal(t, "user-scoped", request(out)["decided_by_subject"])
	require.Equal(t, h.hospital, authority.request.HospitalID)
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=? AND kind='consumption'", id))
	probationStaff := uuid.NewString()
	code, out = h.call("PUT", "/staff/"+probationStaff+"/enrollment", "admin", map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": "SYN-SCOPED-PROBATION", "display_name": "Synthetic Scoped Probation", "job_category": "nurse", "employment_status": "active"},
		"profile":          map[string]any{"personal_email": "synthetic-scoped-probation@example.invalid", "engagement_type": "employee", "start_date": "2026-01-01", "probation_end_date": "2026-10-01"},
		"credentials":      []any{}, "qualifications": []any{}, "experiences": []any{},
	})
	require.Equal(t, 200, code, out)
	code, out = h.assign(probationStaff, types.structure, "2026-01-01", 0)
	require.Equal(t, 200, code, out)
	probationRequest := uuid.NewString()
	code, out = h.file(probationStaff, probationRequest, "admin", full(types.locked, "2026-10-08", "2026-10-08"))
	require.Equal(t, 200, code, out)
	code, out = h.act(probationStaff, probationRequest, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Verify probation standing without profile disclosure"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", request(out)["status"])
	code, out = h.call("GET", "/staff/"+probationStaff+"/enrollment", "scoped", nil)
	require.Equal(t, 403, code, out)
	code, out = h.act(target, id, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent scoped review"})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, request(out)["version"])
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=? AND kind='consumption'", id))
	require.EqualValues(t, 1, h.count("workforce_audit", "resource_id=? AND action='workforce.leave_request.approved'", id))
	var audit struct{ Changes json.RawMessage }
	require.NoError(t, h.scoped(false, func(tx *gorm.DB) error {
		return tx.Table("workforce_audit").Select("changes").Where("resource_id=? AND action='workforce.leave_request.approved'", id).Take(&audit).Error
	}))
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(audit.Changes, &evidence))
	require.Equal(t, authority.grant.AssignmentID, evidence["assignment_id"])
	require.EqualValues(t, 3, evidence["assignment_version"])
	require.Equal(t, actor, evidence["actor_staff_id"])
	require.EqualValues(t, 2, evidence["actor_link_version"])

	for _, path := range []string{"/staff/" + target, "/staff/" + actor} {
		code, out = h.call("GET", path, "scoped", nil)
		require.Equal(t, 404, code, out)
	}
	code, out = h.call("GET", "/hr/payroll/salary-heads", "scoped", nil)
	require.Equal(t, 403, code, out)

	self := uuid.NewString()
	code, out = h.file(actor, self, "clerk", full(types.duty, "2026-10-06", "2026-10-06"))
	require.Equal(t, 200, code, out)
	code, out = h.act(actor, self, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Must not decide own leave"})
	require.Equal(t, 403, code, out)
	code, out = h.call("GET", "/staff/"+actor+"/leave-requests/"+self, "clerk", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "pending", request(out)["status"])
	code, out = h.call("GET", "/leave-review-requests", "scoped", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"], "linked self must not appear in the reviewer queue")
	code, out = h.call("GET", "/staff/"+actor+"/leave-review-requests/"+self, "scoped", nil)
	require.Equal(t, 404, code, out)

	withdrawn := uuid.NewString()
	code, out = h.file(target, withdrawn, "clerk", full(types.duty, "2026-10-07", "2026-10-07"))
	require.Equal(t, 200, code, out)
	authority.withdrawOnCall = authority.calls + 2
	code, out = h.act(target, withdrawn, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Authority withdrawn after middleware"})
	require.Equal(t, 404, code, out)
	authority.withdrawOnCall = 0
	code, out = h.call("GET", "/staff/"+target+"/leave-requests/"+withdrawn, "clerk", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "pending", request(out)["status"])
	require.EqualValues(t, 0, h.count("workforce_leave_ledger", "request_id=?", withdrawn))
	reviewContext := func(fn func(*gorm.DB) error) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-scoped',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_actor_check','authorized',true),set_config('app.workforce_leave_review_actor',?,true),set_config('app.workforce_leave_review_staff',?,true),set_config('app.workforce_leave_review_request',?,true),set_config('app.workforce_leave_review','authorized',true),set_config('app.workforce_leave_ledger','authorized',true)`, h.hospital, actor, target, id).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	require.NoError(t, reviewContext(func(tx *gorm.DB) error {
		var staffIDs, requestIDs, ledgerStaffIDs []string
		if err := tx.Table("workforce_staff").Pluck("id", &staffIDs).Error; err != nil {
			return err
		}
		require.ElementsMatch(t, []string{actor, target}, staffIDs)
		require.NotContains(t, staffIDs, unrelated)
		if err := tx.Table("workforce_leave_requests").Pluck("id", &requestIDs).Error; err != nil {
			return err
		}
		require.Equal(t, []string{id}, requestIDs)
		if err := tx.Table("workforce_leave_ledger").Pluck("staff_id", &ledgerStaffIDs).Error; err != nil {
			return err
		}
		require.Equal(t, []string{target, target}, ledgerStaffIDs)
		result := tx.Exec(`UPDATE workforce_leave_requests SET status='approved' WHERE id=?`, withdrawn)
		require.NoError(t, result.Error)
		require.Zero(t, result.RowsAffected, "the scoped context cannot update another request")
		return nil
	}))
	err = reviewContext(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_staff SET display_name='Must never change' WHERE id=?`, target).Error
	})
	var denied *pgconn.PgError
	require.True(t, errors.As(err, &denied), "expected a database policy denial: %v", err)
	require.Equal(t, "42501", denied.Code)
	require.EqualValues(t, 0, h.count("workforce_hospital_access", "subject='user-scoped'"))

	code, out = h.call("POST", link+"/decision", "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Synthetic reviewer link revoked"})
	require.Equal(t, 200, code, out)
	code, out = h.call("GET", "/leave-review-requests", "scoped", nil)
	require.Equal(t, 404, code, out)
	scopeStatus, scopeBody = h.do("GET", "/v1/workforce/leave-review-scopes", "scoped", "")
	require.Equal(t, 200, scopeStatus, scopeBody)
	require.NoError(t, json.Unmarshal([]byte(scopeBody), &discovered))
	require.Empty(t, discovered.Data, "revoked bindings must disappear from discovery")
	code, out = h.act(target, withdrawn, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Must fail after link revocation"})
	require.Equal(t, 404, code, out)
	code, out = h.act(target, id, "decision", "scoped", map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent scoped review"})
	require.Equal(t, 404, code, out)
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=?", id))
}
