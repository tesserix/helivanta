package globalapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/internal/testutil"
)

type stocktakeReleaseTransport struct{}

func (stocktakeReleaseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"features":{}}`))}, nil
}

func TestStocktakeReviewedReadsSurviveReleaseWithdrawal(t *testing.T) {
	for _, loaded := range []bool{true, false} {
		name := "provider_unavailable"
		if loaded {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			policy, err := release.New(release.Config{URL: "https://stocktake-release.test", ClientKey: "sdk-test", Client: &http.Client{Transport: stocktakeReleaseTransport{}}})
			require.NoError(t, err)
			if loaded {
				require.NoError(t, policy.Refresh(t.Context()))
			}
			var withdrawn atomic.Bool
			f := newPharmacyReceiverFixtureWithAdmission(t, func(method, path string) gin.HandlerFunc {
				admit := policy.Admission(method, path)
				return func(c *gin.Context) {
					if withdrawn.Load() {
						admit(c)
						return
					}
					c.Next()
				}
			})
			f.activate(t)
			stock := f.stock(f.pharmacy, f.outlet, "")
			batch := v7()
			f.call("PUT", stock+"/batches/"+batch, "receiver", f.batch("TEST-WITHDRAWN-STOCKTAKE"), 200)
			f.call("PUT", stock+"/receipts/"+v7(), "receiver", f.receipt(batch, f.quarantine, "TEST received before stocktake withdrawal"), 200)
			f.duty = "/v1/iam/operating-duties/" + v7()
			f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
			f.activate(t)
			root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
			path := root + "/" + v7()
			f.call("PUT", path, "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST recover interrupted reviewed count"}, 200)
			var lines struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &lines))
			require.Len(t, lines.Data, 1)
			countPath := path + "/counts/" + v7()
			countInput := map[string]any{"expected_version": 0, "counts": []map[string]any{{"line_id": lines.Data[0].ID, "counted_units": 27}}, "reason": "TEST physical observation before withdrawal"}
			count := f.call("PUT", countPath, "receiver", countInput, 200)
			submitPath := path + "/decisions/" + v7()
			submitInput := map[string]any{"decision": "submit", "expected_version": 1, "reason": "TEST submit before withdrawal"}
			submit := f.call("PUT", submitPath, "receiver", submitInput, 200)

			withdrawn.Store(true)
			var header struct {
				State string `json:"state"`
			}
			require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, "receiver", nil, 200)), &header))
			require.Equal(t, "submitted", header.State)
			var history struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(f.call("GET", root, "receiver", nil, 200)), &history))
			require.Len(t, history.Data, 1)
			require.True(t, strings.HasSuffix(path, "/"+history.Data[0].ID))
			require.JSONEq(t, count, f.call("GET", countPath, "receiver", nil, 200))
			require.JSONEq(t, submit, f.call("GET", submitPath, "receiver", nil, 200))
			for _, actor := range []string{"owner", "foreign"} {
				for _, resource := range []string{path, root, countPath, submitPath} {
					f.call("GET", resource, actor, nil, 404)
				}
			}
			f.checks.down.Store(true)
			f.call("GET", path, "receiver", nil, 503)
			f.checks.down.Store(false)
			blocked := 503
			if loaded {
				blocked = 404
			}
			f.call("GET", path+"/lines", "receiver", nil, blocked)
			f.call("GET", path+"/register.csv", "receiver", nil, blocked)
			f.call("GET", root+"/product-choices", "receiver", nil, blocked)
			f.call("GET", root+"/quarantine-choices", "receiver", nil, blocked)
			f.call("PUT", countPath, "receiver", countInput, blocked)
			f.call("PUT", submitPath, "receiver", submitInput, blocked)
			f.call("PUT", root+"/"+v7(), "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST withdrawal prevents a new count"}, blocked)
			cancelPath := path + "/cancellations/" + v7()
			cancelInput := map[string]any{"decision": "cancel", "expected_version": 2, "reason": "TEST end interrupted stocktake safely"}
			for _, actor := range []string{"owner", "foreign"} {
				f.call("PUT", path+"/cancellations/"+v7(), actor, cancelInput, 404)
			}
			f.call("PUT", strings.Replace(cancelPath, f.outlet, f.sibling, 1), "receiver", cancelInput, 404)
			f.call("PUT", strings.Replace(submitPath, "/decisions/", "/cancellations/", 1), "receiver", cancelInput, 409)
			saved := f.call("PUT", cancelPath, "receiver", cancelInput, 200)
			var cancellation struct {
				State           string `json:"state"`
				Decision        string `json:"decision"`
				Version         int64  `json:"version"`
				AdjustmentCount int64  `json:"adjustment_count"`
			}
			require.NoError(t, json.Unmarshal([]byte(saved), &cancellation))
			require.Equal(t, "cancelled", cancellation.State)
			require.Equal(t, "cancel", cancellation.Decision)
			require.EqualValues(t, 3, cancellation.Version)
			require.Zero(t, cancellation.AdjustmentCount)
			require.JSONEq(t, saved, f.call("PUT", cancelPath, "receiver", cancelInput, 200))
			require.JSONEq(t, saved, f.call("GET", strings.Replace(cancelPath, "/cancellations/", "/decisions/", 1), "receiver", nil, 200))
			f.call("PUT", path+"/cancellations/"+v7(), "receiver", map[string]any{"decision": "cancel", "expected_version": 3, "reason": "TEST terminal count cannot be cancelled twice"}, 409)
			for _, decision := range []string{"submit", "approve", "reject"} {
				f.call("PUT", path+"/cancellations/"+v7(), "receiver", map[string]any{"decision": decision, "expected_version": 3, "reason": "TEST cancellation cannot admit another command"}, 400)
			}
			f.ownerOutletTx(t, func(tx *gorm.DB) error {
				var balance struct{ Units, Corrections int64 }
				if err := tx.Raw(`SELECT COALESCE(sum(quantity_units),0) units,count(*)FILTER(WHERE kind='correction') corrections FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&balance).Error; err != nil {
					return err
				}
				require.EqualValues(t, 30, balance.Units)
				require.Zero(t, balance.Corrections)
				return nil
			})
		})
	}
}
