package mediconnect

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type accountConsent struct {
	ID           uuid.UUID  `json:"id"`
	AccountID    uuid.UUID  `json:"account_id"`
	NoticeID     uuid.UUID  `json:"notice_id"`
	Purpose      string     `json:"purpose"`
	Locale       string     `json:"locale"`
	VersionLabel string     `json:"version_label"`
	ContentHash  string     `json:"content_hash"`
	Status       string     `json:"status"`
	NoticeStatus string     `json:"notice_status" gorm:"-"`
	Version      int64      `json:"version"`
	WithdrawnAt  *time.Time `json:"withdrawn_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (accountConsent) TableName() string                 { return "mediconnect_account_consents" }
func (r accountConsent) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const consentColumns = "id,account_id,notice_id,purpose,locale,version_label,content_hash,status,version,withdrawn_at,created_at,updated_at"

func readConsent(tx *gorm.DB, p authn.Principal, id uuid.UUID, row *accountConsent) error {
	return tx.Select(consentColumns).Where("tenant_id=? AND subject=? AND id=?", p.TenantID, p.Subject, id).Take(row).Error
}
func consentNoticeStates(tx *gorm.DB, p authn.Principal, rows []accountConsent) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.NoticeID
	}
	var notices []struct {
		ID     uuid.UUID
		Status string
	}
	if err := tx.Table("mediconnect_notices").Select("id,status").Where("tenant_id=? AND id IN ?", p.TenantID, ids).Find(&notices).Error; err != nil {
		return err
	}
	states := make(map[uuid.UUID]string, len(notices))
	for _, n := range notices {
		states[n.ID] = n.Status
	}
	for i := range rows {
		state, ok := states[rows[i].NoticeID]
		if !ok {
			return errors.New("notice state unavailable")
		}
		rows[i].NoticeStatus = state
	}
	return nil
}
func (h *accountHandlers) publishConsent(tx *gorm.DB, p authn.Principal, subject string, row accountConsent) error {
	data, err := json.Marshal(mediconnectcontract.AccountConsentData{ConsentID: row.ID.String(), AccountID: row.AccountID.String(), NoticeID: row.NoticeID.String(), Purpose: row.Purpose})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *accountHandlers) putConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "consent")
	if !ok {
		return
	}
	var in struct {
		NoticeID    string `json:"notice_id"`
		ContentHash string `json:"content_hash"`
		Accepted    bool   `json:"accepted"`
	}
	if !decode(c, &in) {
		return
	}
	noticeID, err := uuid.Parse(in.NoticeID)
	hash, hashErr := hex.DecodeString(in.ContentHash)
	if err != nil || noticeID.Version() != 7 || noticeID.Variant() != uuid.RFC4122 || noticeID.String() != in.NoticeID || hashErr != nil || len(hash) != 32 || hex.EncodeToString(hash) != in.ContentHash || !in.Accepted {
		respond.Error(c, 400, "invalid_request", "Explicit acceptance of a notice ID and captured content hash is required")
		return
	}
	var row accountConsent
	created := false
	err = h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		var owner account
		if err := readAccount(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, &owner); err != nil {
			return err
		}
		err := readConsent(tx, p, id, &row)
		if err == nil {
			if row.NoticeID != noticeID || row.ContentHash != in.ContentHash {
				return errConsentConflict
			}
			return consentProjection(tx, p, &row)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var notice accountNotice
		if err := readNotice(tx.Clauses(clause.Locking{Strength: "SHARE"}), p, noticeID, &notice); err != nil {
			return err
		}
		if notice.Status != "published" || notice.ContentHash != in.ContentHash {
			return errConsentConflict
		}
		result := tx.Exec(`INSERT INTO mediconnect_account_consents(tenant_id,id,account_id,subject,notice_id,purpose,locale,version_label,content_hash) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING`, p.TenantID, id, owner.ID, p.Subject, noticeID, notice.Purpose, notice.Locale, notice.VersionLabel, notice.ContentHash)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readConsent(tx, p, id, &row); err != nil {
			return err
		}
		if !created {
			return errConsentConflict
		}
		if err := h.publishConsent(tx, p, mediconnectcontract.SubjectAccountConsentRecorded, row); err != nil {
			return err
		}
		return consentProjection(tx, p, &row)
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
func consentProjection(tx *gorm.DB, p authn.Principal, row *accountConsent) error {
	rows := []accountConsent{*row}
	if err := consentNoticeStates(tx, p, rows); err != nil {
		return err
	}
	*row = rows[0]
	return consentReadAudit(tx, p, "mediconnect_account_consents", []uuid.UUID{row.ID})
}
func (h *accountHandlers) getConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "consent")
	if !ok {
		return
	}
	row := accountConsent{ID: id}
	if err := h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		if err := readConsent(tx, p, id, &row); err != nil {
			return err
		}
		return consentProjection(tx, p, &row)
	}); err != nil {
		consentError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *accountHandlers) listConsents(c *gin.Context, params pagination.Params) ([]accountConsent, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	status := c.Query("status")
	purpose := c.Query("purpose")
	if (status != "" && status != "active" && status != "withdrawn") || (purpose != "" && !validNoticePurpose(purpose)) {
		respond.Error(c, 400, "invalid_request", "Invalid consent filters")
		return nil, nil
	}
	var rows []accountConsent
	err := h.linkTransaction(c, p, func(tx *gorm.DB, owner account) error {
		q := tenantdb.ApplyKeyset(tx, params).Select(consentColumns).Where("tenant_id=? AND account_id=? AND subject=?", p.TenantID, owner.ID, p.Subject)
		if status != "" {
			q = q.Where("status=?", status)
		}
		if purpose != "" {
			q = q.Where("purpose=?", purpose)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if err := consentNoticeStates(tx, p, rows); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return consentReadAudit(tx, p, "mediconnect_account_consents", ids)
	})
	if err != nil {
		consentError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *accountHandlers) withdrawConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := consentID(c, "consent")
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decode(c, &in) {
		return
	}
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 2 {
		respond.Error(c, 400, "invalid_request", "Use the recorded version")
		return
	}
	var row accountConsent
	err := h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		var owner account
		if err := readAccount(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, &owner); err != nil {
			return err
		}
		if err := readConsent(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, id, &row); err != nil {
			return err
		}
		if row.Status == "withdrawn" {
			return consentProjection(tx, p, &row)
		}
		if in.ExpectedVersion != row.Version {
			return errConsentConflict
		}
		if err := tx.Exec(`UPDATE mediconnect_account_consents SET status='withdrawn',version=2,withdrawn_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND subject=? AND id=?`, p.TenantID, p.Subject, id).Error; err != nil {
			return err
		}
		if err := readConsent(tx, p, id, &row); err != nil {
			return err
		}
		if err := h.publishConsent(tx, p, mediconnectcontract.SubjectAccountConsentWithdrawn, row); err != nil {
			return err
		}
		return consentProjection(tx, p, &row)
	})
	if err != nil {
		consentError(c, err)
		return
	}
	respond.OK(c, row)
}
