package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type personEvidenceSnapshot struct {
	Permission                     authz.Permission
	Grant                          authz.ScopeGrant
	Actor                          reviewedLeaveActor
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
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'revision_id',?::uuid,'file_id',?::uuid,'assignment_id',?::text,'assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint,'actor_link_id',?::uuid,'actor_link_version',?::bigint,'actor_employment_epoch',?::bigint))`, p.TenantID, hospital, p.Subject, action, snapshot.Review, snapshot.ReviewVersion, snapshot.Revision, snapshot.Evidence.DocumentID, snapshot.Grant.AssignmentID, snapshot.Grant.Version, snapshot.Grant.ScopeEpoch, snapshot.Grant.MembershipEpoch, snapshot.Actor.ID, snapshot.Actor.Version, snapshot.ActorEpoch).Error
}

func (h *handlers) authorizePersonEvidence(c *gin.Context, p authn.Principal, hospital, staff, review, file uuid.UUID, original *personEvidenceSnapshot) (personEvidenceSnapshot, error) {
	var result personEvidenceSnapshot
	check := func(permission authz.Permission) error {
		return h.personReviewTransaction(c, p, hospital, staff, permission, func(tx *gorm.DB, source IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
			var actorEpoch int64
			if err := tx.Table("workforce_staff").Select("employment_epoch").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, actor.StaffID).Take(&actorEpoch).Error; err != nil {
				return err
			}
			if original != nil && (original.Permission != permission || original.Grant != grant || original.Actor != actor || original.ActorEpoch != actorEpoch) {
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
			if stored.UploadedAt == nil || actorEpoch < 1 {
				return errPersonReviewConflict
			}
			binding.ScannedAt = binding.ScannedAt.UTC()
			binding.DefinitionsUpdatedAt = binding.DefinitionsUpdatedAt.UTC()
			result = personEvidenceSnapshot{Permission: permission, Grant: grant, Actor: actor, ActorEpoch: actorEpoch, Review: review, Revision: row.RevisionID, ReviewVersion: row.Version, EmploymentEpoch: source.EmploymentEpoch, IdentityHash: source.IdentityHash, Evidence: *binding, UploadedAt: stored.UploadedAt.UTC()}
			if original != nil && result != *original {
				return errPersonReviewConflict
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
	if h.documents == nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if !p.IdleDeadline.IsZero() {
		var deadline context.CancelFunc
		ctx, deadline = context.WithDeadline(ctx, p.IdleDeadline)
		defer deadline()
	}
	object := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, OwnerKind: "staff", OwnerID: staff, RecordID: staff, FileID: file, Version: 1, UploadedAt: snapshot.UploadedAt}
	content, err := h.documents.ReadReleased(ctx, object, snapshot.Evidence.ReleasedGeneration)
	if err != nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	digest := sha256.Sum256(content.Bytes)
	if content.Generation != snapshot.Evidence.ReleasedGeneration || content.Size != snapshot.Evidence.Size || int64(len(content.Bytes)) != snapshot.Evidence.Size || content.SHA256 != snapshot.Evidence.Sha256 || hex.EncodeToString(digest[:]) != snapshot.Evidence.Sha256 {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	if _, err := h.authorizePersonEvidence(c, p, hospital, staff, review, file, &snapshot); err != nil {
		personReviewError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": snapshot.Evidence.Filename}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, snapshot.Evidence.ContentType, content.Bytes)
}
