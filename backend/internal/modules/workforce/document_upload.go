package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func pendingDocument(tx *gorm.DB, p authn.Principal, hospital, staffID, fileID uuid.UUID) (document, error) {
	owner, err := documentOwner(tx, p, hospital, staffID)
	if err != nil {
		return document{}, err
	}
	var saved document
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(documentColumns).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, fileID).First(&saved).Error; err != nil {
		return document{}, err
	}
	if saved.Status != "upload_pending" {
		return saved, nil
	}
	if saved.RecordVersion != owner.Version {
		return document{}, errDocumentConflict
	}
	if err := documentRecord(tx, p, hospital, staffID, documentInput{RecordKind: saved.RecordKind, RecordID: saved.RecordID}); err != nil {
		return document{}, err
	}
	return saved, nil
}

func (h *handlers) uploadDocument(c *gin.Context) {
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
		respond.NotFound(c, "document")
		return
	}
	var saved document
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, p, hospital, staffID, fileID)
		return err
	})
	if documentUploadError(c, err) {
		return
	}
	if h.documents == nil {
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != saved.ContentType || len(params) != 0 || c.GetHeader("Content-Encoding") != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_document", "Document content type must match the upload intent.")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, saved.Size+1)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || int64(len(data)) != saved.Size {
		respond.Error(c, http.StatusBadRequest, "invalid_document", "Document size must match the upload intent.")
		return
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != saved.SHA256 {
		respond.Error(c, http.StatusBadRequest, "invalid_document", "Document checksum must match the upload intent.")
		return
	}
	if _, err := attachments.InspectContent(data, saved.ContentType); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_document", "Only complete supported static images may be uploaded.")
		return
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, p, hospital, staffID, fileID)
		if err != nil || saved.Status != "upload_pending" {
			return err
		}
		result := tx.Raw(`UPDATE workforce_documents SET uploaded_at=COALESCE(uploaded_at,clock_timestamp()),updated_at=clock_timestamp()
		 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp()
		 RETURNING `+documentColumns, p.TenantID, hospital, staffID, fileID).Scan(&saved)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errDocumentConflict
		}
		return nil
	})
	if documentUploadError(c, err) {
		return
	}
	if saved.Status != "upload_pending" {
		respond.OK(c, saved)
		return
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, OwnerKind: "staff", OwnerID: staffID, RecordID: saved.RecordID, FileID: fileID, Version: 1, UploadedAt: *saved.UploadedAt}
	// Storage runs outside authorization locks; commit rechecks the current grant.
	receipt, err := h.documents.EnsureQuarantine(c.Request.Context(), scope, data)
	if errors.Is(err, attachments.ErrObjectExists) {
		documentUploadError(c, errDocumentConflict)
		return
	}
	if err != nil {
		requestid.Logger(c).Error("staff document quarantine unavailable", "file_id", fileID, "err", err)
		documentUploadError(c, errDocumentsUnavailable)
		return
	}
	var committed bool
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, p, hospital, staffID, fileID)
		if err != nil {
			return err
		}
		if saved.Status != "upload_pending" {
			if saved.SourceGeneration == nil || *saved.SourceGeneration != receipt.Generation {
				return errDocumentConflict
			}
			return nil
		}
		if receipt.Size != saved.Size || receipt.SHA256 != saved.SHA256 {
			return errDocumentConflict
		}
		result := tx.Raw(`UPDATE workforce_documents SET status='scan_pending',source_generation=?,updated_at=clock_timestamp()
		 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp()
		 RETURNING `+documentColumns, receipt.Generation, p.TenantID, hospital, staffID, fileID).Scan(&saved)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errDocumentConflict
		}
		eventData, err := json.Marshal(workforcecontract.DocumentScanRequestedData{HospitalID: hospital.String(), StaffID: staffID.String(), FileID: fileID.String(), ScanAttempt: saved.ScanAttempt})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, DocumentScanRequested, events.Event{Type: DocumentScanRequested, Version: 1, TenantID: p.TenantID, Data: eventData}); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if documentUploadError(c, err) {
		return
	}
	if committed {
		respond.Accepted(c, saved)
		return
	}
	respond.OK(c, saved)
}

func documentUploadError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "The document intent is expired, changed or conflicts with stored bytes. Create a new intent.")
	} else if errors.Is(err, errDocumentsUnavailable) {
		// Storage uncertainty never advances a file into a downloadable state.
		respond.Error(c, http.StatusServiceUnavailable, "documents_unavailable", "Document storage is unavailable. Retry the same upload.")
	} else {
		respondError(c, err)
	}
	return true
}
