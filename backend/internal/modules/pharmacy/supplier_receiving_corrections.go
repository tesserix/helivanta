package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

func (h *supplierReceiving) correct(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	id, ok := receivingID(c, "correction", false)
	if !ok {
		return
	}
	var in f.ReceivingCorrectionInput
	if !decodeSupplierReceiving(c, &in) {
		return
	}
	var result f.ReceivingCorrection
	err := h.with(c, p, s, f.ReviewReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = h.applyCorrection(tx, s, shipment, receipt, id, in, actor)
		return err
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}

func latestReceivingCorrection(tx *gorm.DB, tenant, shipment, receipt uuid.UUID) (f.ReceivingCorrection, error) {
	var row struct{ Body json.RawMessage }
	var result f.ReceivingCorrection
	if err := tx.Table("pharmacy_supplier_receiving_corrections").Select("body").Where("tenant_id=? AND shipment_id=? AND receipt_id=?", tenant, shipment, receipt).Order("version DESC").Take(&row).Error; err != nil {
		return result, err
	}
	if supplierorder.DecodeObject(row.Body, &result) != nil {
		return result, f.ErrUnavailable
	}
	return result, nil
}

func (h *supplierReceiving) correction(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	id, ok := receivingID(c, "correction", false)
	if !ok {
		return
	}
	var result f.ReceivingCorrection
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, _ supplierorder.Actor) error {
		var row struct{ Body json.RawMessage }
		if err := tx.Table("pharmacy_supplier_receiving_corrections").Select("body").Where("tenant_id=? AND shipment_id=? AND receipt_id=? AND id=?", s.TenantID, s.ShipmentID, receipt, id).Take(&row).Error; err != nil {
			return err
		}
		if supplierorder.DecodeObject(row.Body, &result) != nil {
			return f.ErrUnavailable
		}
		return nil
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *supplierReceiving) applyCorrection(tx *gorm.DB, s f.BuyerScope, shipment f.Shipment, receipt, id uuid.UUID, in f.ReceivingCorrectionInput, actor supplierorder.Actor) (f.ReceivingCorrection, error) {
	var result f.ReceivingCorrection
	var row struct{ Body json.RawMessage }
	err := tx.Table("pharmacy_supplier_receiving_corrections").Select("body").Where("tenant_id=? AND shipment_id=? AND receipt_id=? AND id=?", s.TenantID, s.ShipmentID, receipt, id).Take(&row).Error
	if err == nil {
		if supplierorder.DecodeObject(row.Body, &result) != nil {
			return result, f.ErrUnavailable
		}
		if result.ActorSubject != actor.Subject || result.ActorStaffID != actor.StaffID || !reflect.DeepEqual(result.Input, in) {
			return result, f.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	if err = tx.Table("pharmacy_supplier_receipts").Select("body").Where("tenant_id=? AND shipment_id=? AND id=? AND outlet_id=?", s.TenantID, s.ShipmentID, receipt, s.OutletID).Take(&row).Error; err != nil {
		return result, err
	}
	var original f.ReceivingReceipt
	if supplierorder.DecodeObject(row.Body, &original) != nil {
		return result, f.ErrUnavailable
	}
	prior := f.ReceiptCorrectionState{}
	latest, err := latestReceivingCorrection(tx, s.TenantID, s.ShipmentID, receipt)
	if err == nil {
		prior = f.ReceiptCorrectionState{Version: latest.Version, Counts: f.ReceivingTotals{Counted: latest.Input.CountedUnits, Accepted: latest.Input.AcceptedUnits, Rejected: latest.Input.RejectedUnits}, ActorStaffID: latest.ActorStaffID}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	if prior.Version >= 1000 {
		return result, f.ErrConflict
	}
	head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
	if err != nil {
		return result, err
	}
	at, err := supplierstock.Clock(tx)
	if err != nil {
		return result, err
	}
	result, err = f.CorrectReceiving(id, shipment, original, prior, f.ReceivingTotals{Counted: head.CountedUnits, Accepted: head.AcceptedUnits, Rejected: head.RejectedUnits}, head.Version, in, actor, at)
	if err != nil {
		return result, err
	}
	summary := f.ReceivingSummary{ID: id, ShipmentID: s.ShipmentID, OrderID: shipment.OrderID, OwnerTenantID: s.TenantID, Version: head.Version + 1, CountedUnits: result.CumulativeCountedUnits, AcceptedUnits: result.CumulativeAcceptedUnits, RejectedUnits: result.CumulativeRejectedUnits, Reason: in.Reason, ActorSubject: actor.Subject, RecordedAt: at}
	if err = saveReceivingHead(tx, s, shipment, summary); err != nil {
		return result, err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	input, err := json.Marshal(in)
	if err != nil {
		return result, err
	}
	if err = tx.Exec(`INSERT INTO pharmacy_supplier_receiving_corrections(tenant_id,id,shipment_id,receipt_id,outlet_id,version,input,accepted_delta_units,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?::jsonb,?,?,?)`, s.TenantID, id, s.ShipmentID, receipt, s.OutletID, result.Version, string(input), result.AcceptedDeltaUnits, string(body), actor.Subject, actor.StaffID, at).Error; err != nil {
		return result, err
	}
	if result.AcceptedDeltaUnits != 0 {
		move := stockMovement{ID: id, TenantID: s.TenantID, OutletID: s.OutletID, MovementFields: movementInput{LocationID: *original.Input.QuarantineLocationID, BatchID: *original.Input.DestinationBatchID, Kind: "correction", QuantityUnits: result.AcceptedDeltaUnits, Reason: in.Reason, SourceReference: "supplier-correction:" + id.String()}, ActorSubject: actor.Subject, CreatedAt: at}
		if err = tx.Create(&move).Error; err != nil {
			return result, err
		}
	}
	return result, publishReceivingSummary(tx, shipment, summary)
}
