package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var errClassificationVersion = errors.New("document classification version conflict")

func (r documentClassificationRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func classificationScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, hospital, ok := scope(c)
	if !ok {
		return p, hospital, uuid.Nil, uuid.Nil, false
	}
	staff, ok := documentStaff(c)
	if !ok {
		return p, hospital, staff, uuid.Nil, false
	}
	file, err := uuid.Parse(c.Param("file"))
	if err != nil || file == uuid.Nil {
		respond.NotFound(c, "staff document")
		return p, hospital, staff, file, false
	}
	return p, hospital, staff, file, true
}

func classificationOwner(tx *gorm.DB, p authn.Principal, hospital, staff, file uuid.UUID, writing bool) error {
	saved, err := authorizedDocument(tx, staffDocuments{}, p, hospital, staff, file, writing)
	if err != nil {
		return err
	}
	if !contains(saved.RecordKind, "enrollment", "credential", "qualification") || !contains(saved.Category, documentCategoryBindings[saved.RecordKind]...) {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func currentDocumentClassification(tx *gorm.DB, p authn.Principal, hospital, staff, file uuid.UUID) (documentClassification, error) {
	result := documentClassification{FileID: file, HospitalID: hospital, StaffID: staff}
	err := tx.Raw(`SELECT r.version,coalesce(r.classification_revision_id::text,'') AS classification_revision_id,coalesce(m.name,'') AS classification_name,r.created_at AS updated_at
 FROM workforce_document_classifications c
 JOIN workforce_document_classification_revisions r ON (r.tenant_id,r.hospital_id,r.file_id,r.version,r.id)=(c.tenant_id,c.hospital_id,c.id,c.version,c.revision_id)
 LEFT JOIN workforce_hr_master_revisions m ON (m.tenant_id,m.hospital_id,m.id,m.kind)=(r.tenant_id,r.hospital_id,r.classification_revision_id,r.classification_kind)
 WHERE c.tenant_id=? AND c.hospital_id=? AND c.id=?`, p.TenantID, hospital, file).Scan(&result).Error
	result.FileID, result.HospitalID, result.StaffID = file, hospital, staff
	if result.UpdatedAt != nil {
		value := result.UpdatedAt.UTC()
		result.UpdatedAt = &value
	}
	return result, err
}

func classificationReadAudit(tx *gorm.DB, p authn.Principal, hospital, file uuid.UUID) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.document_classification.viewed',?,'{}'::jsonb)`, p.TenantID, hospital, p.Subject, file).Error
}

func (h *handlers) getDocumentClassification(c *gin.Context) {
	p, hospital, staff, file, ok := classificationScope(c)
	if !ok {
		return
	}
	var result documentClassification
	err := h.documentTransaction(c, staffDocuments{}, p, hospital, false, func(tx *gorm.DB) error {
		if err := classificationOwner(tx, p, hospital, staff, file, false); err != nil {
			return err
		}
		var err error
		result, err = currentDocumentClassification(tx, p, hospital, staff, file)
		if err != nil {
			return err
		}
		return classificationReadAudit(tx, p, hospital, file)
	})
	if documentUploadError(c, err) {
		return
	}
	respond.OK(c, result)
}

func (h *handlers) putDocumentClassification(c *gin.Context) {
	p, hospital, staff, file, ok := classificationScope(c)
	if !ok {
		return
	}
	var in documentClassificationInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	reference := *in.ClassificationRevisionID
	if reference != "" {
		id, err := uuid.Parse(reference)
		if err != nil || id == uuid.Nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose a valid document classification.")
			return
		}
		reference = id.String()
	}
	var result documentClassification
	err := h.documentTransaction(c, staffDocuments{}, p, hospital, true, func(tx *gorm.DB) error {
		if err := classificationOwner(tx, p, hospital, staff, file, true); err != nil {
			return err
		}
		var err error
		result, err = currentDocumentClassification(tx, p, hospital, staff, file)
		if err != nil {
			return err
		}
		exactRetry := *in.ExpectedVersion == result.Version-1 && reference == result.ClassificationRevisionID
		if *in.ExpectedVersion != result.Version && !exactRetry {
			return errClassificationVersion
		}
		if reference == result.ClassificationRevisionID {
			return nil
		}
		name := ""
		if err := resolveMasterReferences(tx, p, hospital, []enrollmentMasterReference{{kind: "document_category", field: "classification", id: &reference, name: &name, previous: result.ClassificationRevisionID}}, map[string]bool{"classification": true}); err != nil {
			return err
		}
		if result.Version >= 9007199254740991 {
			return errClassificationVersion
		}
		version, revision := result.Version+1, uuid.New()
		var storedReference any
		if reference != "" {
			storedReference = reference
		}
		if err := tx.Exec(`INSERT INTO workforce_document_classification_revisions(tenant_id,hospital_id,file_id,id,version,classification_revision_id,actor_subject) VALUES(?,?,?,?,?,?,?)`, p.TenantID, hospital, file, revision, version, storedReference, p.Subject).Error; err != nil {
			return err
		}
		if result.Version == 0 {
			if err := tx.Exec(`INSERT INTO workforce_document_classifications(tenant_id,hospital_id,id,version,revision_id) VALUES(?,?,?,?,?)`, p.TenantID, hospital, file, version, revision).Error; err != nil {
				return err
			}
		} else {
			changed := tx.Exec(`UPDATE workforce_document_classifications SET version=?,revision_id=? WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`, version, revision, p.TenantID, hospital, file, result.Version)
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return errClassificationVersion
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.document_classification.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, file, version, result.Version).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.DocumentClassificationChangedData{HospitalID: hospital.String(), StaffID: staff.String(), FileID: file.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		subject := workforcecontract.SubjectDocumentClassificationChanged
		if err := h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		result, err = currentDocumentClassification(tx, p, hospital, staff, file)
		return err
	})
	if errors.Is(err, errClassificationVersion) {
		respond.Conflict(c, "Document classification changed. Reload before saving.")
		return
	}
	if respondMasterReferenceError(c, err) || documentUploadError(c, err) {
		return
	}
	respond.OK(c, result)
}

func (h *handlers) documentClassificationHistory(c *gin.Context, params pagination.Params) ([]documentClassificationRevision, error) {
	p, hospital, staff, file, ok := classificationScope(c)
	if !ok {
		return nil, nil
	}
	rows := []documentClassificationRevision{}
	err := h.documentTransaction(c, staffDocuments{}, p, hospital, false, func(tx *gorm.DB) error {
		if err := classificationOwner(tx, p, hospital, staff, file, false); err != nil {
			return err
		}
		query := tx.Table("workforce_document_classification_revisions AS r").Select(`r.id,r.file_id,r.hospital_id,?::uuid AS staff_id,r.version,coalesce(r.classification_revision_id::text,'') AS classification_revision_id,coalesce(m.name,'') AS classification_name,r.actor_subject,r.created_at`, staff).
			Joins(`LEFT JOIN workforce_hr_master_revisions m ON (m.tenant_id,m.hospital_id,m.id,m.kind)=(r.tenant_id,r.hospital_id,r.classification_revision_id,r.classification_kind)`).
			Where("r.tenant_id=? AND r.hospital_id=? AND r.file_id=?", p.TenantID, hospital, file)
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS classification_history", query), params).Scan(&rows).Error; err != nil {
			return err
		}
		return classificationReadAudit(tx, p, hospital, file)
	})
	if err != nil {
		documentUploadError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].CreatedAt = rows[i].CreatedAt.UTC()
	}
	return rows, nil
}
