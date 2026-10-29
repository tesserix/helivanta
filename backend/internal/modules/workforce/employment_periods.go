package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const employmentPeriodColumns = "id,revision_id,hospital_id,staff_id,version,status,staff_version,employment_epoch,engagement_kind,duration,starts_at,ends_at,contracting_party,COALESCE(sponsor_id::text,'') AS sponsor_id,reason,evidence_reference,proposed_by_subject,actor_subject,created_at,updated_at,input_hash"

var errEmploymentPeriodConflict = errors.New("employment draft or source changed")

type employmentPeriodRecord struct {
	EmploymentPeriodDraft
	InputHash string `json:"-"`
}

func (r EmploymentPeriodDraft) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type employmentPeriodRevision struct{ EmploymentPeriodDraft }

func (r employmentPeriodRevision) PageKey() (time.Time, uuid.UUID) { return r.UpdatedAt, r.ID }

func employmentPeriodScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return p, hospital, uuid.Nil, uuid.Nil, false
	}
	staff, err := uuid.Parse(c.Param("id"))
	if err != nil || staff == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, hospital, staff, uuid.Nil, false
	}
	var id uuid.UUID
	if c.Param("period") != "" {
		id, err = uuid.Parse(c.Param("period"))
		if err != nil || id.Version() != 7 {
			respond.NotFound(c, "employment draft")
			return p, hospital, staff, id, false
		}
	}
	c.Header("Cache-Control", "no-store")
	return p, hospital, staff, id, true
}

func employmentPeriodSource(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID) (EmploymentPeriodSource, error) {
	source := EmploymentPeriodSource{HospitalID: hospital, StaffID: staff}
	err := tx.Table("workforce_staff").Select("version AS staff_version,employment_epoch").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staff).Take(&source).Error
	return source, err
}
func employmentPeriodAudit(tx *gorm.DB, p authn.Principal, hospital, resource uuid.UUID, action string, version int64) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint))`, p.TenantID, hospital, p.Subject, action, resource, version).Error
}
func (h *handlers) employmentPeriodTransaction(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, fn func(*gorm.DB, EmploymentPeriodSource) error) error {
	return h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		// Match enrollment's hospital lock before Staff/sponsor row locks.
		if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
			return err
		}
		source, err := employmentPeriodSource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		return fn(tx, source)
	})
}
func respondEmploymentPeriodError(c *gin.Context, err error) {
	if errors.Is(err, errEmploymentPeriodConflict) {
		respond.Error(c, 409, "employment_draft_conflict", "The employment draft or Staff source changed. Reload before saving.")
		return
	}
	respondError(c, err)
}
func (h *handlers) getEmploymentPeriodSource(c *gin.Context) {
	p, hospital, staff, _, ok := employmentPeriodScope(c)
	if !ok {
		return
	}
	var result EmploymentPeriodSource
	err := h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, source EmploymentPeriodSource) error {
		result = source
		return employmentPeriodAudit(tx, p, hospital, staff, "workforce.employment_source.read", source.StaffVersion)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}
func normalizeEmploymentPeriod(in *EmploymentPeriodDraftInput, staff uuid.UUID) bool {
	in.ContractingParty = strings.TrimSpace(in.ContractingParty)
	in.Reason = strings.TrimSpace(in.Reason)
	in.EvidenceReference = strings.TrimSpace(in.EvidenceReference)
	in.StartsAt = in.StartsAt.UTC()
	if in.EndsAt != nil {
		end := in.EndsAt.UTC()
		in.EndsAt = &end
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || *in.ExpectedVersion >= 9007199254740990 || in.StaffVersion < 1 || in.StaffVersion > 9007199254740990 || in.EmploymentEpoch < 1 || in.EmploymentEpoch > 9007199254740990 {
		return false
	}
	if !contains(in.EngagementKind, "employee", "consultant", "contractor", "agency_worker", "locum", "trainee", "volunteer") || !contains(in.Duration, "fixed", "ongoing") || in.StartsAt.Year() < 1 || in.StartsAt.Year() > 9999 {
		return false
	}
	if in.Duration == "fixed" {
		if in.EndsAt == nil || !in.EndsAt.After(in.StartsAt) || in.EndsAt.Year() > 9999 {
			return false
		}
	} else if in.EndsAt != nil {
		return false
	}
	for _, field := range []struct {
		value    string
		min, max int
	}{{in.ContractingParty, 0, 200}, {in.Reason, 1, 500}, {in.EvidenceReference, 1, 300}} {
		n := utf8.RuneCountInString(field.value)
		if !utf8.ValidString(field.value) || n < field.min || n > field.max || strings.ContainsRune(field.value, '\x00') {
			return false
		}
	}
	if in.SponsorID != "" {
		sponsor, err := uuid.Parse(in.SponsorID)
		if err != nil || sponsor == uuid.Nil || sponsor == staff {
			return false
		}
		in.SponsorID = sponsor.String()
	}
	return !contains(in.EngagementKind, "contractor", "agency_worker") || (in.ContractingParty != "" && in.SponsorID != "")
}
func (h *handlers) putEmploymentPeriodDraft(c *gin.Context) {
	p, hospital, staff, id, ok := employmentPeriodScope(c)
	if !ok {
		return
	}
	var in EmploymentPeriodDraftInput
	if !decodeSized(c, &in, 8<<10) {
		return
	}
	if !normalizeEmploymentPeriod(&in, staff) {
		respond.Error(c, 400, "invalid_request", "Enter valid employment terms, dates, evidence reference and contractor sponsor.")
		return
	}
	raw, err := json.Marshal(in)
	if err != nil {
		respondError(c, err)
		return
	}
	digest := sha256.Sum256(raw)
	intent := hex.EncodeToString(digest[:])
	var result EmploymentPeriodDraft
	err = h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, source EmploymentPeriodSource) error {
		var receipt employmentPeriodRecord
		err := tx.Table("workforce_employment_period_history").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND revision_id=?", p.TenantID, hospital, in.CommandID).Take(&receipt).Error
		if err == nil {
			if receipt.ID != id || receipt.StaffID != staff || receipt.ActorSubject != p.Subject || receipt.InputHash != intent {
				return errEmploymentPeriodConflict
			}
			result = receipt.EmploymentPeriodDraft
			return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.receipt.read", result.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if source.StaffVersion != in.StaffVersion || source.EmploymentEpoch != in.EmploymentEpoch {
			return errEmploymentPeriodConflict
		}
		if in.SponsorID != "" {
			var sponsor staffRecordForEmployment
			if err := tx.Table("workforce_staff").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=? AND employment_status='active'", p.TenantID, hospital, in.SponsorID).Take(&sponsor).Error; err != nil {
				return err
			}
		}
		var current employmentPeriodRecord
		err = tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&current).Error
		values := map[string]any{"revision_id": in.CommandID, "version": *in.ExpectedVersion + 1, "staff_version": in.StaffVersion, "employment_epoch": in.EmploymentEpoch, "engagement_kind": in.EngagementKind, "duration": in.Duration, "starts_at": in.StartsAt, "ends_at": in.EndsAt, "contracting_party": in.ContractingParty, "sponsor_id": nil, "reason": in.Reason, "evidence_reference": in.EvidenceReference, "actor_subject": p.Subject, "input_hash": intent}
		if in.SponsorID != "" {
			values["sponsor_id"] = in.SponsorID
		}
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound) && *in.ExpectedVersion == 0:
			values["tenant_id"], values["hospital_id"], values["staff_id"], values["id"], values["status"], values["proposed_by_subject"] = p.TenantID, hospital, staff, id, "draft", p.Subject
			if err := tx.Table("workforce_employment_period_drafts").Create(values).Error; err != nil {
				return err
			}
		case err == nil && current.Version == *in.ExpectedVersion:
			changed := tx.Table("workforce_employment_period_drafts").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=?", p.TenantID, hospital, staff, id, *in.ExpectedVersion).Updates(values)
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return errEmploymentPeriodConflict
			}
		case err == nil || errors.Is(err, gorm.ErrRecordNotFound):
			return errEmploymentPeriodConflict
		default:
			return err
		}
		if err := tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&result).Error; err != nil {
			return err
		}
		return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.draft.saved", result.Version)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}

type staffRecordForEmployment struct{ ID uuid.UUID }

func (h *handlers) getEmploymentPeriodDraft(c *gin.Context) {
	p, hospital, staff, id, ok := employmentPeriodScope(c)
	if !ok {
		return
	}
	var result EmploymentPeriodDraft
	err := h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&result).Error; err != nil {
			return err
		}
		return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.read", result.Version)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *handlers) listEmploymentPeriodDrafts(c *gin.Context, params pagination.Params) ([]EmploymentPeriodDraft, error) {
	p, hospital, staff, _, ok := employmentPeriodScope(c)
	if !ok {
		return nil, nil
	}
	rows := []EmploymentPeriodDraft{}
	err := h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Find(&rows).Error; err != nil {
			return err
		}
		return employmentPeriodAudit(tx, p, hospital, staff, "workforce.employment_period.list", 0)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) employmentPeriodHistory(c *gin.Context, params pagination.Params) ([]employmentPeriodRevision, error) {
	p, hospital, staff, id, ok := employmentPeriodScope(c)
	if !ok {
		return nil, nil
	}
	rows := []employmentPeriodRevision{}
	err := h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, _ EmploymentPeriodSource) error {
		var current EmploymentPeriodDraft
		if err := tx.Table("workforce_employment_period_drafts").Select("id,version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&current).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "updated_at", Direction: pagination.NewestFirst}).Table("workforce_employment_period_history").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Find(&rows).Error; err != nil {
			return err
		}
		return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.history.read", current.Version)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return nil, nil
	}
	return rows, nil
}
