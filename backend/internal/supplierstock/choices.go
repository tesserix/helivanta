package supplierstock

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type StockChoice struct{ f.SupplierStockChoice }

func (c StockChoice) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

type StockLocationChoice struct{ f.SupplierStockLocationChoice }

func (c StockLocationChoice) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

type StockLocations func(*gorm.DB, f.SourceScope, supplierorder.SupplierOrderLine, uuid.UUID, pagination.Params) ([]StockLocationChoice, error)

func (h HTTP) registerChoices(r *platform.Router, base string) {
	root := strings.TrimSuffix(base, "/allocations/:allocation") + "/lines/:line/stock-choices"
	platform.ResourceListRoute(r, root, authz.Public, 50, h.stockChoices)
	if h.Locations != nil {
		platform.ResourceListRoute(r, root+"/:stock/locations", authz.Public, 50, h.stockLocations)
	}
}
func (h HTTP) withChoice(c *gin.Context, action func(*gorm.DB, f.SourceScope, supplierorder.SupplierOrderLine) error) {
	p, s, _, ok := h.request(c, true)
	if !ok {
		return
	}
	lineID, ok := id(c, "line")
	if !ok {
		return
	}
	if h.Orders == nil {
		Error(c, f.ErrUnavailable)
		return
	}
	proof, err := h.Orders.VerifyOrder(tenantdb.WithActor(c.Request.Context(), p.Subject), s, f.NewReservation)
	if err != nil {
		Error(c, err)
		return
	}
	err = h.with(c, p, s, uuid.Nil, func(ctx context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		return proof.WithCurrent(ctx, func(_ context.Context, order supplierorder.SupplierOrder) error {
			for _, line := range order.Input.Lines {
				if line.ID == lineID {
					if line.Product.Owner != s.Product {
						return f.ErrUnavailable
					}
					if err := action(tx, s, line); err != nil {
						return err
					}
					return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_stock_choices_read',?,?)`, s.TenantID, p.Subject, string(h.Owner)+"_supplier_stock", s.OrderID).Error
				}
			}
			return gorm.ErrRecordNotFound
		})
	})
	if err != nil {
		Error(c, err)
	}
}
func (h HTTP) stockChoices(c *gin.Context, page pagination.Params) ([]StockChoice, error) {
	result := []StockChoice{}
	h.withChoice(c, func(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine) error {
		var err error
		result, err = h.Adapter.Choices(tx, s, line, page)
		return err
	})
	return result, nil
}
func (h HTTP) stockLocations(c *gin.Context, page pagination.Params) ([]StockLocationChoice, error) {
	stock, ok := id(c, "stock")
	if !ok {
		return nil, nil
	}
	result := []StockLocationChoice{}
	h.withChoice(c, func(tx *gorm.DB, s f.SourceScope, line supplierorder.SupplierOrderLine) error {
		var err error
		result, err = h.Locations(tx, s, line, stock, page)
		return err
	})
	return result, nil
}
