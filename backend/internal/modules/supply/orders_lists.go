package supply

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

type listedPurchaseOrder struct{ supplierorder.SupplierOrder }

func (o listedPurchaseOrder) PageKey() (time.Time, uuid.UUID) { return o.CreatedAt, o.ID }

func orderListQuery(c *gin.Context) bool {
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, supplierorder.ErrInvalid)
			return false
		}
	}
	return true
}

func buyerListScope(c *gin.Context) (authn.Principal, buyerOrderScope, bool) {
	p, ok := purchaseOrderPrincipal(c, true)
	var s buyerOrderScope
	if !ok {
		return p, s, false
	}
	for _, field := range []struct {
		name string
		id   *uuid.UUID
	}{{"hospital", &s.hospital}, {"department", &s.department}, {"outlet", &s.outlet}} {
		id, ok := purchaseOrderID(c, field.name, field.name == "hospital")
		if !ok {
			return p, s, false
		}
		*field.id = id
	}
	return p, s, true
}

func (h *purchaseOrderHandlers) listBuyer(c *gin.Context, page pagination.Params) ([]listedPurchaseOrder, error) {
	p, s, ok := buyerListScope(c)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	var result []listedPurchaseOrder
	err := h.buyer(c.Request.Context(), p, s, PermPurchaseOrderRead, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		query := tx.Table("supply_purchase_orders").Select("id,body,created_at").Where("tenant_id=? AND side='buyer'AND hospital_id=? AND department_id=? AND buyer_outlet_id=?", p.TenantID, s.hospital, s.department, s.outlet)
		var err error
		result, err = readOrderPage(query, page, p.TenantID)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'purchase_order_list_read','supply_department_orders',?)`, p.TenantID, p.Subject, s.department).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}

func sourcePathScope(c *gin.Context) (sellerOrderScope, bool) {
	s := sellerOrderScope{product: c.Param("product")}
	if c.Param("hospital") != "" {
		s.product = "pharmacy"
		var ok bool
		s.hospital, ok = purchaseOrderID(c, "hospital", true)
		if !ok {
			return s, false
		}
		s.department, ok = purchaseOrderID(c, "department", false)
		if !ok {
			return s, false
		}
	}
	if s.product != "supply" && s.product != "pharmacy" {
		respond.NotFound(c, "stock owner")
		return s, false
	}
	var ok bool
	s.organization, ok = purchaseOrderID(c, "organization", false)
	if !ok {
		return s, false
	}
	s.location, ok = purchaseOrderID(c, "location", false)
	return s, ok
}

func (h *purchaseOrderHandlers) sellerRequestScope(c *gin.Context, p authn.Principal, id uuid.UUID) (sellerOrderScope, error) {
	if c.Param("organization") == "" {
		return h.lookupSeller(c.Request.Context(), p, id)
	}
	s, ok := sourcePathScope(c)
	if !ok {
		return s, supplierorder.ErrInvalid
	}
	s.order = id
	return s, nil
}

func (h *purchaseOrderHandlers) listSeller(c *gin.Context, page pagination.Params) ([]listedPurchaseOrder, error) {
	p, ok := purchaseOrderPrincipal(c, true)
	if !ok || !orderListQuery(c) {
		return nil, nil
	}
	scope, ok := sourcePathScope(c)
	if !ok {
		return nil, nil
	}
	var result []listedPurchaseOrder
	err := h.seller(c.Request.Context(), p, scope, PermSupplierOrderRead, func(ctx context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		q := tx.Table("supply_purchase_orders").Select("id,body,created_at").Where("tenant_id=? AND side='seller'AND source_product=? AND source_organization_id=? AND source_location_id=?", p.TenantID, scope.product, scope.organization, scope.location)
		if scope.hospital != uuid.Nil {
			q = q.Where("body#>>'{terms,seller,hospital_id}'=?", scope.hospital.String())
		}
		var err error
		result, err = readOrderPage(q, page, p.TenantID)
		if err != nil {
			return err
		}
		if scope.hospital != uuid.Nil {
			for _, row := range result {
				if err = h.withHospitalSellerEndpoint(ctx, scope, row.SupplierOrder, func(context.Context) error { return nil }); err != nil {
					return err
				}
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_order_list_read','supply_source_orders',?)`, p.TenantID, p.Subject, scope.location).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return nil, nil
	}
	return result, nil
}

func readOrderPage(query *gorm.DB, page pagination.Params, tenant string) ([]listedPurchaseOrder, error) {
	var rows []struct {
		ID        uuid.UUID
		Body      json.RawMessage
		CreatedAt time.Time
	}
	q := query.Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]listedPurchaseOrder, 0, len(rows))
	for _, row := range rows {
		var order supplierorder.SupplierOrder
		if supplierorder.DecodeObject(row.Body, &order) != nil || supplierorder.ValidateRecord(order) != nil || order.ID != row.ID || order.OwnerTenantID.String() != tenant || !order.CreatedAt.Equal(row.CreatedAt) {
			return nil, supplierorder.ErrUnavailable
		}
		result = append(result, listedPurchaseOrder{order})
	}
	return result, nil
}

func (h *purchaseOrderHandlers) hospitalSeller(ctx context.Context, p authn.Principal, s sellerOrderScope, permission authz.Permission, action func(context.Context, *gorm.DB, supplierorder.Actor) error) error {
	if s.product != "pharmacy" || s.hospital == uuid.Nil || s.department == uuid.Nil {
		return supplierorder.ErrIneligible
	}
	if _, ok := h.endpoints.(suppliertrade.HospitalDepartmentGuard); !ok {
		return supplierorder.ErrUnavailable
	}
	return h.buyer(ctx, p, buyerOrderScope{hospital: s.hospital, department: s.department, outlet: s.location}, permission, func(ctx context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		mode := "seller_write"
		if permission == PermSupplierOrderRead {
			mode = "seller_read"
		}
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": mode, "app.supply_order_product": s.product, "app.supply_order_organization": s.organization.String(), "app.supply_order_location": s.location.String()}, func() error {
			if s.order == uuid.Nil {
				return action(ctx, tx, actor)
			}
			local, err := loadPurchaseOrder(tx, p.TenantID, s.order)
			if err != nil {
				return err
			}
			return h.withHospitalSellerEndpoint(ctx, s, local.Order, func(current context.Context) error { return action(current, tx, actor) })
		})
	})
}

func (h *purchaseOrderHandlers) withHospitalSellerEndpoint(ctx context.Context, s sellerOrderScope, o supplierorder.SupplierOrder, action func(context.Context) error) error {
	owner, ok := h.endpoints.(suppliertrade.HospitalDepartmentGuard)
	if !ok {
		return supplierorder.ErrUnavailable
	}
	e := o.Terms.Seller
	if o.Side != "seller" || e.Product != "pharmacy" || e.OrganizationID != s.organization || e.LocationID != s.location || e.HospitalID == nil || *e.HospitalID != s.hospital {
		return gorm.ErrRecordNotFound
	}
	return owner.WithHospitalDepartment(ctx, e, s.department, action)
}

func registerOrderLists(r *platform.Router, h *purchaseOrderHandlers) {
	registerShipmentLists(r, h)
	platform.ResourceListRoute(r, "/hospitals/:hospital/departments/:department/outlets/:outlet/purchase-orders", authz.Public, 50, h.listBuyer)
	source := "/stock-owners/:product/organizations/:organization/locations/:location/supplier-orders"
	platform.ResourceListRoute(r, source, authz.Public, 50, h.listSeller)
	hospital := "/hospitals/:hospital/departments/:department/pharmacies/:organization/outlets/:location/supplier-orders"
	platform.ResourceListRoute(r, hospital, authz.Public, 50, h.listSeller)
	for _, base := range []string{source, hospital} {
		r.GET(base+"/:order", authz.Public, h.getSeller)
		r.GET(base+"/:order/commands/:command", authz.Public, h.getSellerCommand)
		r.PUT(base+"/:order/commands/:command", authz.Public, h.decideSeller)
	}
}
