package pharmacy

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"strings"
	"time"
)

type receivingProductChoice struct{ f.ReceivingProductChoice }

func (v receivingProductChoice) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func receivingProductChoices(tx *gorm.DB, s f.Shipment, page pagination.Params) ([]receivingProductChoice, error) {
	var products []catalogueProduct
	q := tx.Select("id,version,brand_name,generic_name,manufacturer,dosage_form,route,ingredients,currency,unit_of_issue,pack_size,created_at").Where("tenant_id=? AND outlet_id=? AND status='active' AND unit_of_issue=? AND pack_size=?", s.BuyerTenantID, s.BuyerLocationID, s.UnitCode, s.PackSize).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Find(&products).Error; err != nil {
		return nil, err
	}
	result := make([]receivingProductChoice, 0, len(products))
	for _, product := range products {
		ingredients := make([]string, 0, len(product.Ingredients))
		for _, ingredient := range product.Ingredients {
			ingredients = append(ingredients, ingredient.Name+" "+ingredient.Strength+" "+ingredient.Unit)
		}
		description := strings.Join(ingredients, "; ") + " · " + product.DosageForm + " · " + product.Route

		result = append(result, receivingProductChoice{f.ReceivingProductChoice{ID: product.ID, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, PharmacyID: s.BuyerOrganizationID, OutletID: s.BuyerLocationID, Version: product.Version, BrandName: product.BrandName, GenericName: product.GenericName, Manufacturer: product.Manufacturer, DosageForm: product.DosageForm, IdentityDescription: description, Currency: product.Currency, UnitCode: product.UnitOfIssue, PackSize: int64(product.PackSize), CreatedAt: product.CreatedAt}})
	}
	return result, nil
}
func (h *supplierReceiving) productChoices(c *gin.Context, page pagination.Params) ([]receivingProductChoice, error) {
	p, scope, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	var result []receivingProductChoice
	err := h.with(c, p, scope, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = receivingProductChoices(tx, shipment, page)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, scope, actor, "pharmacy_receiving_product_choices")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *supplierReceiving) registerChoices(r *platform.Router, base string) {
	platform.ResourceListRoute(r, base+"/receiving/product-choices", authz.Public, 50, h.productChoices)
	platform.ResourceListRoute(r, base+"/receiving/product-choices/:product/batches", authz.Public, 50, h.batchChoices)
	platform.ResourceListRoute(r, base+"/receiving/quarantine-locations", authz.Public, 50, h.quarantineLocations)
	platform.ResourceListRoute(r, base+"/receiving/sellable-locations", authz.Public, 50, h.sellableLocations)
}

type receivingLocationChoice struct{ f.ReceivingLocationChoice }

func (v receivingLocationChoice) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func receivingLocationChoices(tx *gorm.DB, shipment f.Shipment, page pagination.Params, status string) ([]receivingLocationChoice, error) {
	var locations []stockLocation
	q := tx.Select("id,code,name,stock_status,created_at").Where("tenant_id=? AND outlet_id=? AND stock_status=?", shipment.BuyerTenantID, shipment.BuyerLocationID, status).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Find(&locations).Error; err != nil {
		return nil, err
	}
	result := make([]receivingLocationChoice, 0, len(locations))
	for _, location := range locations {
		result = append(result, receivingLocationChoice{f.ReceivingLocationChoice{ID: location.ID, OwnerTenantID: shipment.BuyerTenantID, ShipmentID: shipment.ID, PharmacyID: shipment.BuyerOrganizationID, OutletID: shipment.BuyerLocationID, Code: location.Code, Name: location.Name, StockStatus: location.StockStatus, CreatedAt: location.CreatedAt}})
	}
	return result, nil
}
func (h *supplierReceiving) quarantineLocations(c *gin.Context, page pagination.Params) ([]receivingLocationChoice, error) {
	return h.locationChoices(c, page, "quarantine")
}
func (h *supplierReceiving) sellableLocations(c *gin.Context, page pagination.Params) ([]receivingLocationChoice, error) {
	return h.locationChoices(c, page, "sellable")
}
func (h *supplierReceiving) locationChoices(c *gin.Context, page pagination.Params, status string) ([]receivingLocationChoice, error) {
	p, scope, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	var result []receivingLocationChoice
	err := h.with(c, p, scope, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = receivingLocationChoices(tx, shipment, page, status)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, scope, actor, "pharmacy_receiving_"+status+"_locations")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}

type receivingBatchChoice struct{ f.ReceivingBatchChoice }

func (v receivingBatchChoice) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func receivingBatchChoices(tx *gorm.DB, s f.Shipment, product uuid.UUID, page pagination.Params) ([]receivingBatchChoice, error) {
	var rows []struct {
		ID             uuid.UUID
		ProductID      uuid.UUID
		ProductVersion int64
		LotNumber      string
		ExpiresOn      string
		MrpMinor       *int64
		CreatedAt      time.Time
	}
	q := tx.Table("pharmacy_stock_batches b").Select("b.id,b.product_id,b.product_version,b.lot_number,b.expires_on::text,b.mrp_minor,b.created_at").Joins("JOIN pharmacy_catalogue_products p ON p.tenant_id=b.tenant_id AND p.outlet_id=b.outlet_id AND p.id=b.product_id").Where("b.tenant_id=? AND b.outlet_id=? AND b.product_id=? AND b.lot_number=? AND p.status='active' AND p.unit_of_issue=? AND p.pack_size=?", s.BuyerTenantID, s.BuyerLocationID, product, s.LotNumber, s.UnitCode, s.PackSize).Order("b.created_at DESC,b.id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(b.created_at,b.id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]receivingBatchChoice, 0, len(rows))
	for _, v := range rows {
		result = append(result, receivingBatchChoice{f.ReceivingBatchChoice{ID: v.ID, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, PharmacyID: s.BuyerOrganizationID, OutletID: s.BuyerLocationID, ProductID: v.ProductID, ProductVersion: v.ProductVersion, LotNumber: v.LotNumber, ExpiresOn: v.ExpiresOn, MrpMinor: v.MrpMinor, CreatedAt: v.CreatedAt}})
	}
	return result, nil
}
func (h *supplierReceiving) batchChoices(c *gin.Context, page pagination.Params) ([]receivingBatchChoice, error) {
	p, scope, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	product, ok := receivingID(c, "product", false)
	if !ok {
		return nil, nil
	}
	var result []receivingBatchChoice
	err := h.with(c, p, scope, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = receivingBatchChoices(tx, shipment, product, page)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, scope, actor, "pharmacy_receiving_batch_choices")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}
