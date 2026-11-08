package pharmacy

import (
	"github.com/google/uuid"
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
