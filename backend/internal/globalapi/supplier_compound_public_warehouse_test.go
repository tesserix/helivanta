package globalapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) assertPublicWarehouseFulfilment(t *testing.T, admin, operator operatingDutyFixture, relationship suppliertrade.Relationship, department uuid.UUID, preparer, reviewer string) {
	t.Helper()
	publishStockProduct(t, f.db, "active")
	var found struct {
		VersionID uuid.UUID `json:"version_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(admin.call("GET", "/v1/drugmaster/products/by-gtin/8901234567890", "owner", nil, 200)), &found))
	var product stockproduct.Reference
	require.NoError(t, drugmaster.New().StockProducts(f.db).WithCurrentStockProduct(t.Context(), found.VersionID, func(_ context.Context, ref stockproduct.Reference) error { product = ref; return nil }))
	require.Equal(t, "capsule", product.UnitOfIssue)
	require.EqualValues(t, 10, product.PackSize)
	order := f.assertPublicSupplierOrder(t, admin, operator, relationship, department, preparer, reviewer, supplierorder.SupplierOrderProduct{Owner: "supply", ProductID: product.ProductID, VersionID: &found.VersionID}, product.UnitOfIssue, "TEST-COMPOUND-PUBLIC-WAREHOUSE")
	seller := relationship.Terms.Seller
	base := "/v1/supply/parties/" + seller.OrganizationID.String() + "/operating-locations/" + seller.LocationID.String()
	stock := base + "/stock"
	receipt, identity := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	receiptPath := stock + "/receipts/" + receipt.String()
	admin.call("PUT", receiptPath, "owner", map[string]any{"product_version_id": found.VersionID, "lot_number": "TEST-WAREHOUSE-COMPOUND", "expires_on": "2030-12-31", "unit_code": "pack", "quantity_units": 10, "source_reference": "TEST-COMPOUND-WAREHOUSE-GRN:1", "reason": "TEST counted ten source-owned packs"}, 201)
	admin.call("PUT", receiptPath+"/identity-checks/"+identity.String(), "owner", map[string]any{"expected_version": 0, "product_version_id": found.VersionID, "unit_kind": "pack", "expected_pack_size": 10, "expected_base_units": 100, "reason": "TEST explicit published-owner pack to base-unit verification"}, 201)
	qualityPath := receiptPath + "/quality-decisions/" + uuid.Must(uuid.NewV7()).String()
	quality := map[string]any{"expected_version": 0, "identity_check_id": identity, "identity_version": 1, "decision": "release", "reason": "TEST independent warehouse physical quality release"}
	denied := f.sourceStaffRequest(t, operator.principal, "PUT", qualityPath, quality)
	require.Equal(t, 404, denied.Code, "stock owner hides quality evidence without an independently reviewed quality duty: %s", denied.Body.String())
	f.approveSourceOperatingDuty(t, admin, operator, "supply.stock_quality_reviewer.v1", "TEST separate warehouse stock quality authority")
	approved := f.sourceStaffRequest(t, operator.principal, "PUT", qualityPath, quality)
	require.Equal(t, 201, approved.Code, approved.Body.String())
	replay := f.sourceStaffRequest(t, operator.principal, "PUT", qualityPath, quality)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, approved.Body.String(), replay.Body.String())
	approveSyntheticTradingPolicyForReviewer(t, admin, seller.Jurisdiction, "prescription", "dispatch", "compound-source-reviewer")
	shipments := f.assertPublicStockDispatch(t, operator, order, base+"/supplier-orders/"+order.ID.String(), receipt, nil, department, preparer)
	for _, shipment := range shipments {
		require.Equal(t, "supply", shipment.Product.Owner)
		require.Equal(t, product.ProductID, shipment.Product.ProductID)
		require.Equal(t, found.VersionID, *shipment.Product.VersionID)
		require.Equal(t, product.UnitOfIssue, shipment.UnitCode)
		require.Nil(t, shipment.MrpMinor, "the warehouse does not claim pharmacy-owned printed MRP")
	}
}
