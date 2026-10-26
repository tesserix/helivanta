package practitioner

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type registrationReview struct {
	ID                 uuid.UUID       `json:"id"`
	RevisionID         uuid.UUID       `json:"revision_id"`
	AppointmentID      uuid.UUID       `json:"appointment_id"`
	AppointmentVersion int64           `json:"appointment_version"`
	ReviewReference    uuid.UUID       `json:"review_reference"`
	SourceReference    uuid.UUID       `json:"source_reference"`
	SourceCheckedAt    time.Time       `json:"source_checked_at"`
	Finding            string          `json:"finding"`
	EvidenceIDs        json.RawMessage `json:"evidence_ids"`
	Reviewer           string          `json:"reviewer"`
	CreatedAt          time.Time       `json:"created_at"`
}

func (r *registrationReview) AfterFind(*gorm.DB) error {
	r.SourceCheckedAt = r.SourceCheckedAt.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}
func (r registrationReview) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const reviewColumns = "id,revision_id,appointment_id,appointment_version,review_reference,source_reference,source_checked_at,finding,evidence_ids,reviewer,created_at"

func reviewQuery(tx *gorm.DB, p authn.Principal, parent evidenceParent) *gorm.DB {
	return tx.Table("practitioner_registration_reviews").Select(reviewColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=? AND revision_id=?", p.TenantID, parent.practitioner, parent.registration, parent.revision)
}
func reviewAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return auditIDs(tx, p, "practitioner_registration_reviews", []uuid.UUID{id})
}
func (h *handlers) putRegistrationReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "review")
	if !ok {
		return
	}
	var in *struct {
		AppointmentID      string   `json:"appointment_id"`
		AppointmentVersion int64    `json:"appointment_version"`
		ReviewReference    string   `json:"review_reference"`
		SourceReference    string   `json:"source_reference"`
		SourceCheckedAt    string   `json:"source_checked_at"`
		Finding            string   `json:"finding"`
		EvidenceIDs        []string `json:"evidence_ids"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		respond.Error(c, 400, "invalid_request", "Expected a revision-bound source review")
		return
	}
	appointment, aOK := canonicalStewardReference(in.AppointmentID)
	reference, rOK := canonicalStewardReference(in.ReviewReference)
	source, sOK := canonicalStewardReference(in.SourceReference)
	checked, tOK := stewardUntil(in.SourceCheckedAt)
	validFinding := in.Finding == "active" || in.Finding == "suspended" || in.Finding == "expired" || in.Finding == "not_found" || in.Finding == "unavailable"
	if !aOK || !rOK || !sOK || !tOK || in.AppointmentVersion != 2 || !validFinding || len(in.EvidenceIDs) < 1 || len(in.EvidenceIDs) > 10 {
		respond.Error(c, 400, "invalid_request", "Expected bounded source review evidence and accepted appointment")
		return
	}
	sort.Strings(in.EvidenceIDs)
	for i, file := range in.EvidenceIDs {
		if _, valid := canonicalStewardReference(file); !valid || i > 0 && file == in.EvidenceIDs[i-1] {
			respond.Error(c, 400, "invalid_request", "Expected unique canonical evidence IDs")
			return
		}
	}
	evidence, err := json.Marshal(in.EvidenceIDs)
	if err != nil {
		failure(c, err)
		return
	}
	if !h.reauthorize(c, PermRegistrationReview) {
		return
	}
	var row registrationReview
	created := false
	err = h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		if err := tx.Exec("SELECT set_config('app.practitioner_review','true',true)").Error; err != nil {
			return err
		}
		var head struct{ Version int64 }
		if err := tx.Raw("SELECT version FROM practitioner_registrations WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, parent.practitioner, parent.registration).Scan(&head).Error; err != nil {
			return err
		}
		if head.Version == 0 {
			return gorm.ErrRecordNotFound
		}
		err := reviewQuery(tx, p, parent).Where("id=?", id).Take(&row).Error
		if err == nil {
			var saved []string
			if json.Unmarshal(row.EvidenceIDs, &saved) != nil || row.AppointmentID != appointment || row.AppointmentVersion != in.AppointmentVersion || row.ReviewReference != reference || row.SourceReference != source || !row.SourceCheckedAt.Equal(checked) || row.Finding != in.Finding || row.Reviewer != p.Subject || !slices.Equal(saved, in.EvidenceIDs) {
				return errIntentConflict
			}
			return reviewAudit(tx, p, id)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var revision registrationRevision
		if err := readRevision(tx, p, parent.practitioner, parent.registration, parent.revision, &revision); err != nil {
			return err
		}
		if revision.Version != head.Version {
			return errIntentConflict
		}
		if err := tx.Exec("INSERT INTO practitioner_registration_reviews(tenant_id,practitioner_id,registration_id,revision_id,id,appointment_id,appointment_version,review_reference,source_reference,source_checked_at,finding,evidence_ids,reviewer) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", p.TenantID, parent.practitioner, parent.registration, parent.revision, id, appointment, in.AppointmentVersion, reference, source, checked, in.Finding, string(evidence), p.Subject).Error; err != nil {
			return err
		}
		data, err := json.Marshal(practitionercontract.ReviewRecordedData{PractitionerID: parent.practitioner.String(), RegistrationID: parent.registration.String(), RevisionID: parent.revision.String(), ReviewID: id.String()})
		if err != nil {
			return err
		}
		if err := events.Enqueue(tx, practitionercontract.ReviewRecorded, events.Event{Type: practitionercontract.ReviewRecorded, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		created = true
		return reviewQuery(tx, p, parent).Where("id=?", id).Take(&row).Error
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
func (h *handlers) getRegistrationReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "review")
	if !ok {
		return
	}
	var row registrationReview
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := reviewQuery(tx, p, parent).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return reviewAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listRegistrationReviews(c *gin.Context, params pagination.Params) ([]registrationReview, error) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return nil, nil
	}
	var rows []registrationReview
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var revision registrationRevision
		if err := readRevision(tx, p, parent.practitioner, parent.registration, parent.revision, &revision); err != nil {
			return err
		}
		if err := revisionAudit(tx, p, parent.revision); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(reviewQuery(tx, p, parent), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registration_reviews", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
