package workforce

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type personEvidenceSnapshot struct {
	Permission                     authz.Permission
	Actor                          personReviewAuthority
	ActorEpoch                     int64
	Review, Revision               uuid.UUID
	ReviewVersion, EmploymentEpoch int64
	IdentityHash                   string
	Evidence                       PersonReviewEvidence
	UploadedAt                     time.Time
}

func personEvidenceAudit(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, snapshot personEvidenceSnapshot, released bool) error {
	action := "workforce.person_review.evidence.authorized"
	if released {
		action = "workforce.person_review.evidence.downloaded"
	}
	values, err := snapshot.Actor.auditValues()
	if err != nil {
		return err
	}
	values["version"], values["revision_id"], values["file_id"] = snapshot.ReviewVersion, snapshot.Revision, snapshot.Evidence.DocumentID
	if snapshot.Actor.Staff != nil {
		values["actor_employment_epoch"] = snapshot.ActorEpoch
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return tx.Exec("INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,?,?,?::jsonb)", p.TenantID, hospital, p.Subject, action, snapshot.Review, string(raw)).Error
}

func (h *handlers) authorizePersonEvidence(c *gin.Context, p authn.Principal, hospital, staff, review, file uuid.UUID, original *personEvidenceSnapshot) (personEvidenceSnapshot, error) {
	var result personEvidenceSnapshot
	check := func(permission authz.Permission) error {
		return h.personReviewTransaction(c, p, hospital, staff, permission, func(tx *gorm.DB, source IdentitySource, actor personReviewAuthority) error {
			var actorEpoch int64
			if actor.Staff != nil {
				if err := tx.Table("workforce_staff").Select("employment_epoch").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, actor.Staff.StaffID).Take(&actorEpoch).Error; err != nil {
					return err
				}
			}
			if original != nil && (original.Permission != permission || !original.Actor.same(actor) || original.ActorEpoch != actorEpoch) {
				return authz.ErrScopedCommandDenied
			}
			row, err := loadPersonReview(tx, p, hospital, staff, review, false, uuid.Nil)
			if err != nil {
				return err
			}
			state, err := personReviewState(tx, p, source, row.PersonReview)
			if err != nil {
				return err
			}
			if !contains(state, "pending", "approved") {
				return errPersonReviewConflict
			}
			var binding *PersonReviewEvidence
			for i := range row.Evidence {
				if row.Evidence[i].DocumentID == file {
					binding = &row.Evidence[i]
					break
				}
			}
			if binding == nil {
				return gorm.ErrRecordNotFound
			}
			var stored document
			if err := tx.Select(documentColumns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, file).Take(&stored).Error; err != nil {
				return err
			}
			if stored.UploadedAt == nil || actor.Staff != nil && actorEpoch < 1 {
				return errPersonReviewConflict
			}
			binding.ScannedAt = binding.ScannedAt.UTC()
			binding.DefinitionsUpdatedAt = binding.DefinitionsUpdatedAt.UTC()
			result = personEvidenceSnapshot{Permission: permission, Actor: actor, ActorEpoch: actorEpoch, Review: review, Revision: row.RevisionID, ReviewVersion: row.Version, EmploymentEpoch: source.EmploymentEpoch, IdentityHash: source.IdentityHash, Evidence: *binding, UploadedAt: stored.UploadedAt.UTC()}
			if original != nil {
				retained, current := *original, result
				retained.Actor, current.Actor = personReviewAuthority{}, personReviewAuthority{}
				if retained != current {
					return errPersonReviewConflict
				}
			}
			return personEvidenceAudit(tx, p, hospital, result, original != nil)
		})
	}
	if original != nil {
		err := check(original.Permission)
		return result, err
	}
	err := check(PermIdentityChecksManage)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		err = check(PermIdentityChecksReview)
	}
	return result, err
}

func (h *handlers) downloadPersonReviewEvidence(c *gin.Context) {
	p, hospital, staff, review, ok := personReviewParams(c)
	if !ok {
		return
	}
	file, err := uuid.Parse(c.Param("file"))
	if err != nil || file == uuid.Nil {
		respond.NotFound(c, "person review evidence")
		return
	}
	snapshot, err := h.authorizePersonEvidence(c, p, hospital, staff, review, file, nil)
	if err != nil {
		personReviewError(c, err)
		return
	}
	proof := releasedReviewFile{Filename: snapshot.Evidence.Filename, ContentType: snapshot.Evidence.ContentType, Size: snapshot.Evidence.Size, SHA256: snapshot.Evidence.Sha256, Generation: snapshot.Evidence.ReleasedGeneration}
	content, err := h.readReviewEvidence(c, p, hospital, staff, file, snapshot.UploadedAt, proof)
	if err != nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	if _, err := h.authorizePersonEvidence(c, p, hospital, staff, review, file, &snapshot); err != nil {
		personReviewError(c, err)
		return
	}
	sendReviewEvidence(c, proof, content)
}
