package pharmacy

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var errLowStockAudit = errors.New("low-stock disclosure audit unavailable")

func lowStockReadAudit(c *gin.Context, tx *gorm.DB, p authn.Principal, kind string, resource uuid.UUID, action string) error {
	if err := auditRegisterRead(c, tx, kind, p.TenantID, p.Subject, resource, action); err != nil {
		return errors.Join(errLowStockAudit, err)
	}
	return nil
}

func lowStockError(c *gin.Context, err error) {
	if errors.Is(err, errLowStockAudit) {
		respond.Error(c, 503, "low_stock_evidence_unavailable", "Low-stock read evidence could not be retained. Refresh before using this view.")
		return
	}
	catalogueError(c, err)
}

func reorderReadQuery(c *gin.Context, paged bool) bool {
	for key, values := range c.Request.URL.Query() {
		if !paged || (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "unsupported low-stock query")
			return false
		}
	}
	return true
}

const reorderProductColumns = "id,outlet_id,sku,brand_name,unit_of_issue,version,status"

type reorderProductChoice struct {
	ReorderProduct `gorm:"embedded"`
	CreatedAt      time.Time `json:"-"`
}

func (r reorderProductChoice) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *catalogueHandlers) listReorderProducts(c *gin.Context, params pagination.Params) ([]reorderProductChoice, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !reorderReadQuery(c, true) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	rows := []reorderProductChoice{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
			return err
		}
		query := tx.Table("pharmacy_catalogue_products").Select(reorderProductColumns+",created_at").Where("tenant_id=? AND outlet_id=? AND status='active'", p.TenantID, outlet)
		if err := tenantdb.ApplyKeyset(query, params).Find(&rows).Error; err != nil {
			return err
		}
		return lowStockReadAudit(c, tx, p, "low_stock_products", outlet, "list_read")
	})
	if err != nil {
		lowStockError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) getReorderProduct(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !reorderReadQuery(c, false) {
		return
	}
	product, ok := catalogueID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var result ReorderProduct
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
			return err
		}
		if err := tx.Table("pharmacy_catalogue_products").Select(reorderProductColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, product).Take(&result).Error; err != nil {
			return err
		}
		return lowStockReadAudit(c, tx, p, "low_stock_product", product, "record_read")
	})
	if err != nil {
		lowStockError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getLowStockOverview(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !reorderReadQuery(c, false) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	result := LowStockOverview{OutletID: outlet}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
			return err
		}
		var location struct{ Timezone string }
		if err := tx.Table("pharmacy_outlets").Select("timezone").Where("tenant_id=? AND id=?", p.TenantID, outlet).Take(&location).Error; err != nil {
			return err
		}
		zone, err := time.LoadLocation(location.Timezone)
		if err != nil {
			return err
		}
		var observed time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&observed).Error; err != nil {
			return err
		}
		result.ObservedAt = observed.UTC().Format(time.RFC3339Nano)
		result.LocalDate = observed.In(zone).Format(time.DateOnly)
		result.TimeZone = location.Timezone
		if err := tx.Table("(?) low_stock", lowStockSource(tx, p.TenantID, outlet, observed)).Count(&result.LowStock).Error; err != nil {
			return err
		}
		return lowStockReadAudit(c, tx, p, "low_stock_overview", outlet, "record_read")
	})
	if err != nil {
		lowStockError(c, err)
		return
	}
	respond.OK(c, result)
}
