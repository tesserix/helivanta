package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
)

type releasedReviewFile struct {
	Filename, ContentType, SHA256 string
	Size, Generation              int64
}

func (h *handlers) readReviewEvidence(c *gin.Context, p authn.Principal, hospital, staff, file uuid.UUID, uploaded time.Time, proof releasedReviewFile) ([]byte, error) {
	if h.documents == nil {
		return nil, errDocumentsUnavailable
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if !p.IdleDeadline.IsZero() {
		var deadline context.CancelFunc
		ctx, deadline = context.WithDeadline(ctx, p.IdleDeadline)
		defer deadline()
	}
	object := attachments.ObjectScope{TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, OwnerKind: "staff", OwnerID: staff, RecordID: staff, FileID: file, Version: 1, UploadedAt: uploaded}
	content, err := h.documents.ReadReleased(ctx, object, proof.Generation)
	if err != nil {
		return nil, errDocumentsUnavailable
	}
	digest := sha256.Sum256(content.Bytes)
	if content.Generation != proof.Generation || content.Size != proof.Size || int64(len(content.Bytes)) != proof.Size || content.SHA256 != proof.SHA256 || hex.EncodeToString(digest[:]) != proof.SHA256 {
		return nil, errDocumentsUnavailable
	}
	return content.Bytes, nil
}

func sendReviewEvidence(c *gin.Context, proof releasedReviewFile, content []byte) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": proof.Filename}))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, proof.ContentType, content)
}
