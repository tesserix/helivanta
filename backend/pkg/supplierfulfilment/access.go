package supplierfulfilment

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/supplierorder"
)

type OrderPurpose string

const (
	NewReservation OrderPurpose = "reserve"
	DispatchOrder  OrderPurpose = "dispatch"
	RetainedOrder  OrderPurpose = "retained"
)

// SourceScope names the stock owner's exact order; it is not a permission.
type SourceScope struct {
	TenantID, OrderID, OrganizationID, LocationID uuid.UUID
	HospitalID, DepartmentID                      uuid.UUID
	Product                                       string
}

func (s SourceScope) Valid() bool {
	return s.TenantID != uuid.Nil && recordID(s.OrderID) && recordID(s.OrganizationID) && recordID(s.LocationID) && (s.Product == "supply" || s.Product == "pharmacy")
}

// OrderAuthorization is a single-use publication fence, never a wire token or Stock grant.
type OrderAuthorization interface {
	WithCurrent(context.Context, func(context.Context, supplierorder.SupplierOrder) error) error
}
type CurrentOrderGuard interface {
	VerifyOrder(context.Context, SourceScope, OrderPurpose) (OrderAuthorization, error)
}

// BuyerScope must be checked against both current department authority and owned outlet governance.
type BuyerScope struct{ TenantID, HospitalID, DepartmentID, OutletID, ShipmentID uuid.UUID }

func (s BuyerScope) Valid() bool {
	return s.TenantID != uuid.Nil && s.HospitalID != uuid.Nil && recordID(s.DepartmentID) && recordID(s.OutletID) && recordID(s.ShipmentID)
}

type ReceivingPurpose string

const (
	ReadReceiving   ReceivingPurpose = "read"
	RecordReceiving ReceivingPurpose = "record"
	ReviewReceiving ReceivingPurpose = "review"
)

// ReceivingAuthority is SupplyConnect's shipment/order publication and Workforce actor boundary.
// PharmaConnect must still enforce its own outlet governance, identity and physical-stock rules.
type ReceivingAuthority interface {
	WithShipment(context.Context, authn.Principal, BuyerScope, ReceivingPurpose, func(context.Context, Shipment, supplierorder.Actor) error) error
}

// ShipmentSafety freezes current source product/lot safety for a retained transaction.
// The caller must already hold the verified destination shipment/Staff guard.
type ShipmentSafety interface {
	WithSafeShipment(context.Context, Shipment, func(context.Context) error) error
}

// HospitalSourceAuthority admits canonical hospital Staff at an owned governed pharmacy outlet.
// It confers no authority over an independently owned pharmacy or another department.
type HospitalSourceAuthority interface {
	WithOutlet(context.Context, authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, func(context.Context, supplierorder.Actor) error) error
}

// StockCancellation releases only undispatched owner commitments under the order owner's retained cancellation fence.
type StockCancellation interface {
	ReleaseOrder(context.Context, supplierorder.SupplierOrder) error
}
