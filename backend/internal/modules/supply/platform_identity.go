package supply

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
)

func (h *handlers) getPlatformIdentity(c *gin.Context) {
	p, id, ok := registryScope(c)
	if !ok {
		return
	}
	var current party
	load := func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, id, false); err != nil {
			return err
		}
		return tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id,version").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, id).First(&current).Error
	}
	if err := h.transaction(c.Request.Context(), p, id, false, false, load); err != nil {
		supplierError(c, err)
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.Error(c, 400, "invalid_request", "platform identity is selected by your authenticated workspace")
		return
	}
	if h.identity == nil {
		respond.Error(c, 503, "organization_identity_unavailable", "Platform identity verification is not available. Contact your platform administrator.")
		return
	}
	proof, err := h.identity.Verify(c.Request.Context(), uuid.MustParse(p.TenantID))
	if errors.Is(err, organizationidentity.ErrIneligible) {
		respond.Error(c, 409, "organization_identity_inactive", "Your workspace identity or product entitlement is inactive. Contact your platform administrator.")
		return
	}
	if err != nil {
		respond.Error(c, 503, "organization_identity_unavailable", "Platform identity could not be verified. Try again before continuing onboarding.")
		return
	}
	version := current.Version
	err = h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if err := load(tx); err != nil {
			return err
		}
		if current.Version != version {
			return errConflict
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'platform_identity_read','supply_parties',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		supplierError(c, err)
		return
	}
	respond.OK(c, organizationidentity.PartyProof{PartyID: id, PartyVersion: version, Identity: proof})
}
