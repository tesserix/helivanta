package mediconnect

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PermNoticeManage allows tenant-reviewed publication; it confers no receipt access.
const PermNoticeManage authz.Permission = "mediconnect.notice.manage"

var errConsentConflict = errors.New("notice or consent conflicts")

// NoticeContent pins the exact tenant-published plain-text notice.
type NoticeContent struct {
	Purpose      string `json:"purpose"`
	Locale       string `json:"locale"`
	VersionLabel string `json:"version_label"`
	Title        string `json:"title"`
	Text         string `json:"text"`
}
type noticeInput struct {
	NoticeContent
	ReviewReference string `json:"review_reference"`
}
type accountNotice struct {
	ID              uuid.UUID `json:"id"`
	NoticeContent   `gorm:"embedded"`
	ContentHash     string     `json:"content_hash"`
	ReviewReference uuid.UUID  `json:"-"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	RetiredAt       *time.Time `json:"retired_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (accountNotice) TableName() string                 { return "mediconnect_notices" }
func (n accountNotice) PageKey() (time.Time, uuid.UUID) { return n.CreatedAt, n.ID }

const noticeColumns = "id,purpose,locale,version_label,title,text,content_hash,review_reference,status,version,retired_at,created_at,updated_at"

func consentID(c *gin.Context, key string) (uuid.UUID, bool) {
	raw := c.Param(key)
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != raw {
		respond.NotFound(c, "resource")
		return uuid.Nil, false
	}
	return id, true
}
func validNoticePurpose(p string) bool {
	return p == "account_processing" || p == "communications" || p == "analytics"
}
func validNoticeText(s string, max int) bool {
	return utf8.ValidString(s) && len(s) <= max && strings.TrimSpace(s) != "" && !strings.ContainsRune(s, 0)
}
func (n noticeInput) valid() bool {
	locale, err := language.Parse(n.Locale)
	ref, refErr := uuid.Parse(n.ReviewReference)
	return validNoticePurpose(n.Purpose) && err == nil && locale.String() == n.Locale && len(n.Locale) >= 2 && len(n.Locale) <= 35 && validNoticeText(n.VersionLabel, 64) && validNoticeText(n.Title, 200) && validNoticeText(n.Text, 8192) && refErr == nil && ref.Version() == 7 && ref.Variant() == uuid.RFC4122 && ref.String() == n.ReviewReference
}
func consentError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "resource")
	case errors.Is(err, errConsentConflict) || errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, 409, "consent_conflict", "Notice or consent state conflicts; reload before retrying")
	default:
		respond.InternalErr(c, errors.New("account consent operation failed"), "Account consent operation unavailable")
	}
	c.Abort()
}
func consentReadAudit(tx *gorm.DB, p authn.Principal, resource string, ids []uuid.UUID) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read',?,value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, resource, string(encoded)).Error
}
func readNotice(tx *gorm.DB, p authn.Principal, id uuid.UUID, row *accountNotice) error {
	return tx.Select(noticeColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(row).Error
}
func (h *accountHandlers) publishNotice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "notice")
	if !ok {
		return
	}
	var input noticeInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !input.valid() {
		respond.Error(c, 400, "invalid_request", "Provide reviewed plain-text notice content, canonical locale and review reference")
		return
	}
	encoded, err := json.Marshal(input.NoticeContent)
	if err != nil {
		consentError(c, err)
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row accountNotice
	created := false
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.mediconnect_notice_write','true',true)`).Error; err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO mediconnect_notices(tenant_id,id,purpose,locale,version_label,title,text,content_hash,review_reference,publisher_subject) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING`, p.TenantID, id, input.Purpose, input.Locale, input.VersionLabel, input.Title, input.Text, hash, input.ReviewReference, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readNotice(tx, p, id, &row); err != nil {
			return err
		}
		if row.ContentHash != hash || row.ReviewReference.String() != input.ReviewReference {
			return errConsentConflict
		}
		if !created {
			return consentReadAudit(tx, p, "mediconnect_notices", []uuid.UUID{id})
		}
		return nil
	})
	if err != nil {
		consentError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *accountHandlers) getNotice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "notice")
	if !ok {
		return
	}
	var row accountNotice
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		if err := readNotice(tx, p, id, &row); err != nil {
			return err
		}
		return consentReadAudit(tx, p, "mediconnect_notices", []uuid.UUID{id})
	})
	if err != nil {
		consentError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *accountHandlers) listNotices(c *gin.Context, params pagination.Params) ([]accountNotice, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	status := c.DefaultQuery("status", "published")
	purpose := c.Query("purpose")
	locale := c.Query("locale")
	if (status != "published" && status != "retired") || (purpose != "" && !validNoticePurpose(purpose)) || len(locale) > 35 {
		respond.Error(c, 400, "invalid_request", "Invalid notice filters")
		return nil, nil
	}
	var rows []accountNotice
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Select(noticeColumns).Where("tenant_id=? AND status=?", p.TenantID, status)
		if purpose != "" {
			q = q.Where("purpose=?", purpose)
		}
		if locale != "" {
			q = q.Where("locale=?", locale)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return consentReadAudit(tx, p, "mediconnect_notices", ids)
	})
	if err != nil {
		consentError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *accountHandlers) retireNotice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "notice")
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decode(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion > 2 {
		respond.Error(c, 400, "invalid_request", "Use the recorded version")
		return
	}
	var row accountNotice
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.mediconnect_notice_write','true',true)`).Error; err != nil {
			return err
		}
		if err := readNotice(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, id, &row); err != nil {
			return err
		}
		if row.Status == "retired" {
			return consentReadAudit(tx, p, "mediconnect_notices", []uuid.UUID{id})
		}
		if input.ExpectedVersion != row.Version {
			return errConsentConflict
		}
		if err := tx.Exec(`UPDATE mediconnect_notices SET status='retired',version=2,retired_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND id=?`, p.TenantID, id).Error; err != nil {
			return err
		}
		return readNotice(tx, p, id, &row)
	})
	if err != nil {
		consentError(c, err)
		return
	}
	respond.OK(c, row)
}
