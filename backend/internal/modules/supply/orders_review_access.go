package supply

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

func (h *purchaseOrderHandlers) sourceReviewAccess(c *gin.Context) {
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
	var result supplierorder.SupplierOrderReviewAccess
	err := h.seller(c.Request.Context(), p, scope, PermSupplierOrderAccept, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		if _, err := loadPurchaseOrder(tx, p.TenantID, scope.order); err != nil {
			return err
		}
		result = supplierorder.SupplierOrderReviewAccess{OwnerTenantID: uuid.MustParse(p.TenantID), OrderID: scope.order, OrganizationID: scope.organization, LocationID: scope.location, Product: scope.product, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID}
		if scope.hospital != uuid.Nil {
			result.HospitalID = &scope.hospital
			result.DepartmentID = &scope.department
		}
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&result.ObservedAt).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_order_review_access_read','supply_purchase_orders',?)`, p.TenantID, p.Subject, scope.order).Error
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	respond.OK(c, result)
}
