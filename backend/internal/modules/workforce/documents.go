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

type documentInput struct {
	RecordKind  string    `json:"record_kind" binding:"required,oneof=enrollment credential qualification"`
	RecordID    uuid.UUID `json:"record_id" binding:"required"`
	Category    string    `json:"category" binding:"required,oneof=employment_evidence qualification_evidence credential_evidence identity_evidence"`
	Filename    string    `json:"filename" binding:"required,max=180"`
	ContentType string    `json:"content_type" binding:"required,oneof=image/png image/jpeg"`
	Size        int64     `json:"size" binding:"required,min=1,max=10485760"`
	SHA256      string    `json:"sha256" binding:"required,len=64"`
}

type document struct {
	ID                   uuid.UUID  `json:"id"`
	HospitalID           uuid.UUID  `json:"hospital_id"`
	StaffID              uuid.UUID  `json:"staff_id"`
	RecordID             uuid.UUID  `json:"record_id"`
	RecordKind           string     `json:"record_kind"`
	RecordVersion        int64      `json:"record_version"`
	Category             string     `json:"category"`
	Filename             string     `json:"filename"`
	ContentType          string     `json:"content_type"`
	Size                 int64      `json:"size"`
	SHA256               string     `json:"sha256"`
	Status               string     `json:"status"`
	ScanAttempt          int64      `json:"scan_attempt"`
	UploadedAt           *time.Time `json:"uploaded_at"`
	SourceGeneration     *int64     `json:"-"`
	ReleasedGeneration   *int64     `json:"-"`
	EngineVersion        *string    `json:"-"`
	DefinitionVersion    *int64     `json:"-"`
	DefinitionsUpdatedAt *time.Time `json:"-"`
	ScannedAt            *time.Time `json:"scanned_at"`
	FailureCode          *string    `json:"failure_code"`
	ExpiresAt            time.Time  `json:"expires_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (document) TableName() string                 { return "workforce_documents" }
func (d document) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

const documentColumns = "id,hospital_id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at"

var documentDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errDocumentConflict = errors.New("document intent conflicts")
var errDocumentsUnavailable = errors.New("document storage not configured")

func (in *documentInput) valid(staffID uuid.UUID) bool {
	in.Filename = strings.TrimSpace(in.Filename)
	if in.RecordID == uuid.Nil || in.Filename == "" || in.Filename == "." || in.Filename == ".." || strings.ContainsAny(in.Filename, "/\\") || !documentDigest.MatchString(in.SHA256) || in.Size > attachments.MaxFileBytes {
		return false
	}
	for _, r := range in.Filename {
		if unicode.IsControl(r) {
			return false
		}
	}
	return (in.RecordKind == "enrollment" && in.RecordID == staffID && contains(in.Category, "employment_evidence", "identity_evidence")) ||
		(in.RecordKind == "credential" && in.Category == "credential_evidence") || (in.RecordKind == "qualification" && in.Category == "qualification_evidence")
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
	var saved document
	var created bool
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		owner, err := documentOwner(tx, p, hospital, staffID)
		if err != nil {
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
			if err := documentRecord(tx, p, hospital, staffID, in); err != nil {
				return err
			}
			result := tx.Exec(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,id) DO NOTHING`, p.TenantID, hospital, id, staffID, in.RecordID, in.RecordKind, owner.Version, in.Category, in.Filename, in.ContentType, in.Size, in.SHA256)
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
	if err != nil {
		respondError(c, err)
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
