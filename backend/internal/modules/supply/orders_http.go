package supply

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

func purchaseOrderRoutes(r *platform.Router, deps platform.Deps) {
	h := &purchaseOrderHandlers{db: deps.DB, bus: deps.Bus, authority: deps.ScopedAuthorizer, actors: deps.PurchaseOrderActors, departments: deps.DepartmentScopes, operating: deps.OperatingAuthorizer, relationships: deps.TradingRelationships, endpoints: deps.TradingEndpoints, products: deps.OrderProducts}
	buyer := "/hospitals/:hospital/departments/:department/outlets/:outlet/purchase-orders/:order"
	r.PUT(buyer, authz.Public, h.prepareBuyer)
	r.GET(buyer, authz.Public, h.getBuyer)
	r.GET(buyer+"/proposal-receipt", authz.Public, h.getBuyerProposal)
	r.PUT(buyer+"/commands/:command", authz.Public, h.decideBuyer)
	r.GET(buyer+"/commands/:command", authz.Public, h.getBuyerCommand)
	source := "/supplier-orders/:order"
	r.GET(source, authz.Public, h.getSeller)
	r.PUT(source+"/commands/:command", authz.Public, h.decideSeller)
	r.GET(source+"/commands/:command", authz.Public, h.getSellerCommand)
}
func purchaseOrderPrincipal(c *gin.Context) (authn.Principal, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, false
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, false
	}
	for key, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(key)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "context_changed", "Your workspace or account changed. Reopen the purchase order.")
			return p, false
		}
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, supplierorder.ErrInvalid)
		return p, false
	}
	return p, true
}
func purchaseOrderID(c *gin.Context, key string, hospital bool) (uuid.UUID, bool) {
	raw := c.Param(key)
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw || id.Variant() != uuid.RFC4122 || (!hospital && id.Version() != 7) {
		respond.NotFound(c, "purchase order")
		return uuid.Nil, false
	}
	return id, true
}
func buyerPurchaseOrderRequest(c *gin.Context) (authn.Principal, buyerOrderScope, bool) {
	p, ok := purchaseOrderPrincipal(c)
	var scope buyerOrderScope
	if !ok {
		return p, scope, false
	}
	for _, item := range []struct {
		name string
		id   *uuid.UUID
	}{{"hospital", &scope.hospital}, {"department", &scope.department}, {"outlet", &scope.outlet}, {"order", &scope.order}, {"command", &scope.command}} {
		if item.name == "command" && c.Param(item.name) == "" {
			continue
		}
		id, valid := purchaseOrderID(c, item.name, item.name == "hospital")
		if !valid {
			return p, scope, false
		}
		*item.id = id
	}
	return p, scope, true
}
func decodePurchaseOrder(c *gin.Context, into any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || suppliertrade.DecodeObject(raw, into) != nil {
		respond.BadRequest(c, supplierorder.ErrInvalid)
		return false
	}
	return true
}
func purchaseOrderError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credential_revoked", "Sign in again before accessing the purchase order.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, supplierorder.ErrIneligible), errors.Is(err, suppliertrade.ErrIneligible), errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, authz.ErrDepartmentReferenceChanged):
		respond.NotFound(c, "purchase order")
	case errors.Is(err, supplierorder.ErrConflict), errors.Is(err, suppliertrade.ErrConflict):
		respond.Error(c, 409, "order_changed", "The order, actor or current source changed. Refresh the order and recover the original command.")
	case errors.Is(err, supplierorder.ErrInvalid), errors.Is(err, suppliertrade.ErrInvalid):
		respond.BadRequest(c, supplierorder.ErrInvalid)
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Error(c, 403, "order_forbidden", "Current Staff and purchase-order authority are required.")
	case errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514"):
		respond.Error(c, 409, "order_changed", "The document number, command identity or current order version conflicts.")
	default:
		respond.Error(c, 503, "order_unavailable", "The current order authority or source could not be verified. No new commitment was confirmed.")
	}
}
func (h *purchaseOrderHandlers) getBuyer(c *gin.Context)         { h.readBuyer(c, false) }
func (h *purchaseOrderHandlers) getBuyerProposal(c *gin.Context) { h.readBuyer(c, true) }
func (h *purchaseOrderHandlers) readBuyer(c *gin.Context, proposal bool) {
	p, scope, ok := buyerPurchaseOrderRequest(c)
	if !ok {
		return
	}
	var result supplierorder.SupplierOrder
	err := h.buyer(c.Request.Context(), p, scope, PermPurchaseOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		local, err := loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		result = local.Order
		if proposal {
			if suppliertrade.DecodeObject(local.ProposalReceipt, &result) != nil || result.PreparedBy != p.Subject {
				return gorm.ErrRecordNotFound
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'purchase_order_read','supply_purchase_orders',?)`, p.TenantID, p.Subject, scope.order).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *purchaseOrderHandlers) getBuyerCommand(c *gin.Context) {
	p, scope, ok := buyerPurchaseOrderRequest(c)
	if !ok {
		return
	}
	var result supplierorder.SupplierOrderCommandReceipt
	err := h.buyer(c.Request.Context(), p, scope, PermPurchaseOrderRead, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		var err error
		result, err = recoverPurchaseOrderCommand(tx, p.TenantID, scope.order, scope.command, actor, nil)
		return err
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *purchaseOrderHandlers) getSeller(c *gin.Context) {
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	scope, err := h.lookupSeller(c.Request.Context(), p, id)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	var result supplierorder.SupplierOrder
	err = h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		local, err := loadPurchaseOrder(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		result = local.Order
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_order_read','supply_purchase_orders',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *purchaseOrderHandlers) getSellerCommand(c *gin.Context) {
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	command, ok := purchaseOrderID(c, "command", false)
	if !ok {
		return
	}
	scope, err := h.lookupSeller(c.Request.Context(), p, id)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	var result supplierorder.SupplierOrderCommandReceipt
	err = h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		var err error
		result, err = recoverPurchaseOrderCommand(tx, p.TenantID, id, command, actor, nil)
		return err
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
