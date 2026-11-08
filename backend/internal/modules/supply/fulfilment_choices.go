package supply

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"time"
)

func (h *warehouseFulfilment) Choices(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine, page pagination.Params) ([]supplierstock.StockChoice, error) {
	if line.Product.Owner != "supply" || line.Product.VersionID == nil {
		return nil, f.ErrInvalid
	}
	var rows []struct {
		ID        uuid.UUID
		CreatedAt time.Time
	}
	q := tx.Table("supply_stock_receipts r").Select("r.id,r.created_at").Joins(`JOIN LATERAL(SELECT id,version,document FROM supply_stock_identity_checks WHERE tenant_id=r.tenant_id AND party_id=r.party_id AND premise_id=r.premise_id AND receipt_id=r.id ORDER BY version DESC LIMIT 1)i ON true`).Joins(`JOIN LATERAL(SELECT identity_check_id,identity_version,decision,released_base_units FROM supply_stock_quality_decisions WHERE tenant_id=r.tenant_id AND party_id=r.party_id AND premise_id=r.premise_id AND receipt_id=r.id ORDER BY version DESC LIMIT 1)q ON true`).Joins(`LEFT JOIN supply_stock_fulfilment_balances u ON u.tenant_id=r.tenant_id AND u.party_id=r.party_id AND u.premise_id=r.premise_id AND u.receipt_id=r.id`).Where(`r.tenant_id=? AND r.party_id=? AND r.premise_id=? AND r.product_version_id=? AND r.expires_on>(clock_timestamp()AT TIME ZONE'Asia/Kolkata')::date AND i.document->>'status'='matched' AND i.document#>>'{source,product_id}'=? AND i.document#>>'{source,unit_of_issue}'=? AND q.decision='release' AND q.identity_check_id=i.id AND q.identity_version=i.version AND q.released_base_units>COALESCE(u.dispatched_units,0)+COALESCE((SELECT sum(reserved_units)FROM supply_supplier_holds WHERE tenant_id=r.tenant_id AND location_id=r.premise_id AND stock_id=r.id AND stock_location_id='00000000-0000-0000-0000-000000000000'::uuid AND expires_at>clock_timestamp()),0)`, s.TenantID, s.OrganizationID, s.LocationID, *line.Product.VersionID, line.Product.ProductID.String(), line.UnitCode).Order("r.created_at DESC,r.id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(r.created_at,r.id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	at, err := supplierstock.Clock(tx)
	if err != nil {
		return nil, err
	}
	result := make([]supplierstock.StockChoice, 0, len(rows))
	for _, row := range rows {
		evidence, err := warehouseSupplierEvidence(tx, f.AllocationPlan{OwnerTenantID: s.TenantID, SourceOrganizationID: s.OrganizationID, SourceLocationID: s.LocationID, Product: line.Product, UnitCode: line.UnitCode, Input: f.ReserveInput{StockID: row.ID}})
		if err != nil {
			return nil, err
		}
		if evidence.AvailableUnits < 1 {
			return nil, f.ErrConflict
		}
		result = append(result, supplierstock.StockChoice{SupplierStockChoice: f.SupplierStockChoice{ID: row.ID, OwnerTenantID: s.TenantID, OrderID: s.OrderID, LineID: line.ID, OrganizationID: s.OrganizationID, LocationID: s.LocationID, Product: line.Product, LotNumber: evidence.LotNumber, ExpiresOn: evidence.ExpiresOn, Description: evidence.Description, PackSize: evidence.PackSize, UnitCode: line.UnitCode, AvailableUnits: evidence.AvailableUnits, CreatedAt: row.CreatedAt, ObservedAt: at}})
	}
	return result, nil
}
