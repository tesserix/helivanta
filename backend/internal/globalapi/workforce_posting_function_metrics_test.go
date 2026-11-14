package globalapi

import (
	"context"
	"math"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostingProfileAcceptsOnlyCanonicalFunctionNamesAndBoundedMetrics(t *testing.T) {
	names := postingProfileFunctionNames()
	require.Contains(t, names, "workforce_posting_proposal_decider")
	require.Contains(t, names, "workforce_employment_generation_context")
	row := postingFunctionMetric{Name: "workforce_posting_proposal_decider", Calls: 3, TotalMS: 123.5, SelfMS: 100}
	require.True(t, validPostingFunctionMetric(row, names))
	for _, change := range []func(*postingFunctionMetric){
		func(r *postingFunctionMetric) { r.Name = "workforce_synthetic_private_source_proof" },
		func(r *postingFunctionMetric) { r.Calls = -1 },
		func(r *postingFunctionMetric) { r.TotalMS = math.Inf(1) },
		func(r *postingFunctionMetric) { r.SelfMS = math.NaN() },
		func(r *postingFunctionMetric) { r.SelfMS = r.TotalMS + 1 },
	} {
		next := row
		change(&next)
		require.False(t, validPostingFunctionMetric(next, names))
	}
}

type postingFunctionMetric struct {
	Name            string
	Calls           int64
	TotalMS, SelfMS float64
}

func postingProfileFunctionNames() []string {
	pattern := regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(workforce_[a-z0-9_]+)\s*\(`)
	names := []string{}
	for _, migration := range workforce.New().Migrations() {
		for _, match := range pattern.FindAllStringSubmatch(migration.SQL, -1) {
			names = append(names, match[1])
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func validPostingFunctionMetric(row postingFunctionMetric, known []string) bool {
	return slices.Contains(known, row.Name) && row.Calls >= 0 && row.Calls <= 9007199254740991 &&
		row.TotalMS >= 0 && row.TotalMS <= 9007199254740991 && row.SelfMS >= 0 && row.SelfMS <= row.TotalMS &&
		!math.IsNaN(row.TotalMS) && !math.IsInf(row.TotalMS, 0) &&
		!math.IsNaN(row.SelfMS) && !math.IsInf(row.SelfMS, 0)
}

func postingFunctionFailureMetrics(t *testing.T, db *tenantdb.DB, tenant string) {
	t.Helper()
	if os.Getenv("HELIVANTA_TEST_POSTGRES_TRACK_FUNCTIONS") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
	defer cancel()
	known := postingProfileFunctionNames()
	var rows []postingFunctionMetric
	err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{Logger: logger.Discard})
		if err := tx.Exec(`SELECT pg_stat_force_next_flush()`).Error; err != nil {
			return err
		}
		return tx.Table("pg_stat_user_functions").Select("funcname AS name,calls,total_time AS total_ms,self_time AS self_ms").Where("schemaname='public' AND calls>0 AND funcname IN ?", known).Order("self_time DESC,funcname").Limit(30).Scan(&rows).Error
	})
	if err != nil || len(rows) == 0 {
		t.Log("posting_function_metrics available=false")
		return
	}
	for _, row := range rows {
		if !validPostingFunctionMetric(row, known) {
			t.Log("posting_function_metrics available=false")
			return
		}
	}
	for _, row := range rows {
		t.Logf("posting_function_metrics function=%s calls=%d total_ms=%.2f self_ms=%.2f", row.Name, row.Calls, row.TotalMS, row.SelfMS)
	}
}
