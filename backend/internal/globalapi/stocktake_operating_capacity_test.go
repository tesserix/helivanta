package globalapi

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStocktakeReviewedCapacityAt10000Buckets(t *testing.T) {
	if os.Getenv("HELIVANTA_STOCKTAKE_CAPACITY") != "1" {
		t.Skip("run the focused hosted capacity gate")
	}
	f := newPharmacyReceiverFixture(t)
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO pharmacy_stock_batches(tenant_id,outlet_id,id,product_id,lot_number,expires_on,mrp_minor)
   SELECT ?,?,('019a2b00-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,'TEST-REVIEWED-CAP-'||i,'2030-12-31',10000 FROM generate_series(1,10000)i`, testutil.TenantA, f.outlet, f.product).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
   SELECT ?,?,('019a2b01-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,('019a2b00-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,'receipt',10,'TEST reviewed capacity fixture','TEST-reviewed-capacity:'||i,'user-owner' FROM generate_series(1,10000)i`, testutil.TenantA, f.outlet, f.quarantine).Error
	})
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	path := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes/" + v7()
	measure := func(name string, run func() string) string {
		start := time.Now()
		defer func() { t.Logf("reviewed %s elapsed %s", name, time.Since(start)) }()
		result := run()
		require.Less(t, time.Since(start), 5*time.Second, "reviewed %s budget", name)
		return result
	}
	measure("10000-bucket start", func() string {
		return f.call("PUT", path, "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST reviewed maximum count"}, 200)
	})
	entries := make([]map[string]any, 0, 10000)
	seen := make(map[string]bool, 10000)
	var timings []time.Duration
	cursor := ""
	for {
		request := path + "/lines?limit=100"
		if cursor != "" {
			request += "&cursor=" + cursor
		}
		start := time.Now()
		raw := f.call("GET", request, "receiver", nil, 200)
		timings = append(timings, time.Since(start))
		var page struct {
			Data []struct {
				ID        string `json:"id"`
				BookUnits int64  `json:"book_units"`
			} `json:"data"`
			PageInfo struct {
				Next string `json:"nextCursor"`
			} `json:"pageInfo"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &page))
		require.Len(t, page.Data, 100)
		for _, line := range page.Data {
			require.False(t, seen[line.ID])
			seen[line.ID] = true
			require.EqualValues(t, 10, line.BookUnits)
			units := 10
			if len(entries) < 500 {
				units = 9
			}
			entries = append(entries, map[string]any{"line_id": line.ID, "counted_units": units})
		}
		cursor = page.PageInfo.Next
		if cursor == "" {
			break
		}
	}
	require.Len(t, seen, 10000)
	require.Len(t, timings, 100)
	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	p99 := timings[(len(timings)*99+99)/100-1]
	t.Logf("reviewed 100 pages p99 %s", p99)
	require.Less(t, p99, 300*time.Millisecond)
	version := 0
	for i := 0; i < len(entries); i += 100 {
		f.call("PUT", path+"/counts/"+v7(), "receiver", map[string]any{"expected_version": version, "counts": entries[i : i+100], "reason": fmt.Sprintf("TEST reviewed physical observation batch %d", version)}, 200)
		version++
	}
	f.call("PUT", path+"/decisions/"+v7(), "receiver", map[string]any{"decision": "submit", "expected_version": version, "reason": "TEST reviewed all physical observations"}, 200)
	activateReviewedStocktakeReviewer(t, &f)
	approvalPath := path + "/decisions/" + v7()
	approval := map[string]any{"decision": "approve", "expected_version": version + 1, "reason": "TEST independently review maximum corrections", "extra_units_disposition": "no_extra_units"}
	posted := measure("500-adjustment post", func() string { return f.call("PUT", approvalPath, "stock-reviewer", approval, 200) })
	var receipt struct {
		State           string `json:"state"`
		Version         int64  `json:"version"`
		AdjustmentCount int64  `json:"adjustment_count"`
	}
	require.NoError(t, json.Unmarshal([]byte(posted), &receipt))
	require.Equal(t, "posted", receipt.State)
	require.EqualValues(t, version+2, receipt.Version)
	require.EqualValues(t, 500, receipt.AdjustmentCount)
	require.JSONEq(t, posted, f.call("PUT", approvalPath, "stock-reviewer", approval, 200))
	require.JSONEq(t, posted, f.call("GET", approvalPath, "stock-reviewer", nil, 200))
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections,COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 500, ledger.Corrections)
		require.EqualValues(t, 99500, ledger.Units)
		var commands int64
		if err := tx.Table("pharmacy_stocktake_commands").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND kind='count' AND actor_subject='user-receiver'", testutil.TenantA, f.outlet, path[len(path)-36:]).Count(&commands).Error; err != nil {
			return err
		}
		require.EqualValues(t, 100, commands)
		return nil
	})
}
