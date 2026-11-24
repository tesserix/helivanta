package globalapi

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeCapacity(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, root, pharmacy, outlet, product, quarantine, owner, counter, reviewer string) {
	t.Helper()
	if os.Getenv("HELIVANTA_HOSPITAL_STOCKTAKE_CAPACITY") != "1" {
		return
	}
	require.Empty(t, os.Getenv("HELIVANTA_HOSPITAL_STOCKTAKE_BROWSER_URL"), "capacity uses its isolated source fixture, not the browser's additional count")
	ownerTx := func(run func(*gorm.DB) error) {
		t.Helper()
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject',?,true)`, pharmacy, outlet, owner).Error; err != nil {
				return err
			}
			return run(tx)
		}))
	}
	seed := func(first, last int) {
		t.Helper()
		ownerTx(func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO pharmacy_stock_batches(tenant_id,outlet_id,id,product_id,product_version,lot_number,expires_on,mrp_minor)
 SELECT ?,?,('019a2b20-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,1,'TEST-HOSPITAL-CAP-'||i,'2030-12-31',12500 FROM generate_series(?::integer,?::integer)i`, testutil.TenantA, outlet, product, first, last).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
 SELECT ?,?,('019a2b21-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,('019a2b20-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,'receipt',10,'TEST hospital capacity fixture','TEST-hospital-capacity:'||i,? FROM generate_series(?::integer,?::integer)i`, testutil.TenantA, outlet, quarantine, owner, first, last).Error
		})
	}
	// The real workflow already has one 32-unit bucket; bulk fixture loading adds only capacity data.
	seed(1, 9999)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_stocktake_capacity_statistics", SQL: "ANALYZE pharmacy_stock_batches; ANALYZE pharmacy_stock_movements;"}}))
	measure := func(name string, run func() map[string]any) map[string]any {
		t.Helper()
		started := time.Now()
		result := run()
		elapsed := time.Since(started)
		t.Logf("hospital %s elapsed %s", name, elapsed)
		require.Less(t, elapsed, 5*time.Second, name)
		return result
	}
	take := v7()
	path := root + "/" + take
	input := map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": quarantine, "reason": "TEST hospital maximum physical count"}
	started := measure("10000-bucket start", func() map[string]any { return call(t, "PUT", path, counter, input, 200) })
	require.EqualValues(t, 10000, started["snapshot_rows"])
	require.EqualValues(t, 0, started["version"])
	require.Equal(t, started, call(t, "PUT", path, counter, input, 200))
	counts := make([]map[string]any, 0, 10000)
	seen := make(map[string]bool, 10000)
	var durations []time.Duration
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 101, "bounded pagination must terminate")
		query := path + "/lines?limit=100"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		before := time.Now()
		page := call(t, "GET", query, counter, nil, 200)
		durations = append(durations, time.Since(before))
		rows := page["data"].([]any)
		require.Len(t, rows, 100)
		for _, raw := range rows {
			row := raw.(map[string]any)
			id := row["id"].(string)
			require.False(t, seen[id], "a bucket cannot appear twice")
			seen[id] = true
			book := row["book_units"].(float64)
			require.Contains(t, []float64{10, 32}, book)
			units := book
			if len(counts) < 500 {
				units--
			}
			counts = append(counts, map[string]any{"line_id": id, "counted_units": units})
		}
		continuation := page["page"].(map[string]any)
		if !continuation["has_more"].(bool) {
			break
		}
		next := continuation["next_cursor"].(string)
		require.NotEmpty(t, next)
		require.NotEqual(t, cursor, next)
		cursor = next
	}
	require.Len(t, seen, 10000)
	require.Len(t, durations, 100)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p99 := durations[98]
	t.Logf("hospital 100 pages p99 %s max %s", p99, durations[99])
	require.Less(t, p99, 300*time.Millisecond)
	var originalPath string
	var originalInput, originalReceipt map[string]any
	for offset := 0; offset < len(counts); offset += 100 {
		command := path + "/counts/" + v7()
		input := map[string]any{"expected_version": offset / 100, "counts": counts[offset : offset+100], "reason": fmt.Sprintf("TEST hospital physical observation batch %d", offset/100)}
		receipt := call(t, "PUT", command, counter, input, 200)
		require.EqualValues(t, offset/100+1, receipt["version"])
		if offset == 0 {
			originalPath, originalInput, originalReceipt = command, input, receipt
		}
	}
	submitted := call(t, "PUT", path+"/decisions/"+v7(), counter, map[string]any{"expected_version": 100, "decision": "submit", "reason": "TEST hospital complete physical count"}, 200)
	require.Equal(t, "submitted", submitted["state"])
	approval := map[string]any{"expected_version": 101, "decision": "approve", "reason": "TEST independent hospital maximum shortage review", "extra_units_disposition": "no_extra_units"}
	call(t, "PUT", path+"/decisions/"+v7(), counter, approval, 409)
	decision := path + "/decisions/" + v7()
	posted := measure("500-adjustment post", func() map[string]any { return call(t, "PUT", decision, reviewer, approval, 200) })
	require.Equal(t, "posted", posted["state"])
	require.EqualValues(t, 102, posted["version"])
	require.EqualValues(t, 500, posted["adjustment_count"])
	require.Equal(t, posted, call(t, "PUT", decision, reviewer, approval, 200))
	require.Equal(t, posted, call(t, "GET", decision, reviewer, nil, 200))
	require.Equal(t, originalReceipt, call(t, "PUT", originalPath, counter, originalInput, 200))
	require.Equal(t, originalReceipt, call(t, "GET", originalPath, counter, nil, 200))
	ownerTx(func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*)FILTER(WHERE kind='correction')corrections,COALESCE(sum(quantity_units),0)units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 501, ledger.Corrections)
		require.EqualValues(t, 99522, ledger.Units)
		var commands int64
		if err := tx.Table("pharmacy_stocktake_commands").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND kind='count' AND actor_subject=?", testutil.TenantA, outlet, take, counter).Count(&commands).Error; err != nil {
			return err
		}
		require.EqualValues(t, 100, commands)
		return nil
	})
	seed(10000, 10000)
	rejected := root + "/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", rejected, counter, input, 400)
	call(t, "GET", rejected, counter, nil, 404)
}
