package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func ValidateReceivingResponse(v ReceivingResponseRecord) error {
	if v.ActorStaffID == uuid.Nil {
		return ErrInvalid
	}
	return ValidateSharedReceivingResponse(ShareReceivingResponse(v))
}

// ShareReceivingResponse excludes the source-private Staff binding from bilateral evidence.
func ShareReceivingResponse(v ReceivingResponseRecord) ReceivingSharedResponse {
	return ReceivingSharedResponse{ID: v.ID, OwnerTenantID: v.OwnerTenantID, BuyerTenantID: v.BuyerTenantID, OrderID: v.OrderID, ShipmentID: v.ShipmentID, SourceOrganizationID: v.SourceOrganizationID, SourceLocationID: v.SourceLocationID, SourceProduct: v.SourceProduct, SummaryVersion: v.SummaryVersion, Response: v.Response, Reason: v.Reason, ActorSubject: v.ActorSubject, CreatedAt: v.CreatedAt}
}
func ValidateSharedReceivingResponse(v ReceivingSharedResponse) error {
	for _, id := range []uuid.UUID{v.ID, v.OwnerTenantID, v.BuyerTenantID, v.OrderID, v.ShipmentID, v.SourceOrganizationID, v.SourceLocationID} {
		if id == uuid.Nil {
			return ErrInvalid
		}
	}
	if v.OwnerTenantID == v.BuyerTenantID || v.SummaryVersion < 1 || v.SummaryVersion > 9007199254740991 || v.CreatedAt.IsZero() || (v.SourceProduct != "supply" && v.SourceProduct != "pharmacy") || (v.Response != "confirmed" && v.Response != "disputed") || !suppliertrade.Text(v.Reason, 500) || !suppliertrade.Text(v.ActorSubject, 200) {
		return ErrInvalid
	}
	return nil
}
