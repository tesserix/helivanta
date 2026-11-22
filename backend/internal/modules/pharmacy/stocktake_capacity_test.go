package pharmacy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
	var bookQuery string
	var bookArguments []any
	profiledInsert := false
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		before := func(tx *gorm.DB) {
			if tx.Statement.Table == "pharmacy_stocktake_lines" && !profiledInsert {
				profiledInsert = true
				tx.InstanceSet("stocktake-insert-profile", true)
				tx.AddError(tx.Exec("SAVEPOINT stocktake_insert_profile").Error)
			}
			if strings.HasPrefix(tx.Statement.SQL.String(), "WITH products AS") {
				bookQuery = tx.Statement.SQL.String()
				bookArguments = append([]any(nil), tx.Statement.Vars...)
			}
			tx.InstanceSet("stocktake-capacity-start", time.Now())
		}
		after := func(tx *gorm.DB) {
			if _, ok := tx.InstanceGet("stocktake-insert-profile"); ok && tx.Error == nil {
				profileStocktakeInsert(t, tx)
			}
			value, ok := tx.InstanceGet("stocktake-capacity-start")
			if !ok {
				return
			}
			elapsed := time.Since(value.(time.Time))
			if elapsed < 50*time.Millisecond && tx.Error == nil {
				return
			}
			stage := "other"
			query := tx.Statement.SQL.String()
			for _, table := range []string{"pharmacy_stocktake_lines", "pharmacy_stocktake_commands", "pharmacy_stocktake_counts", "pharmacy_stocktake_adjustments", "pharmacy_stocktakes", "pharmacy_stock_movements", "audit_events"} {
				if strings.Contains(query, table) {
					stage = table
					break
				}
			}
			if strings.HasPrefix(query, "WITH products AS") {
				stage = "book_snapshot"
			}
			code := "none"
			var pg *pgconn.PgError
			if errors.As(tx.Error, &pg) {
				code = pg.Code
			} else if tx.Error != nil {
				code = "non_sql_error"
			}
			t.Logf("database stage=%s elapsed=%s error_class=%s", stage, elapsed, code)
		}
		if err := tx.Callback().Create().Before("gorm:create").Register("test:stocktake-capacity-before", before); err != nil {
			return err
		}
		if err := tx.Callback().Create().After("gorm:create").Register("test:stocktake-capacity-after", after); err != nil {
			return err
		}
		if err := tx.Callback().Raw().Before("gorm:raw").Register("test:stocktake-capacity-before", before); err != nil {
			return err
		}
		if err := tx.Callback().Raw().After("gorm:raw").Register("test:stocktake-capacity-after", after); err != nil {
			return err
		}
		if err := tx.Callback().Row().Before("gorm:row").Register("test:stocktake-capacity-before", before); err != nil {
			return err
		}
		return tx.Callback().Row().After("gorm:row").Register("test:stocktake-capacity-after", after)
	}))
	path := outletPath("/stock/stocktakes/") + uuid.Must(uuid.NewV7()).String()
	start := time.Now()
	response := do(r, "PUT", path, "editor", fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST capacity count"}`, location))
	startElapsed := time.Since(start)
	t.Logf("10000-bucket start returned HTTP %d in %s", response.Code, startElapsed)
	require.NotEmpty(t, bookQuery)
	stocktakeSnapshotPlan(t, db, bookQuery, bookArguments)
	require.Equal(t, 200, response.Code, response.Body.String())
	var header struct {
		SnapshotRows int `json:"snapshot_rows"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &header))
	require.Equal(t, 10000, header.SnapshotRows)
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
	response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"submit","expected_version":100,"reason":"TEST capacity submission"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	approvalPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	body := `{"decision":"approve","expected_version":101,"reason":"TEST independent capacity review","extra_units_disposition":"no_extra_units"}`
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
	require.Less(t, postElapsed, 5*time.Second, "ADR-0073 post budget")
	replayed := do(r, "PUT", approvalPath, "reviewer", body)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, response.Body.String(), replayed.Body.String())
}

// Re-execute one rolled-back batch to measure its real trigger work, retaining one final insert.
func profileStocktakeInsert(t *testing.T, tx *gorm.DB) {
	t.Helper()
	query := tx.Statement.SQL.String()
	arguments := append([]any(nil), tx.Statement.Vars...)
	if err := tx.Exec("ROLLBACK TO SAVEPOINT stocktake_insert_profile").Error; err != nil {
		tx.AddError(err)
		return
	}
	var document string
	if err := tx.Raw("EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+query, arguments...).Row().Scan(&document); err != nil {
		tx.AddError(err)
		return
	}
	if err := tx.Exec("RELEASE SAVEPOINT stocktake_insert_profile").Error; err != nil {
		tx.AddError(err)
		return
	}
	var plans []struct {
		ExecutionTime float64          `json:"Execution Time"`
		Triggers      []map[string]any `json:"Triggers"`
	}
	if err := json.Unmarshal([]byte(document), &plans); err != nil {
		tx.AddError(err)
		return
	}
	if len(plans) != 1 {
		tx.AddError(fmt.Errorf("expected one insert execution plan"))
		return
	}
	var triggers []map[string]any
	for _, trigger := range plans[0].Triggers {
		safe := map[string]any{}
		for _, key := range []string{"Trigger Name", "Constraint Name", "Relation", "Time", "Calls"} {
			if value, ok := trigger[key]; ok {
				safe[key] = value
			}
		}
		triggers = append(triggers, safe)
	}
	safe, err := json.Marshal(map[string]any{"execution_ms": plans[0].ExecutionTime, "triggers": triggers})
	if err != nil {
		tx.AddError(err)
		return
	}
	t.Logf("snapshot insert trigger metrics (no SQL or values): %s", safe)
}

func stocktakeSnapshotPlan(t *testing.T, db *tenantdb.DB, query string, arguments []any) {
	t.Helper()
	var document string
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Raw("EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+query, arguments...).Row().Scan(&document)
	}))
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal([]byte(document), &plans))
	require.Len(t, plans, 1)
	var summarize func(map[string]any) map[string]any
	summarize = func(node map[string]any) map[string]any {
		result := map[string]any{}
		for _, key := range []string{"Node Type", "Relation Name", "Actual Rows", "Actual Loops", "Actual Total Time", "Shared Hit Blocks", "Shared Read Blocks"} {
			if value, ok := node[key]; ok {
				result[key] = value
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			var nested []map[string]any
			for _, child := range children {
				nested = append(nested, summarize(child.(map[string]any)))
			}
			result["Plans"] = nested
		}
		return result
	}
	safe, err := json.Marshal(summarize(plans[0].Plan))
	require.NoError(t, err)
	t.Logf("snapshot execution plan (no SQL or conditions): %s", safe)
}
