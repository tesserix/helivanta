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

type supplierReturnSummaryRow struct {
	SupplierReturnSummary `gorm:"embedded"`
	CreatedAt             time.Time `json:"-"`
}

func (supplierReturnSummaryRow) TableName() string                 { return "pharmacy_supplier_returns" }
func (r supplierReturnSummaryRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *catalogueHandlers) listSupplierReturns(c *gin.Context, params pagination.Params) ([]supplierReturnSummaryRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	if !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierReturnSummaryRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		const columns = `id,outlet_id,number,payee_name,document_reference,currency,expected_credit_minor,line_count,created_at,
   to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS prepared_at,
   CASE WHEN EXISTS(SELECT 1 FROM pharmacy_supplier_return_cancellations c WHERE c.tenant_id=pharmacy_supplier_returns.tenant_id AND c.outlet_id=pharmacy_supplier_returns.outlet_id AND c.return_id=pharmacy_supplier_returns.id)THEN 'cancelled' WHEN EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatches d WHERE d.tenant_id=pharmacy_supplier_returns.tenant_id AND d.outlet_id=pharmacy_supplier_returns.outlet_id AND d.return_id=pharmacy_supplier_returns.id)THEN 'dispatched' ELSE 'draft' END state,
   CASE WHEN EXISTS(SELECT 1 FROM pharmacy_supplier_return_cancellations c WHERE c.tenant_id=pharmacy_supplier_returns.tenant_id AND c.outlet_id=pharmacy_supplier_returns.outlet_id AND c.return_id=pharmacy_supplier_returns.id)THEN 1 WHEN EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatches d WHERE d.tenant_id=pharmacy_supplier_returns.tenant_id AND d.outlet_id=pharmacy_supplier_returns.outlet_id AND d.return_id=pharmacy_supplier_returns.id)THEN 1 ELSE 0 END version`
		if err := tenantdb.ApplyKeyset(tx, params).Select(columns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error; err != nil {
			return err
		}
		if err := auditRegisterRead(c, tx, "supplier_returns", p.TenantID, p.Subject, outlet, "list_read"); err != nil {
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
