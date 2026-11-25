package pharmacy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/tax"
	"gorm.io/gorm"
)

var errPurchaseTaxInvalid = errors.New("purchase tax evidence does not match the source bill")
var errPurchaseTaxConflict = errors.New("purchase tax evidence changed or command identity was reused")

func validatePurchaseTax(in PurchaseTaxInput, bill payable) error {
	scheme, ok := countryprofile.Tax("IN")
	if !ok || bill.Fields.Currency != "INR" || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || len(in.Lines) != len(bill.Fields.Lines) || len(in.Lines) == 0 || len(in.Lines) > 100 {
		return errPurchaseTaxInvalid
	}
	if in.Replaces != nil && !stocktakeRecordID(*in.Replaces) {
		return errPurchaseTaxInvalid
	}
	supplier, ok := scheme.Jurisdiction(in.SupplierState)
	if !ok {
		return errPurchaseTaxInvalid
	}
	if _, ok = scheme.Jurisdiction(in.PlaceOfSupply); !ok {
		return errPurchaseTaxInvalid
	}
	if in.SupplierGstin != nil && tax.ValidateRegistration(scheme, in.SupplierState, "gstin", *in.SupplierGstin) != nil {
		return errPurchaseTaxInvalid
	}
	for i, line := range in.Lines {
		original := bill.Fields.Lines[i]
		if line.Position != i+1 || (line.SupplyNature == "outside_gst" && line.RatePpm != 0) || (line.SupplyNature != "outside_gst" && !tax.ValidRate(tax.Nature(line.SupplyNature), line.RatePpm)) {
			return errPurchaseTaxInvalid
		}
		components := [][2]int64{{line.CgstMinor, line.CgstRecoverableMinor}, {line.SgstMinor, line.SgstRecoverableMinor}, {line.IgstMinor, line.IgstRecoverableMinor}, {line.UtgstMinor, line.UtgstRecoverableMinor}}
		var total, recoverable int64
		for _, pair := range components {
			if pair[0] < 0 || pair[0] > 1000000000000 || pair[1] < 0 || pair[1] > pair[0] {
				return errPurchaseTaxInvalid
			}
			total += pair[0]
			recoverable += pair[1]
		}
		if total != original.TaxMinor || recoverable != original.RecoverableTaxMinor || (total > 0 && (in.SupplierGstin == nil || line.SupplyNature != "taxable" || line.RatePpm == 0)) {
			return errPurchaseTaxInvalid
		}
		if in.SupplierState != in.PlaceOfSupply {
			if line.CgstMinor+line.SgstMinor+line.UtgstMinor != 0 {
				return errPurchaseTaxInvalid
			}
		} else if line.CgstMinor-line.SgstMinor-line.UtgstMinor > 1 || line.SgstMinor+line.UtgstMinor-line.CgstMinor > 1 || line.IgstMinor != 0 || (supplier.Kind == "union_territory" && line.SgstMinor != 0) || (supplier.Kind != "union_territory" && line.UtgstMinor != 0) {
			return errPurchaseTaxInvalid
		}
	}
	return nil
}

type purchaseTaxRecord struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	OutletID     uuid.UUID
	PayableID    uuid.UUID
	Input        PurchaseTaxInput `gorm:"embedded"`
	IntentSHA256 string           `gorm:"column:intent_sha256"`
	LineCount    int
	ActorSubject string
	CreatedAt    time.Time
}

func (purchaseTaxRecord) TableName() string { return "pharmacy_purchase_tax_evidence" }

type purchaseTaxLine struct {
	TenantID   uuid.UUID
	OutletID   uuid.UUID
	PayableID  uuid.UUID
	EvidenceID uuid.UUID
	Fields     PurchaseTaxLineInput `gorm:"embedded"`
}

func (purchaseTaxLine) TableName() string { return "pharmacy_purchase_tax_lines" }

func purchaseTaxRoutes(s *platform.Router, h *catalogueHandlers) {
	s.GET("/payables/:id/purchase-tax", PermAccountsExport, h.getPurchaseTax)
	s.PUT("/payables/:id/purchase-tax/:evidence", PermAccountsExport, h.putPurchaseTax)
}
func loadPurchaseTax(tx *gorm.DB, tenant string, outlet, bill, id uuid.UUID) (PurchaseTaxReceipt, error) {
	var row purchaseTaxRecord
	if err := tx.Where("tenant_id=? AND outlet_id=? AND payable_id=? AND id=?", tenant, outlet, bill, id).First(&row).Error; err != nil {
		return PurchaseTaxReceipt{}, err
	}
	var lines []purchaseTaxLine
	if err := tx.Where("tenant_id=? AND outlet_id=? AND payable_id=? AND evidence_id=?", tenant, outlet, bill, id).Order("position").Find(&lines).Error; err != nil {
		return PurchaseTaxReceipt{}, err
	}
	row.Input.Lines = make([]PurchaseTaxLineInput, len(lines))
	for i, line := range lines {
		row.Input.Lines[i] = line.Fields
	}
	return PurchaseTaxReceipt{PurchaseTaxInput: row.Input, ID: id, OutletID: outlet, PayableID: bill, RecordedBy: row.ActorSubject, RecordedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}
func currentPurchaseTaxID(tx *gorm.DB, tenant string, outlet, bill uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID
	err := tx.Raw(`SELECT e.id FROM pharmacy_purchase_tax_evidence e WHERE e.tenant_id=? AND e.outlet_id=? AND e.payable_id=?
 AND NOT EXISTS(SELECT 1 FROM pharmacy_purchase_tax_evidence n WHERE n.tenant_id=e.tenant_id AND n.outlet_id=e.outlet_id AND n.payable_id=e.payable_id AND n.replaces=e.id)`, tenant, outlet, bill).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) != 1 {
		return nil, errPurchaseTaxConflict
	}
	return &ids[0], nil
}
func purchaseTaxError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errPurchaseTaxInvalid):
		respond.Error(c, http.StatusBadRequest, "purchase_tax_invalid", "Check each source line's GST rate, jurisdiction, tax components and recoverable amounts against the posted bill.")
	case errors.Is(err, errPurchaseTaxConflict), errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, http.StatusConflict, "purchase_tax_conflict", "The evidence changed or this command was used with other details. Reload the bill before recording again.")
	default:
		payableError(c, err)
	}
}
func (h *catalogueHandlers) getPurchaseTax(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !payableSubject(c, p.Subject, "reading purchase tax evidence") {
		return
	}
	bill, ok := catalogueID(c)
	if !ok {
		return
	}
	view := PurchaseTaxView{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if _, err := loadPayable(tx, p.TenantID, outlet, bill); err != nil {
			return err
		}
		id, err := currentPurchaseTaxID(tx, p.TenantID, outlet, bill)
		if err != nil {
			return err
		}
		if id != nil {
			row, err := loadPurchaseTax(tx, p.TenantID, outlet, bill, *id)
			if err != nil {
				return err
			}
			view.Evidence = &row
		}
		return auditRegisterRead(c, tx, "purchase_tax", p.TenantID, p.Subject, outlet, "accounts_read", "payable_id", bill.String())
	})
	if err != nil {
		purchaseTaxError(c, err)
		return
	}
	respond.OK(c, view)
}
func (h *catalogueHandlers) putPurchaseTax(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !payableSubject(c, p.Subject, "recording purchase tax evidence") {
		return
	}
	bill, ok := catalogueID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("evidence"))
	if err != nil || !stocktakeRecordID(id) {
		respond.NotFound(c, "purchase tax evidence")
		return
	}
	var in PurchaseTaxInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.SupplierGstin != nil {
		value := tax.NormalizeRegistration(*in.SupplierGstin)
		in.SupplierGstin = &value
	}
	var receipt PurchaseTaxReceipt
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		doc, err := loadPayable(tx, p.TenantID, outlet, bill)
		if err != nil {
			return err
		}
		if err = validatePurchaseTax(in, doc); err != nil {
			return err
		}
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		if err = stockLock(tx, p.TenantID+":"+outlet.String()+":purchase-tax:"+bill.String()); err != nil {
			return err
		}
		var hashes []string
		if err = tx.Raw(`SELECT intent_sha256 FROM pharmacy_purchase_tax_evidence WHERE tenant_id=? AND outlet_id=? AND payable_id=? AND id=?`, p.TenantID, outlet, bill, id).Scan(&hashes).Error; err != nil {
			return err
		}
		if len(hashes) > 0 {
			if hashes[0] != digest {
				return errPurchaseTaxConflict
			}
			receipt, err = loadPurchaseTax(tx, p.TenantID, outlet, bill, id)
			return err
		}
		current, err := currentPurchaseTaxID(tx, p.TenantID, outlet, bill)
		if err != nil {
			return err
		}
		if !sameTallyMap(in.Replaces, current) {
			return errPurchaseTaxConflict
		}
		row := purchaseTaxRecord{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, PayableID: bill, Input: in, IntentSHA256: digest, LineCount: len(in.Lines), ActorSubject: p.Subject}
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		lines := make([]purchaseTaxLine, len(in.Lines))
		for i, line := range in.Lines {
			lines[i] = purchaseTaxLine{TenantID: row.TenantID, OutletID: outlet, PayableID: bill, EvidenceID: id, Fields: line}
		}
		if err = tx.Create(&lines).Error; err != nil {
			return err
		}
		receipt, err = loadPurchaseTax(tx, p.TenantID, outlet, bill, id)
		return err
	})
	if err != nil {
		purchaseTaxError(c, err)
		return
	}
	respond.OK(c, receipt)
}
