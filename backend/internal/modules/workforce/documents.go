package workforce

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (document) TableName() string                 { return "workforce_documents" }
func (d document) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

const documentColumns = "id,hospital_id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at"

var documentDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errDocumentConflict = errors.New("document intent conflicts")
var errDocumentsUnavailable = errors.New("document storage not configured")

// documentGate authorises one route family's files for a staff member and binds new files to a record that still takes them.
type documentGate interface {
	authorize(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, writing bool) error
	// bind returns the version a new file records, or fails when the record no longer takes files.
	bind(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) (int64, error)
	owns(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) error
	downloadAction() string
}

// staffDocuments gates HR enrollment, credential and qualification files.
type staffDocuments struct{}

func (staffDocuments) authorize(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, _ bool) error {
	_, err := documentOwner(tx, p, hospital, staffID)
	return err
}

func (staffDocuments) bind(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) (int64, error) {
	var owner staff
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("version").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staffID).First(&owner).Error; err != nil {
		return 0, err
	}
	return owner.Version, documentRecord(tx, p, hospital, staffID, documentInput{RecordKind: kind, RecordID: record})
}

func (staffDocuments) owns(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, kind string, record uuid.UUID) error {
	return documentRecord(tx, p, hospital, staffID, documentInput{RecordKind: kind, RecordID: record})
}

func (staffDocuments) downloadAction() string { return "workforce.document.downloaded" }

func validDocumentFile(filename *string, digest string, size int64) bool {
	*filename = strings.TrimSpace(*filename)
	if *filename == "" || *filename == "." || *filename == ".." || strings.ContainsAny(*filename, "/\\") || !documentDigest.MatchString(digest) || size > attachments.MaxFileBytes {
		return false
	}
	for _, r := range *filename {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (in *documentInput) valid(staffID uuid.UUID) bool {
	return in.RecordID != uuid.Nil && validDocumentFile(&in.Filename, in.SHA256, in.Size) &&
		contains(in.Category, documentCategoryBindings[in.RecordKind]...) && (in.RecordKind != "enrollment" || in.RecordID == staffID)
}

func documentOwner(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID) (staff, error) {
	if err := enrollmentScope(tx, p, hospital); err != nil {
		return staff{}, err
	}
	var owner staff
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(staffColumns+",version").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staffID).First(&owner).Error
	return owner, err
}

func documentRecord(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, in documentInput) error {
	if in.RecordKind == "enrollment" {
		return nil
	}
	table := "workforce_enrollment_qualifications"
	if in.RecordKind == "credential" {
		table = "workforce_enrollment_credentials"
	}
	var record struct{ ID uuid.UUID }
	return tx.Table(table).Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, in.RecordID).Take(&record).Error
}

func documentStaff(c *gin.Context) (uuid.UUID, bool) {
	if expected := c.GetHeader("X-Helivanta-Expected-Tenant"); expected != "" {
		p, _, ok := authn.TenantPrincipal(c)
		requested, err := uuid.Parse(expected)
		if !ok || err != nil || requested == uuid.Nil || requested.String() != p.TenantID {
			respond.NotFound(c, "staff document")
			return uuid.Nil, false
		}
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return uuid.Nil, false
	}
	return id, true
}

func (h *handlers) putDocument(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("file"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "document")
		return
	}
	var in documentInput
	if !decode(c, &in) {
		return
	}
	if !in.valid(staffID) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid document intent")
		return
	}
	h.registerDocument(c, staffDocuments{}, p, hospital, staffID, id, in)
}

// registerDocument records an upload intent once; an identical replay returns it and a different one conflicts.
func (h *handlers) registerDocument(c *gin.Context, gate documentGate, p authn.Principal, hospital, staffID, id uuid.UUID, in documentInput) {
	var saved document
	var created bool
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := gate.authorize(tx, p, hospital, staffID, true); err != nil {
			return err
		}
		existing := tx.Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, id).First(&saved).Error
		if existing != nil && !errors.Is(existing, gorm.ErrRecordNotFound) {
			return existing
		}
		if errors.Is(existing, gorm.ErrRecordNotFound) {
			if h.documents == nil {
				return errDocumentsUnavailable
			}
			version, err := gate.bind(tx, p, hospital, staffID, in.RecordKind, in.RecordID)
			if err != nil {
				return err
			}
			result := tx.Exec(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,id) DO NOTHING`, p.TenantID, hospital, id, staffID, in.RecordID, in.RecordKind, version, in.Category, in.Filename, in.ContentType, in.Size, in.SHA256)
			if result.Error != nil {
				return result.Error
			}
			created = result.RowsAffected == 1
			if err := tx.Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, id).First(&saved).Error; err != nil {
				return err
			}
		}
		if saved.RecordID != in.RecordID || saved.RecordKind != in.RecordKind || saved.Category != in.Category || saved.Filename != in.Filename || saved.ContentType != in.ContentType || saved.Size != in.Size || saved.SHA256 != in.SHA256 {
			return errDocumentConflict
		}
		return nil
	})
	if errors.Is(err, errDocumentConflict) {
		respond.Conflict(c, "This upload identifier already belongs to a different document intent.")
		return
	}
	if errors.Is(err, errDocumentsUnavailable) {
		respond.Error(c, http.StatusServiceUnavailable, "documents_unavailable", "Document uploads are not configured.")
		return
	}
	if documentUploadError(c, err) {
		return
	}
	if created {
		respond.Created(c, saved)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listDocuments(c *gin.Context, params pagination.Params) ([]document, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return nil, nil
	}
	var rows []document
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if _, err := documentOwner(tx, p, hospital, staffID); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(documentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Find(&rows).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "staff documents")
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("staff document listing unavailable")
	}
	return rows, nil
}
