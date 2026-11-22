package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ADR-0073's latency budgets are measured alone, outside the parallel coverage run.
func TestStocktakeCapacityAt10000Buckets(t *testing.T) {
	if os.Getenv("HELIVANTA_STOCKTAKE_CAPACITY") != "1" {
		t.Skip("run the isolated Stocktake capacity workflow")
	}
	r, _ := stocktakeHarness(t)
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
