package pharmacy

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

var ReadSupplierReceiptForTest = (&supplierReceiving{}).read

var TestReceivingStockLock = stockLock
var TestSaveReceivingHead = saveReceivingHead
var TestReadReceivingHead = readReceivingHead

func PostSupplierQuarantineForTest(tx *gorm.DB, p authn.Principal, s f.Shipment, r f.ReceivingReceipt, version int64) error {
	return (&supplierReceiving{}).postQuarantine(tx, p, s, r, version)
}
func ApplySupplierCorrectionForTest(tx *gorm.DB, s f.BuyerScope, shipment f.Shipment, receipt, id uuid.UUID, in f.ReceivingCorrectionInput, actor supplierorder.Actor) (f.ReceivingCorrection, error) {
	return (&supplierReceiving{}).applyCorrection(tx, s, shipment, receipt, id, in, actor)
}

var TestSupplierReceiptPage = receiptPage
var TestSupplierCorrectionPage = correctionPage

var TestReceivingProductChoices = receivingProductChoices
var TestReceivingLocationChoices = receivingLocationChoices

func ApplySupplierReceivingReleaseForTest(ctx context.Context, tx *gorm.DB, deps platform.Deps, p authn.Principal, s f.BuyerScope, shipment f.Shipment, receipt, id uuid.UUID, in f.ReceivingReleaseInput, actor supplierorder.Actor) (f.ReceivingRelease, error) {
	h := &supplierReceiving{owner: &catalogueHandlers{db: deps.DB, bus: deps.Bus, supplierSafety: deps.SupplierShipmentSafety}}
	return h.applyReceivingRelease(ctx, tx, p, s, shipment, receipt, id, in, actor)
}
