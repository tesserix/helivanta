package globalapi

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var postingMetricFunctions = []string{
	"workforce_posting_proposal_head_guard", "workforce_posting_proposal_revision_guard",
	"workforce_posting_chain_review_guard", "workforce_posting_chain_source_guard",
	"workforce_posting_chain_integrity", "workforce_posting_root_approval_integrity",
	"workforce_posting_cutoff_guard", "workforce_posting_cutoff_integrity",
	"workforce_posting_proposal_context", "workforce_posting_proposal_decider",
	"workforce_posting_chain_approval_context", "workforce_posting_approval_context",
	"workforce_posting_root_approval_context", "workforce_posting_target_current",
}

type postingFunctionMetric struct {
	Name            string
	Calls           int64
	TotalMS, SelfMS float64
}

func validPostingFunctionMetric(row postingFunctionMetric) bool {
	known := false
	for _, name := range postingMetricFunctions {
		if row.Name == name {
			known = true
			break
		}
	}
	return known && row.Calls >= 0 && row.Calls <= 9007199254740991 &&
		row.TotalMS >= 0 && row.SelfMS >= 0 && row.SelfMS <= row.TotalMS &&
		!math.IsNaN(row.TotalMS) && !math.IsInf(row.TotalMS, 0) &&
		!math.IsNaN(row.SelfMS) && !math.IsInf(row.SelfMS, 0)
}

func postingFunctionFailureMetrics(t *testing.T, db *tenantdb.DB, tenant string) {
	t.Helper()
	if os.Getenv("HELIVANTA_TEST_POSTGRES_TRACK_FUNCTIONS") != "1" || !t.Failed() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
	defer cancel()
	var rows []postingFunctionMetric
	err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{Logger: logger.Discard})
		if err := tx.Exec(`SELECT pg_stat_force_next_flush()`).Error; err != nil {
			return err
		}
		return tx.Table("pg_stat_user_functions").Select("funcname AS name,calls,total_time AS total_ms,self_time AS self_ms").Where("schemaname='public' AND funcname IN ?", postingMetricFunctions).Order("self_time DESC,funcname").Limit(len(postingMetricFunctions)).Scan(&rows).Error
	})
	if err != nil || len(rows) == 0 {
		t.Log("posting_function_metrics available=false")
		return
	}
	for _, row := range rows {
		if !validPostingFunctionMetric(row) {
			t.Log("posting_function_metrics available=false")
			return
		}
	}
	for _, row := range rows {
		t.Logf("posting_function_metrics function=%s calls=%d total_ms=%.2f self_ms=%.2f", row.Name, row.Calls, row.TotalMS, row.SelfMS)
	}
}

func TestPostingFunctionMetricsExcludeUnknownNamesAndUnsafeNumbers(t *testing.T) {
	row := postingFunctionMetric{Name: "workforce_posting_proposal_head_guard", Calls: 3, TotalMS: 123.5, SelfMS: 100}
	require.True(t, validPostingFunctionMetric(row))
	for _, change := range []func(*postingFunctionMetric){
		func(r *postingFunctionMetric) { r.Name = "SYNTHETIC-PRIVATE-SOURCE-PROOF" },
		func(r *postingFunctionMetric) { r.Calls = -1 },
		func(r *postingFunctionMetric) { r.TotalMS = math.Inf(1) },
		func(r *postingFunctionMetric) { r.SelfMS = math.NaN() },
		func(r *postingFunctionMetric) { r.SelfMS = r.TotalMS + 1 },
	} {
		next := row
		change(&next)
		require.False(t, validPostingFunctionMetric(next))
	}
}
