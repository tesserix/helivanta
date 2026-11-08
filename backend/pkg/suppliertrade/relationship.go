package suppliertrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"slices"
	"time"
)

var ErrConflict = errors.New("supplier relationship state changed")

type Relationship = SupplierRelationship
type RelationshipExchange = SupplierRelationshipExchange

func normalizeTerms(t Terms) Terms {
	t.ValidFrom = t.ValidFrom.UTC().Truncate(time.Microsecond)
	t.ExpiresAt = t.ExpiresAt.UTC().Truncate(time.Microsecond)
	return t
}
func TermsDigest(t Terms) (string, error) {
	raw, err := json.Marshal(normalizeTerms(t))
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func PrepareRelationship(id uuid.UUID, terms Terms, actor string, at time.Time) (Relationship, error) {
	terms = normalizeTerms(terms)
	if !recordID(id) || !Text(actor, 200) || ValidateTerms(terms, at) != nil {
		return Relationship{}, ErrInvalid
	}
	digest, err := TermsDigest(terms)
	if err != nil {
		return Relationship{}, err
	}
	at = at.UTC().Truncate(time.Microsecond)
	return Relationship{OwnerTenantID: terms.Seller.TenantID, ID: id, Side: "seller", Terms: terms, TermsSha256: digest, Status: "pending_review", Version: 1, AuthorityEpoch: 1, RemoteEpoch: 0, PreparedBy: actor, CreatedAt: at, UpdatedAt: at}, nil
}
func RelationshipCurrent(r Relationship, at time.Time) bool {
	return !at.IsZero() && r.Status == "active" && r.SignedBy != nil && r.RemoteSignedBy != nil && r.RemoteEpoch > 0 && at.Before(r.Terms.ExpiresAt) && !at.Before(r.Terms.ValidFrom)
}
func relationExchange(r Relationship, phase, actor string) RelationshipExchange {
	return RelationshipExchange{RelationshipID: r.ID, Terms: r.Terms, TermsSha256: r.TermsSha256, SourceTenantID: r.OwnerTenantID, SourceEpoch: r.AuthorityEpoch, Phase: phase, SignatorySubject: actor}
}
func DecideRelationship(r Relationship, expected int64, decision, actor string, at time.Time) (Relationship, *RelationshipExchange, error) {
	if !Text(actor, 200) || at.IsZero() {
		return r, nil, ErrInvalid
	}
	if expected != r.Version || r.Version >= 9007199254740991 || r.AuthorityEpoch >= 9007199254740991 {
		return r, nil, ErrConflict
	}
	phase := ""
	switch decision {
	case "approve":
		if !at.Before(r.Terms.ExpiresAt) || actor == r.PreparedBy || (r.RemoteSignedBy != nil && actor == *r.RemoteSignedBy) {
			return r, nil, ErrConflict
		}
		if r.Side == "seller" && r.Status == "pending_review" {
			r.Status = "offered"
			phase = "offered"
		} else if r.Side == "buyer" && r.Status == "incoming" {
			r.Status = "accepted_pending_ack"
			phase = "accepted"
		} else {
			return r, nil, ErrConflict
		}
		r.SignedBy = &actor
	case "reject":
		if (r.Side == "seller" && r.Status != "pending_review") || (r.Side == "buyer" && r.Status != "incoming") || actor == r.PreparedBy {
			return r, nil, ErrConflict
		}
		r.Status = "rejected"
		phase = "rejected"
		r.SignedBy = &actor
	case "suspend":
		if r.Status != "offered" && r.Status != "accepted_pending_ack" && r.Status != "active" {
			return r, nil, ErrConflict
		}
		r.Status = "suspended"
		phase = "suspended"
	default:
		return r, nil, ErrInvalid
	}
	r.Version++
	r.AuthorityEpoch++
	at = at.UTC().Truncate(time.Microsecond)
	if at.After(r.UpdatedAt) {
		r.UpdatedAt = at
	}
	if decision == "reject" && r.Side == "seller" {
		return r, nil, nil
	}
	event := relationExchange(r, phase, actor)
	return r, &event, nil
}
func ApplyRelationshipExchange(current *Relationship, e RelationshipExchange, destination uuid.UUID, at time.Time) (Relationship, *RelationshipExchange, error) {
	empty := Relationship{}
	if !slices.Contains([]string{"offered", "accepted", "activated", "rejected", "suspended"}, e.Phase) || at.IsZero() || !recordID(e.RelationshipID) || e.SourceEpoch < 1 || e.SourceEpoch > 9007199254740991 || !Text(e.SignatorySubject, 200) || ValidateTerms(e.Terms, e.Terms.ValidFrom) != nil {
		return empty, nil, ErrInvalid
	}
	digest, err := TermsDigest(e.Terms)
	if err != nil || digest != e.TermsSha256 {
		return empty, nil, ErrInvalid
	}
	side := "seller"
	source := e.Terms.Buyer.TenantID
	if destination == e.Terms.Buyer.TenantID {
		side = "buyer"
		source = e.Terms.Seller.TenantID
	} else if destination != e.Terms.Seller.TenantID {
		return empty, nil, ErrInvalid
	}
	if source != e.SourceTenantID || source == destination {
		return empty, nil, ErrInvalid
	}
	var r Relationship
	if current == nil {
		if side != "buyer" || (e.Phase != "offered" && e.Phase != "rejected" && e.Phase != "suspended") {
			return empty, nil, ErrInvalid
		}
		stamp := at.UTC().Truncate(time.Microsecond)
		r = Relationship{OwnerTenantID: destination, ID: e.RelationshipID, Side: side, Terms: normalizeTerms(e.Terms), TermsSha256: digest, Status: "incoming", Version: 1, AuthorityEpoch: 1, PreparedBy: "system:supplier-relationship", CreatedAt: stamp, UpdatedAt: stamp}
	} else {
		r = *current
		if r.OwnerTenantID != destination || r.ID != e.RelationshipID || r.Side != side || r.TermsSha256 != digest {
			return empty, nil, ErrInvalid
		}
		if e.SourceEpoch < r.RemoteEpoch {
			return r, nil, nil
		}
		if e.SourceEpoch == r.RemoteEpoch {
			// Activation acknowledges the seller's existing decision epoch.
			if side != "buyer" || e.Phase != "activated" || r.Status != "accepted_pending_ack" {
				return r, nil, nil
			}
			if r.RemoteSignedBy == nil || *r.RemoteSignedBy != e.SignatorySubject {
				return empty, nil, ErrInvalid
			}
		}
		if r.Version >= 9007199254740991 {
			return r, nil, ErrConflict
		}
		r.Version++
	}
	// A terminal local decision is never undone by a later remote approval.
	terminal := r.Status == "rejected" || r.Status == "suspended"
	r.RemoteEpoch = e.SourceEpoch
	r.RemoteSignedBy = &e.SignatorySubject
	var reply *RelationshipExchange
	if !terminal {
		switch e.Phase {
		case "offered":
			if side != "buyer" || r.Status != "incoming" {
				return empty, nil, ErrInvalid
			}
		case "accepted":
			if side != "seller" || r.Status != "offered" || r.SignedBy == nil || *r.SignedBy == e.SignatorySubject {
				return empty, nil, ErrInvalid
			}
			r.Status = "active"
			ack := relationExchange(r, "activated", *r.SignedBy)
			reply = &ack
		case "activated":
			if side != "buyer" || r.Status != "accepted_pending_ack" || r.SignedBy == nil || *r.SignedBy == e.SignatorySubject {
				return empty, nil, ErrInvalid
			}
			r.Status = "active"
		case "rejected":
			r.Status = "rejected"
		case "suspended":
			r.Status = "suspended"
		default:
			return empty, nil, ErrInvalid
		}
	}
	stamp := at.UTC().Truncate(time.Microsecond)
	if stamp.After(r.UpdatedAt) {
		r.UpdatedAt = stamp
	}
	return r, reply, nil
}
