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

const taxEvidenceKind = "tax_declaration"

// taxEvidence gates one tax year's declaration evidence: payroll managers attach to a pending proposal, payroll viewers read.
type taxEvidence struct{ year string }

func (e taxEvidence) authorize(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, writing bool) error {
	if writing {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", taxDeclarationsLock+":"+staffID.String()); err != nil {
			return err
		}
	} else {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_payroll_read','authorized',true)`).Error; err != nil {
			return err
		}
	}
	return requireHospitalStaff(tx, p.TenantID, hospital, staffID)
}

func (e taxEvidence) bind(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) (int64, error) {
	if kind != taxEvidenceKind {
		return 0, gorm.ErrRecordNotFound
	}
	var r struct {
		Sequence int64
		Status   string
	}
	if err := tx.Table("workforce_staff_tax_declaration_revisions").Select("sequence,status").Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND id=?", p.TenantID, hospital, staffID, e.year, record).Take(&r).Error; err != nil {
		return 0, err
	}
	if r.Status != "pending" {
		return 0, errDeclarationState
	}
	return r.Sequence, nil
}

func (e taxEvidence) owns(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) error {
	if kind != taxEvidenceKind {
		return gorm.ErrRecordNotFound
	}
	return tx.Table("workforce_staff_tax_declaration_revisions").Select("id").
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND id=?", p.TenantID, hospital, staffID, e.year, record).Take(&struct{ ID uuid.UUID }{}).Error
}

func (taxEvidence) downloadAction() string { return "workforce.tax_declaration.evidence_downloaded" }

func taxEvidenceParams(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, taxEvidence, bool) {
	p, hospital, ok := scope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, taxEvidence{}, false
	}
	staff, ok := staffParam(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, taxEvidence{}, false
	}
	year, ok := declarationYearParam(c)
	return p, hospital, staff, taxEvidence{year: year}, ok
}

func taxEvidenceFile(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("file"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "declaration evidence")
		return uuid.Nil, false
	}
	return id, true
}

func (h *handlers) listTaxEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := taxEvidenceParams(c)
	if !ok {
		return
	}
	revision, err := uuid.Parse(c.Query("revision_id"))
	if err != nil || revision == uuid.Nil {
		respond.NotFound(c, "tax declaration proposal")
		return
	}
	rows := []document{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := gate.authorize(tx, p, hospital, staff, false); err != nil {
			return err
		}
		if err := gate.owns(tx, p, hospital, staff, taxEvidenceKind, revision); err != nil {
			return err
		}
		return evidenceRows(tx, taxEvidenceKind, p.TenantID, hospital, staff, revision, &rows)
	})
	if documentUploadError(c, err) {
		return
	}
	respond.OK(c, gin.H{"evidence": rows, "uploads_available": h.documents != nil})
}

func (h *handlers) putTaxEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := taxEvidenceParams(c)
	if !ok {
		return
	}
	id, ok := taxEvidenceFile(c)
	if !ok {
		return
	}
	var in taxEvidenceInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	if in.RevisionID == uuid.Nil || !validDocumentFile(&in.Filename, in.Sha256, in.Size) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose a PNG or JPEG of up to 10 MiB with a safe file name.")
		return
	}
	h.registerDocument(c, gate, p, hospital, staff, id, documentInput{RecordKind: taxEvidenceKind, RecordID: in.RevisionID, Category: "tax_declaration_evidence",
		Filename: in.Filename, ContentType: in.ContentType, Size: in.Size, SHA256: in.Sha256})
}

func (h *handlers) uploadTaxEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := taxEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := taxEvidenceFile(c); ok {
		h.receiveDocument(c, gate, p, hospital, staff, id)
	}
}

func (h *handlers) retryTaxEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := taxEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := taxEvidenceFile(c); ok {
		h.requeueDocument(c, gate, p, hospital, staff, id)
	}
}

func (h *handlers) downloadTaxEvidence(c *gin.Context) {
	p, hospital, staff, gate, ok := taxEvidenceParams(c)
	if !ok {
		return
	}
	if id, ok := taxEvidenceFile(c); ok {
		h.sendDocument(c, gate, p, hospital, staff, id)
	}
}
