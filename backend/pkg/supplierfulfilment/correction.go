package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"time"
)

// ReceiptCorrectionState is the owner's effective receipt count, never a submitted balance.
type ReceiptCorrectionState struct {
	Version      int64
	Counts       ReceivingTotals
	ActorStaffID uuid.UUID
}

// CorrectReceiving records an independent recount; the stock owner must enforce quarantine custody atomically.
func CorrectReceiving(id uuid.UUID, s Shipment, original ReceivingReceipt, prior ReceiptCorrectionState, total ReceivingTotals, summaryVersion int64, in ReceivingCorrectionInput, reviewer supplierorder.Actor, at time.Time) (ReceivingCorrection, error) {
	empty := ReceivingCorrection{}
	if ValidateShipment(s) != nil || original.OwnerTenantID != s.BuyerTenantID || original.ShipmentID != s.ID || original.OrderID != s.OrderID || original.OutletID != s.BuyerLocationID || !recordID(original.ID) || original.ActorStaffID == uuid.Nil || !validTotals(total, s.QuantityUnits) {
		return empty, ErrUnavailable
	}
	if prior.Version == 0 {
		prior = ReceiptCorrectionState{Version: 1, Counts: ReceivingTotals{Counted: original.Input.CountedUnits, Accepted: original.Input.AcceptedUnits, Rejected: original.Input.RejectedUnits}, ActorStaffID: original.ActorStaffID}
	}
	if prior.Version < 1 || prior.Version >= 9007199254740991 || !validTotals(prior.Counts, s.QuantityUnits) || prior.ActorStaffID == uuid.Nil || total.Counted < prior.Counts.Counted || total.Accepted < prior.Counts.Accepted || total.Rejected < prior.Counts.Rejected {
		return empty, ErrUnavailable
	}
	if !recordID(id) || !validActor(reviewer) || !suppliertrade.Text(in.Reason, 500) || at.IsZero() || at.Before(original.CreatedAt) || !validTotals(ReceivingTotals{Counted: in.CountedUnits, Accepted: in.AcceptedUnits, Rejected: in.RejectedUnits}, s.QuantityUnits) {
		return empty, ErrInvalid
	}
	if in.ExpectedSummaryVersion != summaryVersion || in.ExpectedReceiptVersion != prior.Version || summaryVersion < 1 || summaryVersion >= 9007199254740991 || reviewer.StaffID == original.ActorStaffID || reviewer.StaffID == prior.ActorStaffID {
		return empty, ErrConflict
	}
	next := ReceivingTotals{Counted: total.Counted - prior.Counts.Counted + in.CountedUnits, Accepted: total.Accepted - prior.Counts.Accepted + in.AcceptedUnits, Rejected: total.Rejected - prior.Counts.Rejected + in.RejectedUnits}
	if !validTotals(next, s.QuantityUnits) || (in.CountedUnits == prior.Counts.Counted && in.AcceptedUnits == prior.Counts.Accepted && in.RejectedUnits == prior.Counts.Rejected) {
		return empty, ErrConflict
	}
	delta := in.AcceptedUnits - prior.Counts.Accepted
	if in.AcceptedUnits > 0 && (original.MovementID == nil || original.Input.DestinationBatchID == nil || original.Input.QuarantineLocationID == nil) {
		return empty, ErrConflict
	}
	if delta > 0 && (!in.IdentityConfirmed || s.ExpiresOn <= at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)) {
		return empty, ErrConflict
	}
	result := ReceivingCorrection{ID: id, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, ReceiptID: original.ID, OutletID: s.BuyerLocationID, Version: prior.Version + 1, Input: in, AcceptedDeltaUnits: delta, CumulativeCountedUnits: next.Counted, CumulativeAcceptedUnits: next.Accepted, CumulativeRejectedUnits: next.Rejected, ActorSubject: reviewer.Subject, ActorStaffID: reviewer.StaffID, CreatedAt: at.UTC().Truncate(time.Microsecond)}
	if delta != 0 {
		result.MovementID = &id
	}
	return result, nil
}
