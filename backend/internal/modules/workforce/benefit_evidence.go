package workforce

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const benefitEvidenceKind = "benefit_proposal"

// benefitEvidence gates the eligibility evidence of the one coverage proposal named by the path: locked benefits managers attach, benefits readers read.
type benefitEvidence struct{ enrollment, proposal uuid.UUID }

func (e benefitEvidence) authorize(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, writing bool) error {
	var err error
	if writing {
		err = benefitsWrite(tx, p, hospital, "staff:"+staffID.String())
	} else {
		err = benefitsRead(tx, p.TenantID, hospital)
	}
	if err != nil {
		return err
	}
	return requireHospitalStaff(tx, p.TenantID, hospital, staffID)
}

func (e benefitEvidence) bind(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) (int64, error) {
	if kind != benefitEvidenceKind || record != e.proposal {
		return 0, gorm.ErrRecordNotFound
	}
	var r struct {
		BaseVersion int64
		Status      string
	}
	if err := e.row(tx, p, hospital, staffID).Select("base_version,status").Clauses(clause.Locking{Strength: "SHARE"}).Take(&r).Error; err != nil {
		return 0, err
	}
	if r.Status != "pending" {
		return 0, errBenefitState
	}
	return r.BaseVersion + 1, nil
}

func (e benefitEvidence) owns(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) error {
	if kind != benefitEvidenceKind || record != e.proposal {
		return gorm.ErrRecordNotFound
	}
	return e.row(tx, p, hospital, staffID).Select("id").Take(&struct{ ID uuid.UUID }{}).Error
}

func (benefitEvidence) downloadAction() string { return "workforce.benefits.evidence_downloaded" }

func (e benefitEvidence) row(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID) *gorm.DB {
	return tx.Table("workforce_benefit_enrollment_proposals").
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND enrollment_id=? AND id=?", p.TenantID, hospital, staffID, e.enrollment, e.proposal)
}

func benefitEvidenceParams(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, benefitEvidence, bool) {
	p, hospital, ok := scope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, benefitEvidence{}, false
	}
	ids, ok := benefitIDs(c, "id", "enrollment", "proposal")
	if !ok {
		return p, uuid.Nil, uuid.Nil, benefitEvidence{}, false
	}
	return p, hospital, ids[0], benefitEvidence{enrollment: ids[1], proposal: ids[2]}, true
}

func benefitEvidenceFile(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("file"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "benefit evidence")
		return uuid.Nil, false
	}
	return id, true
}

func (h *handlers) listBenefitEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := benefitEvidenceParams(c)
	if !ok {
		return
	}
	rows := []document{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := gate.authorize(tx, p, hospital, staff, false); err != nil {
			return err
		}
		if err := gate.owns(tx, p, hospital, staff, benefitEvidenceKind, gate.proposal); err != nil {
			return err
		}
		return evidenceRows(tx, benefitEvidenceKind, p.TenantID, hospital, staff, gate.proposal, &rows)
	})
	if documentUploadError(c, err) {
		return
	}
	respond.OK(c, gin.H{"evidence": rows, "uploads_available": h.documents != nil})
}

func (h *handlers) putBenefitEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := benefitEvidenceParams(c)
	if !ok {
		return
	}
	id, ok := benefitEvidenceFile(c)
	if !ok {
		return
	}
	var in benefitEvidenceInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	if !validDocumentFile(&in.Filename, in.Sha256, in.Size) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose a PNG or JPEG of up to 10 MiB with a safe file name.")
		return
	}
	h.registerDocument(c, gate, p, hospital, staff, id, documentInput{RecordKind: benefitEvidenceKind, RecordID: gate.proposal, Category: "benefit_eligibility_evidence",
		Filename: in.Filename, ContentType: in.ContentType, Size: in.Size, SHA256: in.Sha256})
}

func (h *handlers) uploadBenefitEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := benefitEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := benefitEvidenceFile(c); ok {
		h.receiveDocument(c, gate, p, hospital, staff, id)
	}
}

func (h *handlers) retryBenefitEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := benefitEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := benefitEvidenceFile(c); ok {
		h.requeueDocument(c, gate, p, hospital, staff, id)
	}
}

func (h *handlers) downloadBenefitEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := benefitEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := benefitEvidenceFile(c); ok {
		h.sendDocument(c, gate, p, hospital, staff, id)
	}
}
