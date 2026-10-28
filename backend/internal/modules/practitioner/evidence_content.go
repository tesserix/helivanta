package practitioner

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

func releasedEvidence(row credentialEvidence) bool {
	return row.Status == "available" && row.UploadedAt != nil && row.SourceGeneration != nil && row.ReleasedGeneration != nil && row.ScannedAt != nil
}
func (h *handlers) downloadEvidence(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
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
	if !h.reauthorize(c, PermEvidenceRead) {
		return
	}
	var row credentialEvidence
	err := h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		if err := evidenceQuery(tx, p, parent).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if !releasedEvidence(row) {
			return ErrEvidenceNotReady
		}
		return evidenceAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if h.files == nil {
		failure(c, errEvidenceUnavailable)
		return
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), OwnerKind: "practitioner", OwnerID: parent.practitioner, RecordID: parent.revision, FileID: id, Version: 1, UploadedAt: *row.UploadedAt}
	content, err := h.files.ReadReleased(ctx, scope, *row.ReleasedGeneration)
	if err != nil || content.Generation != *row.ReleasedGeneration || content.Size != row.Size || content.SHA256 != row.SHA256 {
		failure(c, errEvidenceUnavailable)
		return
	}
	typ, err := attachments.InspectContent(content.Bytes, row.ContentType)
	if err != nil || typ != row.ContentType {
		failure(c, errEvidenceUnavailable)
		return
	}
	if !h.reauthorize(c, PermEvidenceRead) {
		return
	}
	err = h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		var current credentialEvidence
		if err := lockedEvidence(tx, p, parent, id, &current); err != nil {
			return err
		}
		if !releasedEvidence(current) || *current.ReleasedGeneration != content.Generation || *current.SourceGeneration != *row.SourceGeneration || !current.UploadedAt.Equal(*row.UploadedAt) || current.Size != content.Size || current.SHA256 != content.SHA256 || current.ContentType != typ {
			return ErrEvidenceNotReady
		}
		return evidenceAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	suffix := ".png"
	if typ == "image/jpeg" {
		suffix = ".jpg"
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "credential-evidence-" + id.String() + suffix}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, typ, content.Bytes)
}

type evidenceRevocation struct {
	ID              uuid.UUID `json:"id"`
	FileID          uuid.UUID `json:"file_id"`
	ReviewReference uuid.UUID `json:"review_reference"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

func (e *evidenceRevocation) AfterFind(*gorm.DB) error { e.CreatedAt = e.CreatedAt.UTC(); return nil }

const evidenceRevocationColumns = "id,file_id,review_reference,status,created_at"

func revocationQuery(tx *gorm.DB, p authn.Principal, parent evidenceParent, file, id uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_evidence_revocations").Select(evidenceRevocationColumns).Where("tenant_id=? AND practitioner_id=? AND credential_id=? AND credential_kind=? AND revision_id=? AND file_id=? AND id=?", p.TenantID, parent.practitioner, parent.credential, parent.kind, parent.revision, file, id)
}
func (h *handlers) revokeEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	file, ok := resourceID(c, "file")
	if !ok {
		return
	}
	id, ok := resourceID(c, "revocation")
	if !ok {
		return
	}
	var in *struct {
		ReviewReference string `json:"review_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		respond.Error(c, 400, "invalid_request", "Expected an evidence review reference")
		return
	}
	reference, valid := canonicalStewardReference(in.ReviewReference)
	if !valid {
		respond.Error(c, 400, "invalid_request", "Expected an opaque review reference")
		return
	}
	if !h.reauthorize(c, PermEvidenceManage) {
		return
	}
	var receipt evidenceRevocation
	created := false
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, true); err != nil {
			return err
		}
		var row credentialEvidence
		if err := lockedEvidence(tx, p, parent, file, &row); err != nil {
			return err
		}
		err := revocationQuery(tx, p, parent, file, id).Take(&receipt).Error
		if err == nil {
			if receipt.ReviewReference != reference {
				return errIntentConflict
			}
			return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_evidence_revocations',?)", p.TenantID, p.Subject, id).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !releasedEvidence(row) {
			return errIntentConflict
		}
		if err := tx.Exec("INSERT INTO practitioner_evidence_revocations(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,file_id,id,review_reference,created_by) VALUES(?,?,?,?,?,?,?,?,?)", p.TenantID, parent.practitioner, parent.credential, parent.kind, parent.revision, file, id, reference, p.Subject).Error; err != nil {
			return err
		}
		if err := evidenceStateEvent(tx, evidenceScanInput(p.TenantID, parent, file, row.ScanAttempt), "revoked"); err != nil {
			return err
		}
		if err := revocationQuery(tx, p, parent, file, id).Take(&receipt).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, receipt)
	} else {
		respond.OK(c, receipt)
	}
}
