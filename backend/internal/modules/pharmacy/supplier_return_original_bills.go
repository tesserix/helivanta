package pharmacy

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type supplierReturnOriginalBillRow struct {
	SupplierReturnOriginalBill `gorm:"embedded"`
	CreatedAt                  time.Time `json:"-"`
}

func (r supplierReturnOriginalBillRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *catalogueHandlers) listSupplierReturnOriginalBills(c *gin.Context, params pagination.Params) ([]supplierReturnOriginalBillRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	if !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierReturnOriginalBillRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_payables p").Select("p.id,p.outlet_id,p.payee_name,p.document_reference,to_char(p.issued_on,'YYYY-MM-DD') AS issued_on,p.currency,p.created_at").Where("p.tenant_id=? AND p.outlet_id=?", p.TenantID, outlet).
			Where("EXISTS(SELECT 1 FROM pharmacy_payable_lines l WHERE l.tenant_id=p.tenant_id AND l.outlet_id=p.outlet_id AND l.payable_id=p.id AND l.category='inventory')")
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if err := auditRegisterRead(c, tx, "supplier_return_original_bills", p.TenantID, p.Subject, outlet, "list_read"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return nil, nil
	}
	return rows, nil
}
