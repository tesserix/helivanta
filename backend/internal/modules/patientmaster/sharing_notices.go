package patientmaster

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

// SharingNoticeContent pins wording and its reviewed policy reference.
type SharingNoticeContent struct {
	Purpose               string `json:"purpose"`
	Locale                string `json:"locale"`
	VersionLabel          string `json:"version_label"`
	Title                 string `json:"title"`
	Text                  string `json:"text"`
	WithdrawalConsequence string `json:"withdrawal_consequence"`
	PolicyReference       string `json:"policy_reference"`
}

func (in SharingNoticeContent) valid() bool {
	locale, err := language.Parse(in.Locale)
	text := func(s string, max int) bool {
		return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsRune(s, 0)
	}
	return in.Purpose == "care_coordination" && err == nil && locale.String() == in.Locale && len(in.Locale) >= 2 && len(in.Locale) <= 35 && text(in.VersionLabel, 64) && text(in.Title, 200) && text(in.Text, 4096) && text(in.WithdrawalConsequence, 1024) && validReviewUUID(in.PolicyReference, true)
}

type sharingNotice struct {
	ID                   uuid.UUID `json:"id"`
	HospitalID           uuid.UUID `json:"hospital_id"`
	SharingNoticeContent `gorm:"embedded"`
	ContentHash          string     `json:"content_hash"`
	CreatorSubject       string     `json:"-"`
	ReviewerSubject      *string    `json:"-"`
	ReviewReference      *uuid.UUID `json:"-"`
	Status               string     `json:"status"`
	Version              int64      `json:"version"`
	ApprovedAt           *time.Time `json:"approved_at"`
	RetiredAt            *time.Time `json:"retired_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (sharingNotice) TableName() string { return "patient_sharing_notices" }

const sharingNoticeColumns = "id,hospital_id,purpose,locale,version_label,title,text,withdrawal_consequence,policy_reference,content_hash,creator_subject,reviewer_subject,review_reference,status,version,approved_at,retired_at,created_at,updated_at"

func readSharingNotice(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, row *sharingNotice) error {
	return tx.Select(sharingNoticeColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(row).Error
}
func sharingNoticeReadAudit(tx *gorm.DB, p authn.Principal, row sharingNotice) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','patient_sharing_notices',?)`, p.TenantID, p.Subject, row.ID).Error
}
func (h *patientHandlers) publishSharingNotice(tx *gorm.DB, p authn.Principal, row sharingNotice) error {
	data, err := json.Marshal(patientmastercontract.SharingNoticeChangedData{NoticeID: row.ID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, patientmastercontract.SubjectSharingNoticeChanged, events.Event{Type: patientmastercontract.SubjectSharingNoticeChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *patientHandlers) putSharingNotice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("notice"), true) {
		respond.NotFound(c, "sharing notice")
		return
	}
	var in SharingNoticeContent
	if !decodePatientInput(c, &in) {
		return
	}
	if !in.valid() {
		respond.BadRequest(c, errors.New("invalid sharing notice"))
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		patientError(c, err)
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row sharingNotice
	created := false
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO patient_sharing_notices(tenant_id,hospital_id,id,purpose,locale,version_label,title,text,withdrawal_consequence,policy_reference,content_hash,creator_subject) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, p.TenantID, hospital, c.Param("notice"), in.Purpose, in.Locale, in.VersionLabel, in.Title, in.Text, in.WithdrawalConsequence, in.PolicyReference, hash, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readSharingNotice(tx, p, hospital, c.Param("notice"), &row); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				var count int64
				if e := tx.Model(&sharingNotice{}).Where("tenant_id=? AND hospital_id=? AND purpose=? AND locale=? AND version_label=?", p.TenantID, hospital, in.Purpose, in.Locale, in.VersionLabel).Count(&count).Error; e != nil {
					return e
				}
				if count > 0 {
					return errConsentAuthorityConflict
				}
				return gorm.ErrRecordNotFound
			}
			return err
		}
		if row.ContentHash != hash || row.CreatorSubject != p.Subject {
			return errConsentAuthorityConflict
		}
		if created {
			return h.publishSharingNotice(tx, p, row)
		}
		return sharingNoticeReadAudit(tx, p, row)
	})
	if err != nil {
		sharingNoticeError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *patientHandlers) getSharingNotice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("notice"), true) {
		respond.NotFound(c, "sharing notice")
		return
	}
	var row sharingNotice
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := readSharingNotice(tx, p, hospital, c.Param("notice"), &row); err != nil {
			return err
		}
		return sharingNoticeReadAudit(tx, p, row)
	})
	if err != nil {
		sharingNoticeError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *patientHandlers) approveSharingNotice(c *gin.Context) { h.changeSharingNotice(c, true) }
func (h *patientHandlers) retireSharingNotice(c *gin.Context)  { h.changeSharingNotice(c, false) }
func (h *patientHandlers) changeSharingNotice(c *gin.Context, approve bool) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("notice"), true) {
		respond.NotFound(c, "sharing notice")
		return
	}
	var version int64
	reference := ""
	if approve {
		var in struct {
			ExpectedVersion int64  `json:"expected_version"`
			ReviewReference string `json:"review_reference"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		if in.ExpectedVersion != 1 || !validReviewUUID(in.ReviewReference, true) {
			respond.BadRequest(c, errors.New("invalid notice approval"))
			return
		}
		version = in.ExpectedVersion
		reference = in.ReviewReference
	} else {
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		if in.ExpectedVersion < 1 || in.ExpectedVersion > 3 {
			respond.BadRequest(c, errors.New("invalid notice version"))
			return
		}
		version = in.ExpectedVersion
	}
	var row sharingNotice
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := readSharingNotice(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, hospital, c.Param("notice"), &row); err != nil {
			return err
		}
		if approve {
			if row.CreatorSubject == p.Subject {
				return errConsentAuthorityConflict
			}
			if row.ApprovedAt != nil {
				if row.ReviewerSubject == nil || *row.ReviewerSubject != p.Subject || row.ReviewReference == nil || row.ReviewReference.String() != reference {
					return errConsentAuthorityConflict
				}
				return sharingNoticeReadAudit(tx, p, row)
			}
			if row.Status != "draft" || row.Version != version {
				return errConsentAuthorityConflict
			}
			result := tx.Exec(`UPDATE patient_sharing_notices SET status='approved',version=2,approved_at=clock_timestamp(),updated_at=clock_timestamp(),reviewer_subject=?,review_reference=? WHERE tenant_id=? AND hospital_id=? AND id=? AND status='draft' AND version=1`, p.Subject, reference, p.TenantID, hospital, row.ID)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errConsentAuthorityConflict
			}
		} else {
			if row.Status == "retired" {
				if version != row.Version && version != row.Version-1 {
					return errConsentAuthorityConflict
				}
				return sharingNoticeReadAudit(tx, p, row)
			}
			if row.Version != version {
				return errConsentAuthorityConflict
			}
			result := tx.Exec(`UPDATE patient_sharing_notices SET status='retired',version=version+1,retired_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`, p.TenantID, hospital, row.ID, version)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errConsentAuthorityConflict
			}
		}
		if err := readSharingNotice(tx, p, hospital, c.Param("notice"), &row); err != nil {
			return err
		}
		return h.publishSharingNotice(tx, p, row)
	})
	if err != nil {
		sharingNoticeError(c, err)
		return
	}
	respond.OK(c, row)
}

func sharingNoticeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errConsentAuthorityConflict):
		respond.Error(c, 409, "sharing_notice_conflict", "Sharing notice state or input changed")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "sharing notice")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		respond.Error(c, 503, "sharing_notice_unavailable", "Sharing notice unavailable; reconcile submitted commands before retrying")
	default:
		respond.InternalErr(c, errors.New("sharing notice operation failed"), "Sharing notice operation unavailable")
	}
}
