package pharmacy

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

type supplierCreditVoidRow struct {
	ID, TenantID, OutletID, CreditID uuid.UUID
	Fields                           SupplierCreditVoidInput `gorm:"embedded"`
	IntentSHA256, ActorSubject       string
	CreatedAt                        time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditVoidRow) TableName() string { return "pharmacy_supplier_credit_voids" }

const supplierCreditVoidColumns = "id,tenant_id,outlet_id,credit_id,expected_version,reason,intent_sha256,actor_subject,created_at" // #nosec G101 -- Static SQL identifiers; no credential values.

func (r supplierCreditVoidRow) receipt() (SupplierCreditVoidReceipt, error) {
	if r.Fields.ExpectedVersion == nil || *r.Fields.ExpectedVersion < 1 || *r.Fields.ExpectedVersion >= 1000000000000 {
		return SupplierCreditVoidReceipt{}, errSupplierCreditEvidence
	}
	return SupplierCreditVoidReceipt{SupplierCreditVoidInput: r.Fields, ID: r.ID, OutletID: r.OutletID, CreditID: r.CreditID, Version: *r.Fields.ExpectedVersion + 1, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}
func (h *catalogueHandlers) putSupplierCreditVoid(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "entry")
	if !ok {
		return
	}
	var in SupplierCreditVoidInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditVoid(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use an explicit current credit version and void reason.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditVoidReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		var row supplierCreditVoidRow
		err := tx.Select(supplierCreditVoidColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&row).Error
		if err == nil {
			if row.CreditID != credit || row.ActorSubject != p.Subject || row.IntentSHA256 != digest {
				return errSupplierCreditConflict
			}
			receipt, err = row.receipt()
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row = supplierCreditVoidRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, CreditID: credit, Fields: in, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Select(supplierCreditVoidColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&row).Error; err != nil {
			return err
		}
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: credit.String(), EntryID: id.String(), Operation: "voided"})
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) getSupplierCreditVoid(c *gin.Context) { h.readSupplierCreditVoid(c, false) }
func (h *catalogueHandlers) recoverSupplierCreditVoid(c *gin.Context) {
	h.readSupplierCreditVoid(c, true)
}
func (h *catalogueHandlers) readSupplierCreditVoid(c *gin.Context, recovery bool) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	var entry uuid.UUID
	if recovery {
		entry, ok = supplierCreditID(c, "entry")
		if !ok {
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditVoidReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if recovery {
			if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
				return err
			}
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		var row supplierCreditVoidRow
		q := tx.Select(supplierCreditVoidColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=?", p.TenantID, outlet, credit)
		if recovery {
			q = q.Where("id=? AND actor_subject=?", entry, p.Subject)
		}
		if err := q.Take(&row).Error; err != nil {
			return err
		}
		var err error
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		resource, action := "supplier_credit_void", "viewed"
		if recovery {
			resource, action = "supplier_credit_void_receipt", "recovery_read"
		}
		return auditRegisterRead(c, tx, resource, p.TenantID, p.Subject, row.ID, action)
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
