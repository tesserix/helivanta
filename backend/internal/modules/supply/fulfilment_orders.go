package supply

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"sync/atomic"
	"time"
)

type fulfilmentOrderOwner struct{ deps platform.Deps }
type fulfilmentOrderProof struct {
	owner        *fulfilmentOrderOwner
	scope        f.SourceScope
	purpose      f.OrderPurpose
	snapshot     supplierorder.SupplierOrder
	relationship suppliertrade.RelationshipAuthorization
	consumed     atomic.Bool
}

// FulfilmentOrders exposes consent publication, never stock writes or a user grant.
func (*Module) FulfilmentOrders(deps platform.Deps) f.CurrentOrderGuard {
	return &fulfilmentOrderOwner{deps: deps}
}
func (g *fulfilmentOrderOwner) VerifyOrder(ctx context.Context, s f.SourceScope, purpose f.OrderPurpose) (f.OrderAuthorization, error) {
	if !s.Valid() || (purpose != f.NewReservation && purpose != f.DispatchOrder && purpose != f.RetainedOrder) {
		return nil, f.ErrInvalid
	}
	if g.deps.DB == nil || tenantdb.ActorFrom(ctx) == "" || g.deps.DB.GuardedCommandActive(ctx) {
		return nil, f.ErrUnavailable
	}
	var snapshot supplierorder.SupplierOrder
	err := g.deps.DB.WithTenant(ctx, s.TenantID.String(), func(tx *gorm.DB) error {
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "lookup", "app.supply_order_id": s.OrderID.String()}, func() error {
			row, err := loadPurchaseOrder(tx, s.TenantID.String(), s.OrderID)
			if err != nil {
				return err
			}
			snapshot = row.Order
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if !sourceOrderMatches(snapshot, s) {
		return nil, supplierorder.ErrIneligible
	}
	p := &fulfilmentOrderProof{owner: g, scope: s, purpose: purpose, snapshot: snapshot}
	if purpose != f.RetainedOrder {
		if g.deps.TradingRelationships == nil || g.deps.TradingEndpoints == nil || g.deps.OrderProducts == nil {
			return nil, f.ErrUnavailable
		}
		if snapshot.Status != "accepted" || !snapshot.Input.ExpiresAt.After(time.Now()) {
			return nil, f.ErrConflict
		}
		p.relationship, err = g.deps.TradingRelationships.VerifyRelationship(ctx, suppliertrade.RelationshipScope{TenantID: s.TenantID.String(), ID: snapshot.Input.RelationshipID, TermsSha256: snapshot.Input.TermsSha256, Purpose: "new_business"})
		if err != nil {
			return nil, err
		}
		if purpose == f.DispatchOrder {
			if g.deps.SourceChecks == nil {
				return nil, f.ErrUnavailable
			}
			if _, err = g.deps.SourceChecks.Check(ctx, suppliertrade.SourceCheckInput{Endpoint: snapshot.Terms.Buyer, Category: snapshot.Terms.Category, Operation: "receive"}); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}
func sourceOrderMatches(o supplierorder.SupplierOrder, s f.SourceScope) bool {
	return o.ID == s.OrderID && o.OwnerTenantID == s.TenantID && o.Side == "seller" && o.Terms.Seller.Product == s.Product && o.Terms.Seller.OrganizationID == s.OrganizationID && o.Terms.Seller.LocationID == s.LocationID
}
func (p *fulfilmentOrderProof) WithCurrent(ctx context.Context, action func(context.Context, supplierorder.SupplierOrder) error) error {
	if p == nil || p.owner == nil || action == nil || p.consumed.Swap(true) || !p.owner.deps.DB.GuardedCommandActive(ctx) {
		return f.ErrUnavailable
	}
	g, s := p.owner, p.scope
	return g.deps.DB.WithTenantCommand(ctx, s.TenantID.String(), func(tx *gorm.DB) error {
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "seller_read", "app.supply_order_product": s.Product, "app.supply_order_organization": s.OrganizationID.String(), "app.supply_order_location": s.LocationID.String()}, func() error {
			// The stock owner's work joins this same guarded command; no network call occurs here.
			if err := tx.Exec(`SELECT id FROM supply_purchase_orders WHERE tenant_id=? AND id=? FOR SHARE`, s.TenantID, s.OrderID).Error; err != nil {
				return err
			}
			row, err := loadPurchaseOrder(tx, s.TenantID.String(), s.OrderID)
			if err != nil {
				return err
			}
			o := row.Order
			if !sourceOrderMatches(o, s) || o.InputSha256 != p.snapshot.InputSha256 {
				return f.ErrConflict
			}
			if p.purpose == f.RetainedOrder {
				return action(ctx, o)
			}
			if o.Version != p.snapshot.Version || o.Status != "accepted" || !o.Input.ExpiresAt.After(time.Now()) {
				return f.ErrConflict
			}
			if err = g.deps.DB.ConstrainGuardedCommand(ctx, s.TenantID.String(), o.Input.ExpiresAt); err != nil {
				return err
			}
			return g.deps.TradingRelationships.WithVerifiedRelationship(ctx, p.relationship, func(ctx context.Context, _ suppliertrade.Relationship) error {
				operation := "order"
				if p.purpose == f.DispatchOrder {
					operation = "dispatch"
				}
				return g.deps.TradingEndpoints.WithCurrentTradingEndpoint(ctx, o.Terms.Seller, o.Terms.Category, operation, func(ctx context.Context, _ suppliertrade.EndpointReference) error {
					return g.deps.OrderProducts.WithCurrentOrderProducts(ctx, o.Terms.Seller, o.Input.Lines, func(ctx context.Context) error { return action(ctx, o) })
				})
			})
		})
	})
}
