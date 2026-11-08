package supplierfulfilment

import (
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"time"
)

// ReceivingTotals is owner-read counted custody; it is never a client balance.
type ReceivingTotals struct{ Counted, Accepted, Rejected int64 }

func validTotals(t ReceivingTotals, limit int64) bool {
	return t.Counted >= 0 && t.Counted <= limit && t.Accepted >= 0 && t.Accepted <= t.Counted && t.Rejected >= 0 && t.Rejected <= t.Counted && t.Accepted+t.Rejected == t.Counted
}

// ValidateShipment checks immutable evidence shape; transport and owner provenance are separate gates.
func ValidateShipment(s Shipment) error {
	hash, e := hex.DecodeString(s.OrderInputSha256)
	if e != nil || len(hash) != 32 || !recordID(s.ID) || !recordID(s.AllocationID) || !recordID(s.OrderID) || s.SourceTenantID == uuid.Nil || s.BuyerTenantID == uuid.Nil || s.SourceTenantID == s.BuyerTenantID || s.HospitalID == uuid.Nil || !recordID(s.SourceOrganizationID) || !recordID(s.SourceLocationID) || !recordID(s.BuyerOrganizationID) || !recordID(s.BuyerLocationID) || !suppliertrade.Text(s.HandoffReference, 200) || !suppliertrade.Text(s.ActorSubject, 200) || s.DispatchedAt.IsZero() || !suppliertrade.Text(s.LotNumber, 100) || !suppliertrade.Text(s.ProductDescription, 2000) || s.PackSize < 1 || s.PackSize > maximumUnits || s.MrpMinor != nil && (*s.MrpMinor < 0 || *s.MrpMinor > 1000000000000) {
		return ErrInvalid
	}
	if _, e = time.Parse(time.DateOnly, s.ExpiresOn); e != nil {
		return ErrInvalid
	}
	if supplierorder.ValidateProductLines([]supplierorder.SupplierOrderLine{{ID: s.LineID, Product: s.Product, QuantityUnits: s.QuantityUnits, UnitCode: s.UnitCode}}, s.Product.Owner) != nil {
		return ErrInvalid
	}
	return nil
}

// Receive records an attributable count and its cumulative destination-owner budget.
func Receive(id uuid.UUID, s Shipment, in ReceivingInput, current ReceivingTotals, actor supplierorder.Actor, at time.Time) (ReceivingReceipt, error) {
	var empty ReceivingReceipt
	if ValidateShipment(s) != nil {
		return empty, ErrUnavailable
	}
	if !recordID(id) || !validActor(actor) || at.Before(s.DispatchedAt) || at.IsZero() || in.CountedUnits < 1 || in.CountedUnits > maximumUnits || in.AcceptedUnits < 0 || in.RejectedUnits < 0 || in.AcceptedUnits > in.CountedUnits || in.RejectedUnits > in.CountedUnits || in.AcceptedUnits+in.RejectedUnits != in.CountedUnits || !suppliertrade.Text(in.Reason, 500) || !suppliertrade.Text(in.ObservedLotNumber, 100) {
		return empty, ErrInvalid
	}
	if in.MrpMinor != nil && (*in.MrpMinor < 0 || *in.MrpMinor > 1000000000000) {
		return empty, ErrInvalid
	}
	if _, err := time.Parse(time.DateOnly, in.ObservedExpiresOn); err != nil {
		return empty, ErrInvalid
	}
	if !validTotals(current, s.QuantityUnits) {
		return empty, ErrUnavailable
	}
	if in.CountedUnits > s.QuantityUnits-current.Counted {
		return empty, ErrConflict
	}
	if in.AcceptedUnits > 0 {
		if !in.IdentityConfirmed || in.DestinationProductID == nil || !recordID(*in.DestinationProductID) || in.DestinationProductVersion == nil || *in.DestinationProductVersion < 1 || *in.DestinationProductVersion > 9007199254740991 || in.DestinationBatchID == nil || !recordID(*in.DestinationBatchID) || in.QuarantineLocationID == nil || !recordID(*in.QuarantineLocationID) || in.MrpMinor == nil || *in.MrpMinor < 0 || *in.MrpMinor > 1000000000000 {
			return empty, ErrInvalid
		}
		if in.ObservedLotNumber != s.LotNumber || in.ObservedExpiresOn != s.ExpiresOn || s.ExpiresOn <= at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly) || s.MrpMinor != nil && *s.MrpMinor != *in.MrpMinor {
			return empty, ErrConflict
		}
	} else if in.IdentityConfirmed || in.DestinationProductID != nil || in.DestinationProductVersion != nil || in.DestinationBatchID != nil || in.QuarantineLocationID != nil {
		return empty, ErrInvalid
	}
	result := ReceivingReceipt{ID: id, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, OrderID: s.OrderID, HospitalID: s.HospitalID, PharmacyID: s.BuyerOrganizationID, OutletID: s.BuyerLocationID, Input: in, CumulativeCountedUnits: current.Counted + in.CountedUnits, CumulativeAcceptedUnits: current.Accepted + in.AcceptedUnits, CumulativeRejectedUnits: current.Rejected + in.RejectedUnits, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, CreatedAt: at.UTC().Truncate(time.Microsecond)}
	if in.AcceptedUnits > 0 {
		result.MovementID = &id
	}
	return frozen(result)
}

// CloseReceiving records the remaining short quantity without changing physical stock or financial liability.
func CloseReceiving(id uuid.UUID, s Shipment, current ReceivingTotals, in ReceivingClosureInput, actor supplierorder.Actor, at time.Time) (ReceivingClosure, error) {
	if ValidateShipment(s) != nil || !validTotals(current, s.QuantityUnits) {
		return ReceivingClosure{}, ErrUnavailable
	}
	if !recordID(id) || !validActor(actor) || at.Before(s.DispatchedAt) || at.IsZero() || !suppliertrade.Text(in.Reason, 500) {
		return ReceivingClosure{}, ErrInvalid
	}
	if in.ExpectedCountedUnits != current.Counted {
		return ReceivingClosure{}, ErrConflict
	}
	return ReceivingClosure{ID: id, OwnerTenantID: s.BuyerTenantID, ShipmentID: s.ID, CountedUnits: current.Counted, AcceptedUnits: current.Accepted, RejectedUnits: current.Rejected, ShortUnits: s.QuantityUnits - current.Counted, Reason: in.Reason, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, CreatedAt: at.UTC().Truncate(time.Microsecond)}, nil
}

// ValidateReceivingSummary bounds a recipient's progress against immutable source dispatch evidence.
func ValidateReceivingSummary(v ReceivingSummary, s Shipment) error {
	if ValidateShipment(s) != nil || !recordID(v.ID) || v.ShipmentID != s.ID || v.OrderID != s.OrderID || v.OwnerTenantID != s.BuyerTenantID || v.Version < 1 || v.Version > 9007199254740991 || !validTotals(ReceivingTotals{Counted: v.CountedUnits, Accepted: v.AcceptedUnits, Rejected: v.RejectedUnits}, s.QuantityUnits) || !suppliertrade.Text(v.Reason, 500) || !suppliertrade.Text(v.ActorSubject, 200) || v.RecordedAt.IsZero() || v.RecordedAt.Before(s.DispatchedAt) {
		return ErrInvalid
	}
	if v.Closed && v.ShortUnits != s.QuantityUnits-v.CountedUnits || !v.Closed && v.ShortUnits != 0 {
		return ErrInvalid
	}
	return nil
}
