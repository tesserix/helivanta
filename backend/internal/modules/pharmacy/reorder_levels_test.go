package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

type reorderReceipt struct {
	ID              string `json:"id"`
	MinimumUnits    *int64 `json:"minimum_units"`
	ExpectedVersion int64  `json:"expected_version"`
	ProductVersion  int64  `json:"product_version"`
	Version         int64  `json:"version"`
	Unit            string `json:"unit_of_issue"`
	RecordedAt      string `json:"recorded_at"`
}

func reorderBody(minimum string, expected, productVersion int64) string {
	return fmt.Sprintf(`{"minimum_units":%s,"expected_version":%d,"product_version":%d}`, minimum, expected, productVersion)
}
func setReorder(t *testing.T, r *gin.Engine, product, minimum string, expected int64) reorderReceipt {
	t.Helper()
	current := do(r, "GET", productPath(product), "editor", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	var p struct{ Version int64 }
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &p))
	res := do(r, "PUT", productPath(product)+"/reorder-levels/"+uuid.Must(uuid.NewV7()).String(), "editor", reorderBody(minimum, expected, p.Version))
	require.Equal(t, 200, res.Code, res.Body.String())
	var receipt reorderReceipt
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &receipt))
	return receipt
}
func lowProducts(t *testing.T, r *gin.Engine) []struct {
	ID        string
	Available string `json:"available_units"`
} {
	t.Helper()
	res := do(r, "GET", outletPath("/stock/low-products"), "editor", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	var body struct {
		Data []struct {
			ID        string
			Available string `json:"available_units"`
		}
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	return body.Data
}

func TestReorderScopeReplayConcurrencyAndHistory(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	product, _, _ := stockFixture(t, r)
	current := do(r, "GET", productPath(product)+"/reorder-level", "editor", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, `{"level":null}`, current.Body.String())
	first := setReorder(t, r, product, "0", 0)
	original := reorderBody("0", 0, first.ProductVersion)
	command := productPath(product) + "/reorder-levels/" + first.ID
	second := setReorder(t, r, product, "6", 1)
	replay := do(r, "PUT", command, "editor", original)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	var receipt reorderReceipt
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &receipt))
	require.Equal(t, first, receipt)
	for _, method := range []string{"GET", "PUT"} {
		for _, token := range []string{"sibling", "foreign"} {
			require.Equal(t, 404, do(r, method, command, token, original).Code)
		}
		require.Equal(t, 403, do(r, method, command, "denied", original).Code)
		require.Equal(t, 401, do(r, method, command, "unknown", original).Code)
	}
	require.Equal(t, 404, do(r, "PUT", command, "reader", original).Code)
	require.Equal(t, 200, do(r, "GET", command, "reader", "").Code)
	require.Equal(t, 409, do(r, "PUT", command, "editor", reorderBody("1", 0, first.ProductVersion)).Code)
	require.Equal(t, 409, do(r, "PUT", productPath(product)+"/reorder-levels/"+uuid.Must(uuid.NewV7()).String(), "editor", original).Code)
	require.Equal(t, 404, do(r, "GET", strings.Replace(command, pharmaOutlet, "0199e000-0000-7000-8000-000000000002", 1), "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", strings.Replace(command, pharmaOrganization, "0199e000-0000-7000-8000-000000000020", 1), "editor", "").Code)
	for _, query := range []string{"?tenant_id=forged", "?as_of=2026-01-01", "?limit=2&limit=3", "?cursor="} {
		require.Equal(t, 400, do(r, "GET", outletPath("/stock/low-products")+query, "editor", "").Code)
	}
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "GET", outletPath("/stock/low-products"), token, "").Code)
	}
	codes := make(chan int, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			codes <- do(r, "PUT", productPath(product)+"/reorder-levels/"+uuid.Must(uuid.NewV7()).String(), "editor", reorderBody("8", second.Version, second.ProductVersion)).Code
		}()
	}
	close(start)
	a, b := <-codes, <-codes
	require.ElementsMatch(t, []int{200, 409}, []int{a, b})
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)", pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		var counts struct{ Revisions, Audits int64 }
		if err := tx.Raw(`SELECT (SELECT count(*) FROM pharmacy_reorder_levels WHERE product_id=?) revisions,
   (SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_reorder_levels') audits`, product).Scan(&counts).Error; err != nil {
			return err
		}
		require.Equal(t, int64(3), counts.Revisions)

		require.Equal(t, counts.Revisions, counts.Audits)
		var supported bool
		if err := tx.Raw("SELECT pharmacy_operating_choice_support()").Scan(&supported).Error; err != nil {
			return err
		}
		require.True(t, supported, "adding stock configuration preserves the native location-choice security inventory")
		if err := tx.Exec("SELECT set_config('app.pharmacy_operating_choices','{}',true)").Error; err != nil {
			return err
		}
		var disclosed int64
		if err := tx.Table("pharmacy_reorder_levels").Where("product_id=?", product).Count(&disclosed).Error; err != nil {
			return err
		}
		require.Zero(t, disclosed, "a governance-selection purpose cannot inherit ordinary stock configuration reads")
		return nil
	}))
	// The read and identical replay both reauthorize after an outlet grant changes.
	grantCatalogue(t, r, "user-editor", "reader")
	require.Equal(t, 404, do(r, "PUT", command, "editor", original).Code)
	require.Equal(t, 200, do(r, "GET", command, "editor", "").Code)
}

func TestReorderUsesReleasedUnexpiredAvailableStockAndOverview(t *testing.T) {
	w := newReleaseWorld(t)
	first := setReorder(t, w.r, w.product, "0", 0)
	rows := lowProducts(t, w.r)
	require.Len(t, rows, 1)
	require.Equal(t, "0", rows[0].Available, "quarantine does not conceal a shortage")
	release := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":10,"reason":"TEST low stock release"}`, w.batch, w.quarantine, w.sellable))
	require.Equal(t, 200, release.Code, release.Body.String())
	require.Empty(t, lowProducts(t, w.r), "above minimum")
	setReorder(t, w.r, w.product, "10", first.Version)
	rows = lowProducts(t, w.r)
	require.Len(t, rows, 1)
	require.Equal(t, "10", rows[0].Available, "at minimum is low")
	overview := do(w.r, "GET", outletPath("/overview"), "editor", "")
	require.Equal(t, 200, overview.Code, overview.Body.String())
	var body struct {
		Stock struct {
			Low int64 `json:"low_stock"`
		}
	}
	require.NoError(t, json.Unmarshal(overview.Body.Bytes(), &body))
	require.Equal(t, int64(len(rows)), body.Stock.Low)
	setReorder(t, w.r, w.product, "null", 2)
	require.Empty(t, lowProducts(t, w.r), "explicit disable")
	// A product with no batches is still included, even with a zero minimum.
	product := uuid.Must(uuid.NewV7()).String()
	pbody := strings.Replace(productBody(0, "TEST No batches"), "TEST-AMOX-500", "TEST-ZERO", 1)
	require.Equal(t, 200, do(w.r, "PUT", productPath(product), "editor", pbody).Code)
	setReorder(t, w.r, product, "0", 0)
	rows = lowProducts(t, w.r)
	require.Len(t, rows, 1)
	require.Equal(t, product, rows[0].ID)
	require.Equal(t, "0", rows[0].Available)
	setReorder(t, w.r, w.product, "10", 3)
	// No lost rows or duplicate products across the standard keyset boundary.
	page := do(w.r, "GET", outletPath("/stock/low-products?limit=1"), "editor", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	var list struct {
		Data []struct{ ID string }
		Page struct {
			Cursor string `json:"next_cursor"`
			More   bool   `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &list))
	require.True(t, list.Page.More)
	require.Len(t, list.Data, 1)
	next := do(w.r, "GET", outletPath("/stock/low-products?limit=1&cursor="+list.Page.Cursor), "editor", "")
	require.Equal(t, 200, next.Code, next.Body.String())
	var tail struct {
		Data []struct{ ID string }
		Page struct {
			More bool `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &tail))
	require.Len(t, tail.Data, 1)
	require.False(t, tail.Page.More)
	require.NotEqual(t, list.Data[0].ID, tail.Data[0].ID)
}

func TestReorderIgnoresTodayExpiryInOutletZoneAndInvalidatedUnits(t *testing.T) {
	w := newReleaseWorld(t)
	setReorder(t, w.r, w.product, "0", 0)
	// The real owner clock determines expiry, not the Go fixture's historical clock.
	zone, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	today := time.Now().In(zone).Format(time.DateOnly)
	batch := receive(t, w.r, w.product, w.quarantine, today, `,"mrp_minor":12500`)
	require.NotEmpty(t, batch)
	require.Len(t, lowProducts(t, w.r), 1)
	product := uuid.Must(uuid.NewV7()).String()
	pbody := strings.Replace(productBody(0, "TEST Unit change"), "TEST-AMOX-500", "TEST-UNIT", 1)
	require.Equal(t, 200, do(w.r, "PUT", productPath(product), "editor", pbody).Code)
	setReorder(t, w.r, product, "0", 0)
	require.Len(t, lowProducts(t, w.r), 2)
	changed := strings.Replace(strings.Replace(pbody, `"expected_version":0`, `"expected_version":1`, 1), `"unit_of_issue":"capsule"`, `"unit_of_issue":"pack"`, 1)
	res := do(w.r, "PUT", productPath(product), "editor", changed)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Len(t, lowProducts(t, w.r), 1, "old units cannot silently be reinterpreted")
	setReorder(t, w.r, product, "0", 1)
	require.Len(t, lowProducts(t, w.r), 2)
}

func TestReorderSharedAvailabilityAtLocalMidnightAndSafetyBlock(t *testing.T) {
	w := newReleaseWorld(t)
	setReorder(t, w.r, w.product, "10", 0)
	release := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":10,"reason":"TEST expiry boundary"}`, w.batch, w.quarantine, w.sellable))
	require.Equal(t, 200, release.Code, release.Body.String())
	for _, tt := range []struct{ instant, want string }{{"2030-10-30T18:29:59Z", "10"}, {"2030-10-30T18:30:00Z", "0"}} {
		t.Run(tt.instant, func(t *testing.T) {
			observed, err := time.Parse(time.RFC3339, tt.instant)
			require.NoError(t, err)
			require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)", pharmaOutlet, pharmaOrganization).Error; err != nil {
					return err
				}
				var rows []struct {
					Available string `gorm:"column:available_units"`
				}
				err := tx.Raw("SELECT COALESCE(SUM(on_hand::numeric),0)::text available_units FROM pharmacy_sellable_buckets(?,?,?,timezone('Asia/Kolkata',?::timestamptz)::date)", testutil.TenantA, pharmaOutlet, w.product, observed).Scan(&rows).Error
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, tt.want, rows[0].Available)
				return err
			}))
		})
	}
	incident := do(w.r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(w.batch, w.sellable))
	require.Equal(t, 200, incident.Code, incident.Body.String())
	rows := lowProducts(t, w.r)
	require.Len(t, rows, 1)
	require.Equal(t, "0", rows[0].Available, "an incident blocks the lot even on the released shelf")
}
