package practitioner

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

type registrationReconciliation struct {
	ID               uuid.UUID       `json:"id"`
	RevisionID       uuid.UUID       `json:"revision_id"`
	ActiveReviewID   uuid.UUID       `json:"active_review_id"`
	AdverseReviewIDs json.RawMessage `json:"adverse_review_ids"`
	ReasonReference  uuid.UUID       `json:"reason_reference"`
	AuthorizedBy     string          `json:"authorized_by"`
	CreatedAt        time.Time       `json:"created_at"`
	IntentSHA256     string          `json:"-"`
}

func (r *registrationReconciliation) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}
func (r registrationReconciliation) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const reconciliationColumns = "id,revision_id,active_review_id,adverse_review_ids,reason_reference,created_by AS authorized_by,created_at,intent_sha256"

func reconciliationQuery(tx *gorm.DB, p authn.Principal, owner, reg uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_registration_reconciliations").Select(reconciliationColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=?", p.TenantID, owner, reg)
}
func (h *handlers) reconcileRegistrationSource(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "reconciliation")
	if !ok {
		return
	}
	var in *struct {
		RevisionID       string   `json:"revision_id"`
		ActiveReviewID   string   `json:"active_review_id"`
		AdverseReviewIDs []string `json:"adverse_review_ids"`
		ReasonReference  string   `json:"reason_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		respond.Error(c, 400, "invalid_request", "Expected reconciliation intent")
		return
	}
	_, ok = canonicalStewardReference(in.RevisionID)
	_, ok = parseReferencePair(in.ActiveReviewID, ok)
	_, ok = parseReferencePair(in.ReasonReference, ok)
	slices.Sort(in.AdverseReviewIDs)
	for i, value := range in.AdverseReviewIDs {
		_, ok = parseReferencePair(value, ok)
		if i > 0 && value == in.AdverseReviewIDs[i-1] {
			ok = false
		}
	}
	if !ok || len(in.AdverseReviewIDs) < 1 || len(in.AdverseReviewIDs) > 10 {
		respond.Error(c, 400, "invalid_request", "Expected canonical bounded reconciliation references")
		return
	}
	intent, err := json.Marshal(in)
	if err != nil {
		failure(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(intent))
	adverse, err := json.Marshal(in.AdverseReviewIDs)
	if err != nil {
		failure(c, err)
		return
	}
	if !h.reauthorize(c, PermRegistrationVerify) {
		return
	}
	if err := h.auditVerificationAttempt(c, p, owner, reg); err != nil {
		failure(c, err)
		return
	}
	if !h.reauthorize(c, PermRegistrationVerify) {
		return
	}
	var row registrationReconciliation
	created := false
	err = h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		if err := tx.Exec("SELECT set_config('app.practitioner_reconcile','true',true)").Error; err != nil {
			return err
		}
		var head struct{ ID uuid.UUID }
		if err := tx.Raw("SELECT id FROM practitioner_registrations WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, owner, reg).Scan(&head).Error; err != nil {
			return err
		}
		if head.ID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		err := reconciliationQuery(tx, p, owner, reg).Where("id=?", id).Take(&row).Error
		if err == nil {
			if row.IntentSHA256 != digest || row.AuthorizedBy != p.Subject {
				return errIntentConflict
			}
			return auditIDs(tx, p, "practitioner_registration_reconciliations", []uuid.UUID{id})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec("INSERT INTO practitioner_registration_reconciliations(tenant_id,practitioner_id,registration_id,id,revision_id,active_review_id,adverse_review_ids,reason_reference,intent_sha256,created_by) VALUES(?,?,?,?,?,?,?::jsonb,?,?,?)", p.TenantID, owner, reg, id, in.RevisionID, in.ActiveReviewID, string(adverse), in.ReasonReference, digest, p.Subject).Error; err != nil {
			return err
		}
		if err := reconciliationQuery(tx, p, owner, reg).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		data, err := json.Marshal(practitionercontract.RegistrationSourceReconciledData{PractitionerID: owner.String(), RegistrationID: reg.String(), ReconciliationID: id.String()})
		if err != nil {
			return err
		}
		if err := events.Enqueue(tx, practitionercontract.RegistrationSourceReconciled, events.Event{Type: practitionercontract.RegistrationSourceReconciled, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		created = true
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
func (h *handlers) getRegistrationReconciliation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "reconciliation")
	if !ok {
		return
	}
	var row registrationReconciliation
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := reconciliationQuery(tx, p, owner, reg).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_registration_reconciliations", []uuid.UUID{id})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listRegistrationReconciliations(c *gin.Context, params pagination.Params) ([]registrationReconciliation, error) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return nil, nil
	}
	var rows []registrationReconciliation
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var head struct{ ID uuid.UUID }
		if err := tx.Table("practitioner_registrations").Select("id").Where("tenant_id=? AND practitioner_id=? AND id=?", p.TenantID, owner, reg).Take(&head).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_registrations", []uuid.UUID{reg}); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(reconciliationQuery(tx, p, owner, reg), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registration_reconciliations", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
