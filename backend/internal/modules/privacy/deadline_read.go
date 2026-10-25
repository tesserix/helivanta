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

type caseDeadline struct {
	PolicyID                   uuid.UUID  `json:"policy_id"`
	Version                    int64      `json:"version"`
	StartedAt                  time.Time  `json:"started_at"`
	AcknowledgeBy              time.Time  `json:"acknowledge_by"`
	ResolveBy                  time.Time  `json:"resolve_by"`
	AcknowledgedAt             *time.Time `json:"acknowledged_at"`
	PausedAt                   *time.Time `json:"paused_at"`
	PausedMicroseconds         int64      `json:"paused_microseconds"`
	Terminal                   bool       `json:"terminal"`
	WorkerObservedAt           *time.Time `json:"worker_observed_at"`
	AcknowledgementEscalatedAt *time.Time `json:"acknowledgement_escalated_at"`
	ResolutionEscalatedAt      *time.Time `json:"resolution_escalated_at"`
}

func (caseDeadline) TableName() string { return "privacy_case_deadlines" }

const deadlineColumns = "policy_id,version,started_at,acknowledge_by,resolve_by,acknowledged_at,paused_at,paused_microseconds,terminal,worker_observed_at,acknowledgement_escalated_at,resolution_escalated_at"

func (h *handlers) deadline(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "request")
	if !ok {
		return
	}
	var row caseDeadline
	escalations := make([]deadlineEscalation, 0, 2)
	found := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := scope(tx, p, false); err != nil {
			return err
		}
		if err := allowOfficer(tx, p); err != nil {
			return err
		}
		var owner request
		if err := tx.Select("id").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&owner).Error; err != nil {
			return err
		}
		if err := audit(tx, p, "privacy_requests", id); err != nil {
			return err
		}
		err := tx.Select(deadlineColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if err := tx.Table("privacy_deadline_escalations").Select("kind,occurred_at,officer_id,routing").Where("tenant_id=? AND request_id=?", p.TenantID, id).Order("occurred_at,kind").Limit(2).Find(&escalations).Error; err != nil {
			return err
		}
		return audit(tx, p, "privacy_case_deadlines", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if !found {
		respond.OK(c, gin.H{"monitoring": "unmonitored"})
		return
	}
	monitoring := "pending"
	if row.WorkerObservedAt != nil {
		monitoring = "observed"
	}
	if row.ResolutionEscalatedAt != nil {
		monitoring = "escalated"
	}
	if row.Terminal {
		monitoring = "stopped"
	}
	respond.OK(c, struct {
		Monitoring string `json:"monitoring"`
		caseDeadline
		Escalations []deadlineEscalation `json:"escalations"`
	}{monitoring, row, escalations})
}

func (d *caseDeadline) AfterFind(*gorm.DB) error {
	for _, stamp := range []*time.Time{&d.StartedAt, &d.AcknowledgeBy, &d.ResolveBy, d.AcknowledgedAt, d.PausedAt, d.WorkerObservedAt, d.AcknowledgementEscalatedAt, d.ResolutionEscalatedAt} {
		if stamp != nil {
			*stamp = stamp.UTC()
		}
	}
	return nil
}

type deadlineEscalation struct {
	Kind       string     `json:"kind"`
	OccurredAt time.Time  `json:"occurred_at"`
	OfficerID  *uuid.UUID `json:"officer_id"`
	Routing    string     `json:"routing"`
}

func (e *deadlineEscalation) AfterFind(*gorm.DB) error { e.OccurredAt = e.OccurredAt.UTC(); return nil }
