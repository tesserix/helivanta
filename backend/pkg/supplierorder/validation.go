package supplierorder

import (
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func recordID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

// ValidateInput checks the immutable intent, never current product, stock or actor authority.
func ValidateInput(terms suppliertrade.Terms, in SupplierOrderInput, at time.Time) error {
	digest, err := suppliertrade.TermsDigest(terms)
	if err != nil || suppliertrade.ValidateTerms(terms, at) != nil || !recordID(in.RelationshipID) || digest != in.TermsSha256 || !suppliertrade.Text(in.PurchaseOrderReference, 200) || !suppliertrade.Text(in.QuoteReference, 200) || !suppliertrade.Text(in.Reason, 500) || !in.ExpiresAt.After(at) || in.ExpiresAt.After(terms.ExpiresAt) || len(in.Lines) < 1 || len(in.Lines) > maximumLines {
		return ErrInvalid
	}
	return ValidateProductLines(in.Lines, terms.Seller.Product)
}

// ValidateRecord verifies retained evidence; it does not authorize a new commitment.
func ValidateRecord(order SupplierOrder) error {
	_, _, hash, err := frozenIntent(order.Terms, order.Input)
	if err != nil || hash != order.InputSha256 || !recordID(order.ID) || ValidateInput(order.Terms, order.Input, order.Terms.ValidFrom) != nil || !suppliertrade.Text(order.PreparedBy, 200) || order.CreatedAt.IsZero() || order.UpdatedAt.Before(order.CreatedAt) || order.Version < 1 || order.Version > maximumVersion || order.AuthorityEpoch < 1 || order.AuthorityEpoch > order.Version || order.RemoteEpoch < 0 || order.RemoteEpoch > maximumVersion {
		return ErrInvalid
	}
	if (order.Side == "buyer" && order.OwnerTenantID != order.Terms.Buyer.TenantID) || (order.Side == "seller" && order.OwnerTenantID != order.Terms.Seller.TenantID) || (order.Side != "buyer" && order.Side != "seller") {
		return ErrInvalid
	}
	if order.BuyerDepartmentID != nil && (!recordID(*order.BuyerDepartmentID) || order.Side != "buyer") {
		return ErrInvalid
	}
	if order.Side == "buyer" && (order.PreparedStaffID == nil || *order.PreparedStaffID == uuid.Nil || order.PreparedStaffID.Variant() != uuid.RFC4122) {
		return ErrInvalid
	}
	if order.Side == "seller" && order.PreparedStaffID != nil {
		return ErrInvalid
	}
	if (order.SignedBy == nil) != (order.SignedStaffID == nil) || (order.SignedStaffID != nil && (*order.SignedStaffID == uuid.Nil || order.SignedStaffID.Variant() != uuid.RFC4122)) {
		return ErrInvalid
	}
	if order.SignedBy != nil && !suppliertrade.Text(*order.SignedBy, 200) {
		return ErrInvalid
	}
	if order.RemoteEpoch == 0 {
		if order.Side != "buyer" || order.RemotePhase != "" || order.RemoteSignedBy != nil {
			return ErrInvalid
		}
	} else {
		if order.RemoteSignedBy == nil || !suppliertrade.Text(*order.RemoteSignedBy, 200) {
			return ErrInvalid
		}
		if order.Side == "buyer" && !slices.Contains([]string{"accepted", "rejected", "cancelled"}, order.RemotePhase) {
			return ErrInvalid
		}
		if order.Side == "seller" && !slices.Contains([]string{"approved", "cancel_requested"}, order.RemotePhase) {
			return ErrInvalid
		}
	}
	if order.Side == "buyer" && slices.Contains([]string{"offered", "accepted", "rejected", "cancellation_requested"}, order.Status) && order.PreparedStaffID != nil && order.SignedStaffID != nil && *order.PreparedStaffID == *order.SignedStaffID {
		return ErrInvalid
	}
	switch order.Status {
	case "pending_review":
		if order.Side != "buyer" || order.SignedBy != nil || order.RemoteEpoch != 0 {
			return ErrInvalid
		}
	case "offered":
		if order.Side != "buyer" || order.SignedBy == nil || *order.SignedBy == order.PreparedBy {
			return ErrInvalid
		}
	case "incoming":
		if order.Side != "seller" || order.SignedBy != nil || order.RemotePhase != "approved" {
			return ErrInvalid
		}
	case "accepted":
		if order.SignedBy == nil || order.RemoteSignedBy == nil || *order.SignedBy == *order.RemoteSignedBy {
			return ErrInvalid
		}
	case "cancellation_requested":
		if order.SignedBy == nil {
			return ErrInvalid
		}
	case "rejected", "cancelled":
		if order.SignedBy == nil && order.RemoteSignedBy == nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
