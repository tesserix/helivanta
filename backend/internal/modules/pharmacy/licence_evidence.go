package pharmacy

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// An outlet licence image follow the supply licence evidence lifecycle
// (#1386 B-2a): intent, exact upload, quarantine, scan by the pharmacy
// evidence worker, release. Only a released page can be downloaded.

type licenceEvidenceInput struct{ LicenceEvidenceFields }

type licenceEvidence struct {
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"-"`
	OutletID             uuid.UUID  `json:"outlet_id"`
	LicenceID            uuid.UUID  `json:"licence_id"`
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

func (licenceEvidence) TableName() string                 { return "pharmacy_licence_evidence" }
func (e licenceEvidence) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

const licenceEvidenceColumns = "id,tenant_id,outlet_id,licence_id,filename,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,uploader_subject,expires_at,created_at,updated_at"

var errLicenceEvidenceUnavailable = errors.New("private licence evidence dependency unavailable")

type licenceEvidenceHandlers struct {
	*catalogueHandlers
	files *attachments.GCS
}

func licenceEvidenceRoutes(s *platform.Router, h *catalogueHandlers, files *attachments.GCS) {
	e := &licenceEvidenceHandlers{catalogueHandlers: h, files: files}
	s.PUT("/licences/:id/evidence/:file", PermLicenceManage, e.putLicenceEvidence)
	s.PUT("/licences/:id/evidence/:file/content", PermLicenceManage, e.uploadLicenceEvidence)
	s.POST("/licences/:id/evidence/:file/retry/:attempt", PermLicenceManage, e.retryLicenceEvidence)
	s.GET("/licences/:id/evidence/:file/download", PermLicenceEvidenceRead, e.downloadLicenceEvidence)
	platform.ListRoute(s, "/licences/:id/evidence", PermLicenceEvidenceRead, e.listLicenceEvidence)
}

func (e licenceEvidence) objectScope() attachments.ObjectScope {
	return attachments.ObjectScope{TenantID: e.TenantID, OwnerKind: "pharmacy_outlet", OwnerID: e.OutletID, RecordKind: "licence", RecordID: e.LicenceID, FileID: e.ID, Version: 1, UploadedAt: *e.UploadedAt}
}

// licenceEvidenceScope resolves the outlet, licence and (when named) file from the path.
func licenceEvidenceScope(c *gin.Context, withFile bool) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	licence, ok := catalogueID(c)
	if !ok || !withFile {
		return p, outlet, licence, uuid.Nil, ok
	}
	file, err := uuid.Parse(c.Param("file"))
	if err != nil || file.Version() != 7 || file.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "licence evidence")
		return p, outlet, licence, uuid.Nil, false
	}
	return p, outlet, licence, file, true
}

// authorized loads the image under the caller's outlet grant; writes lock it.
func authorizedLicenceEvidence(tx *gorm.DB, tenant string, outlet, licence, file uuid.UUID, lock bool) (licenceEvidence, error) {
	var saved licenceEvidence
	q := tx.Select(licenceEvidenceColumns)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Where("tenant_id=? AND outlet_id=? AND licence_id=? AND id=?", tenant, outlet, licence, file).First(&saved).Error
	return saved, err
}

func validLicenceEvidenceInput(in licenceEvidenceInput) bool {
	return in.Filename == strings.TrimSpace(in.Filename) && in.Filename != "." && in.Filename != ".." && !strings.ContainsAny(in.Filename, "/\\") &&
		!strings.ContainsFunc(in.Filename, unicode.IsControl) && sha256Pattern.MatchString(in.Sha256)
}

func (h *licenceEvidenceHandlers) putLicenceEvidence(c *gin.Context) {
	p, outlet, licence, id, ok := licenceEvidenceScope(c, true)
	if !ok {
		return
	}
	var in licenceEvidenceInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validLicenceEvidenceInput(in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a PNG or JPEG up to 10 MiB with a valid filename and checksum.")
		return
	}
	var saved licenceEvidence
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if _, err := loadLicenceEvidenceDeclaration(tx, p.TenantID, outlet, licence); err != nil {
			return err
		}
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":licence-evidence:"+id.String()); err != nil {
			return err
		}
		var err error
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, id, false)
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
			return errLicenceEvidenceUnavailable
		}
		saved = licenceEvidence{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, LicenceID: licence, Filename: in.Filename, ContentType: in.ContentType, Size: in.Size, SHA256: in.Sha256, Status: "upload_pending", ScanAttempt: 1, UploaderSubject: p.Subject}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		created = true
		saved, err = authorizedLicenceEvidence(tx, p.TenantID, outlet, licence, id, false)
		return err
	})
	if licenceEvidenceError(c, err) {
		return
	}
	if created {
		respond.Created(c, saved)
	} else {
		respond.OK(c, saved)
	}
}

func (h *licenceEvidenceHandlers) listLicenceEvidence(c *gin.Context, params pagination.Params) ([]licenceEvidence, error) {
	p, outlet, licence, _, ok := licenceEvidenceScope(c, false)
	if !ok {
		return nil, nil
	}
	var rows []licenceEvidence
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if _, err := loadLicenceEvidenceDeclaration(tx, p.TenantID, outlet, licence); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(licenceEvidenceColumns).Where("tenant_id=? AND outlet_id=? AND licence_id=?", p.TenantID, outlet, licence).Find(&rows).Error
	})
	if err != nil {
		licenceEvidenceError(c, err)
	}
	return rows, nil
}

func licenceEvidenceError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "pharmacy_licence_evidence_single" {
		respond.Conflict(c, "This licence already has an evidence file. Record a new licence declaration if its document needs correction.")
		return true
	}
	if errors.As(err, &pg) && pg.ConstraintName == "pharmacy_licence_evidence_reviewed" {
		respond.Conflict(c, "This licence has already been reviewed. Record a new declaration for different evidence.")
		return true
	}
	if errors.Is(err, errLicenceEvidenceUnavailable) {
		// Uncertain storage or scan state never permits access to protected bytes.
		respond.Error(c, http.StatusServiceUnavailable, "evidence_unavailable", "Licence evidence processing is unavailable. Retry the same request.")
		return true
	}
	if errors.Is(err, errStockIdentity) {
		respond.Conflict(c, "The evidence intent changed or is no longer waiting for this step. Retry the original request or reload its status.")
		return true
	}
	stockError(c, err)
	return true
}

// loadLicenceEvidenceDeclaration checks the immutable owner record inside outlet scope.
func loadLicenceEvidenceDeclaration(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (outletLicence, error) {
	var saved outletLicence
	err := tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND l.id=?`, tenant, outlet, id).Scan(&saved).Error
	if err == nil && saved.ID == uuid.Nil {
		err = gorm.ErrRecordNotFound
	}
	return saved, err
}
