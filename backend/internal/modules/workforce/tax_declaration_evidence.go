package workforce

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const taxEvidenceKind = "tax_declaration"
const taxEvidenceLimit = 10

var errDeclarationEvidenceScanning = errors.New("tax declaration evidence is still being scanned")

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
		// Expired intents never received bytes and no longer count towards the limit.
		return tx.Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND record_kind=? AND record_id=? AND NOT(status='upload_pending' AND expires_at<=clock_timestamp())",
			p.TenantID, hospital, staff, taxEvidenceKind, revision).Order("created_at DESC,id DESC").Limit(taxEvidenceLimit).Find(&rows).Error
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

// settleDeclarationEvidence freezes a proposal's evidence at its decision and revokes what was readable unless the proposal took effect.
func settleDeclarationEvidence(tx *gorm.DB, tenant string, hospital, staff, proposal uuid.UUID, to string) error {
	var scanning int64
	if err := tx.Table("workforce_documents").Where("tenant_id=? AND hospital_id=? AND record_kind=? AND record_id=? AND status='scan_pending'", tenant, hospital, taxEvidenceKind, proposal).Count(&scanning).Error; err != nil {
		return err
	}
	if scanning > 0 {
		return errDeclarationEvidenceScanning
	}
	var settled []document
	if err := tx.Raw(`UPDATE workforce_documents SET frozen=true,status=CASE WHEN ?::text<>'approved' AND status='available' THEN 'revoked' ELSE status END,updated_at=clock_timestamp()
	 WHERE tenant_id=? AND hospital_id=? AND record_kind=? AND record_id=? AND NOT frozen RETURNING `+documentColumns, to, tenant, hospital, taxEvidenceKind, proposal).Scan(&settled).Error; err != nil {
		return err
	}
	for _, d := range settled {
		if d.Status != "revoked" {
			continue
		}
		if err := documentStateEvent(tx, workforcecontract.DocumentScanInput{TenantID: tenant, HospitalID: hospital.String(), StaffID: staff.String(), FileID: d.ID.String(), ScanAttempt: d.ScanAttempt}, "revoked"); err != nil {
			return err
		}
	}
	return nil
}
