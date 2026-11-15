package workforce

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strconv"
	"time"
)

func (h *handlers) employmentDocumentGate(c *gin.Context, operation documentPreparationOperation) (*employmentPreparationDocuments, bool) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return nil, false
	}
	staff, ok := documentStaff(c)
	if !ok {
		return nil, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respondIntakeError(c, errIntakeWorkspaceRequired)
		return nil, false
	}
	query := c.Request.URL.Query()
	for key, values := range query {
		allowed := contains(key, "staff_version", "employment_epoch") || operation == documentPreparationList && contains(key, "limit", "cursor")
		if len(values) != 1 || !allowed {
			respondIntakeError(c, errIntakeProjection)
			return nil, false
		}
	}
	basis := map[string]int64{}
	for _, key := range []string{"staff_version", "employment_epoch"} {
		value, err := strconv.ParseInt(query.Get(key), 10, 64)
		if err != nil || value < 1 || value > 9007199254740990 || strconv.FormatInt(value, 10) != query.Get(key) {
			respondIntakeError(c, errIntakeProjection)
			return nil, false
		}
		basis[key] = value
	}
	var file uuid.UUID
	if operation != documentPreparationList {
		var err error
		file, err = uuid.Parse(c.Param("file"))
		if err != nil || file == uuid.Nil {
			respond.NotFound(c, "employment document")
			return nil, false
		}
	}
	return &employmentPreparationDocuments{h: h, principal: p, hospital: hospital, staff: staff, file: file, version: basis["staff_version"], epoch: basis["employment_epoch"], operation: operation}, true
}
func (h *handlers) putEmploymentPreparationDocument(c *gin.Context) {
	gate, ok := h.employmentDocumentGate(c, documentPreparationRegister)
	if !ok {
		return
	}
	id, input, ok := documentIntent(c, gate.staff)
	if !ok {
		return
	}
	if id != gate.file || input.RecordKind != "enrollment" || input.RecordID != gate.staff || input.Category != "employment_evidence" {
		respond.Error(c, 400, "invalid_request", "Choose employment evidence for this Staff record.")
		return
	}
	h.registerDocument(c, gate, gate.principal, gate.hospital, gate.staff, gate.file, input)
}
func (h *handlers) employmentPreparationFile(operation documentPreparationOperation) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		gate, ok := h.employmentDocumentGate(c, operation)
		if !ok {
			return
		}
		switch operation {
		case documentPreparationMetadata:
			var row document
			err := gate.transaction(c, gate.principal, gate.hospital, func(tx *gorm.DB) error {
				var err error
				row, err = authorizedDocument(tx, gate, gate.principal, gate.hospital, gate.staff, gate.file, false)
				return err
			})
			if err != nil {
				documentUploadError(c, err)
				return
			}
			respond.OK(c, gate.view(row))
		case documentPreparationUpload:
			h.receiveDocument(c, gate, gate.principal, gate.hospital, gate.staff, gate.file)
		case documentPreparationDownload:
			h.sendDocument(c, gate, gate.principal, gate.hospital, gate.staff, gate.file)
		case documentPreparationRevoke:
			h.withdrawDocument(c, gate, gate.principal, gate.hospital, gate.staff, gate.file)
		case documentPreparationRetry:
			h.requeueDocument(c, gate, gate.principal, gate.hospital, gate.staff, gate.file)
		}
	}
}
func (h *handlers) listEmploymentPreparationDocuments(c *gin.Context, page pagination.Params) ([]document, error) {
	gate, ok := h.employmentDocumentGate(c, documentPreparationList)
	if !ok {
		return nil, nil
	}
	rows := []document{}
	err := gate.transaction(c, gate.principal, gate.hospital, func(tx *gorm.DB) error {
		if err := gate.authorize(tx, gate.principal, gate.hospital, gate.staff, false); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, page).Table("workforce_documents").Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", gate.principal.TenantID, gate.hospital, gate.staff).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if err := gate.audit(tx, row.ID, "workforce.employment_document.file.read"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		documentUploadError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (g *employmentPreparationDocuments) auditDownload(tx *gorm.DB) error {
	return g.audit(tx, g.file, g.downloadAction())
}
func registerEmploymentPreparationDocuments(r *platform.Router, h *handlers) {
	path := "/:hospital/staff/:id/employment-preparation-documents"
	r.PUT(path+"/:file", authz.Public, h.putEmploymentPreparationDocument)
	r.GET(path+"/:file", authz.Public, h.employmentPreparationFile(documentPreparationMetadata))
	r.PUT(path+"/:file/content", authz.Public, h.employmentPreparationFile(documentPreparationUpload))
	r.GET(path+"/:file/content", authz.Public, h.employmentPreparationFile(documentPreparationDownload))
	r.PUT(path+"/:file/revocation", authz.Public, h.employmentPreparationFile(documentPreparationRevoke))
	r.PUT(path+"/:file/retries/:attempt", authz.Public, h.employmentPreparationFile(documentPreparationRetry))
	platform.SealedFilteredQueryListRoute(r, path, authz.Public, "hospital", []platform.QueryFilter{{Key: "staff_version"}, {Key: "employment_epoch"}}, 20, h.listEmploymentPreparationDocuments, func(_ *gin.Context, rows []document) ([]document, error) { return rows, nil }, "id")
}
