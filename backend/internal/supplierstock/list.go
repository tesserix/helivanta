package supplierstock

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

type listedAllocation struct{ f.AllocationView }

func (a listedAllocation) PageKey() (time.Time, uuid.UUID) {
	return a.Allocation.Plan.CreatedAt, a.Allocation.Plan.ID
}

// List reads a bounded owner/order page; authority and the owner frame must already be current.
func (s Store) List(tx *gorm.DB, scope f.SourceScope, page pagination.Params) ([]f.Allocation, error) {
	if s.validate() != nil || !scope.Valid() || scope.Product != string(s.Owner) || page.Limit < 1 || page.Limit > pagination.MaxLimit {
		return nil, f.ErrInvalid
	}
	var rows []struct {
		ID        uuid.UUID
		Body      json.RawMessage
		CreatedAt time.Time
	}
	q := tx.Table(s.Owner.table("allocations")).Select("id,body,created_at").Where("tenant_id=? AND organization_id=? AND location_id=? AND order_id=?", scope.TenantID, scope.OrganizationID, scope.LocationID, scope.OrderID).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]f.Allocation, 0, len(rows))
	for _, row := range rows {
		var a f.Allocation
		if supplierorder.DecodeObject(row.Body, &a) != nil || f.ValidateAllocation(a) != nil || a.Plan.ID != row.ID || !a.Plan.CreatedAt.Equal(row.CreatedAt) || !matches(a, scope) {
			return nil, f.ErrUnavailable
		}
		result = append(result, a)
	}
	return result, nil
}

func (h HTTP) registerList(r *platform.Router, base string) {
	platform.ResourceListRoute(r, strings.TrimSuffix(base, "/:allocation"), authz.Public, 50, h.list)
}
func (h HTTP) list(c *gin.Context, page pagination.Params) ([]listedAllocation, error) {
	p, scope, _, ok := h.request(c, true)
	if !ok {
		return nil, nil
	}
	result := []listedAllocation{}
	err := h.with(c, p, scope, uuid.Nil, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		rows, err := (Store{Owner: h.Owner}).List(tx, scope, page)
		if err != nil {
			return err
		}
		at, err := Clock(tx)
		if err != nil {
			return err
		}
		for _, a := range rows {
			view, err := f.Observe(a, at)
			if err != nil {
				return err
			}
			result = append(result, listedAllocation{view})
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_allocations_list_read',?,?)`, p.TenantID, p.Subject, h.Owner.table("allocations"), scope.OrderID).Error
	})
	if err != nil {
		Error(c, err)
		return nil, nil
	}
	return result, nil
}
