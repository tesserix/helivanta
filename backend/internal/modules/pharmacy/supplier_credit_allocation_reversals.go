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

type supplierCreditAllocationReversalRow struct {
	ID, TenantID, OutletID, CreditID, AllocationID, PayableID uuid.UUID
	Fields                                                    SupplierCreditAllocationReversalInput `gorm:"embedded"`
	AmountMinor                                               int64
	AvailableMinor, OutstandingMinor                          int64  `gorm:"->"`
	PayableReference, CreditDocumentReference, PayeeName      string `gorm:"->"`
	IntentSHA256, ActorSubject                                string
	CreatedAt                                                 time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditAllocationReversalRow) TableName() string {
	return "pharmacy_supplier_credit_allocation_reversals"
}

const supplierCreditAllocationReversalColumns = "id,tenant_id,outlet_id,credit_id,allocation_id,payable_id,expected_version,expected_available_minor,expected_payable_version,expected_outstanding_minor,amount_minor,to_char(occurred_on,'YYYY-MM-DD') AS occurred_on,reason,available_minor,outstanding_minor,payable_reference,credit_document_reference,payee_name,intent_sha256,actor_subject,created_at" // #nosec G101 -- Static SQL column identifiers, not credentials.
func (r supplierCreditAllocationReversalRow) receipt() (SupplierCreditAllocationReversalReceipt, error) {
	in := r.Fields
	if !normalizeSupplierCreditAllocationReversal(&in) || r.AmountMinor < 1 || r.AvailableMinor != *in.ExpectedAvailableMinor+r.AmountMinor || r.OutstandingMinor != *in.ExpectedOutstandingMinor+r.AmountMinor {
		return SupplierCreditAllocationReversalReceipt{}, errSupplierCreditEvidence
	}
	return SupplierCreditAllocationReversalReceipt{SupplierCreditAllocationReversalInput: r.Fields, ID: r.ID, OutletID: r.OutletID, CreditID: r.CreditID, AllocationID: r.AllocationID, PayableID: r.PayableID, AmountMinor: r.AmountMinor, Version: *in.ExpectedVersion + 1, PayableVersion: *in.ExpectedPayableVersion + 1, AvailableMinor: r.AvailableMinor, OutstandingMinor: r.OutstandingMinor, PayableReference: r.PayableReference, CreditDocumentReference: r.CreditDocumentReference, PayeeName: r.PayeeName, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}
func loadSupplierCreditAllocationReversal(tx *gorm.DB, tenant string, outlet, credit, allocation, id uuid.UUID) (supplierCreditAllocationReversalRow, error) {
	var row supplierCreditAllocationReversalRow
	err := tx.Select(supplierCreditAllocationReversalColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND allocation_id=? AND id=?", tenant, outlet, credit, allocation, id).Take(&row).Error
	return row, err
}
func (h *catalogueHandlers) putSupplierCreditAllocationReversal(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	allocation, ok := supplierCreditID(c, "allocation")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "entry")
	if !ok {
		return
	}
	var in SupplierCreditAllocationReversalInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditAllocationReversal(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use the current credit version, observed return total and reversal reason.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditAllocationReversalReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		original, err := loadSupplierCreditAllocation(tx, p.TenantID, outlet, credit, allocation)
		if err != nil {
			return err
		}
		existing, err := loadSupplierCreditAllocationReversal(tx, p.TenantID, outlet, credit, allocation, id)
		if err == nil {
			if existing.ActorSubject != p.Subject || existing.IntentSHA256 != digest {
				return errSupplierCreditConflict
			}
			receipt, err = existing.receipt()
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if original.Fields.AmountMinor == nil {
			return errSupplierCreditEvidence
		}
		row := supplierCreditAllocationReversalRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, CreditID: credit, AllocationID: allocation, PayableID: original.Fields.PayableID, Fields: in, AmountMinor: *original.Fields.AmountMinor, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		row, err = loadSupplierCreditAllocationReversal(tx, p.TenantID, outlet, credit, allocation, id)
		if err != nil {
			return err
		}
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: credit.String(), EntryID: id.String(), Operation: "allocation_reversed"})
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) getSupplierCreditAllocationReversal(c *gin.Context) {
	h.readSupplierCreditAllocationReversal(c, false)
}
func (h *catalogueHandlers) recoverSupplierCreditAllocationReversal(c *gin.Context) {
	h.readSupplierCreditAllocationReversal(c, true)
}
func (h *catalogueHandlers) readSupplierCreditAllocationReversal(c *gin.Context, recovery bool) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	allocation, ok := supplierCreditID(c, "allocation")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	var id uuid.UUID
	if recovery {
		id, ok = supplierCreditID(c, "entry")
		if !ok {
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditAllocationReversalReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if recovery {
			if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
				return err
			}
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		var row supplierCreditAllocationReversalRow
		q := tx.Select(supplierCreditAllocationReversalColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND allocation_id=?", p.TenantID, outlet, credit, allocation)
		if recovery {
			q = q.Where("id=? AND actor_subject=?", id, p.Subject)
		}
		if err := q.Take(&row).Error; err != nil {
			return err
		}
		var err error
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		action := "viewed"
		if recovery {
			action = "recovery_read"
		}
		return auditRegisterRead(c, tx, "supplier_credit_allocation_reversal", p.TenantID, p.Subject, row.ID, action)
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
