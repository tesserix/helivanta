package workforce

import (
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const maxEmploymentEvidenceDownloads = 4

type employmentReviewEvidenceSnapshot struct {
	AuthorityHash, BasisHash [32]byte
	File                     releasedReviewFile
	UploadedAt               time.Time
}

func employmentReviewEvidenceAudit(tx *gorm.DB, p authn.Principal, row EmploymentReview, file uuid.UUID, authority *intakeAuthority, completed bool) error {
	action := "workforce.employment_review.evidence.authorized"
	if completed {
		action = "workforce.employment_review.evidence.downloaded"
	}
	grants, err := json.Marshal(authority.Grants)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'revision_id',?::uuid,'file_id',?::uuid,'grants',?::jsonb,'actor_staff_id',?::uuid,'actor_link_id',?::uuid,'actor_link_version',?::bigint,'actor_employment_epoch',?::bigint))`, p.TenantID, row.HospitalID, p.Subject, action, row.ID, row.Version, row.RevisionID, file, string(grants), authority.Actor.Staff, authority.Actor.Link, authority.Actor.LinkVersion, authority.Actor.EmploymentEpoch).Error
}

func (h *handlers) authorizeEmploymentReviewEvidence(c *gin.Context, p authn.Principal, hospital, staff, review, file uuid.UUID, original *employmentReviewEvidenceSnapshot) (employmentReviewEvidenceSnapshot, error) {
	var snapshot employmentReviewEvidenceSnapshot
	err := h.employmentReviewRead(c, p, hospital, staff, review, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		if authority == nil || !authority.Scoped {
			return authz.ErrScopedCommandDenied
		}
		row, err := loadEmploymentReview(tx, p, hospital, staff, review, uuid.Nil)
		if err != nil {
			return err
		}
		state, err := employmentReviewState(tx, p, source, row.EmploymentReview)
		if err != nil {
			return err
		}
		if !contains(state, "pending", "approved") {
			return errEmploymentReviewConflict
		}
		var selected *EmploymentReviewEvidence
		for i := range row.Evidence {
			if row.Evidence[i].Binding.DocumentID == file {
				selected = &row.Evidence[i]
				break
			}
		}
		if selected == nil {
			return gorm.ErrRecordNotFound
		}
		version := selected.Binding.Version
		binding, current, err := employmentReviewFile(tx, p, source, row.Terms, EmploymentReviewEvidenceSelection{ID: selected.Binding.ID, RevisionID: selected.Binding.RevisionID, Version: &version})
		if err != nil {
			return err
		}
		if current.ClassificationVersion != selected.ClassificationVersion || current.ClassificationRevisionID != selected.ClassificationRevisionID {
			return errEmploymentReviewConflict
		}
		var uploaded *time.Time
		if err := tx.Table("workforce_documents").Select("uploaded_at").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, file).Take(&uploaded).Error; err != nil {
			return err
		}
		if uploaded == nil {
			return errEmploymentReviewConflict
		}
		generation, err := strconv.ParseInt(binding.ReleasedGeneration, 10, 64)
		if err != nil || generation < 1 {
			return errEmploymentReviewConflict
		}
		authorityBytes, err := json.Marshal(authority)
		if err != nil {
			return err
		}
		basisBytes, err := json.Marshal(struct {
			Review       EmploymentReview
			Binding      employmentReviewBinding
			UploadedAt   time.Time
			StaffVersion int64
			Epoch        int64
			IdentityHash string
		}{row.EmploymentReview, binding, uploaded.UTC(), source.StaffVersion, source.EmploymentEpoch, source.IdentityHash})
		if err != nil {
			return err
		}
		snapshot = employmentReviewEvidenceSnapshot{AuthorityHash: sha256.Sum256(authorityBytes), BasisHash: sha256.Sum256(basisBytes), File: releasedReviewFile{Filename: binding.Filename, ContentType: binding.ContentType, SHA256: binding.Sha256, Size: binding.Size, Generation: generation}, UploadedAt: uploaded.UTC()}
		if original != nil {
			if snapshot.AuthorityHash != original.AuthorityHash {
				return authz.ErrScopedCommandDenied
			}
			if snapshot.BasisHash != original.BasisHash || snapshot.File != original.File || !snapshot.UploadedAt.Equal(original.UploadedAt) {
				return errEmploymentReviewConflict
			}
		}
		return employmentReviewEvidenceAudit(tx, p, row.EmploymentReview, file, authority, original != nil)
	})
	return snapshot, err
}

func (h *handlers) downloadEmploymentReviewEvidence(c *gin.Context) {
	p, hospital, staff, review, ok := personReviewParams(c)
	if !ok {
		return
	}
	file, err := uuid.Parse(c.Param("file"))
	if err != nil || file == uuid.Nil {
		respond.NotFound(c, "employment review evidence")
		return
	}
	snapshot, err := h.authorizeEmploymentReviewEvidence(c, p, hospital, staff, review, file, nil)
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	select {
	case h.employmentEvidenceSlots <- struct{}{}:
		defer func() { <-h.employmentEvidenceSlots }()
	default:
		respond.Error(c, 503, "employment_review_busy", "Evidence downloads are busy. Retry shortly.")
		return
	}
	content, err := h.readReviewEvidence(c, p, hospital, staff, file, snapshot.UploadedAt, snapshot.File)
	if err != nil {
		documentUploadError(c, err)
		return
	}
	if _, err := h.authorizeEmploymentReviewEvidence(c, p, hospital, staff, review, file, &snapshot); err != nil {
		employmentReviewError(c, err)
		return
	}
	sendReviewEvidence(c, snapshot.File, content)
}
