package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) listSupplierCreditAllocations(c *gin.Context, params pagination.Params) ([]supplierCreditAllocationViewRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok || !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierCreditAllocationViewRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_supplier_credit_allocations").Select(supplierCreditAllocationViewColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=?", p.TenantID, outlet, credit).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := row.Allocation.receipt(); err != nil {
				return err
			}
		}
		return auditRegisterRead(c, tx, "supplier_credit_allocations", p.TenantID, p.Subject, credit, "list_read")
	})
	if err != nil {
		supplierCreditError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *catalogueHandlers) getSupplierCreditAllocation(c *gin.Context) {
	h.readSupplierCreditAllocation(c, false)
}
func (h *catalogueHandlers) recoverSupplierCreditAllocation(c *gin.Context) {
	h.readSupplierCreditAllocation(c, true)
}
func (h *catalogueHandlers) readSupplierCreditAllocation(c *gin.Context, recovery bool) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "allocation")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditAllocationReceipt
	var view supplierCreditAllocationViewRow
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if recovery {
			if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
				return err
			}
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if err := tx.Table("pharmacy_supplier_credit_allocations").Select(supplierCreditAllocationViewColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND id=?", p.TenantID, outlet, credit, id).Take(&view).Error; err != nil {
			return err
		}
		if recovery && view.Allocation.ActorSubject != p.Subject {
			return gorm.ErrRecordNotFound
		}
		var err error
		receipt, err = view.Allocation.receipt()
		if err != nil {
			return err
		}
		action := "viewed"
		if recovery {
			action = "recovery_read"
		}
		return auditRegisterRead(c, tx, "supplier_credit_allocation", p.TenantID, p.Subject, id, action)
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	if recovery {
		respond.OK(c, receipt)
	} else {
		respond.OK(c, view)
	}
}
