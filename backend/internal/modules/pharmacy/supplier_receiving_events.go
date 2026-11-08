package pharmacy

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

func decodeSupplierReceiving(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || supplierorder.DecodeObject(raw, target) != nil {
		respond.BadRequest(c, errors.New("invalid receiving intent"))
		return false
	}
	return true
}
func publishReceivingSummary(tx *gorm.DB, s f.Shipment, summary f.ReceivingSummary) error {
	raw, err := json.Marshal(pharmacycontract.SupplierReceivingRecordedData{ShipmentID: s.ID, SourceTenantID: s.SourceTenantID, Summary: pharmacycontract.ReceivingSummary(summary)})
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, pharmacycontract.SubjectSupplierReceivingRecorded, events.Event{ID: id.String(), Type: pharmacycontract.SubjectSupplierReceivingRecorded, Version: 1, OccurredAt: time.Now().UTC(), TenantID: s.BuyerTenantID.String(), Data: raw})
}
