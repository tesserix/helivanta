package workforce_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test measures the real owner's forced-RLS point reads.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
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

func TestStaffPointReadJITPreservesTenantIsolation(t *testing.T) {
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "staff": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
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
	for _, tenant := range []struct {
		name, id string
		rows     float64
	}{{"owner", testutil.TenantA, 1}, {"foreign", testutil.TenantB, 0}} {
		t.Run(tenant.name, func(t *testing.T) {
			require.NoError(t, db.WithTenant(ctx, tenant.id, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject','user-staff',true)`, hospital).Error; err != nil {
					return err
				}
				for _, mode := range []string{"on", "off"} {
					if err := tx.Exec(`SELECT set_config('jit',?,true)`, mode).Error; err != nil {
						return err
					}
					var raw string
					if err := tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,version FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, testutil.TenantA, hospital, staff).Scan(&raw).Error; err != nil {
						return err
					}
					var plans []staffPointPlan
					require.NoError(t, json.Unmarshal([]byte(raw), &plans))
					require.Len(t, plans, 1)
					plan := plans[0]
					require.Equal(t, tenant.rows, plan.Plan.ActualRows, "JIT must not change current-tenant row admission")
					if mode == "off" {
						require.Zero(t, plan.JIT.Functions)
					}
					t.Logf("Staff point plan tenant=%s jit=%s operator=%s estimated_rows=%.0f actual_rows=%.0f cost=%.3f planning_ms=%.3f execution_ms=%.3f jit_functions=%d jit_ms=%.3f", tenant.name, mode, plan.Plan.Node, plan.Plan.Rows, plan.Plan.ActualRows, plan.Plan.Cost, plan.Planning, plan.Execution, plan.JIT.Functions, plan.JIT.Timing.Total)
				}
				return nil
			}))
		})
	}
}
