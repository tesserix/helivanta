package practitioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"mime"
	"net/http"
	"time"
)

const EvidenceScanRequested = practitionercontract.EvidenceScanRequested

var errEvidenceUnavailable = errors.New("private evidence storage unavailable")

func lockedEvidence(tx *gorm.DB, p authn.Principal, parent evidenceParent, id uuid.UUID, row *credentialEvidence) error {
	return evidenceQuery(tx, p, parent).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(row).Error
}
func (h *handlers) uploadEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "file")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	var row credentialEvidence
	err := h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, false); e != nil {
			return e
		}
		return evidenceQuery(tx, p, parent).Where("id=?", id).Take(&row).Error
	})
	if err != nil {
		failure(c, err)
		return
	}
	if h.files == nil {
		failure(c, errEvidenceUnavailable)
		return
	}
	typ, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != row.ContentType || len(params) != 0 || c.GetHeader("Content-Encoding") != "" {
		respond.Error(c, 400, "invalid_request", "Evidence content type must match the immutable intent")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, row.Size+1)
	data, err := io.ReadAll(c.Request.Body)
	sum := sha256.Sum256(data)
	if err != nil || int64(len(data)) != row.Size || hex.EncodeToString(sum[:]) != row.SHA256 {
		respond.Error(c, 400, "invalid_request", "Evidence bytes must match the immutable size and checksum")
		return
	}
	if _, err := attachments.InspectContent(data, row.ContentType); err != nil {
		respond.Error(c, 400, "invalid_request", "Expected a complete passive PNG or JPEG")
		return
	}
	if !h.reauthorize(c, PermEvidenceManage) {
		return
	}
	err = h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, true); e != nil {
			return e
		}
		if e := lockedEvidence(tx, p, parent, id, &row); e != nil {
			return e
		}
		if row.Status != "upload_pending" {
			return evidenceAudit(tx, p, id)
		}
		result := tx.Exec("UPDATE practitioner_credential_evidence SET uploaded_at=COALESCE(uploaded_at,GREATEST(practitioner_registry_now(),created_at)) WHERE tenant_id=? AND id=? AND status='upload_pending' AND expires_at>practitioner_registry_now()", p.TenantID, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errIntentConflict
		}
		return lockedEvidence(tx, p, parent, id, &row)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if row.Status != "upload_pending" {
		respond.OK(c, row)
		return
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), OwnerKind: "practitioner", OwnerID: parent.practitioner, RecordID: parent.revision, FileID: id, Version: 1, UploadedAt: *row.UploadedAt}
	receipt, err := h.files.EnsureQuarantine(ctx, scope, data)
	if errors.Is(err, attachments.ErrObjectExists) {
		failure(c, errIntentConflict)
		return
	}
	if err != nil {
		failure(c, errEvidenceUnavailable)
		return
	}
	if !h.reauthorize(c, PermEvidenceManage) {
		return
	}
	queued := false
	err = h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, true); e != nil {
			return e
		}
		if e := lockedEvidence(tx, p, parent, id, &row); e != nil {
			return e
		}
		if receipt.Size != row.Size || receipt.SHA256 != row.SHA256 {
			return errIntentConflict
		}
		if row.Status != "upload_pending" {
			if row.SourceGeneration == nil || *row.SourceGeneration != receipt.Generation {
				return errIntentConflict
			}
			return evidenceAudit(tx, p, id)
		}
		result := tx.Exec("UPDATE practitioner_credential_evidence SET status='scan_pending',source_generation=? WHERE tenant_id=? AND id=? AND status='upload_pending' AND expires_at>practitioner_registry_now()", receipt.Generation, p.TenantID, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errIntentConflict
		}
		payload, e := json.Marshal(evidenceScanData(evidenceScanInput(p.TenantID, parent, id, row.ScanAttempt)))
		if e != nil {
			return e
		}
		if e := events.Enqueue(tx, EvidenceScanRequested, events.Event{Type: EvidenceScanRequested, Version: 1, TenantID: p.TenantID, Data: payload}); e != nil {
			return e
		}
		queued = true
		return lockedEvidence(tx, p, parent, id, &row)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if queued {
		respond.Accepted(c, row)
	} else {
		respond.OK(c, row)
	}
}
