package pharmacy

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"testing"
)

type receivingChoiceMembership struct{}

func (receivingChoiceMembership) IsMember(context.Context, string, string) (bool, error) {
	return false, nil
}
func TestHospitalReceivingRegistersBoundedDestinationProductChoices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	found := false
	for _, route := range r.Declared() {
		if route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receiving/product-choices" {
			found = true
			require.True(t, route.Paginated)
		}
	}
	require.True(t, found, "a receiver must choose an owned current product through the verified shipment scope")
}

func TestHospitalReceivingRegistersBoundedQuarantineAndSellableLocations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	for _, kind := range []string{"quarantine", "sellable"} {
		found := false
		for _, route := range r.Declared() {
			if route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receiving/"+kind+"-locations" {
				found = true
				require.True(t, route.Paginated)
			}
		}
		require.True(t, found, "stock status must be fixed by the owner route, not an arbitrary filter")
	}
}

func TestHospitalReceivingRegistersReceiptBoundPhysicalRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	for _, route := range r.Declared() {
		if route.Method == "PUT" && route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receipts/:receipt/releases/:release" {
			return
		}
	}
	t.Fatal("reviewed hospital receiving needs a receipt-bound physical release command")
}

func TestHospitalReceivingRegistersProductLotBatchChoices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	for _, route := range r.Declared() {
		if route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receiving/product-choices/:product/batches" {
			require.True(t, route.Paginated)
			return
		}
	}
	t.Fatal("receivers need the exact destination lot before choosing an existing or new batch")
}

func TestHospitalReceivingRegistersBoundedReceiptReleaseHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	for _, route := range r.Declared() {
		if route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receipts/:receipt/releases" {
			require.True(t, route.Paginated)
			return
		}
	}
	t.Fatal("physical releases need bounded hospital history, distinct from review evidence")
}

func TestHospitalReceivingRegistersBoundedClearanceEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := platform.NewRouter(gin.New().Group("/v1/pharmacy"), receivingChoiceMembership{})
	supplierReceivingRoutes(r, platform.Deps{})
	for _, route := range r.Declared() {
		if route.Path == "/v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receipts/:receipt/clearances" {
			require.True(t, route.Paginated)
			return
		}
	}
	t.Fatal("independent clearance evidence needs an attributable, bounded history")
}
