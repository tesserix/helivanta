package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func ValidateReceivingResponse(v ReceivingResponseRecord) error {
	for _, id := range []uuid.UUID{v.ID, v.OwnerTenantID, v.BuyerTenantID, v.OrderID, v.ShipmentID, v.SourceOrganizationID, v.SourceLocationID, v.ActorStaffID} {
		if id == uuid.Nil {
			return ErrInvalid
		}
	}
	if v.OwnerTenantID == v.BuyerTenantID || v.SummaryVersion < 1 || v.SummaryVersion > 9007199254740991 || v.CreatedAt.IsZero() || (v.SourceProduct != "supply" && v.SourceProduct != "pharmacy") || (v.Response != "confirmed" && v.Response != "disputed") || !suppliertrade.Text(v.Reason, 500) || !suppliertrade.Text(v.ActorSubject, 200) {
		return ErrInvalid
	}
	return nil
}
