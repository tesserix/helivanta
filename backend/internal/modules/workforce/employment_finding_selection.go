package workforce

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strconv"
	"time"
)

const employmentFindingBasisKey = "workforce.employment_finding_basis"

func (r EmploymentPersonFindingOption) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func employmentFindingBasis(c *gin.Context) (EmploymentPersonFindingSearchInput, error) {
	var input EmploymentPersonFindingSearchInput
	query := c.Request.URL.Query()
	for key, values := range query {
		if len(values) != 1 || !contains(key, "hr_intake", "limit", "cursor", "staff_version", "employment_epoch") {
			return input, errIntakeProjection
		}
	}
	if query.Get("hr_intake") != "v1" {
		return input, errIntakeProjection
	}
	for key, destination := range map[string]**int64{"staff_version": &input.StaffVersion, "employment_epoch": &input.EmploymentEpoch} {
		value, err := strconv.ParseInt(query.Get(key), 10, 64)
		if err != nil || value < 1 || value > 9007199254740990 || strconv.FormatInt(value, 10) != query.Get(key) {
			return input, errIntakeProjection
		}
		*destination = &value
	}
	return input, nil
}
func (h *handlers) withEmploymentFindingSelection(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, input EmploymentPersonFindingSearchInput, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	return h.intakeAuthorityTransaction(c, p, hospital, []authz.Permission{PermEnrollmentManage, PermDocumentManage}, true, func(tx *gorm.DB, scoped bool) error {
		value, present := c.Get(intakeAuthorityKey)
		authority, ok := value.(*intakeAuthority)
		if !scoped || !present || !ok || authority == nil || !authority.Scoped {
			return authz.ErrScopedCommandDenied
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		if source.StaffVersion != *input.StaffVersion || source.EmploymentEpoch != *input.EmploymentEpoch {
			return errEmploymentReviewConflict
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_finding_selection','authorized',true)`).Error; err != nil {
			return err
		}
		return fn(tx, source, authority)
	})
}
func (h *handlers) employmentFindingCandidates(c *gin.Context, page pagination.Params) ([]EmploymentPersonFindingOption, error) {
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	input, err := employmentFindingBasis(c)
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	c.Set(employmentFindingBasisKey, input)
	rows := []EmploymentPersonFindingOption{}
	err = h.withEmploymentFindingSelection(c, p, hospital, staff, input, func(tx *gorm.DB, source IdentitySource, _ *intakeAuthority) error {
		return tenantdb.ApplyKeyset(tx, page).Table("workforce_person_reviews").Select("id,revision_id,hospital_id,staff_id,version,employment_epoch,identity_hash,policy_reference,finding_reference,valid_until,access_intent,declared_subject,created_at").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND status='approved' AND employment_epoch=? AND identity_hash=? AND valid_until>clock_timestamp()", p.TenantID, hospital, staff, source.EmploymentEpoch, source.IdentityHash).Find(&rows).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) authorizedEmploymentFindings(c *gin.Context, candidates []EmploymentPersonFindingOption) ([]EmploymentPersonFindingOption, error) {
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	value, present := c.Get(employmentFindingBasisKey)
	input, valid := value.(EmploymentPersonFindingSearchInput)
	if !present || !valid {
		employmentReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	rows := make([]EmploymentPersonFindingOption, 0, len(candidates))
	err := h.withEmploymentFindingSelection(c, p, hospital, staff, input, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		for _, candidate := range candidates {
			_, err := employmentReviewPerson(tx, p, source, candidate.ID, candidate.RevisionID, candidate.Version)
			if errors.Is(err, errEmploymentReviewPerson) || errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			rows = append(rows, candidate)
		}
		proof, err := json.Marshal(authority)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_review.person_selection.read',?,jsonb_build_object('authority',?::jsonb,'rows',?::int))`, p.TenantID, hospital, p.Subject, staff, string(proof), len(rows)).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
