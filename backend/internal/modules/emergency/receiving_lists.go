package emergency

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

func (f receivingFacility) PageKey() (time.Time, uuid.UUID) { return f.CreatedAt, f.ID }
func (l receivingLink) PageKey() (time.Time, uuid.UUID)     { return l.CreatedAt, l.ID }
func (r receivingRequest) PageKey() (time.Time, uuid.UUID)  { return r.CreatedAt, r.ID }
func (h *handlers) listReceivingFacilities(c *gin.Context, params pagination.Params) ([]receivingFacility, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []receivingFacility
	err := h.transaction(c.Request.Context(), p, uuid.Nil, permissionPresent(c, PermReceivingManage), func(tx *gorm.DB) error {
		query := tx.Table("emergency_receiving_facility_status").Select(receivingFacilityColumns+",access_level").Where("tenant_id=?", p.TenantID)
		return tenantdb.ApplyKeyset(query, params).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) listReceivingLinks(c *gin.Context, params pagination.Params) ([]receivingLink, error) {
	var rows []receivingLink
	var p authn.Principal
	var s uuid.UUID
	var ok bool
	recipient := c.Param("facility") != ""
	if recipient {
		p, s, ok = receivingScope(c)
	} else {
		p, s, ok = scope(c)
	}
	if !ok {
		return nil, nil
	}
	b := s
	if recipient {
		b = uuid.Nil
	}
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		query := tx.Table("emergency_receiving_link_status").Select(receivingLinkColumns)
		if recipient {
			query = query.Where("destination_tenant_id=? AND facility_id=?", p.TenantID, s)
		} else {
			if err := requireAccess(tx, p, s, "read"); err != nil {
				return err
			}
			query = query.Where("tenant_id=? AND base_id=?", p.TenantID, s)
		}
		return tenantdb.ApplyKeyset(query, params).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
func receivingProviderRead(tx *gorm.DB, p authn.Principal, b, d uuid.UUID) error {
	if err := requireAccess(tx, p, b, "read"); err == nil {
		return nil
	}
	var assigned struct{ ID uuid.UUID }
	return tx.Table("emergency_dispatches").Select("id").Where("tenant_id=? AND base_id=? AND id=? AND ? IN(driver_subject,clinician_subject) AND acknowledgement_deadline IS NOT NULL AND state NOT IN('workflow_pending','cleared','cancelled')", p.TenantID, b, d, p.Subject).Take(&assigned).Error
}
func (h *handlers) listReceivingRequests(c *gin.Context, params pagination.Params) ([]receivingRequest, error) {
	var rows []receivingRequest
	var p authn.Principal
	var s uuid.UUID
	var ok bool
	recipient := c.Param("facility") != ""
	if recipient {
		p, s, ok = receivingScope(c)
	} else {
		p, s, ok = scope(c)
	}
	if !ok {
		return nil, nil
	}
	b := s
	if recipient {
		b = uuid.Nil
	}
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		query := tx.Table("emergency_receiving_request_status").Select(receivingRequestColumns)
		if recipient {
			query = query.Where("destination_tenant_id=? AND facility_id=?", p.TenantID, s)
		} else {
			d, ok := resourceID(c, "dispatch")
			if !ok {
				return gorm.ErrRecordNotFound
			}
			if err := receivingProviderRead(tx, p, s, d); err != nil {
				return err
			}
			query = query.Where("tenant_id=? AND base_id=? AND dispatch_id=?", p.TenantID, s, d)
		}
		return tenantdb.ApplyKeyset(query, params).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
