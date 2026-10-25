package privacy

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type handlers struct {
	db               *tenantdb.DB
	deadlinesEnabled bool
}
type officer struct {
	Role            string     `json:"role"`
	EndedReason     *string    `json:"-"`
	EndedReference  *uuid.UUID `json:"-"`
	ID              uuid.UUID  `json:"id"`
	Subject         string     `json:"-"`
	DisplayName     string     `json:"display_name"`
	ContactEmail    string     `json:"contact_email"`
	ReviewReference uuid.UUID  `json:"-"`
	ValidUntil      time.Time  `json:"valid_until"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (officer) TableName() string { return "privacy_officers" }

const officerColumns = "id,role,subject,display_name,contact_email,review_reference,valid_until,status,version,created_at,updated_at,ended_reason,ended_reference"

type request struct {
	ID          uuid.UUID `json:"id"`
	Subject     string    `json:"-"`
	OfficerID   uuid.UUID `json:"officer_id"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (request) TableName() string { return "privacy_requests" }

const requestColumns = "id,subject,officer_id,kind,description,status,version,created_at,updated_at"

var errConflict = errors.New("privacy command conflicts")
var errUnavailable = errors.New("privacy officer unavailable")

func failure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "resource")
	case errors.Is(err, errForbidden):
		respond.Error(c, 403, "privacy_forbidden", "Current officer or principal authority required")
	case errors.Is(err, errConflict) || errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, 409, "privacy_conflict", "State or command conflicts; reload before retrying")
	case errors.Is(err, errWorkflowUnavailable):
		respond.Error(c, 503, "privacy_workflow_unavailable", "Privacy deadline workflow support is not configured")
	case errors.Is(err, errUnavailable):
		respond.Error(c, 503, "privacy_unavailable", "No current accepted privacy officer")
	default:
		respond.InternalErr(c, errors.New("privacy storage operation failed"), "Privacy operation unavailable")
	}
	c.Abort()
}
func validText(s string, n int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= n && !strings.ContainsRune(s, 0)
}
func validID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == s
}
func pathID(c *gin.Context, key string) (uuid.UUID, bool) {
	s := c.Param(key)
	if !validID(s) {
		respond.NotFound(c, "resource")
		return uuid.Nil, false
	}
	return uuid.MustParse(s), true
}
func decode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		respond.Error(c, 400, "invalid_request", "Expected a bounded JSON object")
		return false
	}
	return true
}
func invalid(c *gin.Context) { respond.Error(c, 400, "invalid_request", "Invalid privacy request") }
func scope(tx *gorm.DB, p authn.Principal, exclusive bool) error {
	if err := tx.Exec("SELECT set_config('app.actor_subject',?,true)", p.Subject).Error; err != nil {
		return err
	}
	// Shared operation locks keep officer replacement from racing a disclosure.
	if exclusive {
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "privacy:"+p.TenantID).Error
	}
	return tx.Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))", "privacy:"+p.TenantID).Error
}
func audit(tx *gorm.DB, p authn.Principal, table string, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read',?,?)`, p.TenantID, p.Subject, table, id).Error
}
func activeOfficer(tx *gorm.DB, p authn.Principal, out *officer) error {
	return tx.Select(officerColumns).Where("tenant_id=? AND role='primary' AND status='active' AND valid_until>clock_timestamp()", p.TenantID).Take(out).Error
}
func allowOfficer(tx *gorm.DB, p authn.Principal) error {
	var o officer
	err := actorOfficer(tx, p, &o)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Subject == p.Subject {
		return tx.Exec("SELECT set_config('app.privacy_officer','true',true)").Error
	}
	return nil
}
func (h *handlers) nominate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "officer")
	if !ok {
		return
	}
	var in struct {
		Role            string    `json:"role"`
		Subject         string    `json:"subject"`
		DisplayName     string    `json:"display_name"`
		ContactEmail    string    `json:"contact_email"`
		ReviewReference string    `json:"review_reference"`
		ValidUntil      time.Time `json:"valid_until"`
	}
	if !decode(c, &in) {
		return
	}
	if in.Role == "" {
		in.Role = "primary"
	}
	address, e := mail.ParseAddress(in.ContactEmail)
	if (in.Role != "primary" && in.Role != "escalation") || !validText(in.Subject, 512) || !validText(in.DisplayName, 200) || len(in.ContactEmail) > 254 || e != nil || address.Address != in.ContactEmail || !validID(in.ReviewReference) || in.ValidUntil.IsZero() || in.ValidUntil.Nanosecond()%1000 != 0 {
		invalid(c)
		return
	}
	var row officer
	created := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, true); e != nil {
			return e
		}
		if e := tx.Exec("SELECT set_config('app.privacy_manage','true',true)").Error; e != nil {
			return e
		}
		found := tx.Select(officerColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error
		if found == nil {
			if row.Role != in.Role || row.Subject != in.Subject || row.DisplayName != in.DisplayName || row.ContactEmail != in.ContactEmail || row.ReviewReference.String() != in.ReviewReference || !row.ValidUntil.Equal(in.ValidUntil) {
				return errConflict
			}
			return audit(tx, p, "privacy_officers", id)
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
		if e := tx.Exec("UPDATE privacy_officers SET status='revoked',ended_reason='superseded',ended_reference=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND role=? AND status='nominated'", id, p.TenantID, in.Role).Error; e != nil {
			return e
		}
		if e := tx.Exec(`INSERT INTO privacy_officers(tenant_id,id,role,subject,display_name,contact_email,review_reference,valid_until) VALUES (?,?,?,?,?,?,?,?)`, p.TenantID, id, in.Role, in.Subject, in.DisplayName, in.ContactEmail, in.ReviewReference, in.ValidUntil).Error; e != nil {
			return e
		}
		created = true
		if e := tx.Select(officerColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_officers", id)
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
func (h *handlers) accept(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "officer")
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
	var row officer
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, true); e != nil {
			return e
		}
		if e := tx.Select(officerColumns).Where("tenant_id=? AND id=? AND subject=?", p.TenantID, id, p.Subject).Take(&row).Error; e != nil {
			return e
		}
		if row.Status == "revoked" {
			return errConflict
		}
		if row.Status == "nominated" {
			var valid bool
			if e := tx.Raw("SELECT ?::timestamptz>clock_timestamp()", row.ValidUntil).Scan(&valid).Error; e != nil {
				return e
			}
			if !valid {
				return errConflict
			}
			if e := tx.Exec("SELECT set_config('app.privacy_accept',?,true)", id.String()).Error; e != nil {
				return e
			}
			if e := tx.Exec("UPDATE privacy_officers SET status='revoked',ended_reason='superseded',ended_reference=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND role=? AND status IN ('active','escalation_active')", id, p.TenantID, row.Role).Error; e != nil {
				return e
			}
			active := "active"
			if row.Role == "escalation" {
				active = "escalation_active"
			}
			r := tx.Exec("UPDATE privacy_officers SET status=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND id=? AND status='nominated'", active, p.TenantID, id)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return errConflict
			}
			if e := tx.Select(officerColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; e != nil {
				return e
			}
		}
		return audit(tx, p, "privacy_officers", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) submit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "request")
	if !ok {
		return
	}
	var in struct {
		Kind        string `json:"kind"`
		Description string `json:"description"`
	}
	if !decode(c, &in) {
		return
	}
	if (in.Kind != "access" && in.Kind != "correction" && in.Kind != "erasure" && in.Kind != "grievance") || !validText(in.Description, 4096) {
		invalid(c)
		return
	}
	var row request
	created := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		existing := tx.Select(requestColumns).Where("tenant_id=? AND id=? AND subject=?", p.TenantID, id, p.Subject).Take(&row).Error
		if existing == nil {
			if row.Kind != in.Kind || row.Description != in.Description {
				return errConflict
			}
			return audit(tx, p, "privacy_requests", id)
		}
		if !errors.Is(existing, gorm.ErrRecordNotFound) {
			return existing
		}
		var o officer
		if e := activeOfficer(tx, p, &o); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errUnavailable
			}
			return e
		}
		r := tx.Exec(`INSERT INTO privacy_requests(tenant_id,id,subject,officer_id,kind,description) VALUES (?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING`, p.TenantID, id, p.Subject, o.ID, in.Kind, in.Description)
		if r.Error != nil {
			return r.Error
		}
		created = r.RowsAffected == 1
		if e := tx.Select(requestColumns).Where("tenant_id=? AND id=? AND subject=?", p.TenantID, id, p.Subject).Take(&row).Error; e != nil {
			return e
		}
		if row.Kind != in.Kind || row.Description != in.Description {
			return errConflict
		}
		return audit(tx, p, "privacy_requests", id)
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
func (h *handlers) get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "request")
	if !ok {
		return
	}
	var row request
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		if e := allowOfficer(tx, p); e != nil {
			return e
		}
		if e := tx.Select(requestColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_requests", id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}

func auditMany(tx *gorm.DB, p authn.Principal, table string, ids []uuid.UUID) error {
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read',?,value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, table, string(data)).Error
}

func (o *officer) AfterFind(*gorm.DB) error {
	o.ValidUntil = o.ValidUntil.UTC()
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return nil
}
func (r *request) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return nil
}

func actorOfficer(tx *gorm.DB, p authn.Principal, out *officer) error {
	return tx.Select(officerColumns).Where("tenant_id=? AND subject=? AND status IN ('active','escalation_active') AND valid_until>clock_timestamp()", p.TenantID, p.Subject).Take(out).Error
}
