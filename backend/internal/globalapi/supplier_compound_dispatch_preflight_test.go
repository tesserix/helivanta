package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// These retained canonical orders isolate preflight; they do not prove public procurement or physical dispatch.
func (f supplierCompoundFixture) assertDispatchBuyerPreflight(t *testing.T, buyerActor operatingDutyFixture, buyer suppliertrade.Endpoint, sellers ...suppliertrade.Endpoint) {
	t.Helper()
	modules := f.registry.All()
	deps := platform.Deps{DB: f.db, Authz: f.graph, OrganizationIdentity: supplierIdentityForTest(t), TradingEndpoints: platform.TradingEndpoints(modules, f.db, platform.DepartmentReferences(modules, f.db)), OrderProducts: platform.OrderProducts(modules, f.db, platform.StockProducts(modules, f.db))}
	deps.SourceChecks = tradingSourcePeerForTest(t, deps)
	deps.TradingRelationships = supply.NewRelationshipAuthority(f.db, deps.TradingEndpoints, deps.SourceChecks, deps.OrganizationIdentity)
	orders := supply.New().FulfilmentOrders(deps)
	scopes := make([]fulfilment.SourceScope, 0, len(sellers))
	for _, seller := range sellers {
		relationship := confirmedCompoundRelationship(t, f.db, seller, buyer)
		buyerProof, err := deps.TradingRelationships.VerifyRelationship(t.Context(), suppliertrade.RelationshipScope{TenantID: buyer.TenantID.String(), ID: relationship.ID, TermsSha256: relationship.TermsSha256, Purpose: "new_business"})
		require.NoError(t, err)
		now := time.Now().UTC().Truncate(time.Microsecond)
		product := supplierorder.SupplierOrderProduct{Owner: seller.Product, ProductID: uuid.New(), VersionID: ptr(uuid.New())}
		if seller.Product == "pharmacy" {
			product.ProductID, product.VersionID, product.Version = uuid.Must(uuid.NewV7()), nil, ptr(int64(1))
		}
		input := supplierorder.SupplierOrderInput{RelationshipID: relationship.ID, TermsSha256: relationship.TermsSha256, PurchaseOrderReference: "TEST-PREFLIGHT-" + seller.Product, QuoteReference: "TEST preflight fixture", ExpiresAt: now.Add(30 * time.Minute), Reason: "TEST retained order for current buyer protocol", Lines: []supplierorder.SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: product, QuantityUnits: 10, UnitCode: "capsule"}}}
		order, err := supplierorder.Prepare(uuid.Must(uuid.NewV7()), buyerProof.Snapshot(), input, supplierorder.Actor{Subject: "TEST-preflight-buyer-maker", StaffID: uuid.Must(uuid.NewV7())}, now)
		require.NoError(t, err)
		_, offer, err := supplierorder.Decide(order, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST canonical buyer approval"}, supplierorder.Actor{Subject: "TEST-preflight-buyer-reviewer", StaffID: uuid.Must(uuid.NewV7())}, now)
		require.NoError(t, err)
		require.NotNil(t, offer)
		incoming, err := supplierorder.Apply(nil, *offer, seller.TenantID, now)
		require.NoError(t, err)
		actor := supplierorder.Actor{Subject: "TEST-preflight-source-reviewer", StaffID: uuid.Must(uuid.NewV7())}
		accepted, _, err := supplierorder.Decide(incoming, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST canonical source acceptance"}, actor, now)
		require.NoError(t, err)
		require.NoError(t, f.db.WithTenant(tenantdb.WithActor(t.Context(), actor.Subject), seller.TenantID.String(), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.supply_order_mode','seller_write',true),set_config('app.supply_order_product',?,true),set_config('app.supply_order_organization',?,true),set_config('app.supply_order_location',?,true),set_config('app.supply_order_actor_staff',?,true)`, seller.Product, seller.OrganizationID.String(), seller.LocationID.String(), actor.StaffID.String()).Error; err != nil {
				return err
			}
			for index, state := range []supplierorder.SupplierOrder{incoming, accepted} {
				raw, err := json.Marshal(state)
				if err != nil {
					return err
				}
				if index == 0 {
					err = tx.Exec(`INSERT INTO supply_purchase_orders(tenant_id,id,origin_tenant_id,origin_record_id,side,hospital_id,buyer_outlet_id,source_product,source_organization_id,source_location_id,relationship_id,version,body,actor_subject,actor_staff_id,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?,?)`, seller.TenantID, state.ID, buyer.TenantID, state.ID, state.Side, *buyer.HospitalID, buyer.LocationID, seller.Product, seller.OrganizationID, seller.LocationID, relationship.ID, state.Version, string(raw), actor.Subject, actor.StaffID, state.CreatedAt, state.UpdatedAt).Error
				} else {
					err = tx.Exec(`UPDATE supply_purchase_orders SET body=?::jsonb,version=?,actor_subject=?,actor_staff_id=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, string(raw), state.Version, actor.Subject, actor.StaffID, state.UpdatedAt, seller.TenantID, state.ID, incoming.Version).Error
				}
				if err != nil {
					return err
				}
				if err = tx.Exec(`INSERT INTO supply_purchase_order_history(tenant_id,origin_tenant_id,origin_record_id,order_id,version,body,actor_subject,created_at)VALUES(?,?,?,?,?,?::jsonb,?,?)`, seller.TenantID, buyer.TenantID, state.ID, state.ID, state.Version, string(raw), actor.Subject, state.UpdatedAt).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		scopes = append(scopes, fulfilment.SourceScope{TenantID: seller.TenantID, Product: seller.Product, OrganizationID: seller.OrganizationID, LocationID: seller.LocationID, OrderID: accepted.ID})
	}
	for _, scope := range scopes {
		proof, err := orders.VerifyOrder(tenantdb.WithActor(t.Context(), "TEST-dispatch-preflight"), scope, fulfilment.DispatchOrder)
		require.ErrorIs(t, err, suppliertrade.ErrIneligible, "an order policy cannot replace buyer dispatch eligibility: %s", scope.Product)
		require.Nil(t, proof)
	}
	approveSyntheticTradingPolicyForReviewer(t, buyerActor, "IN-MH", "prescription", "dispatch", "compound-source-reviewer")
	for _, scope := range scopes {
		proof, err := orders.VerifyOrder(tenantdb.WithActor(t.Context(), "TEST-dispatch-preflight"), scope, fulfilment.DispatchOrder)
		require.NoError(t, err, scope.Product)
		require.NotNil(t, proof)
	}
}
