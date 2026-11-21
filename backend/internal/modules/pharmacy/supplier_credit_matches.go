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

type supplierCreditMatchRow struct {
	ID, TenantID, OutletID, CreditID  uuid.UUID
	Fields                            SupplierCreditMatchInput `gorm:"embedded"`
	ReturnNumber, ExpectedCreditMinor int64
	ReturnMatchedMinor                int64 `gorm:"->"`
	IntentSHA256, ActorSubject        string
	CreatedAt                         time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditMatchRow) TableName() string { return "pharmacy_supplier_credit_matches" }

const supplierCreditMatchColumns = "id,tenant_id,outlet_id,credit_id,return_id,expected_version,expected_return_matched_minor,amount_minor,reason,overage_reason,return_number,expected_credit_minor,return_matched_minor,intent_sha256,actor_subject,created_at" // #nosec G101 -- Static SQL column identifiers; no credential values.

func (r supplierCreditMatchRow) receipt() (SupplierCreditMatchReceipt, error) {
	if r.Fields.ExpectedVersion == nil || *r.Fields.ExpectedVersion < 1 || *r.Fields.ExpectedVersion >= 1000000000000 || r.Fields.AmountMinor == nil || r.Fields.ExpectedReturnMatchedMinor == nil || r.ReturnMatchedMinor != *r.Fields.ExpectedReturnMatchedMinor+*r.Fields.AmountMinor {
		return SupplierCreditMatchReceipt{}, errSupplierCreditEvidence
	}
	return SupplierCreditMatchReceipt{SupplierCreditMatchInput: r.Fields, ID: r.ID, OutletID: r.OutletID, CreditID: r.CreditID, Version: *r.Fields.ExpectedVersion + 1, ReturnNumber: r.ReturnNumber, ExpectedCreditMinor: r.ExpectedCreditMinor, ReturnMatchedMinor: r.ReturnMatchedMinor, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}

type supplierCreditMatchViewRow struct {
	Match      supplierCreditMatchRow `gorm:"embedded"`
	ReversalID *uuid.UUID
}

func (r supplierCreditMatchViewRow) PageKey() (time.Time, uuid.UUID) {
	return r.Match.CreatedAt, r.Match.ID
}
func (r supplierCreditMatchViewRow) MarshalJSON() ([]byte, error) {
	receipt, err := r.Match.receipt()
	if err != nil {
		return nil, err
	}
	return json.Marshal(SupplierCreditMatchView{SupplierCreditMatchReceipt: receipt, ReversalID: r.ReversalID})
}

const supplierCreditMatchViewColumns = supplierCreditMatchColumns + `,(SELECT v.id FROM pharmacy_supplier_credit_match_reversals v WHERE v.tenant_id=pharmacy_supplier_credit_matches.tenant_id AND v.outlet_id=pharmacy_supplier_credit_matches.outlet_id AND v.match_id=pharmacy_supplier_credit_matches.id) AS reversal_id`

func supplierCreditMatchRoutes(r *platform.Router, h *catalogueHandlers) {
	m := r.Group("/:credit/matches")
	platform.ResourceListRoute(m, "", PermAccountsRead, 50, h.listSupplierCreditMatches)
	m.PUT("/:match", PermAccountsAdjust, h.putSupplierCreditMatch)
	m.GET("/:match", PermAccountsRead, h.getSupplierCreditMatch)
	m.GET("/:match/context", PermAccountsRead, h.getSupplierCreditMatchContext)
	m.GET("/:match/recovery", PermAccountsRead, h.recoverSupplierCreditMatch)
	m.PUT("/:match/reversals/:entry", PermAccountsAdjust, h.putSupplierCreditMatchReversal)
	m.GET("/:match/reversal", PermAccountsRead, h.getSupplierCreditMatchReversal)
	m.GET("/:match/reversals/:entry/recovery", PermAccountsRead, h.recoverSupplierCreditMatchReversal)
}
func loadSupplierCreditMatch(tx *gorm.DB, tenant string, outlet, credit, id uuid.UUID) (supplierCreditMatchRow, error) {
	var row supplierCreditMatchRow
	err := tx.Select(supplierCreditMatchColumns).Where("tenant_id=? AND outlet_id=? AND credit_id=? AND id=?", tenant, outlet, credit, id).Take(&row).Error
	return row, err
}
func (h *catalogueHandlers) putSupplierCreditMatch(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "match")
	if !ok {
		return
	}
	var in SupplierCreditMatchInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditMatch(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use explicit current credit/return totals, positive whole minor units and a reason.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditMatchReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit); err != nil {
			return err
		}
		existing, err := loadSupplierCreditMatch(tx, p.TenantID, outlet, credit, id)
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
		var source supplierReturnRow
		if err := tx.Select("id,number,expected_credit_minor").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, in.ReturnID).Take(&source).Error; err != nil {
			return err
		}
		row := supplierCreditMatchRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, CreditID: credit, Fields: in, ReturnNumber: source.Number, ExpectedCreditMinor: source.ExpectedCreditMinor, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		row, err = loadSupplierCreditMatch(tx, p.TenantID, outlet, credit, id)
		if err != nil {
			return err
		}
		receipt, err = row.receipt()
		if err != nil {
			return err
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: credit.String(), EntryID: id.String(), Operation: "matched"})
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
