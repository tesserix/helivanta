package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// ADR-0073's latency budgets are measured alone, outside the parallel coverage run.
func TestStocktakeCapacityAt10000Buckets(t *testing.T) {
	if os.Getenv("HELIVANTA_STOCKTAKE_CAPACITY") != "1" {
		t.Skip("run the isolated Stocktake capacity workflow")
	}
	r, db := stocktakeHarness(t)
	product, location, firstBatch := stockFixture(t, r)
	fixtureStart := time.Now()
	for i := range 10000 {
		batch := firstBatch
		if i > 0 {
			batch = uuid.Must(uuid.NewV7()).String()
			response := do(r, "PUT", outletPath("/stock/batches/")+batch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-CAPACITY-%d","expires_on":"2030-10-31","mrp_minor":12500}`, product, i))
			require.Equal(t, 200, response.Code, "batch %d", i)
		}
		response := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", fmt.Sprintf("TEST-CAPACITY-%d", i), 10))
		require.Equal(t, 200, response.Code, "receipt %d", i)
	}
	t.Logf("created 10000 distinct stocked batches through normal APIs in %s", time.Since(fixtureStart))
	path := outletPath("/stock/stocktakes/") + uuid.Must(uuid.NewV7()).String()
	start := time.Now()
	response := do(r, "PUT", path, "editor", fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST capacity count"}`, location))
	startElapsed := time.Since(start)
	t.Logf("10000-bucket start returned HTTP %d in %s", response.Code, startElapsed)
	require.Equal(t, 200, response.Code, response.Body.String())
	var header struct {
		SnapshotRows int `json:"snapshot_rows"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &header))
	require.Equal(t, 10000, header.SnapshotRows)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		var audits struct{ Total, Attributed int64 }
		if err := tx.Raw(`SELECT count(*) total,count(*)FILTER(WHERE a.actor_subject='user-editor' AND a.changed_fields='{}'::text[]) attributed
 FROM audit_events a JOIN pharmacy_stocktake_lines l ON l.tenant_id=a.tenant_id AND l.id=a.resource_id
 WHERE l.tenant_id=? AND l.outlet_id=? AND l.stocktake_id=? AND a.resource_type='pharmacy_stocktake_lines' AND a.action='insert'`, testutil.TenantA, pharmaOutlet, strings.TrimPrefix(path, outletPath("/stock/stocktakes/"))).Scan(&audits).Error; err != nil {
			return err
		}
		require.EqualValues(t, 10000, audits.Total)
		require.Equal(t, audits.Total, audits.Attributed, "every line retains its attributable insert evidence")
		return nil
	}))
	t.Logf("10000-bucket start: %s", startElapsed)
	require.Less(t, startElapsed, 5*time.Second, "ADR-0073 start budget")

	type line struct {
		ID        string `json:"id"`
		BookUnits int64  `json:"book_units"`
	}
	var pageDurations []time.Duration
	var all []line
	pageURL := path + "/lines?limit=100"
	seen := make(map[string]bool, 10000)
	for pages := 0; ; pages++ {
		require.Less(t, pages, 101, "pagination must terminate")
		start = time.Now()
		response = do(r, "GET", pageURL, "reader", "")
		pageDurations = append(pageDurations, time.Since(start))
		require.Equal(t, 200, response.Code, response.Body.String())
		var page struct {
			Data []line `json:"data"`
			Page struct {
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		for _, row := range page.Data {
			require.False(t, seen[row.ID], "snapshot page repeated a bucket")
			seen[row.ID] = true
		}
		all = append(all, page.Data...)
		if !page.Page.HasMore {
			break
		}
		require.NotEmpty(t, page.Page.NextCursor)
		pageURL = path + "/lines?limit=100&cursor=" + url.QueryEscape(page.Page.NextCursor)
	}
	require.Len(t, all, 10000)
	slices.Sort(pageDurations)
	p99 := pageDurations[(len(pageDurations)*99+99)/100-1]
	t.Logf("%d snapshot pages p99=%s max=%s", len(pageDurations), p99, pageDurations[len(pageDurations)-1])
	require.Less(t, p99, 300*time.Millisecond, "ADR-0073 page budget")
	for offset := 0; offset < len(all); offset += 100 {
		counts := make([]map[string]any, 0, 100)
		for i, row := range all[offset : offset+100] {
			require.EqualValues(t, 10, row.BookUnits)
			units := row.BookUnits
			if offset+i < 500 {
				units--
			}
			counts = append(counts, map[string]any{"line_id": row.ID, "counted_units": units})
		}
		body, err := json.Marshal(map[string]any{"expected_version": offset / 100, "counts": counts, "reason": "TEST capacity observations"})
		require.NoError(t, err)
		response = do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "editor", string(body))
		require.Equal(t, 200, response.Code, "count command %d: %s", offset/100, response.Body.String())
	}
	submitStarted := time.Now()
	response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"submit","expected_version":100,"reason":"TEST capacity submission"}`)
	submitElapsed := time.Since(submitStarted)
	t.Logf("10000-bucket submit returned HTTP %d in %s", response.Code, submitElapsed)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Less(t, submitElapsed, 5*time.Second, "complete snapshot submission must fit the command budget")
	approvalPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	body := `{"decision":"approve","expected_version":101,"reason":"TEST independent capacity review","extra_units_disposition":"no_extra_units"}`
	traceStocktakePosting(t, db)
	start = time.Now()
	response = do(r, "PUT", approvalPath, "reviewer", body)
	postElapsed := time.Since(start)
	require.Equal(t, 200, response.Code, response.Body.String())
	var receipt struct {
		State           string `json:"state"`
		AdjustmentCount int    `json:"adjustment_count"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	require.Equal(t, "posted", receipt.State)
	require.Equal(t, 500, receipt.AdjustmentCount)
	t.Logf("500-adjustment post on 10000-bucket count: %s", postElapsed)
	replayed := do(r, "PUT", approvalPath, "reviewer", body)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, response.Body.String(), replayed.Body.String())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reviewer',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*)FILTER(WHERE kind='correction')corrections,COALESCE(sum(quantity_units),0)units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, pharmaOutlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 500, ledger.Corrections, "replay must not create additional physical corrections")
		require.EqualValues(t, 99500, ledger.Units, "only the 500 observed shortages reduce the opening 100000 units")
		return nil
	}))
	require.Less(t, postElapsed, 5*time.Second, "ADR-0073 post budget")
}

func traceStocktakePosting(t *testing.T, db *tenantdb.DB) {
	t.Helper()
	const key = "test:stocktake-posting-start"
	before := func(tx *gorm.DB) { tx.InstanceSet(key, time.Now()) }
	after := func(tx *gorm.DB) {
		start, ok := tx.InstanceGet(key)
		if !ok {
			return
		}
		phase := tx.Statement.Table
		if phase == "" {
			phase = "other SQL"
			for _, name := range []string{"pharmacy_stocktake_commands", "pharmacy_stocktake_differences", "pg_advisory_xact_lock"} {
				if strings.Contains(tx.Statement.SQL.String(), name) {
					phase = name
					break
				}
			}
		}
		t.Logf("stocktake posting phase=%s duration=%s rows=%d", phase, time.Since(start.(time.Time)), tx.RowsAffected)
	}
	var callbacks *gorm.DB
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		callbacks = tx
		for _, register := range []func() error{
			func() error { return tx.Callback().Create().Before("gorm:create").Register(key+":before", before) },
			func() error { return tx.Callback().Create().After("gorm:create").Register(key+":after", after) },
			func() error { return tx.Callback().Raw().Before("gorm:raw").Register(key+":before", before) },
			func() error { return tx.Callback().Raw().After("gorm:raw").Register(key+":after", after) },
			func() error { return tx.Callback().Row().Before("gorm:row").Register(key+":before", before) },
			func() error { return tx.Callback().Row().After("gorm:row").Register(key+":after", after) },
		} {
			if err := register(); err != nil {
				return err
			}
		}
		return nil
	}))
	t.Cleanup(func() {
		for _, name := range []string{key + ":before", key + ":after"} {
			require.NoError(t, callbacks.Callback().Create().Remove(name))
			require.NoError(t, callbacks.Callback().Raw().Remove(name))
			require.NoError(t, callbacks.Callback().Row().Remove(name))
		}
	})
}
