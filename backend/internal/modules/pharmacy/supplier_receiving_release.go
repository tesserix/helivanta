package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/pkg/pagination"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/authn"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

func receivingReleaseValid(in f.ReceivingReleaseInput) bool {
	return in.ExpectedSummaryVersion > 0 && in.ExpectedSummaryVersion <= 9007199254740991 && in.ToLocationID != uuid.Nil && in.ToLocationID.Version() == 7 && in.QuantityUnits > 0 && in.QuantityUnits <= 1000000000 && suppliertrade.Text(in.Reason, 500)
}
func readReceivingRelease(tx *gorm.DB, s f.BuyerScope, receipt, id uuid.UUID) (f.ReceivingRelease, error) {
	var row struct{ Body json.RawMessage }
	var result f.ReceivingRelease
	if err := tx.Table("pharmacy_supplier_receiving_releases").Select("body").Where("tenant_id=? AND shipment_id=? AND outlet_id=? AND receipt_id=? AND id=?", s.TenantID, s.ShipmentID, s.OutletID, receipt, id).Take(&row).Error; err != nil {
		return result, err
	}
	if supplierorder.DecodeObject(row.Body, &result) != nil || result.ID != id || result.ShipmentID != s.ShipmentID || result.ReceiptID != receipt || result.OutletID != s.OutletID || result.OwnerTenantID != s.TenantID {
		return result, f.ErrUnavailable
	}
	return result, nil
}
func (h *supplierReceiving) releaseReceipt(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	id, ok := receivingID(c, "release", false)
	if !ok {
		return
	}
	var result f.ReceivingRelease
	err := h.with(c, p, s, f.ReviewReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = readReceivingRelease(tx, s, receipt, id)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, s, actor, "pharmacy_receiving_release_read")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierReceiving) release(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	id, ok := receivingID(c, "release", false)
	if !ok {
		return
	}
	var in f.ReceivingReleaseInput
	if !decodeSupplierReceiving(c, &in) {
		return
	}
	if !receivingReleaseValid(in) {
		supplierstock.Error(c, f.ErrInvalid)
		return
	}
	var result f.ReceivingRelease
	err := h.with(c, p, s, f.ReviewReceiving, func(ctx context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = h.applyReceivingRelease(ctx, tx, p, s, shipment, receipt, id, in, actor)
		return err
	})
	var refusal errNotReleasable
	if errors.As(err, &refusal) {
		respond.Error(c, 409, "not_releasable", refusal.reason)
		return
	}
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierReceiving) applyReceivingRelease(ctx context.Context, tx *gorm.DB, p authn.Principal, s f.BuyerScope, shipment f.Shipment, receipt, id uuid.UUID, in f.ReceivingReleaseInput, actor supplierorder.Actor) (f.ReceivingRelease, error) {
	result, err := readReceivingRelease(tx, s, receipt, id)
	if err == nil {
		if result.ActorSubject != actor.Subject || result.ActorStaffID != actor.StaffID || !reflect.DeepEqual(result.Input, in) {
			return result, f.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	if !receivingReleaseValid(in) {
		return result, f.ErrInvalid
	}
	var row struct{ Body json.RawMessage }
	if err = tx.Table("pharmacy_supplier_receipts").Select("body").Where("tenant_id=? AND shipment_id=? AND outlet_id=? AND id=?", s.TenantID, s.ShipmentID, s.OutletID, receipt).Take(&row).Error; err != nil {
		return result, err
	}
	var original f.ReceivingReceipt
	if supplierorder.DecodeObject(row.Body, &original) != nil {
		return result, f.ErrUnavailable
	}
	if original.ActorStaffID == actor.StaffID || original.Input.DestinationBatchID == nil || original.Input.QuarantineLocationID == nil {
		return result, f.ErrConflict
	}
	head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
	if err != nil {
		return result, err
	}
	if head.Version != in.ExpectedSummaryVersion || !head.Closed {
		return result, f.ErrConflict
	}
	var clearance int64
	if err = tx.Table("pharmacy_supplier_receiving_clearances").Where("tenant_id=? AND shipment_id=? AND receipt_id=? AND summary_version=?", s.TenantID, s.ShipmentID, receipt, head.Version).Count(&clearance).Error; err != nil {
		return result, err
	}
	if clearance != 1 {
		return result, f.ErrConflict
	}
	accepted := original.Input.AcceptedUnits
	correction, err := latestReceivingCorrection(tx, s.TenantID, s.ShipmentID, receipt)
	if err == nil {
		accepted = correction.Input.AcceptedUnits
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	var released int64
	if err = tx.Table("pharmacy_supplier_receiving_releases").Select("COALESCE(SUM(quantity_units),0)").Where("tenant_id=? AND receipt_id=?", s.TenantID, receipt).Scan(&released).Error; err != nil {
		return result, err
	}
	if in.QuantityUnits > accepted-released {
		return result, f.ErrConflict
	}
	var priorStock int64
	if err = tx.Table("pharmacy_stock_releases").Where("tenant_id=? AND outlet_id=? AND id=?", s.TenantID, s.OutletID, id).Count(&priorStock).Error; err != nil {
		return result, err
	}
	if priorStock != 0 {
		return result, f.ErrConflict
	}
	if h.owner == nil || h.owner.bus == nil {
		return result, f.ErrUnavailable
	}
	at, err := supplierstock.Clock(tx)
	if err != nil {
		return result, err
	}
	result = f.ReceivingRelease{ID: id, OwnerTenantID: s.TenantID, ShipmentID: s.ShipmentID, ReceiptID: receipt, PharmacyID: shipment.BuyerOrganizationID, OutletID: s.OutletID, BatchID: *original.Input.DestinationBatchID, FromLocationID: *original.Input.QuarantineLocationID, Input: in, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, CreatedAt: at}
	body, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	input, err := json.Marshal(in)
	if err != nil {
		return result, err
	}
	if err = tx.Exec(`INSERT INTO pharmacy_supplier_receiving_releases(tenant_id,id,shipment_id,receipt_id,outlet_id,batch_id,from_location_id,to_location_id,quantity_units,summary_version,input,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,?,?,?)`, s.TenantID, id, s.ShipmentID, receipt, s.OutletID, result.BatchID, result.FromLocationID, in.ToLocationID, in.QuantityUnits, head.Version, string(input), string(body), actor.Subject, actor.StaffID, at).Error; err != nil {
		return result, err
	}
	_, err = h.owner.applyRelease(ctx, tx, p, shipment.BuyerOrganizationID, s.OutletID, id, releaseInput{BatchID: result.BatchID, FromLocationID: result.FromLocationID, ToLocationID: in.ToLocationID, QuantityUnits: in.QuantityUnits, Reason: in.Reason})
	return result, err
}

type listedReceivingRelease struct{ f.ReceivingRelease }

func (v listedReceivingRelease) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
func receivingReleasePage(tx *gorm.DB, s f.BuyerScope, receipt uuid.UUID, page pagination.Params) ([]listedReceivingRelease, error) {
	var rows []struct {
		ID   uuid.UUID
		Body json.RawMessage
	}
	q := tx.Table("pharmacy_supplier_receiving_releases").Select("id,body").Where("tenant_id=? AND shipment_id=? AND outlet_id=? AND receipt_id=?", s.TenantID, s.ShipmentID, s.OutletID, receipt).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]listedReceivingRelease, 0, len(rows))
	for _, row := range rows {
		var value f.ReceivingRelease
		if supplierorder.DecodeObject(row.Body, &value) != nil || value.ID != row.ID || value.OwnerTenantID != s.TenantID || value.OutletID != s.OutletID || value.ReceiptID != receipt || value.ShipmentID != s.ShipmentID || !receivingReleaseValid(value.Input) || value.CreatedAt.IsZero() {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedReceivingRelease{value})
	}
	return result, nil
}
func (h *supplierReceiving) releases(c *gin.Context, page pagination.Params) ([]listedReceivingRelease, error) {
	p, s, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return nil, nil
	}
	var result []listedReceivingRelease
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, actor supplierorder.Actor) error {
		if _, err := h.read(tx, s.TenantID, s.ShipmentID, receipt, actor, nil); err != nil {
			return err
		}
		var err error
		result, err = receivingReleasePage(tx, s, receipt, page)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, s, actor, "pharmacy_receiving_release_list")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}
