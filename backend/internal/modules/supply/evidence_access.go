package supply

import (
	"encoding/json"
	"github.com/tesserix/helivanta/pkg/events"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func (h *evidenceHandlers) downloadEvidence(c *gin.Context) {
	p, party, licence, ok := evidenceScope(c)
	if !ok {
		return
	}
	file, ok := resourceID(c, "file")
	if !ok {
		return
	}
	var saved evidence
	err := h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		var err error
		saved, err = h.authorized(tx, p, party, licence, file, registryReaders)
		if err == nil && (saved.Status != "available" || saved.UploadedAt == nil || saved.ReleasedGeneration == nil) {
			return gorm.ErrRecordNotFound
		}
		return err
	})
	if evidenceError(c, err) {
		return
	}
	if h.files == nil {
		evidenceError(c, errEvidenceUnavailable)
		return
	}
	content, err := h.files.ReadReleased(c.Request.Context(), saved.objectScope(), *saved.ReleasedGeneration)
	if err != nil || content.Generation != *saved.ReleasedGeneration || content.Size != saved.Size || content.SHA256 != saved.SHA256 {
		// Generation uncertainty cannot inherit a clean verdict.
		evidenceError(c, errEvidenceUnavailable)
		return
	}
	err = h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		current, err := h.authorized(tx, p, party, licence, file, registryReaders)
		if err != nil {
			return err
		}
		if current.Status != "available" || current.ReleasedGeneration == nil || *current.ReleasedGeneration != content.Generation {
			return gorm.ErrRecordNotFound
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','supply_license_evidence',?)`, p.TenantID, p.Subject, file).Error
	})
	if evidenceError(c, err) {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": saved.Filename}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, saved.ContentType, content.Bytes)
}
func (h *evidenceHandlers) revokeEvidence(c *gin.Context) {
	p, party, licence, ok := evidenceScope(c)
	if !ok {
		return
	}
	file, ok := resourceID(c, "file")
	if !ok {
		return
	}
	var saved evidence
	err := h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		var err error
		saved, err = h.authorized(tx, p, party, licence, file, licenseWriters)
		if err != nil {
			return err
		}
		if saved.Status == "revoked" {
			return nil
		}
		if saved.Status != "available" {
			return errConflict
		}
		res := tx.Raw(`UPDATE supply_license_evidence SET status='revoked' WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='available' RETURNING `+evidenceColumns, p.TenantID, party, licence, file).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errConflict
		}
		return evidenceStateEvent(tx, supplycontract.EvidenceScanInput{TenantID: p.TenantID, PartyID: party.String(), LicenseID: licence.String(), FileID: file.String(), ScanAttempt: saved.ScanAttempt}, "revoked")
	})
	if evidenceError(c, err) {
		return
	}
	respond.OK(c, saved)
}
func (h *evidenceHandlers) retryEvidence(c *gin.Context) {
	p, party, licence, ok := evidenceScope(c)
	if !ok {
		return
	}
	file, ok := resourceID(c, "file")
	if !ok {
		return
	}
	attempt, err := strconv.ParseInt(c.Param("attempt"), 10, 64)
	if err != nil || attempt < 2 || attempt > 9007199254740991 || strconv.FormatInt(attempt, 10) != c.Param("attempt") {
		respond.Error(c, 400, "invalid_request", "Use the next canonical processing attempt.")
		return
	}
	var saved evidence
	queued := false
	err = h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		var err error
		saved, err = h.authorized(tx, p, party, licence, file, licenseWriters)
		if err != nil {
			return err
		}
		if saved.ScanAttempt == attempt {
			return nil
		}
		if saved.Status != "scan_error" || saved.ScanAttempt+1 != attempt {
			return errConflict
		}
		if h.files == nil {
			return errEvidenceUnavailable
		}
		res := tx.Raw(`UPDATE supply_license_evidence SET status='scan_pending',scan_attempt=?,failure_code=NULL WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='scan_error' AND scan_attempt=? RETURNING `+evidenceColumns, attempt, p.TenantID, party, licence, file, attempt-1).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errConflict
		}
		data, err := json.Marshal(supplycontract.EvidenceScanRequestedData{PartyID: party.String(), LicenseID: licence.String(), FileID: file.String(), ScanAttempt: attempt})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, supplycontract.SubjectEvidenceScanRequested, events.Event{Type: supplycontract.SubjectEvidenceScanRequested, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		queued = true
		return nil
	})
	if evidenceError(c, err) {
		return
	}
	if queued {
		respond.Accepted(c, saved)
	} else {
		respond.OK(c, saved)
	}
}
