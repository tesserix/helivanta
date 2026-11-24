package pharmacy_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReorderNarrowProductChoicesAndOverview(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, _, _ := stockFixture(t, r)
	choices := do(r, "GET", outletPath("/catalogue/reorder-products?limit=1"), "editor", "")
	require.Equal(t, 200, choices.Code, choices.Body.String())
	var page struct {
		Data []map[string]any `json:"data"`
		Page struct {
			More bool `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(choices.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, product, page.Data[0]["id"])
	require.ElementsMatch(t, []string{"id", "outlet_id", "sku", "brand_name", "unit_of_issue", "version", "status"}, mapKeys(page.Data[0]))
	require.False(t, page.Page.More)
	current := do(r, "GET", outletPath("/catalogue/reorder-products/")+product, "editor", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	var identity map[string]any
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &identity))
	require.Equal(t, page.Data[0], identity)
	summary := func(want float64) {
		t.Helper()
		result := do(r, "GET", outletPath("/stock/low-products/overview"), "editor", "")
		require.Equal(t, 200, result.Code, result.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &body))
		require.Equal(t, want, body["low_stock"])
		require.NotEmpty(t, body["observed_at"])
		require.NotEmpty(t, body["local_date"])
		require.NotEmpty(t, body["time_zone"])
		require.NotContains(t, body, "bills")
		require.NotContains(t, body, "safety")
	}
	summary(0)
	setReorder(t, r, product, "0", 0)
	summary(1)
	setReorder(t, r, product, "null", 1)
	summary(0)
	for _, path := range []string{"/catalogue/reorder-products", "/catalogue/reorder-products/" + product, "/stock/low-products/overview"} {
		for _, token := range []string{"sibling", "foreign"} {
			require.Equal(t, 404, do(r, "GET", outletPath(path), token, "").Code)
		}
		require.Equal(t, 403, do(r, "GET", outletPath(path), "denied", "").Code)
		require.Equal(t, 401, do(r, "GET", outletPath(path), "unknown", "").Code)
		require.Equal(t, 400, do(r, "GET", outletPath(path)+"?tenant_id=forged", "editor", "").Code)
	}
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}
