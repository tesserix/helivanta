package practitioner

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errStewardInvalid = errors.New("invalid steward appointment")
var errStewardIndependence = errors.New("independent steward nomination required")

type stewardAppointment struct {
	ID              uuid.UUID  `json:"id"`
	Subject         string     `json:"subject"`
	NominatedBy     string     `json:"nominated_by"`
	ReviewReference uuid.UUID  `json:"review_reference"`
	ValidUntil      time.Time  `json:"valid_until"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	Effective       bool       `json:"effective" gorm:"->"`
	AcceptedAt      *time.Time `json:"accepted_at"`
	EndedAt         *time.Time `json:"ended_at"`
	EndedBy         *string    `json:"ended_by"`
	EndReference    *uuid.UUID `json:"end_reference"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (stewardAppointment) TableName() string { return "practitioner_steward_appointments" }
func (s *stewardAppointment) AfterFind(*gorm.DB) error {
	s.ValidUntil = s.ValidUntil.UTC()
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.AcceptedAt != nil {
		v := s.AcceptedAt.UTC()
		s.AcceptedAt = &v
	}
	if s.EndedAt != nil {
		v := s.EndedAt.UTC()
		s.EndedAt = &v
	}
	return nil
}

const stewardColumns = "id,subject,nominated_by,review_reference,valid_until,status,version,(status='active' AND valid_until>practitioner_registry_now()) AS effective,accepted_at,ended_at,ended_by,end_reference,created_at,updated_at"

func canonicalStewardReference(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	return id, err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == raw
}

var stewardTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func stewardUntil(raw string) (time.Time, bool) {
	if !stewardTimestamp.MatchString(raw) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	t = t.UTC()
	return t, err == nil && t.Year() > 0 && t.Year() <= 9999 && t.Nanosecond()%1000 == 0
}
func validStewardSubject(s string) bool {
	if len(s) < 1 || len(s) > 512 || !utf8.ValidString(s) || strings.HasPrefix(s, "service:") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
func (h *handlers) stewardTransaction(ctx context.Context, p authn.Principal, managed bool, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	manage := "false"
	if managed {
		manage = "true"
	}
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject',?,true),set_config('app.practitioner_steward_manage',?,true),set_config('lock_timeout','2s',true),set_config('statement_timeout','5s',true)", p.Subject, manage).Error; e != nil {
			return e
		}
		return fn(tx)
	})
}
func loadSteward(tx *gorm.DB, p authn.Principal, id uuid.UUID, managed, lock bool, row *stewardAppointment) error {
	q := tx.Select(stewardColumns).Where("tenant_id=? AND id=?", p.TenantID, id)
	if !managed {
		q = q.Where("subject=?", p.Subject)
	}
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q.Take(row).Error
}
func stewardAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return auditIDs(tx, p, "practitioner_steward_appointments", []uuid.UUID{id})
}
func (h *handlers) nominateSteward(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "appointment")
	if !ok {
		return
	}
	var in *struct {
		Subject         string `json:"subject"`
		ReviewReference string `json:"review_reference"`
		ValidUntil      string `json:"valid_until"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		failure(c, errStewardInvalid)
		return
	}
	ref, validRef := canonicalStewardReference(in.ReviewReference)
	until, validUntil := stewardUntil(in.ValidUntil)
	if !validRef || !validUntil || !validStewardSubject(in.Subject) {
		failure(c, errStewardInvalid)
		return
	}
	if in.Subject == p.Subject {
		failure(c, errStewardIndependence)
		return
	}
	var row stewardAppointment
	created := false
	err := h.stewardTransaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		e := loadSteward(tx, p, id, true, false, &row)
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var now time.Time
			if e := tx.Raw("SELECT practitioner_registry_now()").Scan(&now).Error; e != nil {
				return e
			}
			if !until.After(now) {
				return errStewardInvalid
			}
			res := tx.Exec("INSERT INTO practitioner_steward_appointments(tenant_id,id,subject,nominated_by,review_reference,valid_until) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, id, in.Subject, p.Subject, ref, until)
			if res.Error != nil {
				return res.Error
			}
			created = res.RowsAffected == 1
			if e := loadSteward(tx, p, id, true, false, &row); e != nil {
				return e
			}
		}
		if row.Subject != in.Subject || row.NominatedBy != p.Subject || row.ReviewReference != ref || !row.ValidUntil.Equal(until) {
			return errIntentConflict
		}
		if !created {
			return stewardAudit(tx, p, id)
		}
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *handlers) getManagedSteward(c *gin.Context) { h.getSteward(c, true) }
func (h *handlers) getOwnSteward(c *gin.Context)     { h.getSteward(c, false) }
func (h *handlers) getSteward(c *gin.Context, managed bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "appointment")
	if !ok {
		return
	}
	var row stewardAppointment
	err := h.stewardTransaction(c.Request.Context(), p, managed, func(tx *gorm.DB) error {
		if e := loadSteward(tx, p, id, managed, false, &row); e != nil {
			return e
		}
		return stewardAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) currentSteward(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	var row stewardAppointment
	err := h.stewardTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := tx.Select(stewardColumns).Where("tenant_id=? AND subject=? AND status='active' AND valid_until>practitioner_registry_now()", p.TenantID, p.Subject).Take(&row).Error; e != nil {
			return e
		}
		if !row.Effective {
			return gorm.ErrRecordNotFound
		}
		return stewardAudit(tx, p, row.ID)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) acceptSteward(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "appointment")
	if !ok {
		return
	}
	var in *struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil || in.ExpectedVersion != 1 {
		failure(c, errStewardInvalid)
		return
	}
	var row stewardAppointment
	err := h.stewardTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := loadSteward(tx, p, id, false, true, &row); e != nil {
			return e
		}
		if row.AcceptedAt != nil {
			return stewardAudit(tx, p, id)
		}
		if row.Status != "nominated" || row.Version != in.ExpectedVersion {
			return errIntentConflict
		}
		if e := tx.Exec("UPDATE practitioner_steward_appointments SET status='expired',version=version+1 WHERE tenant_id=? AND subject=? AND status='active' AND valid_until<=practitioner_registry_now()", p.TenantID, p.Subject).Error; e != nil {
			return e
		}
		res := tx.Exec("UPDATE practitioner_steward_appointments SET status='active',version=version+1 WHERE tenant_id=? AND id=? AND subject=? AND status='nominated' AND version=? AND valid_until>practitioner_registry_now()", p.TenantID, id, p.Subject, in.ExpectedVersion)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errIntentConflict
		}
		if e := loadSteward(tx, p, id, false, false, &row); e != nil {
			return e
		}
		return stewardAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) revokeSteward(c *gin.Context)   { h.endSteward(c, true, "revoked") }
func (h *handlers) withdrawSteward(c *gin.Context) { h.endSteward(c, false, "withdrawn") }
func (h *handlers) endSteward(c *gin.Context, managed bool, status string) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "appointment")
	if !ok {
		return
	}
	var in *struct {
		ExpectedVersion int64  `json:"expected_version"`
		ReviewReference string `json:"review_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		failure(c, errStewardInvalid)
		return
	}
	ref, validRef := canonicalStewardReference(in.ReviewReference)
	if !validRef || in.ExpectedVersion < 1 || in.ExpectedVersion > 2 {
		failure(c, errStewardInvalid)
		return
	}
	var row stewardAppointment
	err := h.stewardTransaction(c.Request.Context(), p, managed, func(tx *gorm.DB) error {
		if e := loadSteward(tx, p, id, managed, true, &row); e != nil {
			return e
		}
		if row.Status == status && row.EndReference != nil && *row.EndReference == ref && row.Version == in.ExpectedVersion+1 {
			return stewardAudit(tx, p, id)
		}
		if row.Version != in.ExpectedVersion || (row.Status != "nominated" && row.Status != "active") {
			return errIntentConflict
		}
		res := tx.Exec("UPDATE practitioner_steward_appointments SET status=?,end_reference=?,version=version+1 WHERE tenant_id=? AND id=? AND version=?", status, ref, p.TenantID, id, in.ExpectedVersion)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errIntentConflict
		}
		if e := loadSteward(tx, p, id, managed, false, &row); e != nil {
			return e
		}
		return stewardAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
