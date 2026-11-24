package release

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLowStockRoutesHaveAnIndependentReleaseBoundary(t *testing.T) {
	const root = "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet"
	for _, tc := range []struct{ method, path, capability string }{
		{"GET", root + "/catalogue/products/:id/reorder-level", "pharmacy_low_stock"},
		{"GET", root + "/catalogue/products/:id/reorder-levels/:command", "pharmacy_low_stock"},
		{"PUT", root + "/catalogue/products/:id/reorder-levels/:command", "pharmacy_low_stock"},
		{"GET", root + "/stock/low-products", "pharmacy_low_stock"},
		{"GET", root + "/stock/low-products/overview", "pharmacy_low_stock"},
		{"GET", root + "/catalogue/reorder-products", "pharmacy_low_stock"},
		{"GET", root + "/catalogue/reorder-products/:id", "pharmacy_low_stock"},
		{"PUT", root + "/catalogue/products/:id", "pharmacy_catalogue"},
		{"GET", root + "/catalogue/products", "pharmacy_catalogue"},
		{"PUT", root + "/stock/movements/:id", "pharmacy_stock"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, known := RouteCapability(tc.method, tc.path)
			require.True(t, known, "the actual admission classifier must recognize the route")
			require.Equal(t, tc.capability, got)
		})
	}
	_, known := RouteCapability("PUT", root+"/stock/low-products/automatic-order")
	require.False(t, known, "low-stock activation must not admit ordering")
}
