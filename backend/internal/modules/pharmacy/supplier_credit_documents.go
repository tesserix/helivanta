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
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) putSupplierCreditDocument(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierCreditID(c, "credit")
	if !ok {
		return
	}
	var in SupplierCreditDocumentInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierCreditDocument(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use an original return, explicit supported amount/currency, valid document date, reference and reason.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierCreditDocumentReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierCreditLock(tx, p.TenantID, outlet, id); err != nil {
			return err
		}
		var source supplierReturnRow
		if err := tx.Select("id,number,payee_reference,payee_name,document_reference,currency").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, in.ReturnID).Take(&source).Error; err != nil {
			return err
		}
		if in.CorrectionOf != nil {
			if _, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, *in.CorrectionOf); err != nil {
				return err
			}
		}
		existing, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, id)
		if err == nil {
			if existing.ActorSubject != p.Subject || existing.IntentSHA256 != digest {
				return errSupplierCreditConflict
			}
			receipt = existing.receipt()
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row := supplierCreditDocumentRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, Fields: in, ReturnNumber: source.Number, PayeeReference: source.PayeeReference, PayeeName: source.PayeeName, OriginalBillReference: source.DocumentReference, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		stored, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		receipt = stored.receipt()
		data := pharmacycontract.SupplierCreditChangedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditID: id.String(), EntryID: id.String(), Operation: "recorded"}
		if in.CorrectionOf != nil {
			previous := in.CorrectionOf.String()
			data.RelatedCreditID = &previous
		}
		return h.publishSupplierCreditChange(tx, p.TenantID, data)
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) publishSupplierCreditChange(tx *gorm.DB, tenant string, payload pharmacycontract.SupplierCreditChangedData) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, pharmacycontract.SubjectSupplierCreditChanged, events.Event{Type: "SupplierCreditChanged", Version: 1, TenantID: tenant, Data: data})
}
