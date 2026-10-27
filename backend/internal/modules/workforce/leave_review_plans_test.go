//go:build workforce_perf

package workforce_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestLeaveReviewRetainedHistoryPlans(t *testing.T) {
	type statement struct {
		kind string
		sql  string
		args []any
	}
	var mu sync.Mutex
	var recording bool
	var statements []statement
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-scoped", authz.RoleStaffMember))
	authority := &leaveScopeAuthority{grant: authz.ScopeGrant{AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1}}
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Roles = fga
		opts.Tokens["scoped"] = testutil.TenantA
		opts.Perms["scoped"] = []authz.Permission{}
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermLoginLinkManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermLoginLinkReview)
		opts.ScopedAuthorizer = func(db *tenantdb.DB) authz.ScopedAuthorizer {
			authority.db = db
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Callback().Query().After("gorm:query").Register("test:leave_review_plan", func(query *gorm.DB) {
					mu.Lock()
					defer mu.Unlock()
					sql := query.Statement.SQL.String()
					kind := ""
					if strings.Contains(sql, "staff_id<>") && strings.Contains(sql, "requested_by_subject<>") {
						kind = "queue"
					} else if strings.Contains(sql, "workforce_login_links") && strings.Contains(sql, "JOIN workforce_hospitals h") {
						kind = "discovery"
					}
					if recording && query.Error == nil && kind != "" {
						statements = append(statements, statement{kind, sql, append([]any(nil), query.Statement.Vars...)})
					}
				})
			}))
			return authority
		}
	})
	code, out := h.call("PUT", "/access/user-profiler", "admin", map[string]any{"level": "manager"})
	require.Equal(t, 200, code, out)
	types := configureLeave(t, h.call, 2026)
	actor := h.enrolled("SYN-PLAN-REVIEWER", types, "2026-01-01")
	seedStaff := h.enrolled("SYN-PLAN-SEED", types, "2026-01-01")
	require.NoError(t, h.scoped(false, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','manage',true),set_config('test.leave_plan_actor',?,true)`, actor).Error; err != nil {
			return err
		}
		return tx.Exec(`DO $fixture$
		 DECLARE n integer; link_id uuid;
		 BEGIN FOR n IN 1..1000 LOOP
		  link_id:=('019a0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid;
		  INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason)
		  VALUES(current_setting('app.tenant_id')::uuid,current_setting('app.workforce_hospital')::uuid,current_setting('test.leave_plan_actor')::uuid,link_id,'user-scoped','pending',1,'user-admin','Synthetic retained binding','user-admin','Synthetic retained binding');
		  UPDATE workforce_login_links SET status='withdrawn',version=2,reason='Withdrawn synthetic binding' WHERE tenant_id=current_setting('app.tenant_id')::uuid AND hospital_id=current_setting('app.workforce_hospital')::uuid AND id=link_id;
		 END LOOP; END $fixture$`).Error
	}))
	link := "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	h.call("PUT", link, "admin", map[string]any{"subject": "user-scoped", "reason": "Synthetic query-plan fixture"})
	code, out = h.call("POST", link+"/decision", "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic fixture verification"})
	require.Equal(t, 200, code, out)
	seed := uuid.NewString()
	code, out = h.file(seedStaff, seed, "clerk", full(types.duty, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	staff := make([]string, 100)
	for i := range staff {
		staff[i] = h.enrolled(fmt.Sprintf("SYN-PLAN-HISTORY-%03d", i), types, "2026-01-01")
	}
	started := time.Now()
	for batch := range 10 {
		require.NoError(t, h.scoped(true, func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO workforce_leave_requests
			 (tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,assignment_revision_id,leave_year,unit,start_on,end_on,half_days,reason,status,requested_by_subject,version)
			 SELECT r.tenant_id,r.hospital_id,md5(a.staff_id::text||d.day::text||?::text)::uuid,a.staff_id,r.leave_type_id,r.leave_type_revision_id,a.revision_id,r.leave_year,'full_day',d.day,d.day,2,'Synthetic retained query-plan history','pending',current_setting('app.workforce_subject'),1
			 FROM workforce_leave_requests r
			 JOIN workforce_staff_leave_assignments a ON (a.tenant_id,a.hospital_id)=(r.tenant_id,r.hospital_id)
			 JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
			 CROSS JOIN (SELECT DATE '2026-01-01'+i AS day FROM generate_series(0,179) i WHERE extract(isodow FROM DATE '2026-01-01'+i)<6 ORDER BY i LIMIT 100) d
			 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND s.employee_code LIKE 'SYN-PLAN-HISTORY-%'`, fmt.Sprint(batch), testutil.TenantA, h.hospital, seed).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_leave_request_days(tenant_id,hospital_id,request_id,day,half_days,holiday,calendar_revision_id)
			 SELECT r.tenant_id,r.hospital_id,r.id,r.start_on,2,false,d.calendar_revision_id
			 FROM workforce_leave_requests r JOIN workforce_leave_request_days d ON (d.tenant_id,d.hospital_id,d.request_id)=(r.tenant_id,r.hospital_id,?::uuid)
			 WHERE r.tenant_id=? AND r.hospital_id=? AND r.reason='Synthetic retained query-plan history' AND r.status='pending'`, seed, testutil.TenantA, h.hospital).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject)
			 SELECT tenant_id,hospital_id,md5(id::text||'filed')::uuid,id,version,status,requested_by_subject FROM workforce_leave_requests
			 WHERE tenant_id=? AND hospital_id=? AND reason='Synthetic retained query-plan history' AND status='pending'`, testutil.TenantA, h.hospital).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE workforce_leave_requests SET status='cancelled',version=2 WHERE tenant_id=? AND hospital_id=? AND reason='Synthetic retained query-plan history' AND status='pending'`, testutil.TenantA, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject)
			 SELECT tenant_id,hospital_id,md5(id::text||'cancelled')::uuid,id,version,status,requested_by_subject FROM workforce_leave_requests
			 WHERE tenant_id=? AND hospital_id=? AND reason='Synthetic retained query-plan history' AND status='cancelled'
			 AND NOT EXISTS(SELECT 1 FROM workforce_leave_request_events e WHERE e.tenant_id=workforce_leave_requests.tenant_id AND e.hospital_id=workforce_leave_requests.hospital_id AND e.request_id=workforce_leave_requests.id AND e.version=2)`, testutil.TenantA, h.hospital).Error
		}))
		t.Logf("retained batch %d/10 committed; elapsed=%s", batch+1, time.Since(started))
	}
	require.EqualValues(t, 100000, h.count("workforce_leave_requests", "status='cancelled'"))
	t.Logf("retained fixture: 100000 requests, 100000 days, 200000 events; setup=%s", time.Since(started))
	for _, person := range staff[:51] {
		code, out = h.file(person, uuid.NewString(), "clerk", full(types.duty, "2026-10-06", "2026-10-06"))
		require.Equal(t, 200, code, out)
	}
	require.NoError(t, h.db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec("ANALYZE workforce_leave_requests; ANALYZE workforce_leave_request_days; ANALYZE workforce_leave_request_events; ANALYZE workforce_staff; ANALYZE workforce_login_links").Error
	}))
	mu.Lock()
	recording = true
	mu.Unlock()
	discoveryStatus, discoveryBody := h.do("GET", "/v1/workforce/leave-review-scopes", "scoped", "")
	require.Equal(t, 200, discoveryStatus, discoveryBody)
	code, out = h.call("GET", "/leave-review-requests", "scoped", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 50)
	page := out["page"].(map[string]any)
	require.Equal(t, true, page["has_more"])
	code, out = h.call("GET", "/leave-review-requests?cursor="+page["next_cursor"].(string), "scoped", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2)
	mu.Lock()
	recording = false
	queries := append([]statement(nil), statements...)
	mu.Unlock()
	require.Len(t, queries, 3, "capture discovery and both actual owner queue queries")
	for i, query := range queries {
		require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-scoped',true),set_config('app.workforce_hospital',?,true)`, h.hospital).Error; err != nil {
				return err
			}
			if query.kind == "discovery" {
				if err := tx.Exec(`SELECT set_config('app.workforce_leave_discovery','authorized',true)`).Error; err != nil {
					return err
				}
			} else if err := tx.Exec(`SELECT set_config('app.workforce_leave_actor_check','authorized',true),set_config('app.workforce_leave_review_actor',?,true),set_config('app.workforce_leave_review_read','authorized',true),set_config('app.workforce_leave_review_read_kind','queue',true),set_config('app.workforce_leave_review_read_staff',?,true),set_config('app.workforce_leave_review_read_request',?,true)`, actor, uuid.Nil.String(), uuid.Nil.String()).Error; err != nil {
				return err
			}
			var raw []byte
			if err := tx.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query.sql, query.args...).Scan(&raw); err != nil {
				return err
			}
			var plan []map[string]any
			if err := json.Unmarshal(raw, &plan); err != nil {
				return err
			}
			t.Logf("%s query %d plan: %s", query.kind, i+1, raw)
			root := plan[0]["Plan"].(map[string]any)
			if query.kind == "discovery" {
				require.Contains(t, string(raw), "workforce_login_links_review_discovery")
				require.Equal(t, float64(1), root["Actual Rows"])
			} else {
				require.Contains(t, string(raw), "workforce_leave_requests_review_queue")
				require.LessOrEqual(t, root["Actual Rows"].(float64), float64(51))
			}
			require.Less(t, root["Shared Hit Blocks"].(float64)+root["Shared Read Blocks"].(float64), float64(5000), "bounded page work must not scan retained history")
			return nil
		}))
	}
}
