package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

// India is the only supported country (#1287); its cases still tell the
// outlet's calendar from UTC's, since 18:45Z is already the 29th in Kolkata.
func TestStockExpiryBandsUseOutletCalendar(t *testing.T) {
	for _, country := range []struct{ code, zone, cutoff, date string }{
		{"IN-before", "Asia/Kolkata", "2024-02-28T18:29:59Z", "2024-02-28"},
		{"IN-after", "Asia/Kolkata", "2024-02-28T18:45:00Z", "2024-02-29"},
	} {
		t.Run(country.code, func(t *testing.T) {
			r, db, _, _ := catalogueSetup(t)
			root := "/v1/pharmacy/pharmacies/" + pharmaOrganization
			require.Equal(t, 200, do(r, "PUT", root, "editor", `{"code":"TEST-PHARMACY","name":"TEST Pharmacy","ownership_type":"independent"}`).Code)
			root += "/outlets/" + pharmaOutlet
			require.Equal(t, 200, do(r, "PUT", root, "editor", fmt.Sprintf(`{"code":"TEST-MAIN","name":"TEST Main","country_code":%q,"timezone":%q}`, country.code[:2], country.zone)).Code)
			grantCatalogue(t, r, "user-editor", "editor")
			product, location, _ := stockFixture(t, r)
			cutoff := country.cutoff
			localDate, err := time.Parse(time.DateOnly, country.date)
			require.NoError(t, err)
			for _, days := range []int{-1, 0, 1, 30, 31, 90, 91, 180, 181} {
				batch := uuid.Must(uuid.NewV7()).String()
				w := do(r, "PUT", root+"/stock/batches/"+batch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":%q,"expires_on":%q,"mrp_minor":12500}`, product, fmt.Sprintf("TEST-EXPIRY-%d", days), localDate.AddDate(0, 0, days).Format(time.DateOnly)))
				require.Equal(t, 200, w.Code, w.Body.String())
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.actor_subject','user-editor',true),set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
						return err
					}
					return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at) VALUES (?,?,?,?,?,'receipt',5,'TEST expiry visibility',?,'user-editor',?::timestamptz)`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, location, batch, fmt.Sprintf("TEST-EXPIRY-%d", days), cutoff).Error
				}))
			}
			w := do(r, "GET", root+"/stock/positions?as_of="+url.QueryEscape(cutoff), "editor", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			var result struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Len(t, result.Data, 9)
			for _, row := range result.Data {
				require.Equal(t, country.zone, row["expiry_time_zone"])
				require.Equal(t, country.date, row["expiry_local_date"])
				expires, err := time.Parse(time.DateOnly, row["expires_on"].(string))
				require.NoError(t, err)
				days := int(expires.Sub(localDate).Hours() / 24)
				require.EqualValues(t, days, row["days_to_expiry"])
				band := "later"
				switch {
				case days < 0:
					band = "expired"
				case days == 0:
					band = "today"
				case days <= 30:
					band = "days_30"
				case days <= 90:
					band = "days_90"
				case days <= 180:
					band = "days_180"
				}
				require.Equal(t, band, row["expiry_band"])
				require.EqualValues(t, 0, row["dispensable_units"])
			}
			for _, filter := range []struct {
				expiry string
				count  int
			}{{"all", 9}, {"expired", 1}, {"today", 1}, {"due_30", 3}, {"due_90", 5}, {"due_180", 7}, {"unexpired", 8}} {
				cursor := ""
				seen := map[string]bool{}
				for {
					path := root + "/stock/positions?as_of=" + url.QueryEscape(cutoff) + "&expiry=" + filter.expiry + "&balance=on_hand&product_id=" + product + "&location_id=" + location + "&limit=2&cursor=" + url.QueryEscape(cursor)
					w := do(r, "GET", path, "editor", "")
					require.Equal(t, 200, w.Code, w.Body.String())
					var page struct {
						Data []struct {
							ID     string
							OnHand int `json:"on_hand_units"`
						}
						Page struct {
							HasMore bool   `json:"has_more"`
							Next    string `json:"next_cursor"`
						}
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
					for _, row := range page.Data {
						require.False(t, seen[row.ID])
						seen[row.ID] = true
						require.Positive(t, row.OnHand)
					}
					if !page.Page.HasMore {
						break
					}
					require.NotEmpty(t, page.Page.Next)
					cursor = page.Page.Next
				}
				require.Len(t, seen, filter.count, filter.expiry)
			}
			for _, query := range []string{"expiry=due_31", "balance=positive", "expiry=all&expiry=today", "balance=all&balance=on_hand"} {
				require.Equal(t, 400, do(r, "GET", root+"/stock/positions?as_of="+url.QueryEscape(cutoff)+"&"+query, "editor", "").Code, query)
			}
		})
	}
}
