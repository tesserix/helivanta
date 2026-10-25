package patientmaster

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

var errSharingRecipientConflict = errors.New("recipient input or state changed")
var errSharingRecipientInvalid = errors.New("recipient validity invalid")

// SharingRecipientContent pins a reviewed representative; it is not a facility master.
type SharingRecipientContent struct {
	OrganizationReference     string    `json:"organization_reference"`
	DisplayName               string    `json:"display_name"`
	GranteeSubject            string    `json:"grantee_subject"`
	PolicyReference           string    `json:"policy_reference"`
	IdentityEvidenceReference string    `json:"identity_evidence_reference"`
	ValidUntil                time.Time `json:"valid_until"`
}

func (in SharingRecipientContent) valid() bool {
	text := func(s string, n int) bool {
		return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= n && !strings.ContainsRune(s, 0)
	}
	return text(in.OrganizationReference, 200) && text(in.DisplayName, 200) && text(in.GranteeSubject, 512) && strings.TrimSpace(in.GranteeSubject) == in.GranteeSubject && validReviewUUID(in.PolicyReference, true) && validReviewUUID(in.IdentityEvidenceReference, true) && !in.ValidUntil.IsZero() && in.ValidUntil.Year() <= 9999 && in.ValidUntil.Nanosecond()%1000 == 0
}

type sharingRecipient struct {
	ID                      uuid.UUID `json:"id"`
	HospitalID              uuid.UUID `json:"hospital_id"`
	SharingRecipientContent `gorm:"embedded"`
	ContentHash             string     `json:"content_hash"`
	CreatorSubject          string     `json:"-"`
	ReviewerSubject         *string    `json:"-"`
	ReviewReference         *uuid.UUID `json:"-"`
	RevokerSubject          *string    `json:"-"`
	RevocationReference     *uuid.UUID `json:"-"`
	Status                  string     `json:"status"`
	Version                 int64      `json:"version"`
	ApprovedAt              *time.Time `json:"approved_at"`
	RevokedAt               *time.Time `json:"revoked_at"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

func (sharingRecipient) TableName() string { return "patient_sharing_recipients" }

const sharingRecipientColumns = "id,hospital_id,organization_reference,display_name,grantee_subject,policy_reference,identity_evidence_reference,valid_until,content_hash,creator_subject,reviewer_subject,review_reference,revoker_subject,revocation_reference,status,version,approved_at,revoked_at,created_at,updated_at"

func readSharingRecipient(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, row *sharingRecipient) error {
	err := tx.Select(sharingRecipientColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(row).Error
	if err == nil {
		row.ValidUntil = row.ValidUntil.UTC()
	}
	return err
}
func sharingRecipientReadAudit(tx *gorm.DB, p authn.Principal, row sharingRecipient) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','patient_sharing_recipients',?)`, p.TenantID, p.Subject, row.ID).Error
}
func (h *patientHandlers) publishSharingRecipient(tx *gorm.DB, p authn.Principal, row sharingRecipient) error {
	data, err := json.Marshal(patientmastercontract.SharingRecipientChangedData{RecipientID: row.ID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, patientmastercontract.SubjectSharingRecipientChanged, events.Event{Type: patientmastercontract.SubjectSharingRecipientChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
func sharingRecipientError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errSharingRecipientConflict):
		respond.Error(c, 409, "sharing_recipient_conflict", "Recipient input or state changed")
	case errors.Is(err, errSharingRecipientInvalid):
		respond.BadRequest(c, errors.New("invalid recipient validity"))
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "sharing recipient")
	default:
		respond.Error(c, 503, "sharing_recipient_unavailable", "Sharing recipient unavailable; reconcile submitted commands before retrying")
	}
}
func (h *patientHandlers) putSharingRecipient(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("recipient"), true) {
		respond.NotFound(c, "sharing recipient")
		return
	}
	var in SharingRecipientContent
	if !decodePatientInput(c, &in) {
		return
	}
	in.ValidUntil = in.ValidUntil.UTC()
	if !in.valid() {
		respond.BadRequest(c, errors.New("invalid sharing recipient"))
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		sharingRecipientError(c, err)
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row sharingRecipient
	created := false
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
			return err
		}
		existing := readSharingRecipient(tx, p, hospital, c.Param("recipient"), &row)
		if existing == nil {
			if row.ContentHash != hash || row.CreatorSubject != p.Subject {
				return errSharingRecipientConflict
			}
			return sharingRecipientReadAudit(tx, p, row)
		}
		if !errors.Is(existing, gorm.ErrRecordNotFound) {
			return existing
		}
		if !in.ValidUntil.After(now) {
			return errSharingRecipientInvalid
		}
		result := tx.Exec(`INSERT INTO patient_sharing_recipients(tenant_id,hospital_id,id,organization_reference,display_name,grantee_subject,policy_reference,identity_evidence_reference,valid_until,content_hash,creator_subject) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, p.TenantID, hospital, c.Param("recipient"), in.OrganizationReference, in.DisplayName, in.GranteeSubject, in.PolicyReference, in.IdentityEvidenceReference, in.ValidUntil, hash, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readSharingRecipient(tx, p, hospital, c.Param("recipient"), &row); err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var count int64
			if err := tx.Model(&sharingRecipient{}).Where("tenant_id=? AND hospital_id=? AND organization_reference=? AND grantee_subject=? AND status IN ('draft','approved')", p.TenantID, hospital, in.OrganizationReference, in.GranteeSubject).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errSharingRecipientConflict
			}
			return gorm.ErrRecordNotFound
		}
		if row.ContentHash != hash || row.CreatorSubject != p.Subject {
			return errSharingRecipientConflict
		}
		if created {
			return h.publishSharingRecipient(tx, p, row)
		}
		return sharingRecipientReadAudit(tx, p, row)
	})
	if err != nil {
		sharingRecipientError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *patientHandlers) getSharingRecipient(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("recipient"), true) {
		respond.NotFound(c, "sharing recipient")
		return
	}
	var row sharingRecipient
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := readSharingRecipient(tx, p, hospital, c.Param("recipient"), &row); err != nil {
			return err
		}
		return sharingRecipientReadAudit(tx, p, row)
	})
	if err != nil {
		sharingRecipientError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *patientHandlers) approveSharingRecipient(c *gin.Context) { h.changeSharingRecipient(c, true) }
func (h *patientHandlers) revokeSharingRecipient(c *gin.Context)  { h.changeSharingRecipient(c, false) }
func (h *patientHandlers) changeSharingRecipient(c *gin.Context, approve bool) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("recipient"), true) {
		respond.NotFound(c, "sharing recipient")
		return
	}
	var version int64
	reference := ""
	if approve {
		var in struct {
			ExpectedVersion   int64  `json:"expected_version"`
			IdentityConfirmed *bool  `json:"identity_confirmed"`
			ReviewReference   string `json:"review_reference"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		if in.ExpectedVersion != 1 || in.IdentityConfirmed == nil || !*in.IdentityConfirmed || !validReviewUUID(in.ReviewReference, true) {
			respond.BadRequest(c, errors.New("invalid recipient review"))
			return
		}
		version = in.ExpectedVersion
		reference = in.ReviewReference
	} else {
		var in struct {
			ExpectedVersion int64  `json:"expected_version"`
			ReasonReference string `json:"reason_reference"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		if in.ExpectedVersion < 1 || in.ExpectedVersion > 3 || !validReviewUUID(in.ReasonReference, true) {
			respond.BadRequest(c, errors.New("invalid recipient revocation"))
			return
		}
		version = in.ExpectedVersion
		reference = in.ReasonReference
	}
	var row sharingRecipient
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := readSharingRecipient(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, hospital, c.Param("recipient"), &row); err != nil {
			return err
		}
		if approve {
			if row.CreatorSubject == p.Subject || row.GranteeSubject == p.Subject {
				return errSharingRecipientConflict
			}
			if row.ApprovedAt != nil {
				if row.ReviewerSubject == nil || *row.ReviewerSubject != p.Subject || row.ReviewReference == nil || row.ReviewReference.String() != reference {
					return errSharingRecipientConflict
				}
				return sharingRecipientReadAudit(tx, p, row)
			}
			var now time.Time
			if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
				return err
			}
			if row.Status != "draft" || row.Version != version || !row.ValidUntil.After(now) {
				return errSharingRecipientConflict
			}
			result := tx.Exec(`UPDATE patient_sharing_recipients SET status='approved',version=2,reviewer_subject=?,review_reference=?,approved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=1 AND status='draft'`, p.Subject, reference, p.TenantID, hospital, row.ID)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errSharingRecipientConflict
			}
		} else {
			if row.Status == "revoked" {
				if row.RevocationReference == nil || row.RevocationReference.String() != reference || (version != row.Version && version != row.Version-1) {
					return errSharingRecipientConflict
				}
				return sharingRecipientReadAudit(tx, p, row)
			}
			if row.Version != version {
				return errSharingRecipientConflict
			}
			result := tx.Exec(`UPDATE patient_sharing_recipients SET status='revoked',version=version+1,revoker_subject=?,revocation_reference=?,revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`, p.Subject, reference, p.TenantID, hospital, row.ID, version)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errSharingRecipientConflict
			}
		}
		if err := readSharingRecipient(tx, p, hospital, c.Param("recipient"), &row); err != nil {
			return err
		}
		return h.publishSharingRecipient(tx, p, row)
	})
	if err != nil {
		sharingRecipientError(c, err)
		return
	}
	respond.OK(c, row)
}
