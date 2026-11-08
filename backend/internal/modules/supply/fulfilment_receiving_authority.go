package supply

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

const (
	PermGoodsReceiptRead   authz.Permission = "supply.goods_receipt.read"
	PermGoodsReceiptRecord authz.Permission = "supply.goods_receipt.record"
	PermGoodsReceiptReview authz.Permission = "supply.goods_receipt.review"
)
const (
	PharmacyHospitalReceivingUse    authz.Permission = "pharmacy.hospital_receiving.use"
	PharmacyHospitalReceivingReview authz.Permission = "pharmacy.hospital_receiving.review"
)

type receivingAuthority struct{ h *purchaseOrderHandlers }

func (*Module) ReceivingAuthority(deps platform.Deps) f.ReceivingAuthority {
	return receivingAuthority{h: &purchaseOrderHandlers{db: deps.DB, authority: deps.ScopedAuthorizer, actors: deps.PurchaseOrderActors, departments: deps.DepartmentScopes, endpoints: deps.TradingEndpoints}}
}
func (g receivingAuthority) WithShipment(ctx context.Context, p authn.Principal, s f.BuyerScope, purpose f.ReceivingPurpose, action func(context.Context, f.Shipment, supplierorder.Actor) error) error {
	if !s.Valid() || p.TenantID != s.TenantID.String() || p.Subject == "" || action == nil {
		return f.ErrInvalid
	}
	permission, extra := PermGoodsReceiptRead, PharmacyHospitalReceivingUse
	switch purpose {
	case f.ReadReceiving:
	case f.RecordReceiving:
		permission = PermGoodsReceiptRecord
	case f.ReviewReceiving:
		permission = PermGoodsReceiptReview
		extra = PharmacyHospitalReceivingReview
	default:
		return f.ErrInvalid
	}
	admit := func(extra authz.Permission) error {
		return g.h.buyer(ctx, p, buyerOrderScope{hospital: s.HospitalID, department: s.DepartmentID, outlet: s.OutletID}, permission, func(ctx context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
			var row struct{ Body json.RawMessage }
			if err := tx.Table("supply_supplier_shipments").Select("body").Where("tenant_id=? AND id=? AND hospital_id=? AND department_id=? AND outlet_id=?", s.TenantID, s.ShipmentID, s.HospitalID, s.DepartmentID, s.OutletID).Take(&row).Error; err != nil {
				return err
			}
			var shipment f.Shipment
			if supplierorder.DecodeObject(row.Body, &shipment) != nil || f.ValidateShipment(shipment) != nil || shipment.ID != s.ShipmentID || shipment.BuyerTenantID != s.TenantID {
				return f.ErrUnavailable
			}
			order, err := loadPurchaseOrder(tx, p.TenantID, shipment.OrderID)
			if err != nil {
				return err
			}
			if !shipmentMatchesBuyerOrder(shipment, order.Order) {
				return f.ErrUnavailable
			}
			return g.h.withBuyerEndpoint(ctx, order.Order, buyerOrderScope{hospital: s.HospitalID, department: s.DepartmentID, outlet: s.OutletID, order: shipment.OrderID}, func(ctx context.Context) error { return action(ctx, shipment, actor) })
		}, extra)
	}
	err := admit(extra)
	// Read-only reviewers hold explicit Pharma review authority instead of its use permission.
	if purpose == f.ReadReceiving && errors.Is(err, authz.ErrScopedCommandDenied) {
		return admit(PharmacyHospitalReceivingReview)
	}
	return err
}
