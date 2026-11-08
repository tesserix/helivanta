package pharmacy

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// receivingProduct is the catalogue identity a receiver needs to name a lot; prices, tax and regulatory fields stay with editors.
type receivingProduct struct {
	ID          uuid.UUID    `json:"id"`
	TenantID    uuid.UUID    `json:"-"`
	OutletID    uuid.UUID    `json:"outlet_id"`
	SKU         string       `json:"sku"`
	BrandName   string       `json:"brand_name"`
	GenericName string       `json:"generic_name"`
	DosageForm  string       `json:"dosage_form"`
	Ingredients []ingredient `json:"ingredients" gorm:"serializer:json;type:jsonb"`
	PackSize    int          `json:"pack_size"`
	UnitOfIssue string       `json:"unit_of_issue"`
	Currency    string       `json:"currency"`
	Version     int64        `json:"version"`
	CreatedAt   time.Time    `json:"created_at"`
}

func (receivingProduct) TableName() string                 { return "pharmacy_catalogue_products" }
func (r receivingProduct) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const receivingProductColumns = "id,tenant_id,outlet_id,sku,brand_name,generic_name,dosage_form,ingredients,pack_size,unit_of_issue,currency,version,created_at"

// operatingStockRoutes are public at the router: the reviewed duty, not a tenant role, admits each request (ADR-0064).
func operatingStockRoutes(g *platform.Router, h *catalogueHandlers) {
	s := g.Group("/pharmacies/:pharmacy/operating-locations/:outlet/stock")
	platform.ListRoute(s, "/products", authz.Public, h.listReceivingProducts)
	platform.ListRoute(s, "/locations", authz.Public, h.listOperatingLocations)
	platform.ListRoute(s, "/batches", authz.Public, h.listOperatingBatches)
	s.GET("/batches/:id", authz.Public, h.getOperatingBatch)
	s.PUT("/batches/:id", authz.Public, h.putOperatingBatch)
	platform.ListRoute(s, "/receipts", authz.Public, h.listOperatingReceipts)
	s.GET("/receipts/:id", authz.Public, h.getOperatingReceipt)
	s.PUT("/receipts/:id", authz.Public, h.putOperatingReceipt)
}

// operatingStockScope rechecks the credential and the caller's expected session; every malformed scope is the same 404.
func operatingStockScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	if !authn.Refresh(c) {
		return authn.Principal{}, uuid.Nil, uuid.Nil, false
	}
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	if values, present := c.Request.Header["X-Helivanta-Expected-Subject"]; present && (len(values) != 1 || values[0] != p.Subject) {
		respond.Error(c, http.StatusConflict, "session_changed", "Your signed-in account changed. Refresh before recording stock.")
		return p, uuid.Nil, uuid.Nil, false
	}
	pharmacy, okPharmacy := operatingStockID(c.Param("pharmacy"))
	outlet, okOutlet := operatingStockID(c.Param("outlet"))
	if !okPharmacy || !okOutlet {
		respond.NotFound(c, "pharmacy resource")
		return p, uuid.Nil, uuid.Nil, false
	}
	return p, pharmacy, outlet, true
}

func operatingStockResource(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return p, pharmacy, outlet, uuid.Nil, false
	}
	id, ok := operatingStockID(c.Param("id"))
	if !ok {
		respond.NotFound(c, "pharmacy resource")
	}
	return p, pharmacy, outlet, id, ok
}

func operatingStockID(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	return id, err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

// operatingRead runs one duty-admitted read and appends its evidence; resource is nil for a list.
func (h *catalogueHandlers) operatingRead(c *gin.Context, p authn.Principal, pharmacy, outlet uuid.UUID, kind string, resource *uuid.UUID, read func(*gorm.DB) error) bool {
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStockRead, func(tx *gorm.DB, use operatingStockUse) error {
		if err := read(tx); err != nil {
			return err
		}
		return use.audit(tx, "pharmacy.operating_stock.read", kind, resource)
	})
	if err != nil {
		operatingStockError(c, err)
		return false
	}
	return true
}

func (h *catalogueHandlers) listReceivingProducts(c *gin.Context, params pagination.Params) ([]receivingProduct, error) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return nil, nil
	}
	var rows []receivingProduct
	h.operatingRead(c, p, pharmacy, outlet, "product", nil, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(receivingProductColumns).Where("tenant_id=? AND outlet_id=? AND status='active'", p.TenantID, outlet).Find(&rows).Error
	})
	return rows, nil
}

func (h *catalogueHandlers) listOperatingLocations(c *gin.Context, params pagination.Params) ([]stockLocation, error) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return nil, nil
	}
	var rows []stockLocation
	h.operatingRead(c, p, pharmacy, outlet, "location", nil, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(locationColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	return rows, nil
}

func (h *catalogueHandlers) listOperatingBatches(c *gin.Context, params pagination.Params) ([]stockBatch, error) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return nil, nil
	}
	product, ok := stockFilter(c, "product_id", false)
	if !ok {
		return nil, nil
	}
	var rows []stockBatch
	h.operatingRead(c, p, pharmacy, outlet, "batch", nil, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx, params).Select(batchColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
		if product != uuid.Nil {
			q = q.Where("product_id=?", product)
		}
		return q.Find(&rows).Error
	})
	return rows, nil
}

func (h *catalogueHandlers) getOperatingBatch(c *gin.Context) {
	p, pharmacy, outlet, id, ok := operatingStockResource(c)
	if !ok {
		return
	}
	var row stockBatch
	if h.operatingRead(c, p, pharmacy, outlet, "batch", &id, func(tx *gorm.DB) error {
		return tx.Select(batchColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&row).Error
	}) {
		respond.OK(c, row)
	}
}

func (h *catalogueHandlers) listOperatingReceipts(c *gin.Context, params pagination.Params) ([]stockMovement, error) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return nil, nil
	}
	var rows []stockMovement
	h.operatingRead(c, p, pharmacy, outlet, "receipt", nil, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(movementColumns).Where("tenant_id=? AND outlet_id=? AND kind='receipt'", p.TenantID, outlet).Find(&rows).Error
	})
	return rows, nil
}

func (h *catalogueHandlers) getOperatingReceipt(c *gin.Context) {
	p, pharmacy, outlet, id, ok := operatingStockResource(c)
	if !ok {
		return
	}
	var row stockMovement
	if h.operatingRead(c, p, pharmacy, outlet, "receipt", &id, func(tx *gorm.DB) error {
		return tx.Select(movementColumns).Where("tenant_id=? AND outlet_id=? AND id=? AND kind='receipt'", p.TenantID, outlet, id).First(&row).Error
	}) {
		respond.OK(c, row)
	}
}

// putOperatingBatch writes duty evidence only when the batch is created; an identical replay answers the stored row.
func (h *catalogueHandlers) putOperatingBatch(c *gin.Context) {
	p, pharmacy, outlet, id, ok := operatingStockResource(c)
	if !ok {
		return
	}
	var in batchInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validBatch(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid batch identity or full expiry date")
		return
	}
	var result stockBatch
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStockReceive, func(tx *gorm.DB, use operatingStockUse) error {
		row, created, err := recordBatch(tx, p.TenantID, outlet, id, in)
		if err != nil {
			return err
		}
		result = row
		if !created {
			return nil
		}
		return use.audit(tx, "pharmacy.operating_stock.batch_recorded", "batch", &id)
	})
	if err != nil {
		operatingStockError(c, err)
		return
	}
	respond.OK(c, result)
}

// putOperatingReceipt accepts only a receipt; the movement guard keeps it in quarantine and RLS refuses any other kind.
func (h *catalogueHandlers) putOperatingReceipt(c *gin.Context) {
	p, pharmacy, outlet, id, ok := operatingStockResource(c)
	if !ok {
		return
	}
	var in movementInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validMovement(&in, "receipt") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid stock receipt")
		return
	}
	var result stockMovement
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStockReceive, func(tx *gorm.DB, use operatingStockUse) error {
		row, created, err := h.recordMovement(tx, p, pharmacy, outlet, id, in)
		if err != nil {
			return err
		}
		result = row
		if !created {
			return nil
		}
		return use.audit(tx, "pharmacy.operating_stock.receipt_recorded", "receipt", &id)
	})
	if err != nil {
		operatingStockError(c, err)
		return
	}
	respond.OK(c, result)
}
