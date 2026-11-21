package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

type supplierCreditAllocationRow struct {
	ID, TenantID, OutletID, CreditID                     uuid.UUID
	Fields                                               SupplierCreditAllocationInput `gorm:"embedded"`
	AvailableMinor, OutstandingMinor                     int64                         `gorm:"->"`
	PayableReference, CreditDocumentReference, PayeeName string                        `gorm:"->"`
	IntentSHA256, ActorSubject                           string
	CreatedAt                                            time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditAllocationRow) TableName() string { return "pharmacy_supplier_credit_allocations" }

const supplierCreditAllocationColumns = "id,tenant_id,outlet_id,credit_id,payable_id,expected_version,expected_available_minor,expected_payable_version,expected_outstanding_minor,amount_minor,to_char(occurred_on,'YYYY-MM-DD') AS occurred_on,reason,available_minor,outstanding_minor,payable_reference,credit_document_reference,payee_name,intent_sha256,actor_subject,created_at" // #nosec G101 -- Static SQL column identifiers, not credentials.
func (r supplierCreditAllocationRow) receipt() (SupplierCreditAllocationReceipt, error) {
	in := r.Fields
	if !normalizeSupplierCreditAllocation(&in) || r.AvailableMinor != *in.ExpectedAvailableMinor-*in.AmountMinor || r.OutstandingMinor != *in.ExpectedOutstandingMinor-*in.AmountMinor {
		return SupplierCreditAllocationReceipt{}, errSupplierCreditEvidence
	}
	return SupplierCreditAllocationReceipt{SupplierCreditAllocationInput: r.Fields, ID: r.ID, OutletID: r.OutletID, CreditID: r.CreditID, Version: *in.ExpectedVersion + 1, PayableVersion: *in.ExpectedPayableVersion + 1, AvailableMinor: r.AvailableMinor, OutstandingMinor: r.OutstandingMinor, PayableReference: r.PayableReference, CreditDocumentReference: r.CreditDocumentReference, PayeeName: r.PayeeName, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}

type supplierCreditAllocationViewRow struct {
	Allocation supplierCreditAllocationRow `gorm:"embedded"`
	ReversalID *uuid.UUID
}

func (r supplierCreditAllocationViewRow) PageKey() (time.Time, uuid.UUID) {
	return r.Allocation.CreatedAt, r.Allocation.ID
}
func (r supplierCreditAllocationViewRow) MarshalJSON() ([]byte, error) {
	receipt, err := r.Allocation.receipt()
	if err != nil {
		return nil, err
	}
	return json.Marshal(SupplierCreditAllocationView{SupplierCreditAllocationReceipt: receipt, ReversalID: r.ReversalID})
}

const supplierCreditAllocationViewColumns = supplierCreditAllocationColumns + `,(SELECT v.id FROM pharmacy_supplier_credit_allocation_reversals v WHERE v.tenant_id=pharmacy_supplier_credit_allocations.tenant_id AND v.outlet_id=pharmacy_supplier_credit_allocations.outlet_id AND v.allocation_id=pharmacy_supplier_credit_allocations.id)AS reversal_id`

func loadSupplierCreditAllocation(tx *gorm.DB, tenant string, outlet, credit, id uuid.UUID) (supplierCreditAllocationRow, error) {
	var row supplierCreditAllocationRow
	err := tx.Select(supplierCreditAllocationColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND id=?", tenant, outlet, credit, id).Take(&row).Error
	return row, err
}
func supplierCreditAllocationRoutes(r *platform.Router, h *catalogueHandlers) {
	a := r.Group("/:credit/allocations")
	platform.ResourceListRoute(a, "", PermAccountsRead, 50, h.listSupplierCreditAllocations)
	a.PUT("/:allocation", PermAccountsAdjust, h.putSupplierCreditAllocation)
	a.GET("/:allocation", PermAccountsRead, h.getSupplierCreditAllocation)
	a.GET("/:allocation/recovery", PermAccountsRead, h.recoverSupplierCreditAllocation)
	a.PUT("/:allocation/reversals/:entry", PermAccountsAdjust, h.putSupplierCreditAllocationReversal)
	a.GET("/:allocation/reversal", PermAccountsRead, h.getSupplierCreditAllocationReversal)
	a.GET("/:allocation/reversals/:entry/recovery", PermAccountsRead, h.recoverSupplierCreditAllocationReversal)
}
func (h *catalogueHandlers) putSupplierCreditAllocation(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "allocation")
	if !ok {
		return
	}
	var in SupplierCreditAllocationInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditAllocation(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a bill, explicit current versions/balances, supported date and conserved amount.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditAllocationReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		existing, err := loadSupplierCreditAllocation(tx, p.TenantID, outlet, credit, id)
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
		var bill struct{ ID uuid.UUID }
		if err := tx.Table("pharmacy_payables").Select("id").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, in.PayableID).Take(&bill).Error; err != nil {
			return err
		}
		row := supplierCreditAllocationRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, CreditID: credit, Fields: in, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		row, err = loadSupplierCreditAllocation(tx, p.TenantID, outlet, credit, id)
		if err != nil {
			return err
		}
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: credit.String(), EntryID: id.String(), Operation: "allocated"})
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
