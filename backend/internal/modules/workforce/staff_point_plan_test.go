package workforce_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test measures the real owner's forced-RLS point reads.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type staffPointPlan struct {
	Plan struct {
		Node       string  `json:"Node Type"`
		Rows       float64 `json:"Plan Rows"`
		ActualRows float64 `json:"Actual Rows"`
		Cost       float64 `json:"Total Cost"`
	} `json:"Plan"`
	Planning  float64 `json:"Planning Time"`
	Execution float64 `json:"Execution Time"`
	JIT       struct {
		Functions int `json:"Functions"`
		Timing    struct {
			Total float64 `json:"Total"`
		} `json:"Timing"`
	} `json:"JIT"`
}

type staffPointPlanPreviousModule struct{ *workforce.Module }

func (module staffPointPlanPreviousModule) Migrations() []tenantdb.Migration {
	all := module.Module.Migrations()
	return all[:len(all)-1]
}

type staffPointPolicyWriteRule struct {
	Name, Command, Roles, WriteCheck string
	Permissive                       bool
}

func TestStaffPointReadPlansPreserveTenantIsolation(t *testing.T) {
	module := workforce.New()
	migrations := module.Migrations()
	forward := migrations[len(migrations)-1]
	require.Equal(t, "0120_workforce_staff_policy_plans", forward.ID)
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "staff": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{staffPointPlanPreviousModule{module}},
	})
	hospital, staff := uuid.NewString(), uuid.NewString()
	root := "/v1/workforce/hospitals/" + hospital
	for _, command := range []struct{ path, token, body string }{
		{root, "admin", `{"name":"Synthetic Staff query plans"}`},
		{root + "/access/user-staff", "admin", `{"level":"manager"}`},
		{root + "/staff/" + staff, "staff", `{"employee_code":"PLAN-01","display_name":"Synthetic plan Staff","job_category":"nurse","employment_status":"active"}`},
	} {
		response := testutil.Do(router, http.MethodPut, command.path, command.token, command.body)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	writeRules := func() []staffPointPolicyWriteRule {
		var rules []staffPointPolicyWriteRule
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT polname AS name,polcmd::text AS command,polroles::text AS roles,polpermissive AS permissive,COALESCE(pg_get_expr(polwithcheck,polrelid),'')AS write_check FROM pg_policy WHERE polrelid='workforce_staff'::regclass ORDER BY polname`).Scan(&rules).Error
		}))
		return rules
	}
	measure := func(phase string, repeats int) map[string]staffPointPlan {
		measured := make(map[string]staffPointPlan)
		for _, tenant := range []struct {
			name, id string
			rows     float64
		}{{"owner", testutil.TenantA, 1}, {"foreign", testutil.TenantB, 0}} {
			t.Run(phase+"/"+tenant.name, func(t *testing.T) {
				require.NoError(t, db.WithTenant(ctx, tenant.id, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject','user-staff',true)`, hospital).Error; err != nil {
						return err
					}
					for attempt := range repeats {
						var raw string
						if err := tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,version FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, testutil.TenantA, hospital, staff).Scan(&raw).Error; err != nil {
							return err
						}
						var plans []staffPointPlan
						require.NoError(t, json.Unmarshal([]byte(raw), &plans))
						require.Len(t, plans, 1)
						plan := plans[0]
						require.Equal(t, tenant.rows, plan.Plan.ActualRows, "cached predicate plans must preserve current-tenant row admission")
						if attempt == 0 {
							measured[tenant.name] = plan
						}
						t.Logf("Staff point plan phase=%s tenant=%s attempt=%d operator=%s estimated_rows=%.0f actual_rows=%.0f cost=%.3f planning_ms=%.3f execution_ms=%.3f jit_functions=%d jit_ms=%.3f", phase, tenant.name, attempt, plan.Plan.Node, plan.Plan.Rows, plan.Plan.ActualRows, plan.Plan.Cost, plan.Planning, plan.Execution, plan.JIT.Functions, plan.JIT.Timing.Total)
					}
					return nil
				}))
			})
		}
		return measured
	}
	beforeRules := writeRules()
	before := measure("before", 1)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{forward}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "the canonical tenant visibility and write checks must remain visible to the startup linter")
	require.Equal(t, beforeRules, writeRules(), "read planning must retain every policy's write predicate, role, command and permissive/restrictive mode")
	after := measure("after", 2)
	for _, tenant := range []string{"owner", "foreign"} {
		require.Less(t, after[tenant].Plan.Cost, before[tenant].Plan.Cost, "the outer point plan must not expand the stored read predicates")
	}
	integrity := func(expected bool) {
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			var proof struct{ Assignment, Operating, Forced bool }
			if err := tx.Raw(`SELECT workforce_assignment_selection_integrity()AS assignment,workforce_operating_selection_integrity()AS operating,(SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='workforce_staff'::regclass)AS forced`).Scan(&proof).Error; err != nil {
				return err
			}
			require.Equal(t, expected, proof.Assignment)
			require.Equal(t, expected, proof.Operating)
			require.True(t, proof.Forced)
			return nil
		}))
	}
	integrity(true)
	for _, name := range []string{"workforce_staff_read_policy", "workforce_staff_lock_policy"} {
		var original string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_get_functiondef(to_regprocedure(?))`, name+"(workforce_staff)").Scan(&original).Error
		}))
		restoration := tenantdb.Migration{ID: "synthetic_staff_plan_restore_" + uuid.NewString(), SQL: original}
		restore := func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{restoration}))
		}
		t.Cleanup(restore)
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "synthetic_staff_plan_fault_" + uuid.NewString(), SQL: "CREATE OR REPLACE FUNCTION " + name + "(staff_record workforce_staff)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $$ BEGIN RETURN true;END $$;"}}))
		integrity(false)
		restore()
		integrity(true)
	}
}
