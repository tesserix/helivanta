package workforce

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

const (
	hospitalEstablishmentKind = "hospital"
	pharmacyEstablishmentKind = "pharmacy"
)

// pharmacyEstablishment is the only identifier the pharmacy transaction accepts, so a hospital ID cannot reach it.
type pharmacyEstablishment uuid.UUID

// establishmentAdmission answers 404 for an ID registered as another kind and leaves unregistered IDs to the handler.
func (h *handlers) establishmentAdmission(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		id, err := uuid.Parse(c.Param(kind))
		if err != nil {
			return
		}
		var stored string
		err = errors.New("workforce store not configured")
		if h.db != nil {
			err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT establishment_kind FROM workforce_hospitals WHERE tenant_id=? AND id=?`, p.TenantID, id).Scan(&stored).Error
			})
		}
		switch {
		case err != nil:
			// Fails closed: an unreadable registry could be hiding an establishment of the other kind.
			c.Header("Cache-Control", "no-store")
			requestid.Logger(c).ErrorContext(c.Request.Context(), "workforce establishment admission unavailable", "kind", kind, "err", err)
			respond.Error(c, http.StatusServiceUnavailable, "establishment_unavailable", "The establishment registry is unavailable. Retry shortly.")
		case stored != "" && stored != kind:
			c.Header("Cache-Control", "no-store")
			respond.NotFound(c, kind)
		}
	}
}

func pharmacyScope(c *gin.Context) (authn.Principal, pharmacyEstablishment, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, pharmacyEstablishment{}, false
	}
	id, err := uuid.Parse(c.Param("pharmacy"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "pharmacy")
		return p, pharmacyEstablishment{}, false
	}
	return p, pharmacyEstablishment(id), true
}

func (h *handlers) pharmacyTransaction(ctx context.Context, p authn.Principal, fn func(*gorm.DB) error) error {
	return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject', ?, true)`, p.Subject).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func readPharmacyEmployer(tx *gorm.DB, tenant string, pharmacy pharmacyEstablishment) (PharmacyEmployer, error) {
	var employer PharmacyEmployer
	result := tx.Raw(`SELECT id AS pharmacy_id, name, pharmacy_ownership_type AS ownership_type, created_at FROM workforce_hospitals
	 WHERE tenant_id=? AND id=? AND establishment_kind='pharmacy'`, tenant, uuid.UUID(pharmacy)).Scan(&employer)
	if result.Error == nil && result.RowsAffected == 0 {
		return employer, gorm.ErrRecordNotFound
	}
	return employer, result.Error
}

// putPharmacyEmployer mirrors a standalone Pharma organization; name and ownership come only from Pharma.
func (h *handlers) putPharmacyEmployer(c *gin.Context) {
	p, pharmacy, ok := pharmacyScope(c)
	if !ok {
		return
	}
	var in struct{}
	if !decode(c, &in) {
		return
	}
	if h.pharmacyEmployers == nil || h.db == nil {
		respond.Error(c, http.StatusServiceUnavailable, "authz_unavailable", "authorization is temporarily unavailable")
		return
	}
	var employer PharmacyEmployer
	err := h.db.WithGuardedTenantCommand(c.Request.Context(), p.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		source, err := h.pharmacyEmployers.CurrentPharmacyEmployer(ctx, p.TenantID, uuid.UUID(pharmacy))
		if err != nil {
			return err
		}
		return h.pharmacyTransaction(ctx, p, func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,?,'pharmacy',?)
			 ON CONFLICT (tenant_id,id) DO NOTHING`, p.TenantID, source.PharmacyID, source.Name, source.OwnershipType).Error; err != nil {
				return err
			}
			employer, err = readPharmacyEmployer(tx, p.TenantID, pharmacy)
			return err
		})
	})
	switch {
	case errors.Is(err, authz.ErrPharmacyEmployerHidden), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "pharmacy")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable):
		requestid.Logger(c).ErrorContext(c.Request.Context(), "pharmacy employer guard unavailable", "err", err)
		respond.Error(c, http.StatusServiceUnavailable, "authz_unavailable", "authorization is temporarily unavailable")
	case err != nil:
		respondError(c, err)
	default:
		respond.OK(c, employer)
	}
}

func (h *handlers) getPharmacyEmployer(c *gin.Context) {
	p, pharmacy, ok := pharmacyScope(c)
	if !ok {
		return
	}
	var employer PharmacyEmployer
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		employer, err = readPharmacyEmployer(tx, p.TenantID, pharmacy)
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "pharmacy")
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, employer)
}
