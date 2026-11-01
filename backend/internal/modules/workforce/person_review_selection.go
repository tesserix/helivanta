package workforce

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r PersonReviewEvidenceOption) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *handlers) searchPersonReviewEvidence(c *gin.Context, page pagination.Params) ([]PersonReviewEvidenceOption, error) {
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	var input PersonReviewEvidenceSelectionInput
	if !decodeSized(c, &input, 1024) {
		return nil, nil
	}
	if input.StaffVersion == nil || *input.StaffVersion < 1 || *input.StaffVersion > 9007199254740990 || input.EmploymentEpoch == nil || *input.EmploymentEpoch < 1 || *input.EmploymentEpoch > 9007199254740990 || !documentDigest.MatchString(input.IdentityHash) {
		respond.Error(c, 400, "invalid_request", "Refresh the current Staff identity details before choosing evidence.")
		return nil, nil
	}
	rows := []PersonReviewEvidenceOption{}
	err := h.personReviewTransaction(c, p, hospital, staff, PermIdentityChecksManage, func(tx *gorm.DB, source IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		if source.StaffVersion != *input.StaffVersion || source.EmploymentEpoch != *input.EmploymentEpoch || source.IdentityHash != input.IdentityHash {
			return errPersonReviewConflict
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_person_evidence_selection','authorized',true),set_config('app.workforce_person_evidence_version',?::bigint::text,true)`, source.StaffVersion).Error; err != nil {
			return err
		}
		query := tenantdb.ApplyKeyset(tx, page).Table("workforce_documents").Select("id,hospital_id,staff_id,record_version,filename,content_type,size,created_at").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND record_kind='enrollment' AND record_id=? AND category='identity_evidence' AND record_version=? AND status='available' AND content_type IN ('image/png','image/jpeg')", p.TenantID, hospital, staff, staff, source.StaffVersion).Where("source_generation IS NOT NULL AND released_generation IS NOT NULL AND scanned_at IS NOT NULL AND engine_version IS NOT NULL AND definition_version IS NOT NULL AND definitions_updated_at IS NOT NULL")
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if err := personReviewAudit(tx, p, hospital, row.ID, "workforce.person_review.evidence_selection.read", row.RecordVersion, actor, grant); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		personReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
