package pharmacy

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/gtin"
	"github.com/tesserix/helivanta/pkg/stockproduct"
)

var errBarcodeUnknown = errors.New("barcode is not linked at this outlet")
var errBarcodeConflict = errors.New("barcode is already linked to a different product")
var errBarcodeInactive = errors.New("barcode product is not active")
var errBarcodeAmbiguous = errors.New("barcode matches multiple outlet products")

func (h *catalogueHandlers) getBarcode(c *gin.Context)  { h.barcode(c, false) }
func (h *catalogueHandlers) linkBarcode(c *gin.Context) { h.barcode(c, true) }

func (h *catalogueHandlers) barcode(c *gin.Context, writing bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	code, ok := gtin.Normalize(c.Param("gtin"))
	if !ok || c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("provide a valid GTIN-8, GTIN-12, GTIN-13 or GTIN-14 without query parameters"))
		return
	}
	var in BarcodeLinkInput
	if writing && !decodeCatalogue(c, &in) {
		return
	}
	if writing && in.ProductID.Version() != 7 {
		respond.BadRequest(c, errors.New("product_id must be a UUIDv7"))
		return
	}
	var product catalogueProduct
	err := h.transaction(c.Request.Context(), p, outlet, writing, func(tx *gorm.DB) error {
		if writing {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+outlet.String()+":barcode:"+code).Error; err != nil {
				return err
			}
		}
		var link struct{ ProductID uuid.UUID }
		if err := tx.Raw(`SELECT product_id FROM pharmacy_product_barcodes WHERE tenant_id=? AND outlet_id=? AND gtin=?`, p.TenantID, outlet, code).Scan(&link).Error; err != nil {
			return err
		}
		if link.ProductID == uuid.Nil && h.drugCatalogue != nil {
			sources, err := h.drugCatalogue.SearchCatalogue(c.Request.Context(), code)
			if err != nil {
				return stockproduct.ErrUnavailable
			}
			versions := make([][]any, 0, len(sources))
			for _, source := range sources {
				if source.Gtin == nil {
					continue
				}
				if canonical, valid := gtin.Normalize(*source.Gtin); valid && canonical == code {
					versions = append(versions, []any{source.VersionID.String(), source.FormulationVersionID.String()})
				}
			}
			if len(versions) > 0 {
				var ids []uuid.UUID
				if err := tx.Model(&catalogueProduct{}).Where("tenant_id=? AND outlet_id=? AND (drug_master->>'version_id',drug_master->>'formulation_version_id') IN ? AND status='active'", p.TenantID, outlet, versions).Limit(2).Pluck("id", &ids).Error; err != nil {
					return err
				}
				if len(ids) > 1 {
					return errBarcodeAmbiguous
				}
				if len(ids) == 1 {
					link.ProductID = ids[0]
				}
			}
		}
		if writing {
			if link.ProductID != uuid.Nil && link.ProductID != in.ProductID {
				return errBarcodeConflict
			}
			link.ProductID = in.ProductID
		} else if link.ProductID == uuid.Nil {
			return errBarcodeUnknown
		}
		query := tx.Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, link.ProductID)
		if writing {
			query = query.Clauses(clause.Locking{Strength: "SHARE"})
		}
		if err := query.First(&product).Error; err != nil {
			return err
		}
		if product.Status != "active" {
			return errBarcodeInactive
		}
		if writing {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO pharmacy_product_barcodes(id,tenant_id,outlet_id,gtin,product_id,actor_subject) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,outlet_id,gtin) DO NOTHING`, id, p.TenantID, outlet, code, product.ID, p.Subject).Error; err != nil {
				return err
			}
		}
		if err := attachProductTax(tx, p.TenantID, outlet, &product); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'barcode_lookup','pharmacy_catalogue_products',?)`, p.TenantID, p.Subject, product.ID).Error
	})
	switch {
	case errors.Is(err, errBarcodeUnknown):
		respond.Error(c, http.StatusNotFound, "barcode_unknown", "Barcode not linked at this outlet. Link it to an active product.")
	case errors.Is(err, errBarcodeConflict):
		respond.Error(c, http.StatusConflict, "barcode_conflict", "Barcode is already linked to a different product. Reload the barcode before continuing.")
	case errors.Is(err, errBarcodeInactive):
		respond.Error(c, http.StatusConflict, "barcode_inactive", "This barcode's product is not active. Ask a catalogue editor to review it.")
	case errors.Is(err, errBarcodeAmbiguous):
		respond.Error(c, http.StatusConflict, "barcode_ambiguous", "This barcode matches more than one outlet product. Ask a catalogue editor to review the imported pack versions.")
	case errors.Is(err, stockproduct.ErrUnavailable):
		respond.Error(c, http.StatusServiceUnavailable, "catalogue_unavailable", "The medicine catalogue could not be checked. Retry the barcode lookup.")
	case err != nil:
		catalogueError(c, err)
	default:
		respond.OK(c, product)
	}
}
