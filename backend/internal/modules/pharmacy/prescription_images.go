package pharmacy

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// A prescription's scanned pages follow the supply licence evidence lifecycle
// (#1386 B-2a): intent, exact upload, quarantine, scan by the pharmacy
// evidence worker, release. Only a released page can be downloaded.

type prescriptionImageInput struct {
	Filename    string `json:"filename" binding:"required,min=1,max=180"`
	ContentType string `json:"content_type" binding:"required,oneof=image/png image/jpeg"`
	Size        int64  `json:"size" binding:"min=1,max=10485760"`
	Sha256      string `json:"sha256" binding:"required,len=64"`
}

type prescriptionImage struct {
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"-"`
	OutletID             uuid.UUID  `json:"outlet_id"`
	PrescriptionID       uuid.UUID  `json:"prescription_id"`
	Filename             string     `json:"filename"`
	ContentType          string     `json:"content_type"`
	Size                 int64      `json:"size"`
	SHA256               string     `gorm:"column:sha256" json:"-"`
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
	UploaderSubject      string     `json:"uploader_subject"`
	ExpiresAt            time.Time  `json:"expires_at" gorm:"default:now();autoCreateTime:false"`
	CreatedAt            time.Time  `json:"created_at" gorm:"default:now();autoCreateTime:false"`
	UpdatedAt            time.Time  `json:"updated_at" gorm:"default:now();autoUpdateTime:false"`
}

func (prescriptionImage) TableName() string                 { return "pharmacy_prescription_images" }
func (e prescriptionImage) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

const prescriptionImageColumns = "id,tenant_id,outlet_id,prescription_id,filename,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,uploader_subject,expires_at,created_at,updated_at"

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errEvidenceUnavailable = errors.New("private prescription evidence dependency unavailable")

type prescriptionImageHandlers struct {
	*catalogueHandlers
	files *attachments.GCS
}

func prescriptionImageRoutes(s *platform.Router, h *catalogueHandlers, files *attachments.GCS) {
	e := &prescriptionImageHandlers{catalogueHandlers: h, files: files}
	s.PUT("/prescriptions/:id/images/:file", PermPrescriptionRecord, e.putImage)
	s.PUT("/prescriptions/:id/images/:file/content", PermPrescriptionRecord, e.uploadImage)
	s.POST("/prescriptions/:id/images/:file/retry/:attempt", PermPrescriptionRecord, e.retryImage)
	s.GET("/prescriptions/:id/images/:file/download", PermPrescriptionRead, e.downloadImage)
	platform.ListRoute(s, "/prescriptions/:id/images", PermPrescriptionRead, e.listImages)
}

func (e prescriptionImage) objectScope() attachments.ObjectScope {
	return attachments.ObjectScope{TenantID: e.TenantID, OwnerKind: "pharmacy_outlet", OwnerID: e.OutletID, RecordID: e.PrescriptionID, FileID: e.ID, Version: 1, UploadedAt: *e.UploadedAt}
}

// imageScope resolves the outlet, prescription and (when named) file from the path.
func imageScope(c *gin.Context, withFile bool) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	rx, ok := catalogueID(c)
	if !ok || !withFile {
		return p, outlet, rx, uuid.Nil, ok
	}
	file, err := uuid.Parse(c.Param("file"))
	if err != nil || file.Version() != 7 || file.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "prescription image")
		return p, outlet, rx, uuid.Nil, false
	}
	return p, outlet, rx, file, true
}

// authorized loads the image under the caller's outlet grant; writes lock it.
func authorizedImage(tx *gorm.DB, tenant string, outlet, rx, file uuid.UUID, lock bool) (prescriptionImage, error) {
	var saved prescriptionImage
	q := tx.Select(prescriptionImageColumns)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Where("tenant_id=? AND outlet_id=? AND prescription_id=? AND id=?", tenant, outlet, rx, file).First(&saved).Error
	return saved, err
}

func validImageInput(in prescriptionImageInput) bool {
	return in.Filename == strings.TrimSpace(in.Filename) && in.Filename != "." && in.Filename != ".." && !strings.ContainsAny(in.Filename, "/\\") &&
		!strings.ContainsFunc(in.Filename, unicode.IsControl) && sha256Pattern.MatchString(in.Sha256)
}

func (h *prescriptionImageHandlers) putImage(c *gin.Context) {
	p, outlet, rx, id, ok := imageScope(c, true)
	if !ok {
		return
	}
	var in prescriptionImageInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validImageInput(in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a PNG or JPEG up to 10 MiB with a valid filename and checksum.")
		return
	}
	var saved prescriptionImage
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if _, err := loadPrescription(tx, p.TenantID, outlet, rx); err != nil {
			return err
		}
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":prescription-image:"+id.String()); err != nil {
			return err
		}
		var err error
		saved, err = authorizedImage(tx, p.TenantID, outlet, rx, id, false)
		if err == nil {
			if saved.Filename != in.Filename || saved.ContentType != in.ContentType || saved.Size != in.Size || saved.SHA256 != in.Sha256 {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// No private storage means no intent: nothing could ever be uploaded against it.
		if h.files == nil {
			return errEvidenceUnavailable
		}
		saved = prescriptionImage{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, PrescriptionID: rx, Filename: in.Filename, ContentType: in.ContentType, Size: in.Size, SHA256: in.Sha256, Status: "upload_pending", ScanAttempt: 1, UploaderSubject: p.Subject}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		created = true
		saved, err = authorizedImage(tx, p.TenantID, outlet, rx, id, false)
		return err
	})
	if imageError(c, err) {
		return
	}
	if created {
		respond.Created(c, saved)
	} else {
		respond.OK(c, saved)
	}
}

func (h *prescriptionImageHandlers) listImages(c *gin.Context, params pagination.Params) ([]prescriptionImage, error) {
	p, outlet, rx, _, ok := imageScope(c, false)
	if !ok {
		return nil, nil
	}
	var rows []prescriptionImage
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if _, err := loadPrescription(tx, p.TenantID, outlet, rx); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(prescriptionImageColumns).Where("tenant_id=? AND outlet_id=? AND prescription_id=?", p.TenantID, outlet, rx).Find(&rows).Error
	})
	if err != nil {
		imageError(c, err)
	}
	return rows, nil
}

func imageError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errEvidenceUnavailable) {
		// Uncertain storage or scan state never permits access to protected bytes.
		respond.Error(c, http.StatusServiceUnavailable, "evidence_unavailable", "Prescription image processing is unavailable. Retry the same request.")
		return true
	}
	stockError(c, err)
	return true
}
