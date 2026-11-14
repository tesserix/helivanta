package drugmaster

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type stockProductGuard struct{ db *tenantdb.DB }

// StockProducts exposes published catalogue facts without a consumer-owned master.
func (*Module) StockProducts(db *tenantdb.DB) stockproduct.Guard {
	if db == nil {
		return nil
	}
	return stockProductGuard{db: db}
}

func (g stockProductGuard) WithCurrentStockProduct(ctx context.Context, version uuid.UUID, action func(context.Context, stockproduct.Reference) error) error {
	if version == uuid.Nil {
		return stockproduct.ErrIneligible
	}
	if g.db == nil || action == nil {
		return stockproduct.ErrUnavailable
	}
	return g.db.WithSystem(ctx, func(tx *gorm.DB) error {
		// Publication uses the exclusive counterpart; this process-local callback does no source I/O.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(?)`, publicationLock).Error; err != nil {
			return stockproduct.ErrUnavailable
		}
		var rows []stockproduct.Reference
		err := tx.Raw(`SELECT p.product_id,p.version_id,p.formulation_id,f.id AS formulation_version_id,p.brand_name,p.manufacturer,f.label AS formulation_label,p.pack_size,p.unit_of_issue,f.regulatory_class,f.ndps
   FROM (`+productQuery+`) p JOIN LATERAL (
    SELECT x.id,x.label,x.regulatory_class,x.ndps FROM drugmaster_formulation_versions x JOIN drugmaster_releases r ON r.id=x.release_id
    WHERE x.formulation_id=p.formulation_id AND x.valid_from<=`+todayIST+`
    ORDER BY x.valid_from DESC,r.sequence DESC LIMIT 1
   ) f ON true WHERE p.version_id=? AND p.status='active' AND p.pack_size>0 LIMIT 2`, version).Scan(&rows).Error
		if err != nil {
			return stockproduct.ErrUnavailable
		}
		if len(rows) != 1 {
			return stockproduct.ErrIneligible
		}
		return action(ctx, rows[0])
	})
}

func (g stockProductGuard) LookupStockProduct(ctx context.Context, gtin string) (stockproduct.Reference, error) {
	if !validGTIN(gtin) {
		return stockproduct.Reference{}, stockproduct.ErrInvalidBarcode
	}
	if g.db == nil {
		return stockproduct.Reference{}, stockproduct.ErrUnavailable
	}
	gtin = gtin14(gtin)
	var products []Product
	if err := g.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(productQuery+`WHERE p.id IN(SELECT product_id FROM drugmaster_product_versions WHERE gtin=?) AND v.gtin=? LIMIT 2`, gtin, gtin).Scan(&products).Error
	}); err != nil {
		return stockproduct.Reference{}, stockproduct.ErrUnavailable
	}
	if len(products) != 1 {
		return stockproduct.Reference{}, stockproduct.ErrIneligible
	}
	var reference stockproduct.Reference
	err := g.WithCurrentStockProduct(ctx, products[0].VersionID, func(_ context.Context, ref stockproduct.Reference) error { reference = ref; return nil })
	return reference, err
}
func (h *readHandlers) stockReferenceByGTIN(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("provide one valid product barcode"))
		return
	}
	reference, err := (stockProductGuard{db: h.db}).LookupStockProduct(c.Request.Context(), c.Param("gtin"))
	switch {
	case errors.Is(err, stockproduct.ErrInvalidBarcode):
		respond.BadRequest(c, err)
	case errors.Is(err, stockproduct.ErrIneligible):
		respond.NotFound(c, "current stock product")
	case err != nil:
		respond.Error(c, 503, "catalogue_unavailable", "The current product source could not be verified.")
	default:
		respond.OK(c, reference)
	}
}

func (g stockProductGuard) WithCurrentStockProductInCommand(ctx context.Context, tenant string, version uuid.UUID, action func(context.Context, stockproduct.Reference) error) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || action == nil {
		return stockproduct.ErrUnavailable
	}
	return g.db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(?)`, publicationLock).Error; err != nil {
			return stockproduct.ErrUnavailable
		}
		return g.WithCurrentStockProduct(ctx, version, action)
	})
}
