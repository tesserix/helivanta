package privacy

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

func (h *handlers) officer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	var out officer
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		if e := activeOfficer(tx, p, &out); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errUnavailable
			}
			return e
		}
		return audit(tx, p, "privacy_officers", out.ID)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) revoke(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "officer")
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expected_version"`
		ReviewReference string `json:"review_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 2 || !validID(in.ReviewReference) {
		invalid(c)
		return
	}
	var out officer
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, true); e != nil {
			return e
		}
		if e := tx.Exec("SELECT set_config('app.privacy_manage','true',true)").Error; e != nil {
			return e
		}
		if e := tx.Select(officerColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error; e != nil {
			return e
		}
		if out.Status == "revoked" {
			if out.EndedReason == nil || *out.EndedReason != "revoked" || out.EndedReference == nil || out.EndedReference.String() != in.ReviewReference || out.Version != in.ExpectedVersion+1 {
				return errConflict
			}
			return audit(tx, p, "privacy_officers", id)
		}
		if out.Version != in.ExpectedVersion {
			return errConflict
		}
		r := tx.Exec("UPDATE privacy_officers SET status='revoked',ended_reason='revoked',ended_reference=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND id=? AND version=?", in.ReviewReference, p.TenantID, id, in.ExpectedVersion)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return errConflict
		}
		if e := tx.Select(officerColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_officers", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}
