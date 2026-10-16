package supply

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
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *evidenceHandlers) authorized(tx *gorm.DB, p authn.Principal, party, licence, file uuid.UUID, roles []string) (evidence, error) {
	var saved evidence
	owner, err := h.license(tx, p, party, licence, roles)
	if err != nil {
		return saved, err
	}
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(evidenceColumns).Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", p.TenantID, party, licence, file).First(&saved).Error
	if err == nil && saved.Status == "upload_pending" && saved.LicenseVersion != owner.Version {
		return saved, errConflict
	}
	return saved, err
}
func (e evidence) objectScope() attachments.ObjectScope {
	return attachments.ObjectScope{TenantID: e.TenantID, OwnerKind: "organisation", OwnerID: e.PartyID, RecordID: e.LicenseID, FileID: e.ID, Version: 1, UploadedAt: *e.UploadedAt}
}
func (h *evidenceHandlers) uploadEvidence(c *gin.Context) {
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
		return err
	})
	if evidenceError(c, err) {
		return
	}
	if h.files == nil {
		evidenceError(c, errEvidenceUnavailable)
		return
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != saved.ContentType || len(params) != 0 || c.GetHeader("Content-Encoding") != "" {
		respond.Error(c, 400, "invalid_evidence", "Content type must match the original upload intent.")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, saved.Size+1)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || int64(len(data)) != saved.Size {
		respond.Error(c, 400, "invalid_evidence", "File size must match the upload intent.")
		return
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != saved.SHA256 {
		respond.Error(c, 400, "invalid_evidence", "File checksum must match the upload intent.")
		return
	}
	if _, err := attachments.InspectContent(data, saved.ContentType); err != nil {
		respond.Error(c, 400, "invalid_evidence", "Use a complete passive PNG or JPEG. Export a fresh image and try again.")
		return
	}
	err = h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		var err error
		saved, err = h.authorized(tx, p, party, licence, file, licenseWriters)
		if err != nil || saved.Status != "upload_pending" {
			return err
		}
		res := tx.Raw(`UPDATE supply_license_evidence SET uploaded_at=COALESCE(uploaded_at,clock_timestamp()) WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp() RETURNING `+evidenceColumns, p.TenantID, party, licence, file).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errConflict
		}
		return nil
	})
	if evidenceError(c, err) {
		return
	}
	if saved.Status != "upload_pending" {
		respond.OK(c, saved)
		return
	}
	// Storage does not hold authorization locks; the commit rechecks the current grant and source.
	receipt, err := h.files.EnsureQuarantine(c.Request.Context(), saved.objectScope(), data)
	if errors.Is(err, attachments.ErrObjectExists) {
		evidenceError(c, errConflict)
		return
	}
	if err != nil {
		evidenceError(c, errEvidenceUnavailable)
		return
	}
	committed := false
	err = h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		var err error
		saved, err = h.authorized(tx, p, party, licence, file, licenseWriters)
		if err != nil {
			return err
		}
		if saved.Status != "upload_pending" {
			if saved.SourceGeneration == nil || *saved.SourceGeneration != receipt.Generation {
				return errConflict
			}
			return nil
		}
		if receipt.Size != saved.Size || receipt.SHA256 != saved.SHA256 {
			return errConflict
		}
		res := tx.Raw(`UPDATE supply_license_evidence SET status='scan_pending',source_generation=? WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='upload_pending' AND expires_at>clock_timestamp() RETURNING `+evidenceColumns, receipt.Generation, p.TenantID, party, licence, file).Scan(&saved)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errConflict
		}
		payload, err := json.Marshal(supplycontract.EvidenceScanRequestedData{PartyID: party.String(), LicenseID: licence.String(), FileID: file.String(), ScanAttempt: saved.ScanAttempt})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, supplycontract.SubjectEvidenceScanRequested, events.Event{Type: supplycontract.SubjectEvidenceScanRequested, Version: 1, TenantID: p.TenantID, Data: payload}); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if evidenceError(c, err) {
		return
	}
	if committed {
		respond.Accepted(c, saved)
	} else {
		respond.OK(c, saved)
	}
}
