package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

type clearanceInput struct {
	ExpectedSummaryVersion int64  `json:"expected_summary_version"`
	Reason                 string `json:"reason"`
}

func (h *supplierReceiving) clearance(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	id, ok := receivingID(c, "clearance", false)
	if !ok {
		return
	}
	var in clearanceInput
	if !decodeSupplierReceiving(c, &in) {
		return
	}
	if in.ExpectedSummaryVersion < 1 || !suppliertrade.Text(in.Reason, 500) {
		supplierstock.Error(c, f.ErrInvalid)
		return
	}
	var result map[string]any
	err := h.with(c, p, s, f.ReviewReceiving, func(ctx context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var row struct{ Body json.RawMessage }
		if err := tx.Table("pharmacy_supplier_receipts").Select("body").Where("tenant_id=? AND id=? AND shipment_id=?", s.TenantID, receipt, s.ShipmentID).Take(&row).Error; err != nil {
			return err
		}
		var original f.ReceivingReceipt
		if supplierorder.DecodeObject(row.Body, &original) != nil {
			return f.ErrUnavailable
		}
		if original.Input.AcceptedUnits < 1 || original.ActorStaffID == actor.StaffID {
			return f.ErrConflict
		}
		var prior []struct {
			ReceiptID            uuid.UUID
			SummaryVersion       int64
			Reason, ActorSubject string
			ActorStaffID         uuid.UUID
		}
		if err := tx.Table("pharmacy_supplier_receiving_clearances").Where("tenant_id=? AND id=?", s.TenantID, id).Scan(&prior).Error; err != nil {
			return err
		}
		if len(prior) == 1 {
			v := prior[0]
			if v.ReceiptID != receipt || v.SummaryVersion != in.ExpectedSummaryVersion || v.Reason != in.Reason || v.ActorSubject != actor.Subject || v.ActorStaffID != actor.StaffID {
				return f.ErrConflict
			}
			result = map[string]any{"id": id, "receipt_id": receipt, "summary_version": v.SummaryVersion, "state": "review_recorded"}
			return nil
		}
		head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
		if err != nil {
			return err
		}
		if head.Version != in.ExpectedSummaryVersion {
			return f.ErrConflict
		}
		if h.safety == nil {
			return f.ErrUnavailable
		}
		return h.safety.WithSafeShipment(ctx, shipment, func(context.Context) error {
			at, err := supplierstock.Clock(tx)
			if err != nil {
				return err
			}
			if err = tx.Exec(`INSERT INTO pharmacy_supplier_receiving_clearances(tenant_id,id,receipt_id,shipment_id,outlet_id,summary_version,reason,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?,?,?,?)`, s.TenantID, id, receipt, s.ShipmentID, s.OutletID, head.Version, in.Reason, actor.Subject, actor.StaffID, at).Error; err != nil {
				return err
			}
			if err = tx.Exec(`UPDATE pharmacy_supplier_batch_sources SET clearance_version=? WHERE tenant_id=? AND receipt_id=?`, head.Version, s.TenantID, receipt).Error; err != nil {
				return err
			}
			result = map[string]any{"id": id, "receipt_id": receipt, "summary_version": head.Version, "state": "review_recorded"}
			return nil
		})
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}

// safeSupplierBatches runs on the release owner's existing transaction; no receipt means no new dependency.
func (h *catalogueHandlers) safeSupplierBatches(ctx context.Context, tx *gorm.DB, tenant string, outlet, batch uuid.UUID, action func() error) error {
	var rows []struct {
		ID, ShipmentID uuid.UUID
		Body           json.RawMessage
	}
	if err := tx.Table("pharmacy_supplier_batch_sources").Select("receipt_id AS id,shipment_id,source AS body").Where("tenant_id=? AND outlet_id=? AND batch_id=?", tenant, outlet, batch).Limit(1001).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 1000 {
		return f.ErrUnavailable
	}
	if len(rows) == 0 {
		return action()
	}
	if h.supplierSafety == nil {
		return f.ErrUnavailable
	}
	old := ""
	if err := tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_supplier_safe_receipts',true),'')`).Scan(&old).Error; err != nil {
		return err
	}
	var run func(int) error
	run = func(index int) error {
		if index == len(rows) {
			safe := "|"
			for _, row := range rows {
				safe += row.ID.String() + "|"
			}
			if err := tx.Exec(`SELECT set_config('app.pharmacy_supplier_safe_receipts',?,true)`, safe).Error; err != nil {
				return err
			}
			err := action()
			return errors.Join(err, tx.Exec(`SELECT set_config('app.pharmacy_supplier_safe_receipts',?,true)`, old).Error)
		}
		var shipment f.Shipment
		if supplierorder.DecodeObject(rows[index].Body, &shipment) != nil {
			return f.ErrUnavailable
		}
		return h.supplierSafety.WithSafeShipment(ctx, shipment, func(context.Context) error { return run(index + 1) })
	}
	return run(0)
}
