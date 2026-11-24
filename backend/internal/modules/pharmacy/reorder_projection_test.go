package pharmacy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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

func TestReorderDisclosureRequiresCommittedAudit(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, _, _ := stockFixture(t, r)
	level := setReorder(t, r, product, "0", 0)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_reorder_disclosure_fault", SQL: `
CREATE FUNCTION test_reorder_disclosure_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.resource_type LIKE 'pharmacy_low_stock%' OR NEW.resource_type='pharmacy_reorder_level' THEN
  RAISE EXCEPTION 'synthetic private disclosure evidence unavailable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER test_reorder_disclosure_fault BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION test_reorder_disclosure_fault();`}}))
	defer func() {
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "test_reorder_disclosure_restore", SQL: `DROP TRIGGER test_reorder_disclosure_fault ON audit_events;DROP FUNCTION test_reorder_disclosure_fault();`}}))
	}()
	for _, path := range []string{
		"/catalogue/reorder-products", "/catalogue/reorder-products/" + product,
		"/stock/low-products", "/stock/low-products/overview",
		"/catalogue/products/" + product + "/reorder-level",
		"/catalogue/products/" + product + "/reorder-levels/" + level.ID,
	} {
		result := do(r, "GET", outletPath(path), "editor", "")
		require.Equal(t, 503, result.Code, result.Body.String())
		require.NotContains(t, result.Body.String(), product)
		require.NotContains(t, result.Body.String(), "synthetic private")
		require.NotContains(t, result.Body.String(), "minimum_units")
	}
	replay := do(r, "PUT", productPath(product)+"/reorder-levels/"+level.ID, "editor", reorderBody("0", 0, level.ProductVersion))
	require.Equal(t, 503, replay.Code, replay.Body.String())
	require.NotContains(t, replay.Body.String(), "minimum_units")
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}
