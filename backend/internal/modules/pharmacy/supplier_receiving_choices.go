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
	"time"
)

type receivingProductChoice struct{ f.ReceivingProductChoice }

func (v receivingProductChoice) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func receivingProductChoices(tx *gorm.DB, s f.Shipment, page pagination.Params) ([]receivingProductChoice, error) {
	var products []catalogueProduct
	q := tx.Select("id,version,brand_name,generic_name,manufacturer,dosage_form,unit_of_issue,pack_size,created_at").Where("tenant_id=? AND outlet_id=? AND status='active' AND unit_of_issue=? AND pack_size=?", s.BuyerTenantID, s.BuyerLocationID, s.UnitCode, s.PackSize).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Find(&products).Error; err != nil {
		return nil, err
	}
	result := make([]receivingProductChoice, 0, len(products))
	for _, product := range products {
		result = append(result, receivingProductChoice{f.ReceivingProductChoice{ID: product.ID, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, PharmacyID: s.BuyerOrganizationID, OutletID: s.BuyerLocationID, Version: product.Version, BrandName: product.BrandName, GenericName: product.GenericName, Manufacturer: product.Manufacturer, DosageForm: product.DosageForm, UnitCode: product.UnitOfIssue, PackSize: int64(product.PackSize), CreatedAt: product.CreatedAt}})
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
