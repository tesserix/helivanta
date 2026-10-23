package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

var allPharmacyPerms = []authz.Permission{
	"pharmacy.accounts.read", "pharmacy.accounts.write", "pharmacy.accounts.payment",
	"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage",
	"pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.quarantine", "pharmacy.stock.correct",
}

// overviewHarness: "editor" holds every pharmacy permission, "pharmacist"
// stock and catalogue, "cataloguer" catalogue read only. The module clock is
// pinned for the date-based counts; stock expiry uses the database clock, as
// the stock register does.
func overviewHarness(t *testing.T) *gin.Engine {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "cataloguer": testutil.TenantA, "ungranted": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     allPharmacyPerms,
			"pharmacist": {"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.stock.read"},
			"cataloguer": {"pharmacy.catalogue.read"},
			"ungranted":  allPharmacyPerms,
			"foreign":    allPharmacyPerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, subject := range []string{"user-editor", "user-pharmacist", "user-cataloguer"} {
		grantCatalogue(t, r, subject, "editor")
	}
	return r
}

func outletPath(suffix string) string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + suffix
}

type overviewBody struct {
	AsOf     string `json:"as_of"`
	TimeZone string `json:"time_zone"`
	Stock    *struct {
		Expired int `json:"expired"`
		Due30   int `json:"due_30"`
		Due90   int `json:"due_90"`
	} `json:"stock"`
	Safety *struct {
		Recent int `json:"recent"`
	} `json:"safety"`
	Bills *struct {
		Overdue int `json:"overdue"`
		Week    int `json:"week"`
	} `json:"bills"`
	Setup *struct {
		Unclassified        int   `json:"unclassified"`
		RegistrationInForce *bool `json:"registration_in_force"`
	} `json:"setup"`
}

func getOverview(t *testing.T, r *gin.Engine, token string) (int, overviewBody, string) {
	t.Helper()
	w := do(r, "GET", outletPath("/overview"), token, "")
	var body overviewBody
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	}
	return w.Code, body, w.Body.String()
}

// listCount pages a list to the end and counts its rows: what the
// overview's link would show.
func listCount(t *testing.T, r *gin.Engine, path string) int {
	t.Helper()
	n, cursor := 0, ""
	for range 50 {
		url := path + "&limit=200"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		w := do(r, "GET", url, "editor", "")
		require.Equal(t, http.StatusOK, w.Code, path+": "+w.Body.String())
		var page struct {
			Data []json.RawMessage
			Page struct {
				NextCursor *string `json:"next_cursor"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		n += len(page.Data)
		if page.Page.NextCursor == nil {
			return n
		}
		cursor = *page.Page.NextCursor
	}
	t.Fatalf("%s did not end", path)
	return 0
}

// #576: every overview count equals the list its link opens, filtered the
// same way and paged to the end.
func TestOverviewCountsEqualTheirLists(t *testing.T) {
	r := overviewHarness(t)

	// Stock lines on the outlet's own (database-clock) date: expired, within
	// 30, within 90, later, and one received then fully corrected out.
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	today := time.Now().In(kolkata)
	day := func(n int) string { return today.AddDate(0, 0, n).Format(time.DateOnly) }
	product, location, fixtureBatch := stockFixture(t, r)
	stockLine := func(expires string, qty int, emptied bool) {
		batch := uuid.Must(uuid.NewV7()).String()
		w := do(r, "PUT", outletPath("/stock/batches/"+batch), "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-%s","expires_on":%q}`, product, batch[24:], expires))
		require.Equal(t, 200, w.Code, w.Body.String())
		w = do(r, "PUT", outletPath("/stock/movements/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(location, batch, "receipt", "TEST-RECEIPT-"+batch[24:], qty))
		require.Equal(t, 200, w.Code, w.Body.String())
		if emptied {
			w = do(r, "PUT", outletPath("/stock/corrections/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(location, batch, "correction", "TEST-EMPTY-"+batch[24:], -qty))
			require.Equal(t, 200, w.Code, w.Body.String())
		}
	}
	stockLine(day(-1), 5, false)
	stockLine(day(0), 5, false)
	stockLine(day(30), 5, false)
	stockLine(day(31), 5, false)
	stockLine(day(90), 5, false)
	stockLine(day(200), 5, false)
	stockLine(day(10), 5, true)

	// Two concerns recorded now, inside the 30-day window on the pinned clock.
	for range 2 {
		w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(fixtureBatch, location))
		require.Equal(t, 200, w.Code, w.Body.String())
	}

	// Bills against the pinned outlet date, 2026-11-01.
	for ref, due := range map[string]string{"TEST-OVERDUE": "2026-10-20", "TEST-THIS-WEEK": "2026-11-03", "TEST-LATER": "2026-12-01"} {
		body := strings.Replace(expenseBody(ref), `"due_on":"2026-10-31"`, `"due_on":"`+due+`"`, 1)
		w := do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}

	code, got, raw := getOverview(t, r, "editor")
	require.Equal(t, http.StatusOK, code, raw)
	require.Equal(t, "Asia/Kolkata", got.TimeZone)
	require.NotEmpty(t, got.AsOf)
	require.NotNil(t, got.Stock)
	require.Equal(t, 1, got.Stock.Expired)
	require.Equal(t, 2, got.Stock.Due30)
	require.Equal(t, 4, got.Stock.Due90)
	// The register is always read as of a moment; the stock page sends now.
	asOf := url.QueryEscape(time.Now().UTC().Format(time.RFC3339Nano))
	require.Equal(t, listCount(t, r, outletPath("/stock/positions?balance=on_hand&expiry=expired&as_of="+asOf)), got.Stock.Expired)
	require.Equal(t, listCount(t, r, outletPath("/stock/positions?balance=on_hand&expiry=due_30&as_of="+asOf)), got.Stock.Due30)
	require.Equal(t, listCount(t, r, outletPath("/stock/positions?balance=on_hand&expiry=due_90&as_of="+asOf)), got.Stock.Due90)

	require.Equal(t, 2, got.Safety.Recent)
	require.Equal(t, listCount(t, r, outletPath("/stock/safety-incidents?recorded=30d")), got.Safety.Recent)

	require.Equal(t, 1, got.Bills.Overdue)
	require.Equal(t, 1, got.Bills.Week)
	require.Equal(t, listCount(t, r, outletPath("/accounts/payables?due=overdue")), got.Bills.Overdue)
	require.Equal(t, listCount(t, r, outletPath("/accounts/payables?due=week")), got.Bills.Week)

	require.Equal(t, 1, got.Setup.Unclassified)
	require.Equal(t, listCount(t, r, outletPath("/catalogue/products?tax=unclassified")), got.Setup.Unclassified)
	require.NotNil(t, got.Setup.RegistrationInForce)
	require.False(t, *got.Setup.RegistrationInForce)

	w := do(r, "POST", outletPath("/tax-registrations"), "editor", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", ""))
	require.Equal(t, 201, w.Code, w.Body.String())
	_, got, _ = getOverview(t, r, "editor")
	require.True(t, *got.Setup.RegistrationInForce)
}

// A section the person may not see is absent, never zero; lacking one
// permission does not refuse the whole overview.
func TestOverviewSectionsFollowPermissions(t *testing.T) {
	r := overviewHarness(t)

	code, got, raw := getOverview(t, r, "pharmacist")
	require.Equal(t, http.StatusOK, code, raw)
	require.NotNil(t, got.Stock)
	require.NotNil(t, got.Safety)
	require.Nil(t, got.Bills)
	require.NotNil(t, got.Setup)
	require.Nil(t, got.Setup.RegistrationInForce)
	require.NotContains(t, raw, "registration_in_force")
	require.NotContains(t, raw, `"bills"`)

	code, got, raw = getOverview(t, r, "cataloguer")
	require.Equal(t, http.StatusOK, code, raw)
	require.Nil(t, got.Stock)
	require.Nil(t, got.Safety)
	require.Nil(t, got.Bills)
	require.NotNil(t, got.Setup)
}

func TestOverviewOtherTenantAndNoGrant(t *testing.T) {
	r := overviewHarness(t)
	code, _, _ := getOverview(t, r, "ungranted")
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = getOverview(t, r, "foreign")
	require.Equal(t, http.StatusNotFound, code)
}
