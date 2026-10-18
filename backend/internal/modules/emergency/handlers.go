package emergency

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
	"gorm.io/gorm"
)

type handlers struct {
	db      *tenantdb.DB
	transit *transit.Client
}
type base struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Timezone             string    `json:"timezone"`
	PolicyReference      string    `json:"policy_reference"`
	ContactRetentionDays int       `json:"contact_retention_days"`
	AccessLevel          *string   `json:"access_level"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (base) TableName() string                 { return "emergency_bases" }
func (b base) PageKey() (time.Time, uuid.UUID) { return b.CreatedAt, b.ID }

const baseColumns = "id,name,timezone,policy_reference,contact_retention_days,created_at,updated_at"

type baseInput struct {
	Name                 string `json:"name" binding:"required,max=150"`
	Timezone             string `json:"timezone" binding:"required,max=100"`
	PolicyReference      string `json:"policy_reference" binding:"required,max=120"`
	ContactRetentionDays int    `json:"contact_retention_days" binding:"min=1,max=365"`
}

var policyShape = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,120}$`)

func cleanText(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsFunc(s, unicode.IsControl)
}
func decode(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		respond.Error(c, 400, "invalid_request", "invalid request body")
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "expected one JSON object")
		return false
	}
	if err := binding.Validator.ValidateStruct(out); err != nil {
		respond.Error(c, 400, "invalid_request", "invalid request fields")
		return false
	}
	return true
}
func scope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	b, err := uuid.Parse(c.Param("base"))
	if err != nil || b.Version() != 7 {
		respond.NotFound(c, "operating base")
		return p, uuid.Nil, false
	}
	return p, b, true
}
func (h *handlers) transaction(ctx context.Context, p authn.Principal, b uuid.UUID, admin bool, fn func(*gorm.DB) error) error {
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject',?,true),set_config('app.emergency_base',?,true),set_config('app.emergency_admin',?,true),set_config('statement_timeout','3s',true)`, p.Subject, b.String(), map[bool]string{true: "true", false: "false"}[admin]).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func requireAccess(tx *gorm.DB, p authn.Principal, b uuid.UUID, ability string) error {
	var allowed bool
	if err := tx.Raw(`SELECT COALESCE(emergency_allowed(?::uuid,?::uuid,?),false)`, p.TenantID, b, ability).Scan(&allowed).Error; err != nil {
		return err
	}
	if !allowed {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func audit(tx *gorm.DB, p authn.Principal, b, id uuid.UUID, action string, target *string) error {
	return tx.Exec(`INSERT INTO emergency_audit(tenant_id,base_id,actor_subject,action,resource_id,target_subject) VALUES(?,?,?,?,?,?)`, p.TenantID, b, p.Subject, action, id, target).Error
}
func respondError(c *gin.Context, err error) {
	if errors.Is(err, errCaseConflict) {
		respond.Conflict(c, "case content or version conflict")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "resource")
		return
	}
	if errors.Is(err, transit.ErrUnavailable) || errors.Is(err, transit.ErrIntegrity) {
		respond.Error(c, 503, "encryption_unavailable", "protected case data is unavailable")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23503", "42501":
			respond.NotFound(c, "resource")
			return
		case "23505":
			respond.Conflict(c, "resource already exists")
			return
		case "23514", "23502":
			respond.Error(c, 400, "invalid_request", "invalid request fields")
			return
		}
	}
	// Database diagnostics may embed caller or incident content; emit only a fixed error.
	respond.InternalErr(c, errors.New("emergency data operation failed"), "could not process emergency request")
}
func (h *handlers) putBase(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	var in baseInput
	if !decode(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	_, err := time.LoadLocation(in.Timezone)
	if err != nil || !cleanText(in.Name) || !policyShape.MatchString(in.PolicyReference) {
		respond.Error(c, 400, "invalid_request", "invalid operating base fields")
		return
	}
	var out base
	err = h.transaction(c.Request.Context(), p, b, true, func(tx *gorm.DB) error {
		result := tx.Exec(`INSERT INTO emergency_bases(tenant_id,id,name,timezone,policy_reference,contact_retention_days) VALUES(?,?,?,?,?,?)
 ON CONFLICT(tenant_id,id) DO UPDATE SET name=EXCLUDED.name,timezone=EXCLUDED.timezone,policy_reference=EXCLUDED.policy_reference,contact_retention_days=EXCLUDED.contact_retention_days,updated_at=now()
 WHERE (emergency_bases.name,emergency_bases.timezone,emergency_bases.policy_reference,emergency_bases.contact_retention_days) IS DISTINCT FROM (EXCLUDED.name,EXCLUDED.timezone,EXCLUDED.policy_reference,EXCLUDED.contact_retention_days)`, p.TenantID, b, in.Name, in.Timezone, in.PolicyReference, in.ContactRetentionDays)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if err := audit(tx, p, b, b, "base.configured", nil); err != nil {
				return err
			}
		}
		return tx.Select(baseColumns).Where("tenant_id=? AND id=?", p.TenantID, b).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listBases(c *gin.Context, params pagination.Params) ([]base, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []base
	err := h.transaction(c.Request.Context(), p, uuid.Nil, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(baseColumns+`, (SELECT level FROM emergency_base_access a WHERE a.tenant_id=emergency_bases.tenant_id AND a.base_id=emergency_bases.id AND a.subject=?) AS access_level`, p.Subject).Where("tenant_id=?", p.TenantID).Find(&rows).Error
	})
	if err != nil {
		return nil, errors.New("could not discover operating bases")
	}
	return rows, nil
}
func (h *handlers) putAccess(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	var in struct {
		Level string `json:"level" binding:"required,oneof=reader dispatcher supervisor"`
	}
	if !decode(c, &in) {
		return
	}
	if !cleanText(subject) || len(subject) > 200 {
		respond.Error(c, 400, "invalid_request", "invalid subject")
		return
	}
	err := h.transaction(c.Request.Context(), p, b, true, func(tx *gorm.DB) error {
		result := tx.Exec(`INSERT INTO emergency_base_access(tenant_id,base_id,subject,level) VALUES(?,?,?,?) ON CONFLICT(tenant_id,base_id,subject) DO UPDATE SET level=EXCLUDED.level,updated_at=now() WHERE emergency_base_access.level IS DISTINCT FROM EXCLUDED.level`, p.TenantID, b, subject, in.Level)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return audit(tx, p, b, b, "base.access_granted", &subject)
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, gin.H{"base_id": b, "subject": subject, "level": in.Level})
}

func (h *handlers) getAccess(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	var level string
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		return tx.Raw(`SELECT level FROM emergency_base_access WHERE tenant_id=? AND base_id=? AND subject=?`, p.TenantID, b, p.Subject).Scan(&level).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, gin.H{"level": level})
}
func (h *handlers) deleteAccess(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	if !cleanText(subject) || len(subject) > 200 {
		respond.Error(c, 400, "invalid_request", "invalid subject")
		return
	}
	err := h.transaction(c.Request.Context(), p, b, true, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&base{}).Where("tenant_id=? AND id=?", p.TenantID, b).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
		result := tx.Exec(`DELETE FROM emergency_base_access WHERE tenant_id=? AND base_id=? AND subject=?`, p.TenantID, b, subject)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return audit(tx, p, b, b, "base.access_revoked", &subject)
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
