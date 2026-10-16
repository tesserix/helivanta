package supply

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type evidence struct {
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"-"`
	PartyID              uuid.UUID  `json:"party_id"`
	LicenseID            uuid.UUID  `json:"license_id"`
	LicenseVersion       int64      `json:"license_version"`
	Filename             string     `json:"filename"`
	ContentType          string     `json:"content_type"`
	Size                 int64      `json:"size"`
	SHA256               string     `json:"-"`
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

func (evidence) TableName() string                 { return "supply_license_evidence" }
func (e evidence) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

const evidenceColumns = "id,tenant_id,party_id,license_id,license_version,filename,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,uploader_subject,expires_at,created_at,updated_at"

type evidenceHandlers struct {
	*handlers
	files *attachments.GCS
}

func (h *evidenceHandlers) license(tx *gorm.DB, p authn.Principal, party, id uuid.UUID, roles []string) (license, error) {
	var saved license
	if err := requireRoles(tx, p, party, roles); err != nil {
		return saved, err
	}
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(licenseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, party, id).First(&saved).Error
	return saved, err
}
func evidenceScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, party, ok := registryScope(c)
	if !ok {
		return p, party, uuid.Nil, false
	}
	id, ok := resourceID(c, "license")
	return p, party, id, ok
}
func validEvidenceInput(in evidenceInput) bool {
	return in.Filename == strings.TrimSpace(in.Filename) && in.Filename != "." && in.Filename != ".." && !strings.ContainsAny(in.Filename, "/\\") && !strings.ContainsFunc(in.Filename, unicode.IsControl) && validEvidenceFieldsPatterns(in.EvidenceFields) && binding.Validator.ValidateStruct(&in) == nil
}
func (h *evidenceHandlers) putEvidence(c *gin.Context) {
	p, party, licence, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "file")
	if !ok {
		return
	}
	var in evidenceInput
	if !decode(c, &in) {
		return
	}
	if !validEvidenceInput(in) {
		respond.Error(c, 400, "invalid_request", "Use a PNG or JPEG up to 10 MiB with a valid filename, checksum and licence revision.")
		return
	}
	var saved evidence
	created := false
	err := h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		owner, err := h.license(tx, p, party, licence, licenseWriters)
		if err != nil {
			return err
		}
		if err := lockRegistry(tx, p, party, id, "evidence"); err != nil {
			return err
		}
		err = tx.Select(evidenceColumns).Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", p.TenantID, party, licence, id).First(&saved).Error
		if err == nil {
			if saved.LicenseVersion != *in.ExpectedLicenseVersion || saved.Filename != in.Filename || saved.ContentType != in.ContentType || saved.Size != in.Size || saved.SHA256 != in.Sha256 {
				return errConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if owner.Version != *in.ExpectedLicenseVersion {
			return errConflict
		}
		if h.files == nil {
			return errEvidenceUnavailable
		}
		saved = evidence{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: party, LicenseID: licence, LicenseVersion: owner.Version, Filename: in.Filename, ContentType: in.ContentType, Size: in.Size, SHA256: in.Sha256, Status: "upload_pending", ScanAttempt: 1, UploaderSubject: p.Subject}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		created = true
		return tx.Select(evidenceColumns).Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", p.TenantID, party, licence, id).First(&saved).Error
	})
	if evidenceError(c, err) {
		return
	}
	if created {
		respond.Created(c, saved)
	} else {
		respond.OK(c, saved)
	}
}
func (h *evidenceHandlers) listEvidence(c *gin.Context, params pagination.Params) ([]evidence, error) {
	p, party, licence, ok := evidenceScope(c)
	if !ok {
		return nil, nil
	}
	var rows []evidence
	err := h.transaction(c.Request.Context(), p, party, false, false, func(tx *gorm.DB) error {
		if _, err := h.license(tx, p, party, licence, registryReaders); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(evidenceColumns).Where("tenant_id=? AND party_id=? AND license_id=?", p.TenantID, party, licence).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'evidence_list','supply_licenses',?)`, p.TenantID, p.Subject, licence).Error
	})
	if err != nil {
		evidenceError(c, err)
	}
	return rows, nil
}

var errEvidenceUnavailable = errors.New("private licence evidence dependency unavailable")

func evidenceError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errEvidenceUnavailable) {
		// Uncertain storage or scan state never permits access to protected bytes.
		respond.Error(c, 503, "evidence_unavailable", "Licence evidence processing is unavailable. Retry the same request.")
	} else {
		registryError(c, err)
	}
	return true
}
