package workforce

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func authorizedDocument(tx *gorm.DB, gate documentGate, p authn.Principal, hospital, staffID, fileID uuid.UUID, writing bool) (document, error) {
	if err := gate.authorize(tx, p, hospital, staffID, writing); err != nil {
		return document{}, err
	}
	var saved document
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, fileID).First(&saved).Error
	if err == nil {
		err = gate.owns(tx, p, hospital, staffID, saved.RecordKind, saved.RecordID)
	}
	return saved, err
}

func (h *handlers) revokeDocument(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(c.Param("file"))
	if err != nil || fileID == uuid.Nil {
		respond.NotFound(c, "staff document")
		return
	}
	var saved document
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedDocument(tx, staffDocuments{}, p, hospital, staffID, fileID, true)
		if err != nil {
			return err
		}
		if saved.Status == "revoked" {
			return nil
		}
		if saved.Status != "available" {
			return errDocumentConflict
		}
		res := tx.Raw(`UPDATE workforce_documents SET status='revoked',updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='available' RETURNING `+documentColumns, p.TenantID, hospital, staffID, fileID).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errDocumentConflict
		}
		return documentStateEvent(tx, workforcecontract.DocumentScanInput{TenantID: p.TenantID, HospitalID: hospital.String(), StaffID: staffID.String(), FileID: fileID.String(), ScanAttempt: saved.ScanAttempt}, "revoked")
	})
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "Only an available document can be revoked.")
		return
	}
	if documentUploadError(c, err) {
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) retryDocument(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(c.Param("file"))
	if err != nil || fileID == uuid.Nil {
		respond.NotFound(c, "staff document")
		return
	}
	h.requeueDocument(c, staffDocuments{}, p, hospital, staffID, fileID)
}

// requeueDocument starts the next processing attempt of a file whose scan ended in error.
func (h *handlers) requeueDocument(c *gin.Context, gate documentGate, p authn.Principal, hospital, staffID, fileID uuid.UUID) {
	attempt, err := strconv.ParseInt(c.Param("attempt"), 10, 64)
	if err != nil || attempt < 2 || strconv.FormatInt(attempt, 10) != c.Param("attempt") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "A canonical processing attempt greater than one is required.")
		return
	}
	if h.documents == nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	var saved document
	queued := false
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedDocument(tx, gate, p, hospital, staffID, fileID, true)
		if err != nil {
			return err
		}
		if saved.ScanAttempt == attempt {
			return nil
		}
		if saved.Status != "scan_error" || saved.ScanAttempt+1 != attempt {
			return errDocumentConflict
		}
		res := tx.Raw(`UPDATE workforce_documents SET status='scan_pending',scan_attempt=?,failure_code=NULL,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='scan_error' AND scan_attempt=? RETURNING `+documentColumns, attempt, p.TenantID, hospital, staffID, fileID, attempt-1).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errDocumentConflict
		}
		data, err := json.Marshal(workforcecontract.DocumentScanRequestedData{HospitalID: hospital.String(), StaffID: staffID.String(), FileID: fileID.String(), ScanAttempt: attempt})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, DocumentScanRequested, events.Event{Type: DocumentScanRequested, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		queued = true
		return nil
	})
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "Retry requires the next attempt of a document with a processing error. Refresh its status.")
		return
	}
	if documentUploadError(c, err) {
		return
	}
	if queued {
		respond.Accepted(c, saved)
	} else {
		respond.OK(c, saved)
	}
}

func (h *handlers) downloadDocument(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(c.Param("file"))
	if err != nil || fileID == uuid.Nil {
		respond.NotFound(c, "staff document")
		return
	}
	h.sendDocument(c, staffDocuments{}, p, hospital, staffID, fileID)
}

// sendDocument serves released bytes only while the verdict still holds, auditing the read before any byte leaves.
func (h *handlers) sendDocument(c *gin.Context, gate documentGate, p authn.Principal, hospital, staffID, fileID uuid.UUID) {
	if h.documents == nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	var saved document
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedDocument(tx, gate, p, hospital, staffID, fileID, false)
		if err != nil {
			return err
		}
		if saved.Status != "available" || saved.ReleasedGeneration == nil || saved.UploadedAt == nil {
			return errDocumentConflict
		}
		return nil
	})
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "Document is not available for download. Refresh its status.")
		return
	}
	if documentUploadError(c, err) {
		return
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, OwnerKind: "staff", OwnerID: staffID, RecordID: saved.RecordID, FileID: fileID, Version: 1, UploadedAt: *saved.UploadedAt}
	content, err := h.documents.ReadReleased(c.Request.Context(), scope, *saved.ReleasedGeneration)
	if err != nil || content.Generation != *saved.ReleasedGeneration || content.Size != saved.Size || content.SHA256 != saved.SHA256 {
		// Uncertain or changed bytes never inherit a committed scan verdict.
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		current, err := authorizedDocument(tx, gate, p, hospital, staffID, fileID, false)
		if err != nil {
			return err
		}
		if current.Status != "available" || current.ReleasedGeneration == nil || *current.ReleasedGeneration != content.Generation {
			return errDocumentConflict
		}
		return tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES (?,?,?,?,?,jsonb_build_object('file_id',?::uuid))`, p.TenantID, hospital, p.Subject, gate.downloadAction(), staffID, fileID).Error
	})
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "Document is not available for download. Refresh its status.")
		return
	}
	if documentUploadError(c, err) {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": saved.Filename}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, saved.ContentType, content.Bytes)
}
