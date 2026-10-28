package mediconnect

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/text/language"
	"gorm.io/gorm"

	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type accessibilityPreferences struct {
	TextSize string `json:"text_size"`
	Contrast string `json:"contrast"`
	Motion   string `json:"motion"`
}

func (p *accessibilityPreferences) valid() bool {
	return p == nil || ((p.TextSize == "system" || p.TextSize == "large" || p.TextSize == "extra_large") &&
		(p.Contrast == "system" || p.Contrast == "high") && (p.Motion == "system" || p.Motion == "reduced"))
}

type account struct {
	ID               uuid.UUID                `json:"id"`
	Accessibility    accessibilityPreferences `json:"accessibility" gorm:"embedded;embeddedPrefix:accessibility_"`
	Locale           string                   `json:"locale"`
	PreferredChannel string                   `json:"preferred_channel"`
	LowDataMode      bool                     `json:"low_data_mode"`
	Version          int64                    `json:"version"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

const accountColumns = "id, locale, preferred_channel, low_data_mode, accessibility_text_size, accessibility_contrast, accessibility_motion, version, created_at, updated_at"

type accountHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

func decode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Invalid account request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Expected one JSON object")
		return false
	}
	return true
}

func setActor(tx *gorm.DB, subject string) error {
	return tx.Exec("SELECT set_config('app.actor_subject', ?, true)", subject).Error
}

func readAccount(tx *gorm.DB, p authn.Principal, row *account) error {
	return tx.Table("mediconnect_accounts").Select(accountColumns).
		Where("tenant_id = ? AND subject = ?", p.TenantID, p.Subject).Take(row).Error
}

func auditRead(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events (tenant_id, actor_subject, action, resource_type, resource_id)
 VALUES (?, ?, 'read', 'mediconnect_accounts', ?)`, p.TenantID, p.Subject, id).Error
}

func accountError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "account")
	} else {
		respond.InternalErr(c, err, "Account operation unavailable")
	}
}

func (h *accountHandlers) create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	var input *struct{}
	if !decode(c, &input) {
		return
	}
	if input == nil {
		respond.Error(c, 400, "invalid_request", "Expected an object")
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		accountError(c, err)
		return
	}
	var row account
	created := false
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO mediconnect_accounts (id, tenant_id, subject)
   VALUES (?, ?, ?) ON CONFLICT (tenant_id, subject) DO NOTHING`, id, p.TenantID, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readAccount(tx, p, &row); err != nil {
			return err
		}
		if !created {
			return auditRead(tx, p, row.ID)
		}
		data, err := json.Marshal(mediconnectcontract.AccountCreatedData{AccountID: row.ID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, mediconnectcontract.SubjectAccountCreated, events.Event{
			Type: mediconnectcontract.SubjectAccountCreated, Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		accountError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}

func (h *accountHandlers) get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	var row account
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		if err := readAccount(tx, p, &row); err != nil {
			return err
		}
		// A failed audit aborts the disclosure instead of silently losing evidence.
		return auditRead(tx, p, row.ID)
	})
	if err != nil {
		accountError(c, err)
		return
	}
	respond.OK(c, row)
}

type preferencesInput struct {
	Accessibility    *accessibilityPreferences `json:"accessibility"`
	ExpectedVersion  int64                     `json:"expected_version"`
	Locale           string                    `json:"locale"`
	PreferredChannel string                    `json:"preferred_channel"`
	LowDataMode      *bool                     `json:"low_data_mode"`
}

func (h *accountHandlers) preferences(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	var input preferencesInput
	if !decode(c, &input) {
		return
	}
	_, localeErr := language.Parse(input.Locale)
	if !input.Accessibility.valid() || input.ExpectedVersion < 1 || input.ExpectedVersion > 9007199254740991 || input.LowDataMode == nil ||
		len(input.Locale) < 2 || len(input.Locale) > 35 || localeErr != nil ||
		(input.PreferredChannel != "none" && input.PreferredChannel != "email" && input.PreferredChannel != "whatsapp") {
		respond.Error(c, 400, "invalid_request", "Invalid account preferences")
		return
	}
	var textSize, contrast, motion *string
	if input.Accessibility != nil {
		textSize, contrast, motion = &input.Accessibility.TextSize, &input.Accessibility.Contrast, &input.Accessibility.Motion
	}
	var row account
	conflict := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		if err := readAccount(tx, p, &row); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE mediconnect_accounts SET locale = ?, preferred_channel = ?,
   low_data_mode = ?, accessibility_text_size = COALESCE(?, accessibility_text_size),
   accessibility_contrast = COALESCE(?, accessibility_contrast), accessibility_motion = COALESCE(?, accessibility_motion),
   version = version + 1, updated_at = clock_timestamp()
   WHERE tenant_id = ? AND subject = ? AND version = ?`,
			input.Locale, input.PreferredChannel, *input.LowDataMode, textSize, contrast, motion, p.TenantID, p.Subject, input.ExpectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			conflict = true
			return nil
		}
		return readAccount(tx, p, &row)
	})
	if err != nil {
		accountError(c, err)
		return
	}
	if conflict {
		respond.Conflict(c, "Account version changed; reload before updating")
		return
	}
	respond.OK(c, row)
}
