package privacy

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"time"
)

type DeadlinePolicyContent struct {
	VersionLabel           string    `json:"version_label"`
	AcknowledgementSeconds int       `json:"acknowledgement_seconds"`
	ResolutionSeconds      int       `json:"resolution_seconds"`
	PauseWhileWaiting      bool      `json:"pause_while_waiting"`
	ValidUntil             time.Time `json:"valid_until"`
}
type deadlinePolicy struct {
	ID                    uuid.UUID `json:"id"`
	DeadlinePolicyContent `gorm:"embedded"`
	ReviewReference       uuid.UUID  `json:"-"`
	Status                string     `json:"status"`
	Version               int64      `json:"version"`
	ApprovedAt            *time.Time `json:"approved_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (deadlinePolicy) TableName() string { return "privacy_deadline_policies" }
func (p *deadlinePolicy) AfterFind(*gorm.DB) error {
	p.ValidUntil = p.ValidUntil.UTC()
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.ApprovedAt != nil {
		v := p.ApprovedAt.UTC()
		p.ApprovedAt = &v
	}
	return nil
}

const deadlinePolicyColumns = "id,version_label,acknowledgement_seconds,resolution_seconds,pause_while_waiting,valid_until,review_reference,status,version,approved_at,created_at,updated_at"

func (h *handlers) proposeDeadlinePolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "policy")
	if !ok {
		return
	}
	var in struct {
		DeadlinePolicyContent
		ReviewReference   string `json:"review_reference"`
		PauseWhileWaiting *bool  `json:"pause_while_waiting"`
	}
	if !decode(c, &in) {
		return
	}
	if in.PauseWhileWaiting == nil || !validText(in.VersionLabel, 64) || in.AcknowledgementSeconds < 60 || in.ResolutionSeconds < in.AcknowledgementSeconds || in.ResolutionSeconds > 31536000 || !validID(in.ReviewReference) || in.ValidUntil.IsZero() || in.ValidUntil.Nanosecond()%1000 != 0 {
		invalid(c)
		return
	}
	var out deadlinePolicy
	created := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, true); e != nil {
			return e
		}
		if e := tx.Exec("SELECT set_config('app.privacy_policy_write','true',true)").Error; e != nil {
			return e
		}
		found := tx.Select(deadlinePolicyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error
		if found == nil {
			if out.VersionLabel != in.VersionLabel || out.AcknowledgementSeconds != in.AcknowledgementSeconds || out.ResolutionSeconds != in.ResolutionSeconds || out.PauseWhileWaiting != *in.PauseWhileWaiting || !out.ValidUntil.Equal(in.ValidUntil) || out.ReviewReference.String() != in.ReviewReference {
				return errConflict
			}
			return audit(tx, p, "privacy_deadline_policies", id)
		}
		if !errors.Is(found, gorm.ErrRecordNotFound) {
			return found
		}
		var valid bool
		if e := tx.Raw("SELECT ?::timestamptz>clock_timestamp()", in.ValidUntil).Scan(&valid).Error; e != nil {
			return e
		}
		if !valid {
			return errConflict
		}
		if e := tx.Exec(`INSERT INTO privacy_deadline_policies(tenant_id,id,version_label,acknowledgement_seconds,resolution_seconds,pause_while_waiting,review_reference,valid_until) VALUES (?,?,?,?,?,?,?,?)`, p.TenantID, id, in.VersionLabel, in.AcknowledgementSeconds, in.ResolutionSeconds, *in.PauseWhileWaiting, in.ReviewReference, in.ValidUntil).Error; e != nil {
			return e
		}
		created = true
		if e := tx.Select(deadlinePolicyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_deadline_policies", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, out)
	} else {
		respond.OK(c, out)
	}
}
func (h *handlers) approveDeadlinePolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "policy")
	if !ok {
		return
	}
	var in *struct{}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		invalid(c)
		return
	}
	var out deadlinePolicy
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, true); e != nil {
			return e
		}
		var escalation officer
		if e := tx.Select(officerColumns).Where("tenant_id=? AND role='escalation' AND status='escalation_active' AND subject=? AND valid_until>clock_timestamp()", p.TenantID, p.Subject).Take(&escalation).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errForbidden
			}
			return e
		}
		var primary officer
		if e := activeOfficer(tx, p, &primary); e != nil {
			return errUnavailable
		}
		if primary.Subject == p.Subject {
			return errForbidden
		}
		if !h.deadlinesEnabled {
			return errWorkflowUnavailable
		}
		if e := tx.Select(deadlinePolicyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error; e != nil {
			return e
		}
		if out.Status == "retired" {
			return errConflict
		}
		if out.Status == "active" {
			return audit(tx, p, "privacy_deadline_policies", id)
		}
		var valid bool
		if e := tx.Raw("SELECT ?::timestamptz>clock_timestamp()", out.ValidUntil).Scan(&valid).Error; e != nil {
			return e
		}
		if !valid {
			return errConflict
		}
		if e := tx.Exec("SELECT set_config('app.privacy_policy_write','true',true)").Error; e != nil {
			return e
		}
		if e := tx.Exec("UPDATE privacy_deadline_policies SET status='retired',version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND status='active'", p.TenantID).Error; e != nil {
			return e
		}
		r := tx.Exec("UPDATE privacy_deadline_policies SET status='active',version=version+1,approved_by_subject=?,approved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND id=? AND status='draft'", p.Subject, p.TenantID, id)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return errConflict
		}
		if e := tx.Select(deadlinePolicyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_deadline_policies", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}

var errWorkflowUnavailable = errors.New("privacy deadline workflow unavailable")
