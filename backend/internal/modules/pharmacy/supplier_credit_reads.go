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

func (h *catalogueHandlers) getSupplierCreditDocument(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "credit")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditDocumentView
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var err error
		receipt, err = loadSupplierCreditView(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "supplier_credit", p.TenantID, p.Subject, id, "viewed")
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) recoverSupplierCreditDocument(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "credit")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditDocumentReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, id); err != nil {
			return err
		}
		row, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if row.ActorSubject != p.Subject {
			return gorm.ErrRecordNotFound
		}
		receipt = row.receipt()
		return auditRegisterRead(c, tx, "supplier_credit_receipt", p.TenantID, p.Subject, id, "recovery_read")
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) listSupplierCredits(c *gin.Context, params pagination.Params) ([]supplierCreditViewRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	if !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierCreditViewRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_supplier_credit_balances").Select(supplierCreditDocumentColumns+",version,state,matched_minor,allocated_minor,available_minor").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := row.view(); err != nil {
				return err
			}
		}
		return auditRegisterRead(c, tx, "supplier_credits", p.TenantID, p.Subject, outlet, "list_read")
	})
	if err != nil {
		supplierCreditError(c, err)
		return nil, nil
	}
	return rows, nil
}
