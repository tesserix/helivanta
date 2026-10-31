package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

// pendingDocument answers only for a file of the gate's own record, whatever its state, so a replay never reaches another record's file.
func pendingDocument(tx *gorm.DB, gate documentGate, p authn.Principal, hospital, staffID, fileID uuid.UUID) (document, error) {
	saved, err := authorizedDocument(tx, gate, p, hospital, staffID, fileID, true)
	if err != nil {
		return document{}, err
	}
	if saved.Status != "upload_pending" {
		return saved, nil
	}
	version, err := gate.bind(tx, p, hospital, staffID, saved.RecordKind, saved.RecordID)
	if err != nil {
		return document{}, err
	}
	if saved.RecordVersion != version {
		return document{}, errDocumentConflict
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
	h.receiveDocument(c, staffDocuments{}, p, hospital, staffID, fileID)
}

// receiveDocument verifies bytes against the intent, quarantines them, then queues the scan.
func (h *handlers) receiveDocument(c *gin.Context, gate documentGate, p authn.Principal, hospital, staffID, fileID uuid.UUID) {
	if !refreshDocumentPermissions(c, gate, true) {
		return
	}
	var saved document
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, gate, p, hospital, staffID, fileID)
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
		respond.Error(c, http.StatusBadRequest, "invalid_document", "Use a complete PNG or JPEG without embedded files or unsupported metadata. Export a fresh image and try again.")
		return
	}
	if !refreshDocumentPermissions(c, gate, true) {
		return
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, gate, p, hospital, staffID, fileID)
		if err != nil || saved.Status != "upload_pending" {
			return err
		}
		result := tx.Raw(`UPDATE workforce_documents SET uploaded_at=COALESCE(uploaded_at,GREATEST(clock_timestamp(),created_at)),updated_at=clock_timestamp()
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
	if !refreshDocumentPermissions(c, gate, true) {
		return
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var err error
		saved, err = pendingDocument(tx, gate, p, hospital, staffID, fileID)
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

var evidenceDecidedGuards = []string{"workforce_tax_evidence_pending", "workforce_tax_evidence_frozen", "workforce_benefit_evidence_pending", "workforce_benefit_evidence_frozen"}

func documentUploadError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	if errors.Is(err, errDeclarationState) || errors.Is(err, errBenefitState) || isPG && slices.Contains(evidenceDecidedGuards, pg.ConstraintName) {
		respond.ConflictWithDetail(c, "This proposal has been decided, so its evidence can no longer change.", gin.H{"reason": "not_pending"})
	} else if isPG && (pg.ConstraintName == "workforce_tax_evidence_limit" || pg.ConstraintName == "workforce_benefit_evidence_limit") {
		respond.ConflictWithDetail(c, "A proposal takes at most 10 evidence files.", gin.H{"reason": "evidence_limit"})
	} else if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "The document intent is expired, changed or conflicts with stored bytes. Create a new intent.")
	} else if errors.Is(err, errDocumentsUnavailable) {
		// Storage uncertainty never advances a file into a downloadable state.
		respond.Error(c, http.StatusServiceUnavailable, "documents_unavailable", "Document storage is unavailable. Retry the same upload.")
	} else {
		respondError(c, err)
	}
	return true
}
