package supply

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

type listedSupplierShipment struct{ f.Shipment }

func (s listedSupplierShipment) PageKey() (time.Time, uuid.UUID) { return s.DispatchedAt, s.ID }
func shipmentPage(q *gorm.DB, page pagination.Params) ([]listedSupplierShipment, error) {
	var rows []struct {
		ID        uuid.UUID
		Body      json.RawMessage
		CreatedAt time.Time
	}
	q = q.Select("id,body,created_at").Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]listedSupplierShipment, 0, len(rows))
	for _, row := range rows {
		var shipment f.Shipment
		if supplierorder.DecodeObject(row.Body, &shipment) != nil || f.ValidateShipment(shipment) != nil || shipment.ID != row.ID || !shipment.DispatchedAt.Equal(row.CreatedAt) {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedSupplierShipment{shipment})
	}
	return result, nil
}
func (h *purchaseOrderHandlers) listBuyerShipments(c *gin.Context, page pagination.Params) ([]listedSupplierShipment, error) {
	p, s, ok := buyerListScope(c)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	var result []listedSupplierShipment
	err := h.buyer(c.Request.Context(), p, s, PermGoodsReceiptRead, func(ctx context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		var err error
		result, err = shipmentPage(tx.Table("supply_supplier_shipments").Where("tenant_id=? AND hospital_id=? AND department_id=? AND outlet_id=?", p.TenantID, s.hospital, s.department, s.outlet), page)
		if err != nil {
			return err
		}
		for _, row := range result {
			if row.BuyerTenantID.String() != p.TenantID || row.HospitalID != s.hospital || row.BuyerLocationID != s.outlet {
				return f.ErrUnavailable
			}
			order, err := loadPurchaseOrder(tx, p.TenantID, row.OrderID)
			if err != nil {
				return err
			}
			if !shipmentMatchesBuyerOrder(row.Shipment, order.Order) {
				return f.ErrUnavailable
			}
			if err = h.withBuyerEndpoint(ctx, order.Order, s, func(context.Context) error { return nil }); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_shipments_list_read','supply_supplier_shipments',?)`, p.TenantID, p.Subject, s.department).Error
	}, PharmacyHospitalReceivingUse)
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *purchaseOrderHandlers) listSourceShipments(c *gin.Context, page pagination.Params) ([]listedSupplierShipment, error) {
	p, ok := purchaseOrderPrincipal(c, true)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	s, ok := sourcePathScope(c)
	if !ok {
		return nil, nil
	}
	s.order, ok = purchaseOrderID(c, "order", false)
	if !ok {
		return nil, nil
	}
	var result []listedSupplierShipment
	err := h.seller(c.Request.Context(), p, s, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		order, err := loadPurchaseOrder(tx, p.TenantID, s.order)
		if err != nil {
			return err
		}
		result, err = shipmentPage(tx.Table("supply_supplier_dispatched_shipments").Where("tenant_id=? AND order_id=?", p.TenantID, s.order), page)
		if err != nil {
			return err
		}
		buyer := order.Order
		buyer.Side = "buyer"
		buyer.OwnerTenantID = buyer.Terms.Buyer.TenantID
		for _, row := range result {
			if !shipmentMatchesBuyerOrder(row.Shipment, buyer) {
				return f.ErrUnavailable
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_shipments_list_read','supply_supplier_dispatched_shipments',?)`, p.TenantID, p.Subject, s.order).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}
func registerShipmentLists(r *platform.Router, h *purchaseOrderHandlers) {
	platform.ResourceListRoute(r, "/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments", authz.Public, 50, h.listBuyerShipments)
	for _, base := range []string{"/stock-owners/:product/organizations/:organization/locations/:location/supplier-orders", "/hospitals/:hospital/departments/:department/pharmacies/:organization/outlets/:location/supplier-orders"} {
		platform.ResourceListRoute(r, base+"/:order/shipments", authz.Public, 50, h.listSourceShipments)
	}
}
