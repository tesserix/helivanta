package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) getSupplierCreditMatchContext(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	match, ok := supplierCreditID(c, "match")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result SupplierCreditMatchContext
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pharmacy_supplier_credit_lock(?,?,?)", p.TenantID, outlet, credit).Error; err != nil {
			return err
		}
		doc, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		original, err := loadSupplierCreditMatch(tx, p.TenantID, outlet, credit, match)
		if err != nil {
			return err
		}
		if err := tx.Exec("SELECT pharmacy_supplier_credit_return_lock(?,?,?)", p.TenantID, outlet, original.Fields.ReturnID).Error; err != nil {
			return err
		}
		result.Credit, err = loadSupplierCreditContext(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		if err := tx.Table("pharmacy_supplier_returns").Select("id,outlet_id,number,payee_name,document_reference AS original_bill_reference,currency,expected_credit_minor,pharmacy_supplier_return_matched(tenant_id,outlet_id,id) AS matched_minor").Where("tenant_id=? AND outlet_id=? AND id=? AND payee_reference=? AND currency=?", p.TenantID, outlet, original.Fields.ReturnID, doc.PayeeReference, doc.Fields.Currency).Take(&result.Returned).Error; err != nil {
			return err
		}
		var reversal supplierCreditMatchReversalRow
		if err := tx.Table("pharmacy_supplier_credit_match_reversals").Select("id").Where("tenant_id=? AND outlet_id=? AND credit_id=? AND match_id=?", p.TenantID, outlet, credit, match).Find(&reversal).Error; err != nil {
			return err
		}
		receipt, err := original.receipt()
		if err != nil {
			return err
		}
		result.Match = SupplierCreditMatchView{SupplierCreditMatchReceipt: receipt}
		if reversal.ID != uuid.Nil {
			result.Match.ReversalID = &reversal.ID
		}
		return auditRegisterRead(c, tx, "supplier_credit_match_context", p.TenantID, p.Subject, match, "viewed")
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, result)
}
