package pharmacy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

type supplierReturnCancellationRow struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	OutletID     uuid.UUID
	ReturnID     uuid.UUID
	Reason       string
	IntentSHA256 string
	ActorSubject string
	CreatedAt    time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierReturnCancellationRow) TableName() string {
	return "pharmacy_supplier_return_cancellations"
}

const supplierReturnCancellationColumns = "id,tenant_id,outlet_id,return_id,reason,intent_sha256,actor_subject,created_at"

func (r supplierReturnCancellationRow) receipt() SupplierReturnCancellationReceipt {
	version := int64(0)
	return SupplierReturnCancellationReceipt{SupplierReturnCancelInput: SupplierReturnCancelInput{ExpectedVersion: &version, Reason: r.Reason}, ID: r.ID, OutletID: r.OutletID, ReturnID: r.ReturnID, Version: 1, State: "cancelled", CancelledBy: r.ActorSubject, CancelledAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
func (h *catalogueHandlers) cancelSupplierReturn(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	parent, ok := supplierReturnID(c, "return")
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "cancellation")
	if !ok {
		return
	}
	var in SupplierReturnCancelInput
	if !decodeStockCommand(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedVersion == nil || !stocktakeText(in.Reason, 500) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Provide the current draft version and cancellation reason.")
		return
	}
	if *in.ExpectedVersion != 0 {
		supplierReturnError(c, errSupplierReturnConflict)
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnCancellationReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierReturnLock(tx, p.TenantID, outlet, parent); err != nil {
			return err
		}
		if _, _, err := loadSupplierReturn(tx, p.TenantID, outlet, parent); err != nil {
			return err
		}
		var row supplierReturnCancellationRow
		err := tx.Select(supplierReturnCancellationColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&row).Error
		if err == nil {
			if row.ReturnID != parent || row.ActorSubject != p.Subject || row.IntentSHA256 != digest {
				return errSupplierReturnConflict
			}
			receipt = row.receipt()
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var cancelled int64
		if err := tx.Model(&supplierReturnCancellationRow{}).Where("tenant_id=? AND outlet_id=? AND return_id=?", p.TenantID, outlet, parent).Count(&cancelled).Error; err != nil {
			return err
		}
		if cancelled != 0 {
			return errSupplierReturnConflict
		}
		row = supplierReturnCancellationRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ReturnID: parent, Reason: in.Reason, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		receipt = row.receipt()
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) getSupplierReturnCancellation(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	parent, ok := supplierReturnID(c, "return")
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "cancellation")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var row supplierReturnCancellationRow
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Select(supplierReturnCancellationColumns).Where("tenant_id=? AND outlet_id=? AND return_id=? AND id=? AND actor_subject=?", p.TenantID, outlet, parent, id, p.Subject).Take(&row).Error; err != nil {
			return err
		}
		if err := auditRegisterRead(c, tx, "supplier_return_cancellations", p.TenantID, p.Subject, id, "recover"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, row.receipt())
}
