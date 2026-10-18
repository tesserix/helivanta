package medicore_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestVisitInsightsCountWholeCohortWithoutPatientDetails(t *testing.T) {
	r, db, ctx := setup(t)
	ctx = tenantdb.WithActor(ctx, "insights-fixture")
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO medicore_visits (tenant_id, patient_name, department, created_at)
			 SELECT ?, 'Private patient', CASE WHEN n < 151 THEN 'IPD' ELSE 'OPD' END,
			 '2026-10-01T20:00:00Z'::timestamptz FROM generate_series(1,200) n`, tenant).Error
		}))
	}
	w := do(r, "GET", "/v1/medicore/insights?from=2026-10-01&to=2026-10-03&interval=day&timezone=Asia%2FKolkata", "tokA", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var body struct {
		Totals map[string]int `json:"totals"`
		Series []struct {
			Date   string         `json:"date"`
			Counts map[string]int `json:"counts"`
		} `json:"series"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, map[string]int{"IPD": 150, "OPD": 50}, body.Totals)
	require.Len(t, body.Series, 3)
	require.Equal(t, "2026-10-02", body.Series[1].Date)
	require.Equal(t, 150, body.Series[1].Counts["IPD"])
	require.Equal(t, 0, body.Series[0].Counts["IPD"])
	require.Equal(t, 0, body.Series[2].Counts["OPD"])
	require.NotContains(t, w.Body.String(), "Private patient")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestVisitInsightsAtHospitalScale(t *testing.T) {
	r, db, ctx := setup(t)
	ctx = tenantdb.WithActor(ctx, "synthetic-load-fixture")
	var plan []struct {
		Line string `gorm:"column:QUERY PLAN"`
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO medicore_visits (tenant_id,patient_name,department,created_at)
		 SELECT ?, 'Synthetic load fixture', CASE WHEN n%4=0 THEN 'IPD' ELSE 'OPD' END,
		 '2026-01-01T00:00:00Z'::timestamptz+n*interval '5 minutes' FROM generate_series(0,99999)n`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Raw(`EXPLAIN (ANALYZE,BUFFERS)
		 SELECT date_trunc('day',created_at AT TIME ZONE 'UTC')::date::text,department,count(*)
		 FROM medicore_visits WHERE tenant_id=? AND created_at >= '2026-07-01T00:00:00Z' AND created_at < '2026-07-02T00:00:00Z' GROUP BY 1,2`, testutil.TenantA).Scan(&plan).Error
	}))
	lines := make([]string, 0, len(plan))
	for _, row := range plan {
		lines = append(lines, row.Line)
	}
	t.Log("100,000-row query plan:\n" + strings.Join(lines, "\n"))
	w := do(r, "GET", "/v1/medicore/insights?from=2026-07-01&to=2026-07-01&interval=day&timezone=UTC", "tokA", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var body struct {
		Totals map[string]int `json:"totals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, map[string]int{"IPD": 72, "OPD": 216}, body.Totals)
}
