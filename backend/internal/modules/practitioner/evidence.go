package practitioner

import (
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

var evidenceDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type evidenceInput struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

func (in evidenceInput) valid() bool {
	return (in.ContentType == "image/png" || in.ContentType == "image/jpeg") && in.Size >= 1 && in.Size <= attachments.MaxFileBytes && evidenceDigest.MatchString(in.SHA256)
}

type credentialEvidence struct {
	ScanAttempt          int64      `json:"scan_attempt"`
	ID                   uuid.UUID  `json:"id"`
	PractitionerID       uuid.UUID  `json:"practitioner_id"`
	CredentialID         uuid.UUID  `json:"-"`
	CredentialKind       string     `json:"-"`
	RegistrationID       *uuid.UUID `json:"registration_id,omitempty" gorm:"-"`
	QualificationID      *uuid.UUID `json:"qualification_id,omitempty" gorm:"-"`
	RevisionID           uuid.UUID  `json:"revision_id"`
	ContentType          string     `json:"content_type"`
	Size                 int64      `json:"size"`
	SHA256               string     `json:"sha256"`
	UploadedAt           *time.Time `json:"uploaded_at"`
	SourceGeneration     *int64     `json:"-"`
	ReleasedGeneration   *int64     `json:"-"`
	EngineVersion        *string    `json:"-"`
	DefinitionVersion    *int64     `json:"-"`
	DefinitionsUpdatedAt *time.Time `json:"-"`
	ScannedAt            *time.Time `json:"scanned_at"`
	FailureCode          *string    `json:"failure_code"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
}

func (credentialEvidence) TableName() string { return "practitioner_credential_evidence" }
func (e *credentialEvidence) AfterFind(*gorm.DB) error {
	e.RegistrationID = nil
	e.QualificationID = nil
	switch e.CredentialKind {
	case "registration":
		e.RegistrationID = &e.CredentialID
	case "qualification":
		e.QualificationID = &e.CredentialID
	default:
		return errors.New("invalid credential evidence scope")
	}

	e.CreatedAt = e.CreatedAt.UTC()
	e.ExpiresAt = e.ExpiresAt.UTC()
	if e.ScannedAt != nil {
		v := e.ScannedAt.UTC()
		e.ScannedAt = &v
	}
	if e.UploadedAt != nil {
		v := e.UploadedAt.UTC()
		e.UploadedAt = &v
	}
	return nil
}
func (e credentialEvidence) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

const evidenceColumns = "scan_attempt,id,practitioner_id,credential_id,credential_kind,revision_id,content_type,size,sha256,status,created_at,expires_at,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code"

type evidenceParent struct {
	practitioner, credential, revision uuid.UUID
	kind                               string
}

func evidenceScope(c *gin.Context) (authn.Principal, evidenceParent, bool) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, evidenceParent{}, false
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return p, evidenceParent{}, false
	}
	kind, param := "registration", "registration"
	if c.Param("qualification") != "" {
		kind, param = "qualification", "qualification"
	}
	credential, ok := resourceID(c, param)
	if !ok {
		return p, evidenceParent{}, false
	}
	revision, ok := resourceID(c, "revision")
	if !ok {
		return p, evidenceParent{}, false
	}
	return p, evidenceParent{owner, credential, revision, kind}, true
}
func evidenceQuery(tx *gorm.DB, p authn.Principal, parent evidenceParent) *gorm.DB {
	return tx.Table("practitioner_credential_evidence").Select(evidenceColumns).Where("tenant_id=? AND practitioner_id=? AND credential_id=? AND credential_kind=? AND revision_id=?", p.TenantID, parent.practitioner, parent.credential, parent.kind, parent.revision)
}
func evidenceAccess(tx *gorm.DB, manage bool) error {
	return tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage',?,true)", strconv.FormatBool(manage)).Error
}
func evidenceAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_credential_evidence',?)", p.TenantID, p.Subject, id).Error
}
func (h *handlers) putEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "file")
	if !ok {
		return
	}
	var in *evidenceInput
	if !decode(c, &in) {
		return
	}
	if in == nil || !in.valid() {
		respond.Error(c, 400, "invalid_request", "Expected bounded private registration evidence metadata")
		return
	}
	var row credentialEvidence
	created := false
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, true); e != nil {
			return e
		}
		if _, e := credentialRevisionVersion(tx, p, parent); e != nil {
			return e
		}
		result := tx.Exec("INSERT INTO practitioner_credential_evidence(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,id,content_type,size,sha256,created_by) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, parent.practitioner, parent.credential, parent.kind, parent.revision, id, in.ContentType, in.Size, in.SHA256, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if e := evidenceQuery(tx, p, parent).Where("id=?", id).Take(&row).Error; e != nil {
			return e
		}
		if row.ContentType != in.ContentType || row.Size != in.Size || row.SHA256 != in.SHA256 {
			return errIntentConflict
		}
		if !created {
			return evidenceAudit(tx, p, id)
		}
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *handlers) getEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "file")
	if !ok {
		return
	}
	var row credentialEvidence
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, false); e != nil {
			return e
		}
		if e := evidenceQuery(tx, p, parent).Where("id=?", id).Take(&row).Error; e != nil {
			return e
		}
		return evidenceAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listEvidence(c *gin.Context, params pagination.Params) ([]credentialEvidence, error) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return nil, nil
	}
	var rows []credentialEvidence
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := evidenceAccess(tx, false); e != nil {
			return e
		}
		if _, e := credentialRevisionVersion(tx, p, parent); e != nil {
			return e
		}
		if e := credentialRevisionAudit(tx, p, parent); e != nil {
			return e
		}
		if e := tenantdb.ApplyKeyset(evidenceQuery(tx, p, parent), params).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_credential_evidence", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
