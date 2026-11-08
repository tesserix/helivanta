// Package supplierfulfilment defines stock-owner commitments and attributable receiving evidence.
package supplierfulfilment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"time"
)

var ErrInvalid = errors.New("invalid supplier fulfilment intent")
var ErrConflict = errors.New("supplier fulfilment state changed")
var ErrUnavailable = errors.New("supplier fulfilment authority unavailable")

// Budget is supplied by the stock owner while holding the order-line allocation lock.
type Budget struct{ ReservedUnits, DispatchedUnits int64 }

func recordID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func validActor(a supplierorder.Actor) bool {
	return suppliertrade.Text(a.Subject, 200) && a.StaffID != uuid.Nil && a.StaffID.Variant() == uuid.RFC4122
}

// Plan binds an exact accepted order line to an owner-local stock reservation.
func Plan(id uuid.UUID, order supplierorder.SupplierOrder, in ReserveInput, actor supplierorder.Actor, budget Budget, at time.Time) (AllocationPlan, error) {
	var empty AllocationPlan
	at = at.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !recordID(id) || !validActor(actor) || at.IsZero() || !recordID(in.LineID) || !recordID(in.StockID) || in.QuantityUnits < 1 || in.QuantityUnits > maximumUnits || !suppliertrade.Text(in.Reason, 500) || !in.ExpiresAt.After(at) || in.ExpiresAt.Sub(at) > time.Duration(maximumLeaseSeconds)*time.Second {
		return empty, ErrInvalid
	}
	if supplierorder.ValidateRecord(order) != nil {
		return empty, ErrUnavailable
	}
	if order.Side != "seller" || order.OwnerTenantID != order.Terms.Seller.TenantID || order.Status != "accepted" || in.ExpectedOrderVersion != order.Version || at.Before(order.Terms.ValidFrom) || !order.Input.ExpiresAt.After(at) || in.ExpiresAt.After(order.Input.ExpiresAt) || in.ExpiresAt.After(order.Terms.ExpiresAt) {
		return empty, ErrConflict
	}
	if order.Terms.Seller.Product == "pharmacy" {
		if in.StockLocationID == nil || !recordID(*in.StockLocationID) {
			return empty, ErrInvalid
		}
	} else if in.StockLocationID != nil {
		return empty, ErrInvalid
	}
	var line *supplierorder.SupplierOrderLine
	for i := range order.Input.Lines {
		if order.Input.Lines[i].ID == in.LineID {
			line = &order.Input.Lines[i]
			break
		}
	}
	if line == nil {
		return empty, ErrInvalid
	}
	if budget.ReservedUnits < 0 || budget.DispatchedUnits < 0 || budget.ReservedUnits > line.QuantityUnits || budget.DispatchedUnits > line.QuantityUnits || budget.ReservedUnits > line.QuantityUnits-budget.DispatchedUnits {
		return empty, ErrUnavailable
	}
	if in.QuantityUnits > line.QuantityUnits-budget.DispatchedUnits-budget.ReservedUnits {
		return empty, ErrConflict
	}
	result := AllocationPlan{ID: id, OwnerTenantID: order.OwnerTenantID, OrderID: order.ID, OrderInputSha256: order.InputSha256, LineQuantityUnits: line.QuantityUnits, OrderAuthorityEpoch: order.AuthorityEpoch, OrderRemoteEpoch: order.RemoteEpoch, SourceOrganizationID: order.Terms.Seller.OrganizationID, SourceLocationID: order.Terms.Seller.LocationID, BuyerTenantID: order.Terms.Buyer.TenantID, HospitalID: *order.Terms.Buyer.HospitalID, BuyerOrganizationID: order.Terms.Buyer.OrganizationID, BuyerLocationID: order.Terms.Buyer.LocationID, Product: line.Product, UnitCode: line.UnitCode, Input: in, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, CreatedAt: at}
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, ErrInvalid
	}
	if json.Unmarshal(raw, &result) != nil {
		return empty, ErrInvalid
	}
	return result, nil
}

// IntentHash binds a command to its server-verified actor and exact canonical input.
func IntentHash[T any](actor supplierorder.Actor, input T) (string, error) {
	if !validActor(actor) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Actor supplierorder.Actor
		Input T
	}{actor, input})
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// StockEvidence contains owner-read product and physical facts, never a client availability claim.
type StockEvidence struct {
	LotNumber, ExpiresOn, Description string
	PackSize, AvailableUnits          int64
}

// Reserve records an owner-verified stock bucket, never an unchecked client balance.
func Reserve(plan AllocationPlan, evidence StockEvidence, at time.Time) (Allocation, error) {
	lot, expiry, available := evidence.LotNumber, evidence.ExpiresOn, evidence.AvailableUnits
	if !suppliertrade.Text(evidence.Description, 2000) || evidence.PackSize < 1 || evidence.PackSize > maximumUnits {
		return Allocation{}, ErrInvalid
	}
	if ValidatePlan(plan) != nil || !suppliertrade.Text(lot, 100) || at.IsZero() || at.Before(plan.CreatedAt) || available < 0 || available > 9007199254740991 {
		return Allocation{}, ErrInvalid
	}
	day, err := time.Parse(time.DateOnly, expiry)
	if err != nil {
		return Allocation{}, ErrInvalid
	}
	localDay := at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)
	if !plan.Input.ExpiresAt.After(at) || day.Format(time.DateOnly) <= localDay || available < plan.Input.QuantityUnits {
		return Allocation{}, ErrConflict
	}
	return Allocation{Plan: plan, Version: 1, Status: "reserved", LotNumber: lot, ExpiresOn: expiry, ReservedUnits: plan.Input.QuantityUnits, ProductDescription: evidence.Description, PackSize: evidence.PackSize, UpdatedAt: at.UTC().Truncate(time.Microsecond)}, nil
}

// ValidatePlan checks stored evidence shape; publication/actor guards remain mandatory.
func ValidatePlan(p AllocationPlan) error {
	hash, e := hex.DecodeString(p.OrderInputSha256)
	if p.LineQuantityUnits < 1 || p.LineQuantityUnits > maximumUnits || p.Input.QuantityUnits > p.LineQuantityUnits {
		return ErrInvalid
	}
	if e != nil || len(hash) != 32 || !recordID(p.ID) || !recordID(p.OrderID) || p.OwnerTenantID == uuid.Nil || p.BuyerTenantID == uuid.Nil || p.OwnerTenantID == p.BuyerTenantID || p.HospitalID == uuid.Nil || !recordID(p.SourceOrganizationID) || !recordID(p.SourceLocationID) || !recordID(p.BuyerOrganizationID) || !recordID(p.BuyerLocationID) || p.OrderAuthorityEpoch < 1 || p.OrderRemoteEpoch < 1 || p.Input.ExpectedOrderVersion < 1 || !recordID(p.Input.StockID) || !validActor(supplierorder.Actor{Subject: p.ActorSubject, StaffID: p.ActorStaffID}) || !suppliertrade.Text(p.Input.Reason, 500) || p.CreatedAt.IsZero() || !p.Input.ExpiresAt.After(p.CreatedAt) || p.Input.ExpiresAt.Sub(p.CreatedAt) > time.Duration(maximumLeaseSeconds)*time.Second {
		return ErrInvalid
	}
	if p.Product.Owner == "pharmacy" {
		if p.Input.StockLocationID == nil || !recordID(*p.Input.StockLocationID) {
			return ErrInvalid
		}
	} else if p.Input.StockLocationID != nil {
		return ErrInvalid
	}
	if supplierorder.ValidateProductLines([]supplierorder.SupplierOrderLine{{ID: p.Input.LineID, Product: p.Product, QuantityUnits: p.Input.QuantityUnits, UnitCode: p.UnitCode}}, p.Product.Owner) != nil {
		return ErrInvalid
	}
	return nil
}

// ValidateAllocation rejects corrupted counters and mismatched terminal states.
func ValidateAllocation(a Allocation) error {
	if !suppliertrade.Text(a.ProductDescription, 2000) || a.PackSize < 1 || a.PackSize > maximumUnits || ValidatePlan(a.Plan) != nil || a.Version < 1 || a.Version > 9007199254740991 || !suppliertrade.Text(a.LotNumber, 100) || a.UpdatedAt.Before(a.Plan.CreatedAt) || a.ReservedUnits < 0 || a.DispatchedUnits < 0 || a.ReleasedUnits < 0 || a.ReservedUnits > a.Plan.Input.QuantityUnits || a.DispatchedUnits > a.Plan.Input.QuantityUnits || a.ReleasedUnits > a.Plan.Input.QuantityUnits || a.ReservedUnits+a.DispatchedUnits+a.ReleasedUnits != a.Plan.Input.QuantityUnits {
		return ErrInvalid
	}
	if _, e := time.Parse(time.DateOnly, a.ExpiresOn); e != nil {
		return ErrInvalid
	}
	if a.ReservedUnits > 0 {
		if a.Status != "reserved" {
			return ErrInvalid
		}
	} else if a.DispatchedUnits > 0 {
		if a.Status != "dispatched" {
			return ErrInvalid
		}
	} else if a.Status != "released" {
		return ErrInvalid
	}
	return nil
}

// Observe exposes expiry without inventing a stock movement or rewriting an immutable receipt.
func Observe(a Allocation, at time.Time) (AllocationView, error) {
	if ValidateAllocation(a) != nil || at.IsZero() {
		return AllocationView{}, ErrInvalid
	}
	v := AllocationView{Allocation: a, EffectiveStatus: a.Status, EffectiveReservedUnits: a.ReservedUnits, ObservedAt: at.UTC().Truncate(time.Microsecond)}
	if a.ReservedUnits > 0 && !a.Plan.Input.ExpiresAt.After(at) {
		v.EffectiveStatus = "expired"
		v.EffectiveReservedUnits = 0
	}
	return v, nil
}

// Decide applies one human-confirmed partial dispatch or release under the owner lock.
func Decide(a Allocation, in AllocationDecisionInput, actor supplierorder.Actor, at time.Time) (Allocation, error) {
	if ValidateAllocation(a) != nil || !validActor(actor) || at.IsZero() || at.Before(a.UpdatedAt) || !suppliertrade.Text(in.Reason, 500) || in.QuantityUnits < 1 || in.QuantityUnits > maximumUnits || (in.Decision != "release" && in.Decision != "dispatch") {
		return Allocation{}, ErrInvalid
	}
	if in.Decision == "dispatch" && !suppliertrade.Text(in.HandoffReference, 200) || in.Decision == "release" && in.HandoffReference != "" {
		return Allocation{}, ErrInvalid
	}
	if in.ExpectedVersion != a.Version || a.Version >= 9007199254740991 || a.ReservedUnits < in.QuantityUnits {
		return Allocation{}, ErrConflict
	}
	if in.Decision == "dispatch" && (!a.Plan.Input.ExpiresAt.After(at) || a.ExpiresOn <= at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)) {
		return Allocation{}, ErrConflict
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return Allocation{}, ErrInvalid
	}
	var next Allocation
	if json.Unmarshal(raw, &next) != nil {
		return Allocation{}, ErrInvalid
	}
	next.ReservedUnits -= in.QuantityUnits
	if in.Decision == "dispatch" {
		next.DispatchedUnits += in.QuantityUnits
	} else {
		next.ReleasedUnits += in.QuantityUnits
	}
	if next.ReservedUnits == 0 {
		next.Status = "released"
		if next.DispatchedUnits > 0 {
			next.Status = "dispatched"
		}
	}
	next.Version++
	next.UpdatedAt = at.UTC().Truncate(time.Microsecond)
	return next, nil
}

// ShipmentFor produces only the exact physical dispatch, excluding private local Staff identity.
func ShipmentFor(r AllocationCommandReceipt, mrp *int64) (Shipment, error) {
	a := r.Allocation
	p := a.Plan
	if !recordID(r.CommandID) || ValidateAllocation(a) != nil || r.Input.Decision != "dispatch" || r.Input.ExpectedVersion+1 != a.Version || r.Input.QuantityUnits < 1 || r.Input.QuantityUnits > a.DispatchedUnits || !suppliertrade.Text(r.Input.HandoffReference, 200) || !validActor(supplierorder.Actor{Subject: r.ActorSubject, StaffID: r.ActorStaffID}) || !r.RecordedAt.Equal(a.UpdatedAt) || mrp != nil && (*mrp < 0 || *mrp > 1000000000000) {
		return Shipment{}, ErrInvalid
	}
	result := Shipment{ID: r.CommandID, AllocationID: p.ID, OrderID: p.OrderID, LineID: p.Input.LineID, OrderInputSha256: p.OrderInputSha256, SourceTenantID: p.OwnerTenantID, SourceOrganizationID: p.SourceOrganizationID, SourceLocationID: p.SourceLocationID, BuyerTenantID: p.BuyerTenantID, HospitalID: p.HospitalID, BuyerOrganizationID: p.BuyerOrganizationID, BuyerLocationID: p.BuyerLocationID, Product: p.Product, UnitCode: p.UnitCode, QuantityUnits: r.Input.QuantityUnits, LotNumber: a.LotNumber, ExpiresOn: a.ExpiresOn, MrpMinor: mrp, ProductDescription: a.ProductDescription, PackSize: a.PackSize, HandoffReference: r.Input.HandoffReference, ActorSubject: r.ActorSubject, DispatchedAt: r.RecordedAt}
	return frozen(result)
}

func frozen[T any](value T) (T, error) {
	var result T
	raw, err := json.Marshal(value)
	if err != nil || json.Unmarshal(raw, &result) != nil {
		return result, ErrInvalid
	}
	return result, nil
}
