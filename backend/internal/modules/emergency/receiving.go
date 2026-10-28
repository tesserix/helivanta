package emergency

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type receivingFacility struct {
	AccessLevel *string   `json:"access_level"`
	ID          uuid.UUID `json:"id"`
	ReceivingFacilityInput
	VerifiedBy string    `json:"verified_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const receivingFacilityColumns = "id,version,name,enabled,policy_reference,expires_at,verified_by,created_at,updated_at"

func receivingScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	id, ok := resourceID(c, "facility")
	return p, id, ok
}
func permissionPresent(c *gin.Context, permission authz.Permission) bool {
	permissions, ok := authz.PermissionsFrom(c)
	return ok && permissions.Has(permission)
}
func (h *handlers) putReceivingFacility(c *gin.Context) {
	p, id, ok := receivingScope(c)
	if !ok {
		return
	}
	var in ReceivingFacilityInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(in.Name) || !policyShape.MatchString(in.PolicyReference) {
		respond.Error(c, 400, "invalid_request", "invalid receiving facility fields")
		return
	}
	in.ExpiresAt = in.ExpiresAt.Truncate(time.Microsecond)
	var out receivingFacility
	err := h.transaction(c.Request.Context(), p, uuid.Nil, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO emergency_receiving_facilities(tenant_id,id) VALUES(?,?) ON CONFLICT DO NOTHING`, p.TenantID, id).Error; err != nil {
			return err
		}
		var locked struct{ ID uuid.UUID }
		if err := tx.Raw(`SELECT id FROM emergency_receiving_facilities WHERE tenant_id=? AND id=? FOR UPDATE`, p.TenantID, id).Scan(&locked).Error; err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_facility_history").Select(receivingFacilityColumns).Where("tenant_id=? AND id=? AND version=?", p.TenantID, id, in.Version+1).Take(&out).Error
		if err == nil {
			if out.VerifiedBy != p.Subject || out.Name != in.Name || *out.Enabled != *in.Enabled || out.PolicyReference != in.PolicyReference || !out.ExpiresAt.Equal(in.ExpiresAt) {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_facility_revisions(tenant_id,facility_id,version,name,enabled,policy_reference,expires_at,verified_by) VALUES(?,?,?,?,?,?,?,?)`, p.TenantID, id, in.Version+1, in.Name, *in.Enabled, in.PolicyReference, in.ExpiresAt, p.Subject).Error; err != nil {
			return err
		}
		return tx.Table("emergency_receiving_facility_history").Select(receivingFacilityColumns).Where("tenant_id=? AND id=? AND version=?", p.TenantID, id, in.Version+1).Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) getReceivingFacility(c *gin.Context) {
	p, id, ok := receivingScope(c)
	if !ok {
		return
	}
	var out receivingFacility
	err := h.transaction(c.Request.Context(), p, uuid.Nil, permissionPresent(c, PermReceivingManage), func(tx *gorm.DB) error {
		return tx.Table("emergency_receiving_facility_status").Select(receivingFacilityColumns+",access_level").Where("tenant_id=? AND id=?", p.TenantID, id).Order("version DESC").Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}

type receivingAccess struct {
	FacilityID uuid.UUID `json:"facility_id"`
	Subject    string    `json:"subject"`
	ReceivingAccessInput
	VerifiedBy string    `json:"verified_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const receivingAccessColumns = "facility_id,subject,version,level,enabled,policy_reference,expires_at,verified_by,created_at,updated_at"

func (h *handlers) putReceivingAccess(c *gin.Context) {
	p, f, ok := receivingScope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	var in ReceivingAccessInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(subject) || len(subject) > 200 || !policyShape.MatchString(in.PolicyReference) || (*in.Enabled && subject == p.Subject) {
		respond.Error(c, 400, "invalid_request", "independent receiving verification is required")
		return
	}
	in.ExpiresAt = in.ExpiresAt.Truncate(time.Microsecond)
	var out receivingAccess
	err := h.transaction(c.Request.Context(), p, uuid.Nil, true, func(tx *gorm.DB) error {
		var facility struct{ ID uuid.UUID }
		if err := tx.Table("emergency_receiving_facilities").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", p.TenantID, f).Take(&facility).Error; err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_access").Select(receivingAccessColumns).Where("tenant_id=? AND facility_id=? AND subject=? AND version=?", p.TenantID, f, subject, in.Version+1).Take(&out).Error
		if err == nil {
			if out.VerifiedBy != p.Subject || out.Level != in.Level || *out.Enabled != *in.Enabled || out.PolicyReference != in.PolicyReference || !out.ExpiresAt.Equal(in.ExpiresAt) {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_access(tenant_id,facility_id,subject,version,level,enabled,policy_reference,expires_at,verified_by)VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, f, subject, in.Version+1, in.Level, *in.Enabled, in.PolicyReference, in.ExpiresAt, p.Subject).Error; err != nil {
			return err
		}
		return tx.Table("emergency_receiving_access").Select(receivingAccessColumns).Where("tenant_id=? AND facility_id=? AND subject=? AND version=?", p.TenantID, f, subject, in.Version+1).Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) getReceivingAccess(c *gin.Context) {
	p, f, ok := receivingScope(c)
	if !ok {
		return
	}
	var out receivingAccess
	err := h.transaction(c.Request.Context(), p, uuid.Nil, permissionPresent(c, PermReceivingManage), func(tx *gorm.DB) error {
		return tx.Table("emergency_receiving_access").Select(receivingAccessColumns).Where("tenant_id=? AND facility_id=? AND subject=?", p.TenantID, f, c.Param("subject")).Order("version DESC").Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
