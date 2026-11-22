package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) listPayableCreditAllocations(c *gin.Context, params pagination.Params) ([]supplierCreditAllocationViewRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := catalogueID(c)
	if !ok || !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierCreditAllocationViewRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var bill struct{ ID string }
		if err := tx.Table("pharmacy_payables").Select("id").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&bill).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_supplier_credit_allocations").Select(supplierCreditAllocationViewColumns).Where("tenant_id=? AND outlet_id=? AND payable_id=?", p.TenantID, outlet, id).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := row.Allocation.receipt(); err != nil {
				return err
			}
		}
		if err := readRecordFault(); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'list_read','pharmacy_payable_supplier_credit_allocations',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		supplierCreditError(c, err)
		return nil, nil
	}
	return rows, nil
}

func payableCreditJournalIndexes() []tenantdb.ConcurrentIndex {
	return []tenantdb.ConcurrentIndex{{
		Name: "pharmacy_payable_credit_journal", Table: "pharmacy_supplier_credit_allocations",
		Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "outlet_id"}, {Name: "payable_id"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}},
	}}
}
