package practitioner

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

type reconciliationWithdrawal struct {
	ID               uuid.UUID `json:"id"`
	ReconciliationID uuid.UUID `json:"reconciliation_id"`
	ReasonReference  uuid.UUID `json:"reason_reference"`
	AuthorizedBy     string    `json:"authorized_by"`
	CreatedAt        time.Time `json:"created_at"`
	IntentSHA256     string    `json:"-"`
}

func (r *reconciliationWithdrawal) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}
func (r reconciliationWithdrawal) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const reconciliationWithdrawalColumns = "id,reconciliation_id,reason_reference,created_by AS authorized_by,created_at,intent_sha256"

func reconciliationWithdrawalQuery(tx *gorm.DB, p authn.Principal, owner, reg, target uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_reconciliation_withdrawals").Select(reconciliationWithdrawalColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=? AND reconciliation_id=?", p.TenantID, owner, reg, target)
}
func (h *handlers) withdrawRegistrationReconciliation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	target, ok := resourceID(c, "reconciliation")
	if !ok {
		return
	}
	id, ok := resourceID(c, "withdrawal")
	if !ok {
		return
	}
	var in *struct {
		ReasonReference string `json:"reason_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		respond.Error(c, 400, "invalid_request", "Expected withdrawal reason reference")
		return
	}
	reason, ok := canonicalStewardReference(in.ReasonReference)
	if !ok {
		respond.Error(c, 400, "invalid_request", "Expected canonical withdrawal reason reference")
		return
	}
	intent, err := json.Marshal(struct{ Owner, Registration, Reconciliation, Reason uuid.UUID }{owner, reg, target, reason})
	if err != nil {
		failure(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(intent))
	if !h.reauthorize(c, PermRegistrationVerificationRevoke) {
		return
	}
	if err := h.auditVerificationAttempt(c, p, owner, reg); err != nil {
		failure(c, err)
		return
	}
	if !h.reauthorize(c, PermRegistrationVerificationRevoke) {
		return
	}
	var row reconciliationWithdrawal
	created := false
	err = h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var head struct{ ID uuid.UUID }
		if err := tx.Raw("SELECT id FROM practitioner_registrations WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, owner, reg).Scan(&head).Error; err != nil {
			return err
		}
		if head.ID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		err := reconciliationWithdrawalQuery(tx, p, owner, reg, target).Where("id=?", id).Take(&row).Error
		if err == nil {
			if row.IntentSHA256 != digest || row.AuthorizedBy != p.Subject {
				return errIntentConflict
			}
			return auditIDs(tx, p, "practitioner_reconciliation_withdrawals", []uuid.UUID{id})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec("SELECT set_config('app.practitioner_reconciliation_withdraw','true',true)").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO practitioner_reconciliation_withdrawals(tenant_id,practitioner_id,registration_id,reconciliation_id,id,reason_reference,intent_sha256,created_by) VALUES(?,?,?,?,?,?,?,?)", p.TenantID, owner, reg, target, id, reason, digest, p.Subject).Error; err != nil {
			return err
		}
		if err := reconciliationWithdrawalQuery(tx, p, owner, reg, target).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		data, err := json.Marshal(practitionercontract.RegistrationReconciliationWithdrawnData{PractitionerID: owner.String(), RegistrationID: reg.String(), ReconciliationID: target.String(), WithdrawalID: id.String()})
		if err != nil {
			return err
		}
		if err := events.Enqueue(tx, practitionercontract.RegistrationReconciliationWithdrawn, events.Event{Type: practitionercontract.RegistrationReconciliationWithdrawn, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
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
func (h *handlers) getRegistrationReconciliationWithdrawal(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	target, ok := resourceID(c, "reconciliation")
	if !ok {
		return
	}
	id, ok := resourceID(c, "withdrawal")
	if !ok {
		return
	}
	var row reconciliationWithdrawal
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := reconciliationWithdrawalQuery(tx, p, owner, reg, target).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_reconciliation_withdrawals", []uuid.UUID{id})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listRegistrationReconciliationWithdrawals(c *gin.Context, params pagination.Params) ([]reconciliationWithdrawal, error) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return nil, nil
	}
	target, ok := resourceID(c, "reconciliation")
	if !ok {
		return nil, nil
	}
	var rows []reconciliationWithdrawal
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var parent registrationReconciliation
		if err := reconciliationQuery(tx, p, owner, reg).Where("id=?", target).Take(&parent).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_registration_reconciliations", []uuid.UUID{target}); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(reconciliationWithdrawalQuery(tx, p, owner, reg, target), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_reconciliation_withdrawals", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
