package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) getSupplierCreditAllocationContext(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	allocation, ok := supplierCreditID(c, "allocation")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result SupplierCreditAllocationContext
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		var original supplierCreditAllocationViewRow
		if err := tx.Table("pharmacy_supplier_credit_allocations").Select(supplierCreditAllocationViewColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND id=?", p.TenantID, outlet, credit, allocation).Take(&original).Error; err != nil {
			return err
		}
		receipt, err := original.Allocation.receipt()
		if err != nil {
			return err
		}
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":payable:"+receipt.PayableID.String()); err != nil {
			return err
		}
		current, err := loadSupplierCreditContext(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		var bill SupplierCreditPayableContext
		if err := tx.Table("pharmacy_payable_balances").Select(supplierCreditPayableColumns).Where("tenant_id=? AND outlet_id=? AND id=? AND payee_reference=? AND currency=?", p.TenantID, outlet, receipt.PayableID, current.PayeeReference, current.Currency).Take(&bill).Error; err != nil {
			return err
		}
		result = SupplierCreditAllocationContext{Credit: current, Allocation: SupplierCreditAllocationView{SupplierCreditAllocationReceipt: receipt, ReversalID: original.ReversalID}, Payable: bill}
		return auditRegisterRead(c, tx, "supplier_credit_allocation_context", p.TenantID, p.Subject, allocation, "viewed")
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, result)
}
