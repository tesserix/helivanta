package pharmacy

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
	"time"
)

func licenceRevocationRoutes(r *platform.Router, deps platform.Deps) {
	h := &tradingScopeHandlers{db: deps.DB, authority: deps.SourceAdministration}
	base := "/pharmacies/:pharmacy/outlets/:outlet/licences/:id/revocations/:revocation"
	r.PUT(base, PermLicenceReview, h.revokeLicence)
	r.GET(base, PermLicenceReview, h.getLicenceRevocation)
}
func licenceRevocationID(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("revocation"))
	licence, e := uuid.Parse(c.Param("id"))
	if err != nil || e != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || licence.Version() != 7 || licence.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "licence revocation")
		return uuid.Nil, uuid.Nil, false
	}
	return id, licence, true
}
func loadLicenceRevocation(tx *gorm.DB, p authn.Principal, outlet, licence, id uuid.UUID) (suppliertrade.PharmacyLicenceRevocation, error) {
	var result suppliertrade.PharmacyLicenceRevocation
	var reason string
	err := tx.Raw(`SELECT tenant_id,outlet_id,licence_id,id,actor_subject,reason,revoked_at FROM pharmacy_licence_revocations WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=?`, p.TenantID, outlet, licence, id).Row().Scan(&result.OwnerTenantID, &result.OutletID, &result.LicenceID, &result.ID, &result.ActorSubject, &reason, &result.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, gorm.ErrRecordNotFound
	}
	if err != nil {
		return result, err
	}
	result.Input.Reason = reason
	result.RevokedAt = result.RevokedAt.UTC()
	return result, nil
}
func (h *tradingScopeHandlers) revokeLicence(c *gin.Context) {
	if _, _, ok := pharmacyPrincipal(c); !ok {
		return
	}
	id, licence, ok := licenceRevocationID(c)
	if !ok {
		return
	}
	var input suppliertrade.PharmacyLicenceRevocationInput
	if !scopeDecode(c, &input) {
		return
	}
	if !suppliertrade.Text(input.Reason, 500) {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	var result suppliertrade.PharmacyLicenceRevocation
	status := 201
	err := h.transaction(c, PermLicenceReview, true, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":pharmacy-licence:"+outlet.String()+":"+licence.String()).Error; err != nil {
			return err
		}
		existing, err := loadLicenceRevocation(tx, p, outlet, licence, id)
		if err == nil {
			if existing.ActorSubject != p.Subject || existing.Input != input {
				return errTradeScopeConflict
			}
			result = existing
			status = 200
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var parent uuid.UUID
		err = tx.Raw(`SELECT id FROM pharmacy_outlet_licences WHERE tenant_id=? AND outlet_id=? AND id=? FOR UPDATE`, p.TenantID, outlet, licence).Row().Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return gorm.ErrRecordNotFound
		}
		if err != nil {
			return err
		}
		var at time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
			return err
		}
		result = suppliertrade.PharmacyLicenceRevocation{OwnerTenantID: uuid.MustParse(p.TenantID), OutletID: outlet, LicenceID: licence, ID: id, Input: input, ActorSubject: p.Subject, RevokedAt: at.UTC()}
		return tx.Exec(`INSERT INTO pharmacy_licence_revocations(tenant_id,outlet_id,licence_id,id,actor_subject,reason,revoked_at)VALUES(?,?,?,?,?,?,?)`, p.TenantID, outlet, licence, id, p.Subject, input.Reason, at).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		c.JSON(status, result)
	}
}
func (h *tradingScopeHandlers) getLicenceRevocation(c *gin.Context) {
	id, licence, ok := licenceRevocationID(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	var result suppliertrade.PharmacyLicenceRevocation
	err := h.transaction(c, PermLicenceReview, false, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		var err error
		result, err = loadLicenceRevocation(tx, p, outlet, licence, id)
		if err != nil {
			return err
		}
		if result.ActorSubject != p.Subject {
			return gorm.ErrRecordNotFound
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'licence_revocation_read','pharmacy_licence_revocations',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
