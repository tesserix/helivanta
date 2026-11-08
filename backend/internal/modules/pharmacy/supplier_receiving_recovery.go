package pharmacy

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

func (h *supplierReceiving) originalReceipt(tx *gorm.DB, s f.BuyerScope, id uuid.UUID, actor supplierorder.Actor) (f.ReceivingReceipt, error) {
	result, err := h.read(tx, s.TenantID, s.ShipmentID, id, actor, nil)
	if err != nil {
		return result, err
	}
	if result.ActorSubject != actor.Subject || result.ActorStaffID != actor.StaffID {
		return f.ReceivingReceipt{}, gorm.ErrRecordNotFound
	}
	return result, nil
}
func (h *supplierReceiving) original(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	id, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	var result f.ReceivingReceipt
	err := h.with(c, p, s, f.RecordReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = h.originalReceipt(tx, s, id, actor)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, s, actor, "pharmacy_supplier_receipt_recovery_read")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
