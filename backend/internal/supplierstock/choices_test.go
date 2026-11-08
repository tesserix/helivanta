package supplierstock

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"testing"
)

func TestStockSelectionRegistersBoundedOrderLineChoices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1"), closedChoiceMembership{})
	(HTTP{}).Register(r, "/sources/:party/locations/:premise/supplier-orders/:order/allocations/:allocation")
	found := false
	for _, route := range r.Declared() {
		if route.Path == "/v1/sources/:party/locations/:premise/supplier-orders/:order/lines/:line/stock-choices" {
			found = true
			require.True(t, route.Paginated)
		}
	}
	require.True(t, found, "physical selectors must use a bounded exact-order/line owner route")
}

type closedChoiceMembership struct{}

func (closedChoiceMembership) IsMember(context.Context, string, string) (bool, error) {
	return false, nil
}
