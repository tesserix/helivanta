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

type supplierCreditMatchReversalRow struct {
	ID, TenantID, OutletID, CreditID, MatchID, ReturnID uuid.UUID
	Fields                                              SupplierCreditMatchReversalInput `gorm:"embedded"`
	ReturnNumber, AmountMinor                           int64
	ReturnMatchedMinor                                  int64 `gorm:"->"`
	IntentSHA256, ActorSubject                          string
	CreatedAt                                           time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditMatchReversalRow) TableName() string {
	return "pharmacy_supplier_credit_match_reversals"
}

const supplierCreditMatchReversalColumns = "id,tenant_id,outlet_id,credit_id,match_id,return_id,expected_version,expected_return_matched_minor,amount_minor,return_number,return_matched_minor,reason,intent_sha256,actor_subject,created_at" // #nosec G101 -- Static SQL column identifiers; no credential values.

func (r supplierCreditMatchReversalRow) receipt() (SupplierCreditMatchReversalReceipt, error) {
	if r.Fields.ExpectedVersion == nil || *r.Fields.ExpectedVersion < 1 || *r.Fields.ExpectedVersion >= 1000000000000 || r.Fields.ExpectedReturnMatchedMinor == nil || r.AmountMinor < 1 || *r.Fields.ExpectedReturnMatchedMinor < r.AmountMinor || r.ReturnMatchedMinor != *r.Fields.ExpectedReturnMatchedMinor-r.AmountMinor {
		return SupplierCreditMatchReversalReceipt{}, errSupplierCreditEvidence
	}
	return SupplierCreditMatchReversalReceipt{SupplierCreditMatchReversalInput: r.Fields, ID: r.ID, OutletID: r.OutletID, CreditID: r.CreditID, MatchID: r.MatchID, ReturnID: r.ReturnID, ReturnNumber: r.ReturnNumber, AmountMinor: r.AmountMinor, ReturnMatchedMinor: r.ReturnMatchedMinor, Version: *r.Fields.ExpectedVersion + 1, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}
func loadSupplierCreditMatchReversal(tx *gorm.DB, tenant string, outlet, credit, match, id uuid.UUID) (supplierCreditMatchReversalRow, error) {
	var row supplierCreditMatchReversalRow
	err := tx.Select(supplierCreditMatchReversalColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND match_id=? AND id=?", tenant, outlet, credit, match, id).Take(&row).Error
	return row, err
}
func (h *catalogueHandlers) putSupplierCreditMatchReversal(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	match, ok := supplierCreditID(c, "match")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "entry")
	if !ok {
		return
	}
	var in SupplierCreditMatchReversalInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditMatchReversal(&in) {
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
	var receipt SupplierCreditMatchReversalReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		original, err := loadSupplierCreditMatch(tx, p.TenantID, outlet, credit, match)
		if err != nil {
			return err
		}
		existing, err := loadSupplierCreditMatchReversal(tx, p.TenantID, outlet, credit, match, id)
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
		row := supplierCreditMatchReversalRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, CreditID: credit, MatchID: match, ReturnID: original.Fields.ReturnID, Fields: in, ReturnNumber: original.ReturnNumber, AmountMinor: *original.Fields.AmountMinor, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		row, err = loadSupplierCreditMatchReversal(tx, p.TenantID, outlet, credit, match, id)
		if err != nil {
			return err
		}
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: credit.String(), EntryID: id.String(), Operation: "match_reversed"})
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) getSupplierCreditMatchReversal(c *gin.Context) {
	h.readSupplierCreditMatchReversal(c, false)
}
func (h *catalogueHandlers) recoverSupplierCreditMatchReversal(c *gin.Context) {
	h.readSupplierCreditMatchReversal(c, true)
}
func (h *catalogueHandlers) readSupplierCreditMatchReversal(c *gin.Context, recovery bool) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	match, ok := supplierCreditID(c, "match")
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
	var receipt SupplierCreditMatchReversalReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if recovery {
			if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
				return err
			}
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		var row supplierCreditMatchReversalRow
		q := tx.Select(supplierCreditMatchReversalColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND match_id=?", p.TenantID, outlet, credit, match)
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
		return auditRegisterRead(c, tx, "supplier_credit_match_reversal", p.TenantID, p.Subject, row.ID, action)
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
