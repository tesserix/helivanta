package patientmaster

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func hospitalScope(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Query("hospital_id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "hospital")
		c.Abort()
		return uuid.Nil, false
	}
	return id, true
}

func (h *patientHandlers) hospitalTransaction(ctx context.Context, p authn.Principal, hospital uuid.UUID, writing bool, fn func(*gorm.DB) error) error {
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject', ?, true), set_config('app.patient_hospital', ?, true)`, p.Subject, hospital.String()).Error; err != nil {
			return err
		}
		// Lock the grant until commit so revocation cannot race an admitted write.
		var grant struct{ Level string }
		err := tx.Raw(`SELECT level FROM patient_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error
		if err != nil {
			return errors.New("patient access check failed")
		}
		if grant.Level == "" || (writing && grant.Level != "registrar") {
			return gorm.ErrRecordNotFound
		}
		return fn(tx)
	})
}

func patientError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "patient")
		c.Abort()
		return
	}
	// Database errors may embed demographics; only the operation crosses into logs.
	respond.InternalErr(c, errors.New("patient database operation failed"), "could not process patient request")
	c.Abort()
}

func patientAudit(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, action string, rows []patient) error {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID.String()
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO patient_access_audit (tenant_id, hospital_id, actor_subject, action, resource_ids) VALUES (?, ?, ?, ?, ?::jsonb)`, p.TenantID, hospital, p.Subject, action, string(encoded)).Error
}
