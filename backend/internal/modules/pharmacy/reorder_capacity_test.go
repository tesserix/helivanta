package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLowStockCapacityAtTenThousandProducts(t *testing.T) {
	if os.Getenv("HELIVANTA_LOW_STOCK_CAPACITY") != "1" {
		t.Skip("dedicated hosted low-stock capacity gate")
	}
	for _, ownership := range []string{"independent", "hospital_owned"} {
		t.Run(ownership, func(t *testing.T) {
			w := newReleaseWorldForOwnership(t, ownership)
			withStock := map[string]bool{}
			started := time.Now()
			for i := 0; i < 10000; i++ {
				product := uuid.Must(uuid.NewV7()).String()
				body := strings.Replace(productBody(0, fmt.Sprintf("TEST Capacity %05d", i)), "TEST-AMOX-500", fmt.Sprintf("TEST-CAP-%05d", i), 1)
				created := do(w.r, "PUT", productPath(product), "editor", body)
				require.Equal(t, 200, created.Code, created.Body.String())
				if i%10 == 0 {
					classify(t, w.r, product)
					approveClass(t, w.r, product, "H", false)
					batch := receive(t, w.r, product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
					released := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":1,"reason":"TEST capacity shelf release"}`, batch, w.quarantine, w.sellable))
					require.Equal(t, 200, released.Code, released.Body.String())
					withStock[product] = true
				}
				saved := do(w.r, "PUT", productPath(product)+"/reorder-levels/"+uuid.Must(uuid.NewV7()).String(), "editor", reorderBody("2", 0, 1))
				require.Equal(t, 200, saved.Code, saved.Body.String())
			}
			t.Logf("prepared 10,000 configured products, 1,000 released buckets and 9,000 zero-batch products through owner APIs in %s", time.Since(started))
			var pageTimes, countTimes []time.Duration
			seen := map[string]bool{}
			cursor := ""
			for {
				path := outletPath("/stock/low-products?limit=50")
				if cursor != "" {
					path += "&cursor=" + cursor
				}
				start := time.Now()
				result := do(w.r, "GET", path, "editor", "")
				pageTimes = append(pageTimes, time.Since(start))
				require.Equal(t, 200, result.Code, result.Body.String())
				var page struct {
					Data []struct {
						ID        string
						Available string `json:"available_units"`
						Minimum   int64  `json:"minimum_units"`
					} `json:"data"`
					Page struct {
						More   bool    `json:"has_more"`
						Cursor *string `json:"next_cursor"`
					} `json:"page"`
				}
				require.NoError(t, json.Unmarshal(result.Body.Bytes(), &page))
				require.LessOrEqual(t, len(page.Data), 50)
				for _, row := range page.Data {
					require.False(t, seen[row.ID], "keyset pages must not duplicate products")
					seen[row.ID] = true
					require.EqualValues(t, 2, row.Minimum)
					want := "0"
					if withStock[row.ID] {
						want = "1"
					}
					require.Equal(t, want, row.Available)
				}
				if !page.Page.More {
					require.Nil(t, page.Page.Cursor)
					break
				}
				require.NotNil(t, page.Page.Cursor)
				require.NotEqual(t, cursor, *page.Page.Cursor)
				cursor = *page.Page.Cursor
			}
			require.Len(t, seen, 10000)
			for range 100 {
				start := time.Now()
				result := do(w.r, "GET", outletPath("/stock/low-products/overview"), "editor", "")
				countTimes = append(countTimes, time.Since(start))
				require.Equal(t, 200, result.Code, result.Body.String())
				var summary struct {
					Low int64 `json:"low_stock"`
				}
				require.NoError(t, json.Unmarshal(result.Body.Bytes(), &summary))
				require.EqualValues(t, 10000, summary.Low)
			}
			p99 := func(values []time.Duration) time.Duration {
				slices.Sort(values)
				return values[(len(values)*99+99)/100-1]
			}
			pageP99, countP99 := p99(pageTimes), p99(countTimes)
			t.Logf("%s capacity: %d complete pages p99=%s; 100 overview reads p99=%s", ownership, len(pageTimes), pageP99, countP99)
			require.LessOrEqual(t, pageP99, 500*time.Millisecond, "original page-read budget")
			require.LessOrEqual(t, countP99, 500*time.Millisecond, "original overview-read budget")
		})
	}
}
