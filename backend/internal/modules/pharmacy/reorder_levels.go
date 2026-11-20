package pharmacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// UnmarshalJSON distinguishes an explicit disable from an accidentally omitted level.
func (in *reorderLevelInput) UnmarshalJSON(data []byte) error {
	type wire reorderLevelInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, ok := fields["minimum_units"]; !ok {
		return errors.New("minimum_units must be explicit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode((*wire)(in))
}

type reorderLevel struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	OutletID        uuid.UUID
	ProductID       uuid.UUID
	MinimumUnits    *int64
	ExpectedVersion int64
	ProductVersion  int64
	Version         int64
	UnitOfIssue     string
	ActorSubject    string
	CreatedAt       time.Time
}

func (reorderLevel) TableName() string { return "pharmacy_reorder_levels" }
func (r reorderLevel) receipt() ReorderLevelReceipt {
	return ReorderLevelReceipt{ID: r.ID, OutletID: r.OutletID, ProductID: r.ProductID, Version: r.Version, UnitOfIssue: r.UnitOfIssue, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
		reorderLevelInput: reorderLevelInput{MinimumUnits: r.MinimumUnits, ExpectedVersion: &r.ExpectedVersion, ProductVersion: &r.ProductVersion}}
}

func reorderRoutes(s *platform.Router, h *catalogueHandlers) {
	s.GET("/catalogue/products/:id/reorder-level", PermStockRead, h.getReorderLevel)
	s.GET("/catalogue/products/:id/reorder-levels/:command", PermStockRead, h.getReorderReceipt)
	s.PUT("/catalogue/products/:id/reorder-levels/:command", PermCatalogueWrite, h.putReorderLevel)
	platform.ListRoute(s, "/stock/low-products", PermStockRead, h.listLowStock)
}

func (h *catalogueHandlers) getReorderLevel(c *gin.Context)   { h.readReorderLevel(c, false) }
func (h *catalogueHandlers) getReorderReceipt(c *gin.Context) { h.readReorderLevel(c, true) }
func (h *catalogueHandlers) readReorderLevel(c *gin.Context, receipt bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := catalogueID(c)
	if !ok {
		return
	}
	var command uuid.UUID
	if receipt {
		var err error
		command, err = uuid.Parse(c.Param("command"))
		if err != nil || command.Version() != 7 {
			respond.Error(c, 400, "invalid_request", "invalid reorder command")
			return
		}
	}
	var result *ReorderLevelReceipt
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := stockReference(tx, "pharmacy_catalogue_products", p.TenantID, outlet, product); err != nil {
			return err
		}
		q := tx.Where("tenant_id=? AND outlet_id=? AND product_id=?", p.TenantID, outlet, product)
		if receipt {
			q = q.Where("id=?", command)
		} else {
			q = q.Order("version DESC")
		}
		var row reorderLevel
		if err := q.First(&row).Error; err != nil {
			if !receipt && errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		value := row.receipt()
		result = &value
		return nil
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	if receipt {
		respond.OK(c, result)
	} else {
		respond.OK(c, struct {
			Level *ReorderLevelReceipt `json:"level"`
		}{result})
	}
}

func (h *catalogueHandlers) putReorderLevel(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := catalogueID(c)
	if !ok {
		return
	}
	perms, _ := authz.PermissionsFrom(c)
	if !perms.Has(PermStockRead) {
		respond.Error(c, 403, "forbidden", "stock read permission required")
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 {
		respond.Error(c, 400, "invalid_request", "invalid reorder command")
		return
	}
	var in reorderLevelInput
	if !decodeCatalogue(c, &in) {
		return
	}
	var result reorderLevel
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var item catalogueProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, product).First(&item).Error; err != nil {
			return err
		}
		err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, command).First(&result).Error
		if err == nil {
			sameMinimum := (result.MinimumUnits == nil && in.MinimumUnits == nil) || (result.MinimumUnits != nil && in.MinimumUnits != nil && *result.MinimumUnits == *in.MinimumUnits)
			if result.ProductID != product || result.ExpectedVersion != *in.ExpectedVersion || result.ProductVersion != *in.ProductVersion || result.ActorSubject != p.Subject || !sameMinimum {
				return errCatalogueVersion
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var version int64
		if err := tx.Model(&reorderLevel{}).Select("COALESCE(MAX(version),0)").Where("tenant_id=? AND outlet_id=? AND product_id=?", p.TenantID, outlet, product).Scan(&version).Error; err != nil {
			return err
		}
		if version != *in.ExpectedVersion || item.Version != *in.ProductVersion || item.Status != "active" {
			return errCatalogueVersion
		}
		result = reorderLevel{ID: command, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ProductID: product, MinimumUnits: in.MinimumUnits, ExpectedVersion: version, ProductVersion: item.Version, Version: version + 1, UnitOfIssue: item.UnitOfIssue, ActorSubject: p.Subject}
		return tx.Omit("CreatedAt").Clauses(clause.Returning{}).Create(&result).Error
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, result.receipt())
}

type lowStockProduct struct {
	LowStockProduct `gorm:"embedded"`
	CreatedAt       time.Time `json:"-"`
}

func (p lowStockProduct) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

func lowStockSource(tx *gorm.DB, tenant string, outlet uuid.UUID, observed time.Time) *gorm.DB {
	return tx.Table("pharmacy_catalogue_products p").Select(`p.id,p.outlet_id,p.created_at,p.sku,p.brand_name,p.unit_of_issue,r.minimum_units,
 available.units::text AS available_units,? AS observed_at,to_char(timezone(o.timezone,?::timestamptz)::date,'YYYY-MM-DD') AS local_date,o.timezone AS time_zone`, observed.UTC().Format(time.RFC3339Nano), observed).
		Joins("JOIN LATERAL (SELECT minimum_units,unit_of_issue FROM pharmacy_reorder_levels WHERE tenant_id=p.tenant_id AND outlet_id=p.outlet_id AND product_id=p.id ORDER BY version DESC LIMIT 1) r ON true").
		Joins("JOIN pharmacy_outlets o ON o.tenant_id=p.tenant_id AND o.id=p.outlet_id").
		Joins("CROSS JOIN LATERAL (SELECT COALESCE(SUM(on_hand::numeric),0) AS units FROM pharmacy_sellable_buckets(p.tenant_id,p.outlet_id,p.id,timezone(o.timezone,?::timestamptz)::date)) available", observed).
		Where("p.tenant_id=? AND p.outlet_id=? AND p.status='active' AND r.minimum_units IS NOT NULL AND r.unit_of_issue=p.unit_of_issue AND available.units<=r.minimum_units", tenant, outlet)
}

func (h *catalogueHandlers) listLowStock(c *gin.Context, params pagination.Params) ([]lowStockProduct, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "only limit and cursor are supported")
			return nil, nil
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var rows []lowStockProduct
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='8s'").Error; err != nil {
			return err
		}
		var observed time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&observed).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("(?) low_stock", lowStockSource(tx, p.TenantID, outlet, observed)).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
