package supply

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"time"
)

type listedSharedReceivingResponse struct{ f.ReceivingSharedResponse }

func (v listedSharedReceivingResponse) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }

type listedReceivingResponse struct{ f.ReceivingResponseRecord }

func (v listedReceivingResponse) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func responsePageQuery(q *gorm.DB, page pagination.Params, prefix string) *gorm.DB {
	q = q.Order(prefix + "created_at DESC," + prefix + "id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("("+prefix+"created_at,"+prefix+"id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	return q
}
func sourceResponsePage(tx *gorm.DB, order supplierorder.SupplierOrder, shipment uuid.UUID, page pagination.Params) ([]listedReceivingResponse, error) {
	var rows []f.ReceivingResponseRecord
	q := receivingResponseSelect(tx).Where("r.tenant_id=? AND r.order_id=? AND r.shipment_id=?", order.OwnerTenantID, order.ID, shipment)
	if err := responsePageQuery(q, page, "r.").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]listedReceivingResponse, 0, len(rows))
	for _, record := range rows {
		if f.ValidateReceivingResponse(record) != nil || !receivingResponseMatchesOrder(f.ShareReceivingResponse(record), order) || record.ShipmentID != shipment {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedReceivingResponse{record})
	}
	return result, nil
}
func buyerResponsePage(tx *gorm.DB, order supplierorder.SupplierOrder, shipment uuid.UUID, page pagination.Params) ([]listedSharedReceivingResponse, error) {
	var rows []struct {
		ID        uuid.UUID
		Body      json.RawMessage
		CreatedAt time.Time
	}
	q := tx.Table("supply_supplier_received_responses").Where("tenant_id=? AND order_id=? AND shipment_id=?", order.OwnerTenantID, order.ID, shipment)
	if err := responsePageQuery(q, page, "").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]listedSharedReceivingResponse, 0, len(rows))
	for _, row := range rows {
		var record f.ReceivingSharedResponse
		if supplierorder.DecodeObject(row.Body, &record) != nil || f.ValidateSharedReceivingResponse(record) != nil || record.ID != row.ID || !record.CreatedAt.Equal(row.CreatedAt) || !receivingResponseMatchesOrder(record, order) || record.ShipmentID != shipment {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedSharedReceivingResponse{record})
	}
	return result, nil
}
func responseReadAudit(tx *gorm.DB, tenant, subject string, shipment uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_receiving_responses_read','supply_supplier_responses',?)`, tenant, subject, shipment).Error
}
func (h *purchaseOrderHandlers) listSourceReceivingResponses(c *gin.Context, page pagination.Params) ([]listedReceivingResponse, error) {
	p, ok := purchaseOrderPrincipal(c, true)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	scope, ok := sourcePathScope(c)
	if !ok {
		return nil, nil
	}
	scope.order, ok = purchaseOrderID(c, "order", false)
	if !ok {
		return nil, nil
	}
	shipment, ok := purchaseOrderID(c, "shipment", false)
	if !ok {
		return nil, nil
	}
	var result []listedReceivingResponse
	err := h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		order, err := loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		result, err = sourceResponsePage(tx, order.Order, shipment, page)
		if err != nil {
			return err
		}
		return responseReadAudit(tx, p.TenantID, p.Subject, shipment)
	})
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *purchaseOrderHandlers) listBuyerReceivingResponses(c *gin.Context, page pagination.Params) ([]listedSharedReceivingResponse, error) {
	p, scope, ok := buyerListScope(c)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	scope.order, ok = purchaseOrderID(c, "order", false)
	if !ok {
		return nil, nil
	}
	shipment, ok := purchaseOrderID(c, "shipment", false)
	if !ok {
		return nil, nil
	}
	var result []listedSharedReceivingResponse
	err := h.buyer(c.Request.Context(), p, scope, PermGoodsReceiptRead, func(ctx context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		order, err := loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		return h.withBuyerEndpoint(ctx, order.Order, scope, func(context.Context) error {
			var row struct{ Body json.RawMessage }
			if err := tx.Table("supply_supplier_shipments").Select("body").Where("tenant_id=? AND order_id=? AND id=?", p.TenantID, scope.order, shipment).Take(&row).Error; err != nil {
				return err
			}
			var dispatched f.Shipment
			if supplierorder.DecodeObject(row.Body, &dispatched) != nil || !shipmentMatchesBuyerOrder(dispatched, order.Order) {
				return f.ErrUnavailable
			}
			result, err = buyerResponsePage(tx, order.Order, shipment, page)
			if err != nil {
				return err
			}
			return responseReadAudit(tx, p.TenantID, p.Subject, shipment)
		})
	}, PharmacyHospitalReceivingUse)
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}
func registerReceivingResponseLists(r *platform.Router, h *purchaseOrderHandlers) {
	platform.ResourceListRoute(r, "/hospitals/:hospital/departments/:department/outlets/:outlet/purchase-orders/:order/shipments/:shipment/receiving-responses", authz.Public, 50, h.listBuyerReceivingResponses)
	for _, root := range []string{"/stock-owners/:product/organizations/:organization/locations/:location/supplier-orders", "/hospitals/:hospital/departments/:department/pharmacies/:organization/outlets/:location/supplier-orders"} {
		platform.ResourceListRoute(r, root+"/:order/shipments/:shipment/receiving/responses", authz.Public, 50, h.listSourceReceivingResponses)
		r.GET(root+"/:order/shipments/:shipment/receiving/responses/:response", authz.Public, h.getOwnReceivingResponse)
	}
}

func (h *purchaseOrderHandlers) getOwnReceivingResponse(c *gin.Context) {
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	scope, ok := sourcePathScope(c)
	if !ok {
		return
	}
	scope.order, ok = purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	shipment, ok := purchaseOrderID(c, "shipment", false)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "response", false)
	if !ok {
		return
	}
	var result f.ReceivingResponseRecord
	err := h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		order, err := loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		var rows []f.ReceivingResponseRecord
		err = receivingResponseSelect(tx).Where("r.tenant_id=? AND r.order_id=? AND r.shipment_id=? AND r.id=?", p.TenantID, scope.order, shipment, id).Limit(1).Scan(&rows).Error
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return gorm.ErrRecordNotFound
		}
		result = rows[0]
		if f.ValidateReceivingResponse(result) != nil || !receivingResponseMatchesOrder(f.ShareReceivingResponse(result), order.Order) {
			return f.ErrUnavailable
		}
		if result.ActorSubject != actor.Subject || result.ActorStaffID != actor.StaffID {
			return f.ErrConflict
		}
		return responseReadAudit(tx, p.TenantID, p.Subject, shipment)
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}

func receivingResponseSelect(tx *gorm.DB) *gorm.DB {
	return tx.Table("supply_supplier_discrepancy_responses r").Select(`r.id,r.tenant_id AS owner_tenant_id,(o.body#>>'{terms,buyer,tenant_id}')::uuid AS buyer_tenant_id,r.order_id,r.shipment_id,r.source_product,r.source_organization_id,r.source_location_id,r.summary_version,r.response,r.reason,r.actor_subject,r.actor_staff_id,r.created_at`).Joins("JOIN supply_purchase_orders o ON o.tenant_id=r.tenant_id AND o.id=r.order_id")
}
