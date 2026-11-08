package supplierorder

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func copyOrder(order SupplierOrder) (SupplierOrder, error) {
	raw, err := json.Marshal(order)
	if err != nil {
		return SupplierOrder{}, ErrInvalid
	}
	var result SupplierOrder
	if json.Unmarshal(raw, &result) != nil {
		return SupplierOrder{}, ErrInvalid
	}
	return result, nil
}
func exchange(order SupplierOrder, phase, actor string) (SupplierOrderExchange, error) {
	frozen, err := copyOrder(order)
	if err != nil {
		return SupplierOrderExchange{}, err
	}
	order = frozen
	return SupplierOrderExchange{OrderID: order.ID, Terms: order.Terms, Input: order.Input, InputSha256: order.InputSha256, SourceTenantID: order.OwnerTenantID, SourceEpoch: order.AuthorityEpoch, Phase: phase, SignatorySubject: actor}, nil
}

// Decide records an independent review or a local cancellation request.
func Decide(order SupplierOrder, in SupplierOrderDecisionInput, actor Actor, at time.Time) (SupplierOrder, *SupplierOrderExchange, error) {
	if ValidateRecord(order) != nil || !slices.Contains([]string{"approve", "reject", "cancel"}, in.Decision) || !validActor(actor) || !suppliertrade.Text(in.Reason, 500) || at.IsZero() {
		return order, nil, ErrInvalid
	}
	if in.ExpectedVersion != order.Version || order.Version >= maximumVersion || order.AuthorityEpoch >= maximumVersion {
		return order, nil, ErrConflict
	}
	if in.Decision != "cancel" && (actor.Subject == order.PreparedBy || (order.PreparedStaffID != nil && actor.StaffID == *order.PreparedStaffID) || (order.RemoteSignedBy != nil && actor.Subject == *order.RemoteSignedBy)) {
		return order, nil, ErrConflict
	}
	if in.Decision == "approve" && !order.Input.ExpiresAt.After(at) {
		return order, nil, ErrConflict
	}
	next, err := copyOrder(order)
	if err != nil {
		return order, nil, err
	}
	phase := ""
	switch in.Decision {
	case "approve":
		if next.Side == "buyer" && next.Status == "pending_review" {
			next.Status = "offered"
			phase = "approved"
		} else if next.Side == "seller" && next.Status == "incoming" {
			next.Status = "accepted"
			phase = "accepted"
		} else {
			return order, nil, ErrConflict
		}
	case "reject":
		if next.Side == "buyer" && next.Status == "pending_review" {
			next.Status = "rejected"
		} else if next.Side == "seller" && next.Status == "incoming" {
			next.Status = "rejected"
			phase = "rejected"
		} else {
			return order, nil, ErrConflict
		}
	case "cancel":
		if next.Side != "buyer" {
			return order, nil, ErrConflict
		}
		switch next.Status {
		case "pending_review":
			next.Status = "cancelled"
		case "offered", "accepted":
			next.Status = "cancellation_requested"
			phase = "cancel_requested"
		default:
			return order, nil, ErrConflict
		}
	}
	next.Version++
	next.AuthorityEpoch++
	if in.Decision != "cancel" || next.SignedBy == nil {
		next.SignedBy = &actor.Subject
		next.SignedStaffID = &actor.StaffID
	}
	stamp := at.UTC().Truncate(time.Microsecond)
	if stamp.After(next.UpdatedAt) {
		next.UpdatedAt = stamp
	}
	if phase == "" {
		return next, nil, nil
	}
	event, err := exchange(next, phase, actor.Subject)
	if err != nil {
		return order, nil, err
	}
	return next, &event, nil
}

// Apply folds an owner-validated directed exchange into retained order evidence.
func Apply(current *SupplierOrder, event SupplierOrderExchange, destination uuid.UUID, at time.Time) (SupplierOrder, error) {
	var empty SupplierOrder
	terms, input, hash, err := frozenIntent(event.Terms, event.Input)
	if err != nil || hash != event.InputSha256 || !recordID(event.OrderID) || event.SourceEpoch < 1 || event.SourceEpoch > maximumVersion || !suppliertrade.Text(event.SignatorySubject, 200) || at.IsZero() || ValidateInput(terms, input, terms.ValidFrom) != nil {
		return empty, ErrInvalid
	}
	side := "seller"
	source := terms.Buyer.TenantID
	if destination == terms.Buyer.TenantID {
		side = "buyer"
		source = terms.Seller.TenantID
	} else if destination != terms.Seller.TenantID {
		return empty, ErrInvalid
	}
	if source != event.SourceTenantID || source == destination || (side == "buyer" && !slices.Contains([]string{"accepted", "rejected", "cancelled"}, event.Phase)) || (side == "seller" && !slices.Contains([]string{"approved", "cancel_requested"}, event.Phase)) {
		return empty, ErrInvalid
	}
	if current == nil {
		if side != "seller" || (event.Phase != "approved" && event.Phase != "cancel_requested") {
			return empty, ErrInvalid
		}
		stamp := at.UTC().Truncate(time.Microsecond)
		status := "incoming"
		if event.Phase == "cancel_requested" {
			status = "cancelled"
		}
		return SupplierOrder{ID: event.OrderID, OwnerTenantID: destination, Side: side, Terms: terms, Input: input, InputSha256: hash, Status: status, Version: 1, AuthorityEpoch: 1, RemoteEpoch: event.SourceEpoch, RemotePhase: event.Phase, RemoteSignedBy: &event.SignatorySubject, PreparedBy: "system:supplier-order", CreatedAt: stamp, UpdatedAt: stamp}, nil
	}
	if ValidateRecord(*current) != nil {
		return empty, ErrInvalid
	}
	order, err := copyOrder(*current)
	if err != nil {
		return empty, err
	}
	if order.ID != event.OrderID || order.OwnerTenantID != destination || order.Side != side || order.InputSha256 != hash {
		return empty, ErrInvalid
	}
	if event.SourceEpoch < order.RemoteEpoch {
		return order, nil
	}
	if event.SourceEpoch == order.RemoteEpoch {
		if order.RemotePhase != event.Phase || order.RemoteSignedBy == nil || *order.RemoteSignedBy != event.SignatorySubject {
			return empty, ErrInvalid
		}
		return order, nil
	}
	if order.Version >= maximumVersion {
		return empty, ErrConflict
	}
	if order.Status != "cancelled" && order.Status != "rejected" {
		switch event.Phase {
		case "accepted":
			if side != "buyer" || (order.Status != "offered" && order.Status != "cancellation_requested") || order.SignedBy == nil || *order.SignedBy == event.SignatorySubject {
				return empty, ErrInvalid
			}
			if order.Status != "cancellation_requested" {
				order.Status = "accepted"
			}
		case "rejected":
			if side != "buyer" || (order.Status != "offered" && order.Status != "cancellation_requested") {
				return empty, ErrInvalid
			}
			order.Status = "rejected"
		case "cancel_requested":
			if side != "seller" || (order.Status != "incoming" && order.Status != "accepted" && order.Status != "cancellation_requested") {
				return empty, ErrInvalid
			}
			if order.Status == "incoming" {
				order.Status = "cancelled"
			} else {
				order.Status = "cancellation_requested"
			}
		case "cancelled":
			if side != "buyer" || order.Status != "cancellation_requested" {
				return empty, ErrInvalid
			}
			order.Status = "cancelled"
		default:
			return empty, ErrInvalid
		}
	}
	order.Version++
	order.RemoteEpoch = event.SourceEpoch
	order.RemotePhase = event.Phase
	order.RemoteSignedBy = &event.SignatorySubject
	stamp := at.UTC().Truncate(time.Microsecond)
	if stamp.After(order.UpdatedAt) {
		order.UpdatedAt = stamp
	}
	return order, nil
}
