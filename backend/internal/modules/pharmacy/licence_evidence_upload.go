package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
)

func (h *licenceEvidenceHandlers) uploadLicenceEvidence(c *gin.Context) {
	p, outlet, licence, file, ok := licenceEvidenceScope(c, true)
	if !ok {
		return
	}
	var saved licenceEvidence
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) (err error) {
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, true)
		return err
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if h.files == nil {
		licenceEvidenceError(c, errLicenceEvidenceUnavailable)
		return
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != saved.ContentType || len(params) != 0 || c.GetHeader("Content-Encoding") != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_evidence", "Content type must match the original upload intent.")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, saved.Size+1)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || int64(len(data)) != saved.Size {
		respond.Error(c, http.StatusBadRequest, "invalid_evidence", "File size must match the upload intent.")
		return
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != saved.SHA256 {
		respond.Error(c, http.StatusBadRequest, "invalid_evidence", "File checksum must match the upload intent.")
		return
	}
	if _, err := attachments.InspectContent(data, saved.ContentType); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_evidence", "Use a complete passive PNG or JPEG. Scan the page again and retry.")
		return
	}
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, true)
		if err != nil || saved.Status != "upload_pending" {
			return err
		}
		res := tx.Raw(`UPDATE pharmacy_licence_evidence SET uploaded_at=COALESCE(uploaded_at,GREATEST(clock_timestamp(),created_at)) WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp() RETURNING `+licenceEvidenceColumns, p.TenantID, outlet, licence, file).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errStockIdentity
		}
		return nil
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if saved.Status != "upload_pending" {
		respond.OK(c, saved)
		return
	}
	// Storage holds no authorization locks; the commit below rechecks the grant and the row.
	receipt, err := h.files.EnsureQuarantine(c.Request.Context(), saved.objectScope(), data)
	if errors.Is(err, attachments.ErrObjectExists) {
		licenceEvidenceError(c, errStockIdentity)
		return
	}
	if err != nil {
		licenceEvidenceError(c, errLicenceEvidenceUnavailable)
		return
	}
	committed := false
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, true)
		if err != nil {
			return err
		}
		if saved.Status != "upload_pending" {
			if saved.SourceGeneration == nil || *saved.SourceGeneration != receipt.Generation {
				return errStockIdentity
			}
			return nil
		}
		if receipt.Size != saved.Size || receipt.SHA256 != saved.SHA256 {
			return errStockIdentity
		}
		res := tx.Raw(`UPDATE pharmacy_licence_evidence SET status='scan_pending',source_generation=? WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp() RETURNING `+licenceEvidenceColumns, receipt.Generation, p.TenantID, outlet, licence, file).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errStockIdentity
		}
		if err := h.requestScan(tx, p.TenantID, outlet, licence, file, saved.ScanAttempt); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if committed {
		respond.Accepted(c, saved)
	} else {
		respond.OK(c, saved)
	}
}

func (h *licenceEvidenceHandlers) requestScan(tx *gorm.DB, tenant string, outlet, licence, file uuid.UUID, attempt int64) error {
	payload, err := json.Marshal(pharmacycontract.LicenceEvidenceScanRequestedData{OutletID: outlet.String(), LicenceID: licence.String(), FileID: file.String(), ScanAttempt: attempt})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, pharmacycontract.SubjectLicenceEvidenceScanRequested, events.Event{Type: pharmacycontract.SubjectLicenceEvidenceScanRequested, Version: 1, TenantID: tenant, Data: payload})
}

func (h *licenceEvidenceHandlers) downloadLicenceEvidence(c *gin.Context) {
	p, outlet, licence, file, ok := licenceEvidenceScope(c, true)
	if !ok {
		return
	}
	var saved licenceEvidence
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, false)
		if err == nil && (saved.Status != "available" || saved.UploadedAt == nil || saved.ReleasedGeneration == nil) {
			return gorm.ErrRecordNotFound
		}
		return err
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if h.files == nil {
		licenceEvidenceError(c, errLicenceEvidenceUnavailable)
		return
	}
	content, err := h.files.ReadReleased(c.Request.Context(), saved.objectScope(), *saved.ReleasedGeneration)
	if err != nil || content.Generation != *saved.ReleasedGeneration || content.Size != saved.Size || content.SHA256 != saved.SHA256 {
		// Generation uncertainty cannot inherit a clean verdict.
		licenceEvidenceError(c, errLicenceEvidenceUnavailable)
		return
	}
	err = h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		current, err := authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, false)
		if err != nil {
			return err
		}
		if current.Status != "available" || current.ReleasedGeneration == nil || *current.ReleasedGeneration != content.Generation {
			return gorm.ErrRecordNotFound
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','pharmacy_licence_evidence',?)`, p.TenantID, p.Subject, file).Error
	})
	if licenceEvidenceError(c, err) {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": saved.Filename}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, saved.ContentType, content.Bytes)
}

func (h *licenceEvidenceHandlers) retryLicenceEvidence(c *gin.Context) {
	p, outlet, licence, file, ok := licenceEvidenceScope(c, true)
	if !ok {
		return
	}
	attempt, err := strconv.ParseInt(c.Param("attempt"), 10, 64)
	if err != nil || attempt < 2 || attempt > 9007199254740991 || strconv.FormatInt(attempt, 10) != c.Param("attempt") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use the next canonical processing attempt.")
		return
	}
	var saved licenceEvidence
	queued := false
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var err error
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, file, true)
		if err != nil {
			return err
		}
		if saved.ScanAttempt == attempt {
			return nil
		}
		if saved.Status != "scan_error" || saved.ScanAttempt+1 != attempt {
			return errStockIdentity
		}
		if h.files == nil {
			return errLicenceEvidenceUnavailable
		}
		res := tx.Raw(`UPDATE pharmacy_licence_evidence SET status='scan_pending',scan_attempt=?,failure_code=NULL WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='scan_error' AND scan_attempt=? RETURNING `+licenceEvidenceColumns, attempt, p.TenantID, outlet, licence, file, attempt-1).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errStockIdentity
		}
		if err := h.requestScan(tx, p.TenantID, outlet, licence, file, attempt); err != nil {
			return err
		}
		queued = true
		return nil
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if queued {
		respond.Accepted(c, saved)
	} else {
		respond.OK(c, saved)
	}
}
