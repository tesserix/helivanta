package emergency

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

type dispatchEligibility struct {
	DispatchEnabled bool            `json:"dispatch_enabled"`
	CaseID          uuid.UUID       `json:"case_id"`
	CaseVersion     int64           `json:"case_version"`
	AssessmentID    *uuid.UUID      `json:"assessment_id"`
	RequiredClass   *string         `json:"required_class"`
	ValidUntil      *time.Time      `json:"valid_until"`
	Eligible        bool            `json:"eligible"`
	Blockers        json.RawMessage `json:"blockers" gorm:"type:jsonb"`
	ComputedAt      time.Time       `json:"computed_at"`
}

func (h *handlers) getDispatchEligibility(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := caseID(c)
	if !ok {
		return
	}
	var out dispatchEligibility
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "intake"); err != nil {
			return err
		}
		var exists int64
		if err := tx.Table("emergency_cases").Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Count(&exists).Error; err != nil {
			return err
		}
		if exists != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Raw(`SELECT e.case_id,e.case_version,e.assessment_id,e.required_class,e.valid_until,
   cardinality(e.reasons)=0 AS eligible,to_jsonb(e.reasons) AS blockers,statement_timestamp() AS computed_at FROM (
   SELECT c.id AS case_id,c.version AS case_version,a.id AS assessment_id,a.required_class,a.valid_until,
    array_remove(ARRAY[
     CASE WHEN c.status<>'received' THEN 'case_closed' END,
     CASE WHEN a.id IS NULL THEN 'assessment_required' END,
     CASE WHEN a.case_version<>c.version THEN 'case_version_changed' END,
     CASE WHEN a.valid_until<=statement_timestamp() THEN 'assessment_expired' END,
     CASE WHEN r.id IS NULL OR r.id IS DISTINCT FROM a.reviewer_grant_id OR NOT r.enabled OR r.expires_at<=statement_timestamp() THEN 'clinical_delegation_inactive' END,
     CASE WHEN EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=c.tenant_id AND d.base_id=c.base_id AND d.case_id=c.id AND d.state NOT IN('cleared','cancelled')) THEN 'active_dispatch' END
    ]::text[],NULL) AS reasons
   FROM emergency_cases c
   LEFT JOIN LATERAL(SELECT id,case_version,required_class,valid_until,reviewer_subject,reviewer_grant_id FROM emergency_transport_assessments
    WHERE tenant_id=c.tenant_id AND base_id=c.base_id AND case_id=c.id ORDER BY sequence DESC LIMIT 1) a ON true
   LEFT JOIN LATERAL(SELECT id,enabled,expires_at FROM emergency_clinical_reviewers
    WHERE tenant_id=c.tenant_id AND base_id=c.base_id AND subject=a.reviewer_subject ORDER BY version DESC LIMIT 1) r ON true
   WHERE c.tenant_id=? AND c.base_id=? AND c.id=?) e`, p.TenantID, b, id).Scan(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	out.DispatchEnabled = h.dispatchEnabled
	respond.OK(c, out)
}
func (d dispatch) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }
func (h *handlers) getDispatch(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "dispatch")
	if !ok {
		return
	}
	var out dispatch
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		return tx.Select(dispatchColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listDispatches(c *gin.Context, params pagination.Params) ([]dispatch, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	var rows []dispatch
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		query := tx.Where("tenant_id=? AND base_id=?", p.TenantID, b)
		if raw := c.Param("case"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil || id.Version() != 7 {
				return gorm.ErrRecordNotFound
			}
			var count int64
			if err := tx.Table("emergency_cases").Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return gorm.ErrRecordNotFound
			}
			query = query.Where("case_id=?", id)
		}
		return tenantdb.ApplyKeyset(query, params).Select(dispatchColumns).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) getCrewDispatch(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "dispatch")
	if !ok {
		return
	}
	var out dispatch
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		return tx.Select(dispatchColumns).Where("tenant_id=? AND base_id=? AND id=? AND ? IN(driver_subject,clinician_subject) AND acknowledgement_deadline IS NOT NULL AND state NOT IN('workflow_pending','cleared','cancelled')", p.TenantID, b, id, p.Subject).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
