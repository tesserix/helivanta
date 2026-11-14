package pharmacy

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"time"
)

func supplierChoiceProduct(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine) (catalogueProduct, error) {
	var product catalogueProduct
	if line.Product.Owner != "pharmacy" || line.Product.Version == nil {
		return product, f.ErrInvalid
	}
	err := tx.Where("tenant_id=? AND outlet_id=? AND id=? AND version=? AND status='active' AND unit_of_issue=?", s.TenantID, s.LocationID, line.Product.ProductID, *line.Product.Version, line.UnitCode).Take(&product).Error
	return product, err
}
func (h *outletFulfilment) Choices(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine, page pagination.Params) ([]supplierstock.StockChoice, error) {
	product, err := supplierChoiceProduct(tx, s, line)
	if err != nil {
		return nil, err
	}
	description, err := supplierProductDescription(product)
	if err != nil {
		return nil, err
	}
	at, err := supplierstock.Clock(tx)
	if err != nil {
		return nil, err
	}
	today := at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)
	var rows []struct {
		ID                   uuid.UUID
		CreatedAt            time.Time
		LotNumber, ExpiresOn string
		AvailableUnits       int64
	}
	q := tx.Table("pharmacy_stock_batches b").Select("b.id,b.created_at,b.lot_number,to_char(b.expires_on,'YYYY-MM-DD')AS expires_on,v.available_units").Joins(`JOIN LATERAL(SELECT sum(x.on_hand)::bigint AS available_units FROM pharmacy_sellable_buckets(b.tenant_id,b.outlet_id,b.product_id,?::date)x WHERE x.batch_id=b.id)v ON v.available_units>0`, today).Where("b.tenant_id=? AND b.outlet_id=? AND b.product_id=?", s.TenantID, s.LocationID, product.ID).Order("b.created_at DESC,b.id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(b.created_at,b.id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err = q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]supplierstock.StockChoice, 0, len(rows))
	for _, row := range rows {
		result = append(result, supplierstock.StockChoice{SupplierStockChoice: f.SupplierStockChoice{ID: row.ID, OwnerTenantID: s.TenantID, OrderID: s.OrderID, LineID: line.ID, OrganizationID: s.OrganizationID, LocationID: s.LocationID, Product: line.Product, LotNumber: row.LotNumber, ExpiresOn: row.ExpiresOn, Description: description, PackSize: int64(product.PackSize), UnitCode: line.UnitCode, AvailableUnits: row.AvailableUnits, CreatedAt: row.CreatedAt, ObservedAt: at}})
	}
	return result, nil
}
func (h *outletFulfilment) Locations(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine, stock uuid.UUID, page pagination.Params) ([]supplierstock.StockLocationChoice, error) {
	product, err := supplierChoiceProduct(tx, s, line)
	if err != nil {
		return nil, err
	}
	var batch stockBatch
	if err = tx.Where("tenant_id=? AND outlet_id=? AND product_id=? AND id=?", s.TenantID, s.LocationID, product.ID, stock).Take(&batch).Error; err != nil {
		return nil, err
	}
	at, err := supplierstock.Clock(tx)
	if err != nil {
		return nil, err
	}
	today := at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)
	var rows []struct {
		ID             uuid.UUID
		CreatedAt      time.Time
		Code, Name     string
		AvailableUnits int64
	}
	q := tx.Table("pharmacy_stock_locations l").Select("l.id,l.created_at,l.code,l.name,b.on_hand AS available_units").Joins(`JOIN pharmacy_sellable_buckets(?,?,?,?::date)b ON b.location_id=l.id AND b.batch_id=?`, s.TenantID, s.LocationID, product.ID, today, stock).Where("l.tenant_id=? AND l.outlet_id=? AND b.on_hand>0", s.TenantID, s.LocationID).Order("l.created_at DESC,l.id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(l.created_at,l.id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err = q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]supplierstock.StockLocationChoice, 0, len(rows))
	for _, row := range rows {
		result = append(result, supplierstock.StockLocationChoice{SupplierStockLocationChoice: f.SupplierStockLocationChoice{ID: row.ID, StockID: stock, OwnerTenantID: s.TenantID, OrderID: s.OrderID, LineID: line.ID, OrganizationID: s.OrganizationID, LocationID: s.LocationID, Code: row.Code, Name: row.Name, AvailableUnits: row.AvailableUnits, CreatedAt: row.CreatedAt, ObservedAt: at}})
	}
	return result, nil
}
