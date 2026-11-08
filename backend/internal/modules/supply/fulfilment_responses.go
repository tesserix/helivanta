package supply

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
	"time"
)

const PermReceivingResponse authz.Permission = "supply.receiving_response.record"

type discrepancyInput = f.ReceivingResponseInput

func supplierResponseRoutes(r *platform.Router, deps platform.Deps) {
	h := &purchaseOrderHandlers{db: deps.DB, bus: deps.Bus, authority: deps.ScopedAuthorizer, actors: deps.PurchaseOrderActors, departments: deps.DepartmentScopes, operating: deps.OperatingAuthorizer, endpoints: deps.TradingEndpoints}
	registerReceivingResponseLists(r, h)
	base := "/supplier-orders/:order/shipments/:shipment/receiving"
	r.GET(base, authz.Public, h.getSupplierReceiving)
	r.PUT(base+"/responses/:response", authz.Public, h.respondReceiving)
	for _, root := range []string{"/stock-owners/:product/organizations/:organization/locations/:location/supplier-orders", "/hospitals/:hospital/departments/:department/pharmacies/:organization/outlets/:location/supplier-orders"} {
		path := root + "/:order/shipments/:shipment/receiving"
		r.GET(path, authz.Public, h.getSupplierReceiving)
		r.PUT(path+"/responses/:response", authz.Public, h.respondReceiving)
	}
}
func (h *purchaseOrderHandlers) getSupplierReceiving(c *gin.Context) {
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	shipment, ok := purchaseOrderID(c, "shipment", false)
	if !ok {
		return
	}
	scope, err := h.sellerRequestScope(c, p, id)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	var result f.ReceivingSummary
	err = h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		var row struct{ Body json.RawMessage }
		if err := tx.Table("supply_supplier_receiving_updates").Select("body").Where("tenant_id=? AND order_id=? AND shipment_id=?", p.TenantID, id, shipment).Order("version DESC").Take(&row).Error; err != nil {
			return err
		}
		return supplierorder.DecodeObject(row.Body, &result)
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *purchaseOrderHandlers) respondReceiving(c *gin.Context) {
	if h.bus == nil {
		purchaseOrderError(c, supplierorder.ErrUnavailable)
		return
	}
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	shipment, ok := purchaseOrderID(c, "shipment", false)
	if !ok {
		return
	}
	response, ok := purchaseOrderID(c, "response", false)
	if !ok {
		return
	}
	var in discrepancyInput
	if !decodePurchaseOrder(c, &in) {
		return
	}
	if in.ExpectedSummaryVersion < 1 || (in.Response != "confirmed" && in.Response != "disputed") || !suppliertrade.Text(in.Reason, 500) {
		purchaseOrderError(c, supplierorder.ErrInvalid)
		return
	}
	scope, err := h.sellerRequestScope(c, p, id)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	err = h.seller(c.Request.Context(), p, scope, PermReceivingResponse, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		if err := purchaseOrderLock(tx, p.TenantID, id); err != nil {
			return err
		}
		var old []struct {
			ShipmentID                     uuid.UUID
			SummaryVersion                 int64
			Response, Reason, ActorSubject string
			ActorStaffID                   uuid.UUID
		}
		if err := tx.Table("supply_supplier_discrepancy_responses").Where("tenant_id=? AND order_id=? AND id=?", p.TenantID, id, response).Scan(&old).Error; err != nil {
			return err
		}
		if len(old) == 1 {
			v := old[0]
			if v.ShipmentID != shipment || v.SummaryVersion != in.ExpectedSummaryVersion || v.Response != in.Response || v.Reason != in.Reason || v.ActorSubject != actor.Subject || v.ActorStaffID != actor.StaffID {
				return f.ErrConflict
			}
			return nil
		}
		var update struct {
			Version int64
			Body    json.RawMessage
		}
		if err := tx.Table("supply_supplier_receiving_updates").Select("version,body").Where("tenant_id=? AND order_id=? AND shipment_id=?", p.TenantID, id, shipment).Order("version DESC").Take(&update).Error; err != nil {
			return err
		}
		var summary f.ReceivingSummary
		if supplierorder.DecodeObject(update.Body, &summary) != nil {
			return f.ErrUnavailable
		}
		if !summary.Closed || update.Version != in.ExpectedSummaryVersion {
			return f.ErrConflict
		}
		var stored struct {
			CreatedAt     time.Time
			SourceEventID uuid.UUID
		}
		if err := tx.Raw(`INSERT INTO supply_supplier_discrepancy_responses(tenant_id,id,order_id,shipment_id,summary_version,source_product,source_organization_id,source_location_id,response,reason,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,clock_timestamp())RETURNING created_at,source_event_id`, p.TenantID, response, id, shipment, in.ExpectedSummaryVersion, scope.product, scope.organization, scope.location, in.Response, in.Reason, actor.Subject, actor.StaffID).Scan(&stored).Error; err != nil {
			return err
		}
		record := f.ReceivingResponseRecord{ID: response, OwnerTenantID: uuid.MustParse(p.TenantID), BuyerTenantID: summary.OwnerTenantID, OrderID: id, ShipmentID: shipment, SourceProduct: scope.product, SourceOrganizationID: scope.organization, SourceLocationID: scope.location, SummaryVersion: in.ExpectedSummaryVersion, Response: in.Response, Reason: in.Reason, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, CreatedAt: stored.CreatedAt}
		if f.ValidateReceivingResponse(record) != nil {
			return f.ErrInvalid
		}
		body, err := json.Marshal(f.ShareReceivingResponse(record))
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, supplycontract.SubjectSupplierReceivingResponseExchange, events.Event{ID: stored.SourceEventID.String(), Type: supplycontract.SubjectSupplierReceivingResponseExchange, Version: 1, OccurredAt: record.CreatedAt, TenantID: p.TenantID, DestinationTenantID: record.BuyerTenantID.String(), Data: body})
	})
	if err != nil {
		if errors.Is(err, f.ErrConflict) {
			purchaseOrderError(c, supplierorder.ErrConflict)
		} else {
			purchaseOrderError(c, err)
		}
		return
	}
	respond.OK(c, gin.H{"id": response, "shipment_id": shipment, "summary_version": in.ExpectedSummaryVersion, "response": in.Response, "financial_effect": "none"})
}
