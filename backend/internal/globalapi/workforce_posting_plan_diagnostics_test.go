package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type postingPlanMetrics struct {
	TotalCost                                    float64
	Rows, JITFunctions                           int64
	JITExpressions, JITInlining, JITOptimization bool
}

func (m postingPlanMetrics) String() string {
	return fmt.Sprintf("total_cost=%.2f plan_rows=%d jit_functions=%d jit_expressions=%t jit_inlining=%t jit_optimization=%t", m.TotalCost, m.Rows, m.JITFunctions, m.JITExpressions, m.JITInlining, m.JITOptimization)
}

func postingUpdatePlanMetrics(raw []byte) (postingPlanMetrics, bool) {
	var result postingPlanMetrics
	if len(raw) > 1<<20 {
		return result, false
	}
	var plans []struct {
		Plan *struct {
			TotalCost *float64 `json:"Total Cost"`
			Rows      *float64 `json:"Plan Rows"`
		}
		JIT struct {
			Functions int64
			Options   struct{ Expressions, Inlining, Optimization bool }
		}
	}
	if json.Unmarshal(raw, &plans) != nil || len(plans) != 1 || plans[0].Plan == nil || plans[0].Plan.TotalCost == nil || plans[0].Plan.Rows == nil {
		return result, false
	}
	plan := plans[0]
	cost, rows := *plan.Plan.TotalCost, *plan.Plan.Rows
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) || rows < 0 || rows > 9007199254740991 || rows != math.Trunc(rows) || plan.JIT.Functions < 0 {
		return result, false
	}
	result = postingPlanMetrics{TotalCost: cost, Rows: int64(rows), JITFunctions: plan.JIT.Functions, JITExpressions: plan.JIT.Options.Expressions, JITInlining: plan.JIT.Options.Inlining, JITOptimization: plan.JIT.Options.Optimization}
	return result, true
}

var postingUpdateBind = regexp.MustCompile(`\$([0-9]+)`)

func postingUpdateExplainTemplate(query string, bindings int) (string, bool) {
	query = strings.TrimSpace(query)
	if !strings.HasPrefix(query, "UPDATE workforce_posting_proposals SET ") || bindings < 1 || bindings > 64 || len(query) > 8192 || strings.ContainsAny(query, ";\x00") {
		return "", false
	}
	seen, valid := 0, true
	out := postingUpdateBind.ReplaceAllStringFunc(query, func(bind string) string {
		n, err := strconv.Atoi(bind[1:])
		seen++
		if err != nil || n != seen {
			valid = false
		}
		return "?"
	})
	if !valid || seen != bindings {
		return "", false
	}
	return "EXPLAIN (FORMAT JSON) " + out, true
}

func observePostingProposalUpdatePlan(t *testing.T, db *tenantdb.DB, tenant, proposal string) {
	t.Helper()
	var fired atomic.Bool
	require.NoError(t, db.WithTenant(context.Background(), tenant, func(tx *gorm.DB) error {
		return tx.Callback().Raw().Before("gorm:raw").Register("test:posting_update_plan", func(current *gorm.DB) {
			if fired.Load() || !strings.HasPrefix(strings.TrimSpace(current.Statement.SQL.String()), "UPDATE workforce_posting_proposals SET ") {
				return
			}
			matched := false
			for _, value := range current.Statement.Vars {
				if text, ok := value.(string); ok && text == proposal {
					matched = true
					break
				}
			}
			if !matched || fired.Swap(true) {
				return
			}
			template, ok := postingUpdateExplainTemplate(current.Statement.SQL.String(), len(current.Statement.Vars))
			if !ok {
				t.Log("posting_update_plan available=false reason=unsupported_bindings")
				return
			}
			args := append([]any(nil), current.Statement.Vars...)
			diagnostic := current.Session(&gorm.Session{NewDB: true, SkipHooks: true, Logger: logger.Discard})
			var enabled, available bool
			var threshold float64
			if err := diagnostic.Raw("SELECT current_setting('jit') = 'on', pg_jit_available(), current_setting('jit_above_cost')::double precision").Row().Scan(&enabled, &available, &threshold); err == nil {
				t.Logf("posting_update_jit enabled=%t available=%t threshold=%.2f", enabled, available, threshold)
			} else {
				t.Log("posting_update_jit available=false reason=configuration_read_failed")
			}
			started := time.Now()
			var raw []byte
			err := current.Session(&gorm.Session{NewDB: true, SkipHooks: true, Logger: logger.Discard}).Raw(template, args...).Row().Scan(&raw)
			if err != nil {
				t.Logf("posting_update_plan available=false elapsed_ms=%d", time.Since(started).Milliseconds())
				return
			}
			metrics, ok := postingUpdatePlanMetrics(raw)
			if !ok {
				t.Log("posting_update_plan available=false reason=unsupported_metrics")
				return
			}
			t.Logf("posting_update_plan available=true planning_elapsed_ms=%d %s", time.Since(started).Milliseconds(), metrics.String())
		})
	}))
}
