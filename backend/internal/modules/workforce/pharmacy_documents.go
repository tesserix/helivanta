package workforce

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// pharmacyStaffDocuments gates a pharmacy's enrollment, credential and qualification files (ADR-0054 slice 4d); it is not an HR intake gate.
type pharmacyStaffDocuments struct{}

func (pharmacyStaffDocuments) permissions(writing bool) (authz.Permission, authz.Permission) {
	return staffDocuments{}.permissions(writing)
}

func (pharmacyStaffDocuments) authorize(tx *gorm.DB, p authn.Principal, pharmacy, staffID uuid.UUID, _ bool) error {
	return staffDocumentOwner(tx, p, pharmacyEstablishment(pharmacy).establishment(), staffID)
}

func (pharmacyStaffDocuments) bind(tx *gorm.DB, p authn.Principal, pharmacy, staffID uuid.UUID, kind string, record uuid.UUID) (int64, error) {
	return staffDocuments{}.bind(tx, p, pharmacy, staffID, kind, record)
}

func (pharmacyStaffDocuments) owns(tx *gorm.DB, p authn.Principal, pharmacy, staffID uuid.UUID, kind string, record uuid.UUID) error {
	return staffDocuments{}.owns(tx, p, pharmacy, staffID, kind, record)
}

func (pharmacyStaffDocuments) downloadAction() string { return staffDocuments{}.downloadAction() }

func (pharmacyStaffDocuments) view(d document) any { return pharmacyDocumentView(d) }

func (d pharmacyDocument) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

// pharmacyDocumentScope admits the versionless pharmacy family only from the caller's current account and workspace.
func pharmacyDocumentScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, pharmacy, ok := pharmacyStaffScope(c)
	if !ok || !enrollmentSession(c) {
		return p, uuid.Nil, uuid.Nil, false
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, uuid.Nil, uuid.Nil, false
	}
	return p, uuid.UUID(pharmacy), staffID, true
}

func (h *handlers) pharmacyDocumentRecords(c *gin.Context) {
	if p, pharmacy, staffID, ok := pharmacyDocumentScope(c); ok {
		h.recordChoices(c, pharmacyStaffDocuments{}, p, pharmacy, staffID)
	}
}

func (h *handlers) listPharmacyDocuments(c *gin.Context, params pagination.Params) ([]pharmacyDocument, error) {
	p, pharmacy, staffID, ok := pharmacyDocumentScope(c)
	if !ok {
		return nil, nil
	}
	rows, err := h.documentRows(c, pharmacyStaffDocuments{}, p, pharmacy, staffID, params)
	views := make([]pharmacyDocument, 0, len(rows))
	for _, row := range rows {
		views = append(views, pharmacyDocumentView(row))
	}
	return views, err
}

func (h *handlers) putPharmacyDocument(c *gin.Context) {
	p, pharmacy, staffID, ok := pharmacyDocumentScope(c)
	if !ok {
		return
	}
	if id, in, ok := documentIntent(c, staffID); ok {
		h.registerDocument(c, pharmacyStaffDocuments{}, p, pharmacy, staffID, id, in)
	}
}

// pharmacyDocumentFile runs a shared file command through the pharmacy gate, the only gate a pharmacy route can name.
func (h *handlers) pharmacyDocumentFile(command func(*handlers, *gin.Context, documentGate, authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID)) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, pharmacy, staffID, ok := pharmacyDocumentScope(c)
		if !ok {
			return
		}
		fileID, err := uuid.Parse(c.Param("file"))
		if err != nil || fileID == uuid.Nil {
			respond.NotFound(c, "staff document")
			return
		}
		command(h, c, pharmacyStaffDocuments{}, p, pharmacy, staffID, fileID)
	}
}
