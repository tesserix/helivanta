package workforce_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const routingSource = "Synthetic HR circular 2026/11"

func routingPolicy(expected int, mode string, hours any) map[string]any {
	body := map[string]any{"expected_version": expected, "mode": mode, "source_reference": routingSource, "reviewed": true}
	if hours != nil {
		body["escalation_hours"] = hours
	}
	return body
}

func TestLeaveRoutingPolicyIsVersionedAndReviewed(t *testing.T) {
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Perms["foreign"] = append(opts.Perms["foreign"], workforce.PermLeaveConfigure)
	})
	code, out := h.call("GET", "/hr/leave-routing", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, map[string]any{"version": float64(0), "mode": "hospital", "source_reference": ""}, out, "an unconfigured hospital routes to the leave desk")

	for name, body := range map[string]map[string]any{
		"reporting line without interval": routingPolicy(0, "reporting_line", nil),
		"hospital mode with interval":     routingPolicy(0, "hospital", 24),
		"interval above 720 hours":        routingPolicy(0, "reporting_line", 721),
		"interval below one hour":         routingPolicy(0, "reporting_line", -1),
		"unknown mode":                    routingPolicy(0, "department", 24),
		"missing source reference":        with(routingPolicy(0, "reporting_line", 24), map[string]any{"source_reference": "  "}),
		"unreviewed":                      with(routingPolicy(0, "reporting_line", 24), map[string]any{"reviewed": false}),
	} {
		code, out = h.call("PUT", "/hr/leave-routing", "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(0, "reporting_line", 24))
	require.Equal(t, 200, code, out)
	require.Equal(t, float64(1), out["version"])
	require.Equal(t, "reporting_line", out["mode"])
	require.Equal(t, float64(24), out["escalation_hours"])
	require.Equal(t, routingSource, out["source_reference"])
	require.Equal(t, "user-admin", out["reviewed_by_subject"])
	first := out

	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(0, "reporting_line", 24))
	require.Equal(t, 200, code, out)
	require.Equal(t, first, out, "a replay returns the revision it created")
	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(0, "reporting_line", 48))
	require.Equal(t, 409, code, out)

	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(1, "hospital", nil))
	require.Equal(t, 200, code, out)
	require.Equal(t, float64(2), out["version"])
	require.NotContains(t, out, "escalation_hours")
	code, out = h.call("GET", "/hr/leave-routing", "viewer", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, float64(2), out["version"])
	require.Equal(t, "hospital", out["mode"])

	code, out = h.call("PUT", "/hr/leave-routing", "clerk", routingPolicy(2, "reporting_line", 12))
	require.Equal(t, 403, code, out)
	code, out = h.call("PUT", "/hr/leave-routing", "reader", routingPolicy(2, "reporting_line", 12))
	require.Equal(t, 403, code, out)
	code, body := h.do("GET", h.base+"/hr/leave-routing", "foreign", "")
	require.Equal(t, 404, code, body)
	code, body = h.do("PUT", h.base+"/hr/leave-routing", "foreign", `{"expected_version":2,"mode":"hospital","source_reference":"Synthetic foreign","reviewed":true}`)
	require.Equal(t, 404, code, body)
	require.EqualValues(t, 2, h.count("workforce_audit", "action='workforce.leave_routing.changed'"))
	require.EqualValues(t, 2, h.count("workforce_leave_routing_revisions", "true"))
}

type routeScopeAuthority struct {
	db       *tenantdb.DB
	subjects map[string]uuid.UUID
}

func (a *routeScopeAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, _ authz.ScopeGrant, _ time.Time, action func(context.Context) error) error {
	return a.db.WithGuardedTenantCommand(ctx, r.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, action)
}

func (a *routeScopeAuthority) AuthorizeScope(_ context.Context, r authz.ScopeRequest) (authz.ScopeGrant, error) {
	assignment, ok := a.subjects[r.Subject]
	if r.TenantID != testutil.TenantA || !ok || r.Permission != workforce.PermLeaveApprove {
		return authz.ScopeGrant{}, nil
	}
	return authz.ScopeGrant{AssignmentID: assignment.String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1}, nil
}

var routeReviewers = []string{"lead1", "lead2", "lead3", "stranger", "approver"}

func newRoutingHarness(t *testing.T) (*requestHarness, leaveTypes) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	authority := &routeScopeAuthority{subjects: map[string]uuid.UUID{}}
	for _, name := range routeReviewers {
		require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-"+name, authz.RoleStaffMember))
		authority.subjects["user-"+name] = uuid.Must(uuid.NewV7())
	}
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Roles = fga
		for _, name := range routeReviewers[:4] {
			opts.Tokens[name] = testutil.TenantA
			opts.Perms[name] = []authz.Permission{}
		}
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermLoginLinkManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermLoginLinkReview)
		opts.ScopedAuthorizer = func(db *tenantdb.DB) authz.ScopedAuthorizer { authority.db = db; return authority }
	})
	code, out := h.call("PUT", "/access/user-profiler", "admin", map[string]any{"level": "manager"})
	require.Equal(t, 200, code, out)
	types := configureLeave(t, h.call, 2026)
	h.post("2026", 1)
	return h, types
}

func (h *requestHarness) link(staff, subject string) {
	path := "/staff/" + staff + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	code, out := h.call("PUT", path, "admin", map[string]any{"subject": subject, "reason": "Synthetic verified reviewer"})
	require.Equal(h.t, 200, code, out)
	code, out = h.call("POST", path+"/decision", "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic verification"})
	require.Equal(h.t, 200, code, out)
}

func (h *requestHarness) supervise(staff, code, supervisor string) {
	status, current := h.call("GET", "/staff/"+staff+"/enrollment", "admin", nil)
	require.Equal(h.t, 200, status, current)
	status, out := h.call("PUT", "/staff/"+staff+"/enrollment", "admin", map[string]any{
		"expected_version": current["version"],
		"staff":            map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "nurse", "employment_status": "active"},
		"profile":          map[string]any{"supervisor_id": supervisor}, "credentials": []any{}, "qualifications": []any{}, "experiences": []any{},
	})
	require.Equal(h.t, 200, status, out)
}

// openRouteStep simulates the escalation interval passing; steps are otherwise immutable.
func (h *requestHarness) openRouteStep(request string, level int) {
	require.NoError(h.t, h.db.Migrate(h.t.Context(), []tenantdb.Migration{{ID: fmt.Sprintf("test_open_route_%s_%d", request, level), SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true),set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital','` + h.hospital + `',true),set_config('app.workforce_leave_route_pin','` + request + `',true);
 ALTER TABLE workforce_leave_route_steps DISABLE TRIGGER workforce_leave_routing_immutable;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE workforce_leave_route_steps SET opens_at=clock_timestamp()-interval '1 minute' WHERE request_id='` + request + `' AND level=` + fmt.Sprint(level) + `;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'fixture must open exactly one step'; END IF;
 END $$;
 ALTER TABLE workforce_leave_route_steps ENABLE TRIGGER workforce_leave_routing_immutable;`}}))
}

func (h *requestHarness) reviewQueue(user string) map[string]map[string]any {
	code, out := h.call("GET", "/leave-review-requests", user, nil)
	require.Equal(h.t, 200, code, out)
	items := map[string]map[string]any{}
	for _, item := range out["data"].([]any) {
		row := item.(map[string]any)
		items[row["id"].(string)] = row
	}
	return items
}

func (h *requestHarness) decisionEvidence(request string) map[string]any {
	var audit struct{ Changes json.RawMessage }
	require.NoError(h.t, h.scoped(false, func(tx *gorm.DB) error {
		return tx.Table("workforce_audit").Select("changes").Where("resource_id=? AND action='workforce.leave_request.approved'", request).Take(&audit).Error
	}))
	var evidence map[string]any
	require.NoError(h.t, json.Unmarshal(audit.Changes, &evidence))
	return evidence
}

func routeSteps(t *testing.T, out map[string]any) (map[string]any, []map[string]any) {
	routing, ok := out["routing"].(map[string]any)
	require.True(t, ok, "detail must carry the pinned route: %v", out)
	steps := []map[string]any{}
	for _, step := range routing["steps"].([]any) {
		steps = append(steps, step.(map[string]any))
	}
	return routing, steps
}

func TestReportingLineRouteOpensLevelByLevel(t *testing.T) {
	h, types := newRoutingHarness(t)
	nurse := h.enrolled("SYN-ROUTE-NURSE", types, "2026-01-01")
	lead1, lead2, gap, lead3, stranger := h.staff("SYN-ROUTE-LEAD1"), h.staff("SYN-ROUTE-LEAD2"), h.staff("SYN-ROUTE-GAP"), h.staff("SYN-ROUTE-LEAD3"), h.staff("SYN-ROUTE-STRANGER")
	for staff, subject := range map[string]string{lead1: "user-lead1", lead2: "user-lead2", lead3: "user-lead3", stranger: "user-stranger"} {
		h.link(staff, subject)
	}
	h.supervise(nurse, "SYN-ROUTE-NURSE", lead1)
	h.supervise(lead1, "SYN-ROUTE-LEAD1", lead2)
	h.supervise(lead2, "SYN-ROUTE-LEAD2", gap)
	h.supervise(gap, "SYN-ROUTE-GAP", lead3)

	before := uuid.NewString()
	code, out := h.file(nurse, before, "clerk", full(types.duty, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(0, "reporting_line", 24))
	require.Equal(t, 200, code, out)
	routed := uuid.NewString()
	code, out = h.file(nurse, routed, "clerk", full(types.duty, "2026-10-06", "2026-10-06"))
	require.Equal(t, 200, code, out)

	code, out = h.call("GET", "/staff/"+nurse+"/leave-requests/"+before, "clerk", nil)
	require.Equal(t, 200, code, out)
	require.NotContains(t, out, "routing", "a request filed before any policy has no route")
	code, out = h.call("GET", "/staff/"+nurse+"/leave-requests/"+routed, "clerk", nil)
	require.Equal(t, 200, code, out)
	routing, steps := routeSteps(t, out)
	require.EqualValues(t, 1, routing["version"])
	require.Equal(t, true, routing["routed"])
	require.NotContains(t, routing, "fallback")
	require.Len(t, steps, 3, "the ancestor without a reviewed login is skipped")
	opened := make([]time.Time, 3)
	for i, want := range []struct {
		staff string
		open  bool
	}{{lead1, true}, {lead2, false}, {lead3, false}} {
		require.EqualValues(t, i+1, steps[i]["level"])
		require.Equal(t, want.staff, steps[i]["staff_id"])
		require.Equal(t, want.open, steps[i]["open"], "level %d", i+1)
		var err error
		opened[i], err = time.Parse(time.RFC3339Nano, steps[i]["opens_at"].(string))
		require.NoError(t, err)
	}
	require.Equal(t, 24*time.Hour, opened[1].Sub(opened[0]))
	require.Equal(t, 48*time.Hour, opened[2].Sub(opened[0]))

	queue := h.reviewQueue("lead1")
	require.Contains(t, queue, before)
	require.NotContains(t, queue[before], "route_level", "unrouted requests keep hospital-wide review")
	require.EqualValues(t, 1, queue[routed]["route_level"])
	for _, user := range []string{"lead2", "lead3", "stranger"} {
		queue = h.reviewQueue(user)
		require.Contains(t, queue, before, user)
		require.NotContains(t, queue, routed, "%s holds no open step", user)
		code, out = h.call("GET", "/staff/"+nurse+"/leave-review-requests/"+routed, user, nil)
		require.Equal(t, 404, code, "%s: %v", user, out)
		code, out = h.act(nurse, routed, "decision", user, map[string]any{"expected_version": 1, "decision": "approve", "note": "Out-of-route decision"})
		require.Equal(t, 404, code, "%s: %v", user, out)
	}
	code, out = h.call("GET", "/staff/"+nurse+"/leave-review-requests/"+before, "stranger", nil)
	require.Equal(t, 200, code, out)

	h.openRouteStep(routed, 2)
	require.EqualValues(t, 1, h.reviewQueue("lead1")[routed]["route_level"], "an opened step stays open")
	require.EqualValues(t, 2, h.reviewQueue("lead2")[routed]["route_level"])
	require.NotContains(t, h.reviewQueue("lead3"), routed)
	code, out = h.call("GET", "/staff/"+nurse+"/leave-requests/"+routed, "clerk", nil)
	require.Equal(t, 200, code, out)
	_, steps = routeSteps(t, out)
	require.Equal(t, []any{true, true, false}, []any{steps[0]["open"], steps[1]["open"], steps[2]["open"]})

	code, out = h.act(nurse, routed, "decision", "lead2", map[string]any{"expected_version": 1, "decision": "approve", "note": "Escalated reporting-line approval"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", request(out)["status"])
	require.Equal(t, "user-lead2", request(out)["decided_by_subject"])
	evidence := h.decisionEvidence(routed)
	require.EqualValues(t, 2, evidence["route_level"])
	require.EqualValues(t, 1, evidence["routing_version"])
	require.Equal(t, lead2, evidence["actor_staff_id"])

	code, out = h.act(nurse, before, "decision", "stranger", map[string]any{"expected_version": 1, "decision": "approve", "note": "Hospital-wide review of an unrouted request"})
	require.Equal(t, 200, code, out)
	evidence = h.decisionEvidence(before)
	require.NotContains(t, evidence, "route_level")
	require.NotContains(t, evidence, "routing_version")
}

func TestReportingLineRouteFallsBackToTheLeaveDesk(t *testing.T) {
	h, types := newRoutingHarness(t)
	nurse := h.enrolled("SYN-DESK-NURSE", types, "2026-01-01")
	orphan := h.enrolled("SYN-DESK-ORPHAN", types, "2026-01-01")
	lead1, stranger, desk := h.staff("SYN-DESK-LEAD1"), h.staff("SYN-DESK-STRANGER"), h.staff("SYN-DESK-MANAGER")
	h.link(lead1, "user-lead1")
	h.link(stranger, "user-stranger")
	h.link(desk, "user-approver")
	h.supervise(nurse, "SYN-DESK-NURSE", lead1)
	code, out := h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(0, "reporting_line", 24))
	require.Equal(t, 200, code, out)

	unrouteable := uuid.NewString()
	code, out = h.file(orphan, unrouteable, "clerk", full(types.duty, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	code, out = h.call("GET", "/staff/"+orphan+"/leave-requests/"+unrouteable, "clerk", nil)
	require.Equal(t, 200, code, out)
	routing, steps := routeSteps(t, out)
	require.Empty(t, steps)
	require.Equal(t, "no_eligible_reporting_line", routing["fallback"])
	for _, user := range []string{"lead1", "stranger"} {
		require.NotContains(t, h.reviewQueue(user), unrouteable, user)
		code, out = h.act(orphan, unrouteable, "decision", user, map[string]any{"expected_version": 1, "decision": "approve", "note": "Only the desk may decide"})
		require.Equal(t, 404, code, "%s: %v", user, out)
	}
	code, out = h.act(orphan, unrouteable, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": "Leave desk decision"})
	require.Equal(t, 200, code, out)
	evidence := h.decisionEvidence(unrouteable)
	require.Equal(t, "desk", evidence["route_level"])
	require.EqualValues(t, 1, evidence["routing_version"])
	require.NotContains(t, evidence, "actor_staff_id", "a desk decision uses hospital authority, not the scoped grant")

	pinned := uuid.NewString()
	code, out = h.file(nurse, pinned, "clerk", full(types.duty, "2026-10-06", "2026-10-06"))
	require.Equal(t, 200, code, out)
	h.supervise(nurse, "SYN-DESK-NURSE", stranger)
	code, out = h.call("PUT", "/hr/leave-routing", "admin", routingPolicy(1, "hospital", nil))
	require.Equal(t, 200, code, out)
	code, out = h.call("GET", "/staff/"+nurse+"/leave-requests/"+pinned, "clerk", nil)
	require.Equal(t, 200, code, out)
	routing, steps = routeSteps(t, out)
	require.EqualValues(t, 1, routing["version"], "a policy change leaves the pinned route intact")
	require.Len(t, steps, 1)
	require.Equal(t, lead1, steps[0]["staff_id"], "a chain change leaves the pinned route intact")
	require.Contains(t, h.reviewQueue("lead1"), pinned)
	require.NotContains(t, h.reviewQueue("stranger"), pinned)

	later := uuid.NewString()
	code, out = h.file(nurse, later, "clerk", full(types.duty, "2026-10-07", "2026-10-07"))
	require.Equal(t, 200, code, out)
	code, out = h.call("GET", "/staff/"+nurse+"/leave-requests/"+later, "clerk", nil)
	require.Equal(t, 200, code, out)
	routing, steps = routeSteps(t, out)
	require.EqualValues(t, 2, routing["version"])
	require.Equal(t, false, routing["routed"])
	require.Empty(t, steps)
	require.Contains(t, h.reviewQueue("stranger"), later, "hospital mode keeps hospital-wide review")

	code, out = h.act(nurse, pinned, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": "Desk decision with an unrelated scoped link"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "desk", h.decisionEvidence(pinned)["route_level"], "a desk manager without an open step falls back to the desk")

	pin := `SELECT set_config('app.workforce_leave_route_pin','` + later + `',true)`
	for name, refusal := range map[string]struct {
		statements []string
		message    string
	}{
		"unroute a request":    {[]string{`UPDATE workforce_leave_requests SET routed=false,status='cancelled',version=version+1 WHERE id='` + pinned + `'`}, "pinned at filing"},
		"repin a request":      {[]string{`UPDATE workforce_leave_requests SET routing_version=2,status='cancelled',version=version+1 WHERE id='` + pinned + `'`}, "pinned at filing"},
		"move a step":          {[]string{`UPDATE workforce_leave_route_steps SET opens_at=now() WHERE request_id='` + pinned + `'`}, "append only"},
		"remove a step":        {[]string{`DELETE FROM workforce_leave_route_steps WHERE request_id='` + pinned + `'`}, "append only"},
		"add a step later":     {[]string{pin, `INSERT INTO workforce_leave_route_steps(tenant_id,hospital_id,request_id,level,staff_id,opens_at) SELECT tenant_id,hospital_id,id,1,'` + stranger + `',created_at FROM workforce_leave_requests WHERE id='` + later + `'`}, "pinned when the request is filed"},
		"rewrite the policy":   {[]string{`UPDATE workforce_leave_routing_revisions SET escalation_hours=1`}, "append only"},
		"skip a policy number": {[]string{`SELECT set_config('app.workforce_leave_routing','authorized',true)`, `INSERT INTO workforce_leave_routing_revisions(tenant_id,hospital_id,version,mode,source_reference,reviewed_by_subject) VALUES('` + testutil.TenantA + `','` + h.hospital + `',9,'hospital','Synthetic skip','user-admin')`}, "one version at a time"},
	} {
		err := h.scoped(true, func(tx *gorm.DB) error {
			for _, statement := range refusal.statements {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		})
		var denied *pgconn.PgError
		require.True(t, errors.As(err, &denied), "%s: expected a database refusal, got %v", name, err)
		require.Equal(t, "23514", denied.Code, name)
		require.Contains(t, denied.Message, refusal.message, name)
	}
}
