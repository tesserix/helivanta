package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func classificationsPath(product string) string { return productPath(product) + "/tax-classifications" }

func classificationBody(code, nature, rate, basis, effective, reason string) string {
	fields := []string{fmt.Sprintf(`"classification_code":%q,"supply_nature":%q,"rate_percent":%q,"effective_from":%q`, code, nature, rate, effective)}
	if basis != "" {
		fields = append(fields, fmt.Sprintf(`"exemption_basis":%q`, basis))
	}
	if reason != "" {
		fields = append(fields, fmt.Sprintf(`"reason":%q`, reason))
	}
	return "{" + strings.Join(fields, ",") + "}"
}

type productTax struct {
	ClassificationCode string `json:"classification_code"`
	SupplyNature       string `json:"supply_nature"`
	RatePPM            int64  `json:"rate_ppm"`
	EffectiveFrom      string `json:"effective_from"`
}

type productWithTax struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Tax    *productTax `json:"tax"`
}

func putTaxProduct(t *testing.T, r *gin.Engine, sku, status string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(strings.Replace(productBody(0, "TEST "+sku), "TEST-AMOX-500", sku, 1), `"status":"active"`, `"status":"`+status+`"`, 1)
	w := do(r, "PUT", productPath(id), "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	return id
}

func getProductTax(t *testing.T, r *gin.Engine, id string) *productTax {
	t.Helper()
	w := do(r, "GET", productPath(id), "pharmacist", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var p productWithTax
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	return p.Tax
}

func TestTaxClassificationRecordsAndShowsOnTheProduct(t *testing.T) {
	r := taxHarness(t)
	id := putTaxProduct(t, r, "TEST-AMOX", "active")
	require.Nil(t, getProductTax(t, r, id))

	w := do(r, "POST", classificationsPath(id), "owner", classificationBody("3004", "taxable", "5", "", "2026-11-01", ""))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"rate_ppm":50000`)
	require.Equal(t, &productTax{"3004", "taxable", 50000, "2026-11-01"}, getProductTax(t, r, id))

	// A rate change entered ahead of its date does not apply yet.
	require.Equal(t, 201, do(r, "POST", classificationsPath(id), "owner", classificationBody("30049099", "taxable", "0.25", "", "2026-12-01", "")).Code)
	require.Equal(t, int64(50000), getProductTax(t, r, id).RatePPM)
	// A same-day correction replaces today's row.
	require.Equal(t, 400, do(r, "POST", classificationsPath(id), "owner", classificationBody("3004", "taxable", "12", "", "2026-11-01", "")).Code)
	require.Equal(t, 201, do(r, "POST", classificationsPath(id), "owner", classificationBody("3004", "taxable", "12", "", "2026-11-01", "Wrong slab")).Code)
	require.Equal(t, int64(120000), getProductTax(t, r, id).RatePPM)

	w = do(r, "GET", classificationsPath(id), "pharmacist", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct {
			RatePPM  int64 `json:"rate_ppm"`
			InForce  bool  `json:"in_force"`
			Replaced bool  `json:"replaced"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 3)
	state := map[int64][2]bool{}
	for _, row := range page.Data {
		state[row.RatePPM] = [2]bool{row.InForce, row.Replaced}
	}
	require.Equal(t, [2]bool{false, true}, state[50000])
	require.Equal(t, [2]bool{true, false}, state[120000])
	require.Equal(t, [2]bool{false, false}, state[2500])

	// Exempt needs a basis; nil-rated carries no rate.
	exempt := putTaxProduct(t, r, "TEST-EXEMPT", "active")
	require.Equal(t, 201, do(r, "POST", classificationsPath(exempt), "owner", classificationBody("3006", "exempt", "0", "TEST notification entry", "2026-11-01", "")).Code)
	require.Equal(t, &productTax{"3006", "exempt", 0, "2026-11-01"}, getProductTax(t, r, exempt))
}

func TestTaxClassificationRejects(t *testing.T) {
	r := taxHarness(t)
	id := putTaxProduct(t, r, "TEST-AMOX", "active")
	for name, body := range map[string]string{
		"HSN of 5 digits":         classificationBody("30049", "taxable", "5", "", "2026-11-01", ""),
		"HSN with letters":        classificationBody("30A4", "taxable", "5", "", "2026-11-01", ""),
		"three decimals":          classificationBody("3004", "taxable", "5.001", "", "2026-11-01", ""),
		"above 100":               classificationBody("3004", "taxable", "100.01", "", "2026-11-01", ""),
		"taxable at zero":         classificationBody("3004", "taxable", "0", "", "2026-11-01", ""),
		"nil-rated with a rate":   classificationBody("3004", "nil_rated", "5", "", "2026-11-01", ""),
		"exempt without a basis":  classificationBody("3004", "exempt", "0", "", "2026-11-01", ""),
		"basis on a taxable line": classificationBody("3004", "taxable", "5", "TEST notification", "2026-11-01", ""),
		"negative rate":           classificationBody("3004", "taxable", "-5", "", "2026-11-01", ""),
		"before today":            classificationBody("3004", "taxable", "5", "", "2026-10-31", ""),
		"not a date":              classificationBody("3004", "taxable", "5", "", "2026-13-01", ""),
		"unknown nature":          classificationBody("3004", "zero_rated", "0", "", "2026-11-01", ""),
	} {
		t.Run(name, func(t *testing.T) {
			w := do(r, "POST", classificationsPath(id), "owner", body)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	require.Nil(t, getProductTax(t, r, id))

	body := classificationBody("3004", "taxable", "5", "", "2026-11-01", "")
	require.Equal(t, 403, do(r, "POST", classificationsPath(id), "pharmacist", body).Code, "catalogue.write is not tax.manage")
	require.Equal(t, 404, do(r, "POST", classificationsPath(id), "foreign", body).Code)
	require.Equal(t, 404, do(r, "POST", classificationsPath(uuid.Must(uuid.NewV7()).String()), "owner", body).Code)
	require.Equal(t, 404, do(r, "POST", classificationsPath(id), "ungranted", body).Code, "classification needs outlet access")

	withdrawn := putTaxProduct(t, r, "TEST-GONE", "withdrawn")
	require.Equal(t, 409, do(r, "POST", classificationsPath(withdrawn), "owner", body).Code)
	discontinued := putTaxProduct(t, r, "TEST-OLD", "discontinued")
	require.Equal(t, 201, do(r, "POST", classificationsPath(discontinued), "owner", body).Code)
}

func TestTaxClassificationRefusesCountryWithoutScheme(t *testing.T) {
	r := taxHarness(t)
	w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+schemelessOutlet+"/access/user-owner", "editor", `{"level":"editor"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	id := uuid.Must(uuid.NewV7()).String()
	legacyProduct := strings.Replace(productPath(id), pharmaOutlet, schemelessOutlet, 1)
	w = do(r, "PUT", legacyProduct, "owner", productBody(0, "TEST Amoxil"))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "POST", legacyProduct+"/tax-classifications", "owner", classificationBody("3004", "taxable", "5", "", "2026-11-01", ""))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "tax_scheme_unavailable")
}

// "Not classified" is judged on the outlet's date, active and discontinued
// products only.
func TestUnclassifiedFilterUsesTheOutletsDate(t *testing.T) {
	r := taxHarness(t)
	classified := putTaxProduct(t, r, "TEST-DONE", "active")
	tomorrow := putTaxProduct(t, r, "TEST-LATER", "active")
	missing := putTaxProduct(t, r, "TEST-TODO", "discontinued")
	putTaxProduct(t, r, "TEST-GONE", "withdrawn")
	require.Equal(t, 201, do(r, "POST", classificationsPath(classified), "owner", classificationBody("3004", "taxable", "5", "", "2026-11-01", "")).Code)
	require.Equal(t, 201, do(r, "POST", classificationsPath(tomorrow), "owner", classificationBody("3004", "taxable", "5", "", "2026-11-02", "")).Code)

	list := func() []string {
		w := do(r, "GET", strings.TrimSuffix(productPath(""), "/")+"?tax=unclassified", "pharmacist", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct{ Data []productWithTax }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		ids := []string{}
		for _, p := range page.Data {
			ids = append(ids, p.ID)
		}
		return ids
	}
	require.ElementsMatch(t, []string{tomorrow, missing}, list())
	// 00:30 on 2 November in India, still 1 November in UTC.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 19, 0, 0, 0, time.UTC) })
	require.ElementsMatch(t, []string{missing}, list())

	require.Equal(t, 400, do(r, "GET", strings.TrimSuffix(productPath(""), "/")+"?tax=classified", "pharmacist", "").Code)
}

// Saving a product reports its tax as it is, never a false "Not classified".
func TestProductSaveReportsTheTaxInForce(t *testing.T) {
	r := taxHarness(t)
	id := putTaxProduct(t, r, "TEST-AMOX", "active")
	require.Equal(t, 201, do(r, "POST", classificationsPath(id), "owner", classificationBody("3004", "taxable", "5", "", "2026-11-01", "")).Code)
	body := strings.Replace(strings.Replace(productBody(1, "TEST Renamed"), "TEST-AMOX-500", "TEST-AMOX", 1), `"status":"active"`, `"status":"active"`, 1)
	w := do(r, "PUT", productPath(id), "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var saved productWithTax
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, &productTax{"3004", "taxable", 50000, "2026-11-01"}, saved.Tax)
	// Revisions record product identity; tax has its own history.
	w = do(r, "GET", productPath(id)+"/revisions", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"tax"`)
}
