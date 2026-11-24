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
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
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
			require.NoError(t, w.db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)", pharmaOutlet, pharmaOrganization).Error; err != nil {
					return err
				}
				if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
					return err
				}
				observed := time.Now().UTC()
				source := pharmacy.LowStockSource(tx, testutil.TenantA, uuid.MustParse(pharmaOutlet), observed)
				var count int64
				var rows []map[string]any
				countQuery := tx.Session(&gorm.Session{DryRun: true}).Table("(?) low_stock", source).Count(&count)
				pageQuery := tenantdb.ApplyKeyset(tx.Session(&gorm.Session{DryRun: true}), pagination.Params{Limit: 50}).Table("(?) low_stock", source).Find(&rows)
				for label, query := range map[string]*gorm.DB{"overview": countQuery, "first-page": pageQuery} {
					var plan string
					if err := tx.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.Statement.SQL.String(), query.Statement.Vars...).Scan(&plan).Error; err != nil {
						return err
					}
					require.True(t, json.Valid([]byte(plan)), "actual owner query plan")
					t.Logf("%s %s owner query plan: %s", ownership, label, plan)
				}
				return nil
			}))
			type measuredPage struct {
				elapsed time.Duration
				cursor  string
				index   int
			}
			var measured []measuredPage
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
				elapsed := time.Since(start)
				pageTimes = append(pageTimes, elapsed)
				measured = append(measured, measuredPage{elapsed: elapsed, cursor: cursor, index: len(measured)})
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
			for _, sample := range measured {
				if sample.elapsed != pageP99 {
					continue
				}
				params := pagination.Params{Limit: 50}
				if sample.cursor != "" {
					after, err := pagination.Decode(sample.cursor, testutil.TenantA)
					require.NoError(t, err)
					params.After = &after
				}
				t.Logf("%s page p99 at zero-based page %d", ownership, sample.index)
				lowStockPreparedPagePlans(t, &w, ownership, params)
				break
			}
			require.LessOrEqual(t, pageP99, 500*time.Millisecond, "original page-read budget")
			require.LessOrEqual(t, countP99, 500*time.Millisecond, "original overview-read budget")
		})
	}
}

// Prepared generic plans reproduce the driver's repeated-page path; the first
// EXPLAIN alone can hide a later plan switch.
func lowStockPreparedPagePlans(t *testing.T, w *releaseWorld, ownership string, params pagination.Params) {
	t.Helper()
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)", pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
			return err
		}
		var cached []struct {
			Statement    string
			GenericPlans int64
			CustomPlans  int64
		}
		if err := tx.Raw("SELECT statement,generic_plans,custom_plans FROM pg_prepared_statements WHERE statement LIKE '%WITH available AS MATERIALIZED%'AND statement NOT LIKE '%pg_prepared_statements%'ORDER BY statement").Scan(&cached).Error; err != nil {
			return err
		}
		for _, statement := range cached {
			t.Logf("%s cached source plan generic=%d custom=%d: %s", ownership, statement.GenericPlans, statement.CustomPlans, statement.Statement)
		}
		source := pharmacy.LowStockSource(tx, testutil.TenantA, uuid.MustParse(pharmaOutlet), time.Now().UTC())
		var rows []map[string]any
		query := tenantdb.ApplyKeyset(tx.Session(&gorm.Session{DryRun: true}), params).Table("(?) low_stock", source).Find(&rows)
		if err := tx.Exec("PREPARE low_stock_capacity_page AS " + query.Statement.SQL.String()).Error; err != nil {
			return err
		}
		defer tx.Exec("DEALLOCATE low_stock_capacity_page")
		literals := make([]string, len(query.Statement.Vars))
		for i, value := range query.Statement.Vars {
			// Only canonical UUIDs, server timestamps and integer limits can enter this
			// diagnostic EXECUTE. No request strings or arbitrary SQL literals enter it.
			switch value := value.(type) {
			case uuid.UUID:
				literals[i] = "'" + value.String() + "'"
			case time.Time:
				literals[i] = "'" + value.UTC().Format(time.RFC3339Nano) + "'"
			case int:
				literals[i] = fmt.Sprint(value)
			case int64:
				literals[i] = fmt.Sprint(value)
			case string:
				if id, err := uuid.Parse(value); err == nil {
					literals[i] = "'" + id.String() + "'"
					continue
				}
				stamp, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					return fmt.Errorf("unexpected capacity-plan parameter type: %T", value)
				}
				literals[i] = "'" + stamp.UTC().Format(time.RFC3339Nano) + "'"
			default:
				return fmt.Errorf("unexpected capacity-plan parameter type: %T", value)
			}
		}
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			if err := tx.Exec("SELECT set_config('plan_cache_mode',?,true)", mode).Error; err != nil {
				return err
			}
			var plan string
			if err := tx.Raw("EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON)EXECUTE low_stock_capacity_page(" + strings.Join(literals, ",") + ")").Scan(&plan).Error; err != nil {
				return err
			}
			require.True(t, json.Valid([]byte(plan)))
			t.Logf("%s p99-page %s actual-role plan: %s", ownership, mode, plan)
		}
		return nil
	}))
}
