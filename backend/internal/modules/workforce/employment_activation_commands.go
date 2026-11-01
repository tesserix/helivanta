package workforce

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const employmentGenerationColumns = "id,revision_id,hospital_id,staff_id,version,review_id,review_revision_id,review_version,review_staff_version,review_employment_epoch,staff_version,employment_epoch,identity_hash,identity_epoch,starts_at,ends_at,access_intent,declared_subject,activated_by_subject,reason,activated_at,input_hash"

type employmentGenerationRecord struct {
	EmploymentGeneration
	IdentityEpoch int64  `json:"-"`
	InputHash     string `json:"-"`
}

func generationContext(tx *gorm.DB, staff uuid.UUID, mode string) error {
	return tx.Exec(`SELECT set_config('app.workforce_generation',?,true),set_config('app.workforce_generation_staff',?,true)`, mode, staff.String()).Error
}

func loadEmploymentGeneration(tx *gorm.DB, p authn.Principal, hospital, staff, id uuid.UUID) (employmentGenerationRecord, error) {
	var row employmentGenerationRecord
	if err := generationContext(tx, staff, "read"); err != nil {
		return row, err
	}
	err := tx.Table("workforce_employment_generations").Select(employmentGenerationColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&row).Error
	return row, err
}

func validActivationInput(in *EmploymentActivationInput) bool {
	if in.CommandID.Version() != 7 || in.ReviewID.Version() != 7 || in.ReviewRevisionID.Version() != 7 || !in.ActivationExamined || strings.TrimSpace(in.Reason) != in.Reason || !validBenefitText(in.Reason, 1, 500) {
		return false
	}
	for _, version := range []*int64{in.ReviewVersion, in.StaffVersion, in.EmploymentEpoch} {
		if version == nil || *version < 1 || *version > 9007199254740989 {
			return false
		}
	}
	return true
}

func (h *handlers) putEmploymentActivation(c *gin.Context) {
	if !h.activationAvailable(c) {
		return
	}
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("activation"))
	if err != nil || id.Version() != 7 || id.String() != c.Param("activation") {
		respond.NotFound(c, "employment activation")
		return
	}
	var in EmploymentActivationInput
	if !decode(c, &in) {
		return
	}
	if !validActivationInput(&in) {
		respond.Error(c, 400, "invalid_request", "Select an exact approved employment source, current Staff version and reason; confirm the activation review.")
		return
	}
	intent, err := personReviewIntent("employment_activate", in)
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	owner, supported := h.scopeAuthority.(authz.EmploymentActivationGuard)
	if !supported {
		respondIntakeError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	guard := &activationCommandGuard{owner: owner}
	var result EmploymentGenerationView
	err = h.employmentActivationWithGuard(c, p, hospital, staff, in.ReviewID, guard, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		var locked struct{ ID uuid.UUID }
		if err := tx.Table("workforce_staff").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staff).Take(&locked).Error; err != nil {
			return err
		}
		prior, err := loadEmploymentGeneration(tx, p, hospital, staff, id)
		if err == nil {
			if prior.InputHash != intent || prior.ActivatedBySubject != p.Subject || prior.RevisionID != in.CommandID {
				return errEmploymentReviewConflict
			}
			state, _, err := employmentGenerationState(tx, p, source, prior)
			if err != nil {
				return err
			}
			result = EmploymentGenerationView{Generation: prior.EmploymentGeneration, CurrentState: state}
			return employmentActivationAudit(tx, p, hospital, staff, id, prior.Version, "workforce.employment_activation.receipt.read", authority)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		review, err := loadEmploymentReview(tx, p, hospital, staff, in.ReviewID, uuid.Nil)
		if err != nil {
			return err
		}
		if review.RevisionID != in.ReviewRevisionID || review.Version != *in.ReviewVersion || source.StaffVersion != *in.StaffVersion || source.EmploymentEpoch != *in.EmploymentEpoch {
			return errEmploymentReviewConflict
		}
		readiness, err := employmentActivationSource(tx, p, source, review, authority.Actor)
		if err != nil {
			return err
		}
		if !readiness.CanActivate {
			return errEmploymentReviewConflict
		}
		var oldLinks bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND status IN ('pending','active'))`, p.TenantID, hospital, staff).Scan(&oldLinks).Error; err != nil {
			return err
		}
		if oldLinks {
			return errEmploymentReviewConflict
		}
		person, err := employmentReviewPerson(tx, p, source, review.PersonReviewID, review.PersonReviewRevisionID, review.PersonReviewVersion)
		if err != nil {
			return err
		}
		until := person.ValidUntil
		if review.Terms.EndsAt != nil && review.Terms.EndsAt.Before(until) {
			until = *review.Terms.EndsAt
		}
		if review.AccessIntent == "digital" {
			if err := guard.writer.ConsumeEmploymentBasis(guard.ctx, authz.EmploymentBasis{TenantID: p.TenantID, HospitalID: hospital, Subject: review.DeclaredSubject, StaffID: staff, GenerationID: id, EmploymentEpoch: source.EmploymentEpoch + 1, Until: until}); err != nil {
				return err
			}
		}
		if err := tx.Exec(`UPDATE workforce_staff SET employment_status='active',updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=? AND employment_epoch=? AND employment_status='inactive'`, p.TenantID, hospital, staff, source.StaffVersion, source.EmploymentEpoch).Error; err != nil {
			return err
		}
		if err := generationContext(tx, staff, "activate"); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_employment_generations(tenant_id,hospital_id,staff_id,id,revision_id,version,review_id,review_revision_id,review_version,review_staff_version,review_employment_epoch,staff_version,employment_epoch,identity_hash,identity_epoch,starts_at,ends_at,access_intent,declared_subject,activated_by_subject,reason,input_hash) VALUES(?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, staff, id, in.CommandID, review.ID, review.RevisionID, review.Version, source.StaffVersion, source.EmploymentEpoch, source.StaffVersion+1, source.EmploymentEpoch+1, source.IdentityHash, *review.IdentityEpoch, review.Terms.StartsAt, review.Terms.EndsAt, review.AccessIntent, review.DeclaredSubject, p.Subject, in.Reason, intent).Error; err != nil {
			return err
		}
		row, err := loadEmploymentGeneration(tx, p, hospital, staff, id)
		if err != nil {
			return err
		}
		result = EmploymentGenerationView{Generation: row.EmploymentGeneration, CurrentState: "effective"}
		if err := employmentActivationAudit(tx, p, hospital, staff, id, 1, "workforce.employment.activated", authority); err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.StaffEnrollmentChangedData{HospitalID: hospital.String(), StaffID: staff.String(), Version: row.StaffVersion})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, workforcecontract.SubjectStaffEnrollmentChanged, events.Event{Type: workforcecontract.SubjectStaffEnrollmentChanged, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, result)
}

func employmentGenerationState(tx *gorm.DB, p authn.Principal, source IdentitySource, g employmentGenerationRecord) (string, time.Time, error) {
	if source.EmploymentStatus != "active" || source.EmploymentEpoch != g.EmploymentEpoch || source.StaffVersion < g.StaffVersion || source.IdentityHash != g.IdentityHash {
		return "source_changed", time.Time{}, nil
	}
	if err := generationContext(tx, source.StaffID, "guard"); err != nil {
		return "", time.Time{}, err
	}
	var basis struct{ Epoch int64 }
	if err := tx.Table("workforce_identity_epochs").Select("epoch").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, source.HospitalID, source.StaffID).Take(&basis).Error; err != nil {
		return "", time.Time{}, err
	}
	if basis.Epoch != g.IdentityEpoch {
		return "identity_changed", time.Time{}, nil
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_evidence','read',true)`, g.ReviewID.String()).Error; err != nil {
		return "", time.Time{}, err
	}
	review, err := loadEmploymentReview(tx, p, source.HospitalID, source.StaffID, g.ReviewID, uuid.Nil)
	if err != nil {
		return "", time.Time{}, err
	}
	if review.RevisionID != g.ReviewRevisionID || review.Version != g.ReviewVersion || review.IdentityEpoch == nil || *review.IdentityEpoch != g.IdentityEpoch {
		return "source_changed", time.Time{}, nil
	}
	// The immutable consumption relation preserves the independently approved old basis.
	source.StaffVersion, source.EmploymentEpoch = g.ReviewStaffVersion, g.ReviewEmploymentEpoch
	state, err := employmentReviewState(tx, p, source, review.EmploymentReview)
	if err != nil || state != "approved" {
		return state, time.Time{}, err
	}
	state, err = employmentPersonIdentityBasis(tx, p, source, review.EmploymentReview, g.IdentityEpoch)
	if err != nil || state != "approved" {
		return state, time.Time{}, err
	}
	person, err := employmentReviewPerson(tx, p, source, review.PersonReviewID, review.PersonReviewRevisionID, review.PersonReviewVersion)
	if err != nil {
		return "", time.Time{}, err
	}
	until := person.ValidUntil
	if g.EndsAt != nil && g.EndsAt.Before(until) {
		until = *g.EndsAt
	}
	var now time.Time
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
		return "", time.Time{}, err
	}
	if now.Before(g.StartsAt) {
		return "not_started", until, nil
	}
	if !now.Before(until) {
		return "expired", until, nil
	}
	return "effective", until, nil
}
