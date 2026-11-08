// Package supplierorder defines immutable hospital purchase intents and bilateral fulfilment consent.
package supplierorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

var ErrInvalid = errors.New("invalid supplier order intent")
var ErrConflict = errors.New("supplier order state changed")
var ErrIneligible = errors.New("supplier order is not currently eligible")

const maximumVersion = int64(9007199254740991)

// Actor carries server-verified local Staff identity; it is never a command-body permission.
type Actor struct {
	Subject string
	StaffID uuid.UUID
}

func validActor(actor Actor) bool {
	return suppliertrade.Text(actor.Subject, 200) && actor.StaffID != uuid.Nil && actor.StaffID.Variant() == uuid.RFC4122
}

func frozenIntent(terms suppliertrade.Terms, input SupplierOrderInput) (suppliertrade.Terms, SupplierOrderInput, string, error) {
	terms.ValidFrom = terms.ValidFrom.UTC().Truncate(time.Microsecond)
	terms.ExpiresAt = terms.ExpiresAt.UTC().Truncate(time.Microsecond)
	input.ExpiresAt = input.ExpiresAt.UTC().Truncate(time.Microsecond)
	type intent struct {
		Terms suppliertrade.Terms
		Input SupplierOrderInput
	}
	raw, err := json.Marshal(intent{terms, input})
	if err != nil {
		return terms, input, "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	var frozen intent
	if json.Unmarshal(raw, &frozen) != nil {
		return terms, input, "", ErrInvalid
	}
	return frozen.Terms, frozen.Input, hex.EncodeToString(sum[:]), nil
}

// Prepare freezes a hospital draft against the selected bilateral relationship.
func Prepare(id uuid.UUID, r suppliertrade.Relationship, in SupplierOrderInput, actor Actor, at time.Time) (SupplierOrder, error) {
	if !recordID(id) || !validActor(actor) || at.IsZero() {
		return SupplierOrder{}, ErrInvalid
	}
	if r.Side != "buyer" || r.OwnerTenantID != r.Terms.Buyer.TenantID || !suppliertrade.RelationshipCurrent(r, at) {
		return SupplierOrder{}, ErrIneligible
	}
	if r.ID != in.RelationshipID || r.TermsSha256 != in.TermsSha256 || ValidateInput(r.Terms, in, at) != nil {
		return SupplierOrder{}, ErrInvalid
	}
	terms, input, hash, err := frozenIntent(r.Terms, in)
	if err != nil {
		return SupplierOrder{}, err
	}
	at = at.UTC().Truncate(time.Microsecond)
	return SupplierOrder{ID: id, OwnerTenantID: terms.Buyer.TenantID, Side: "buyer", Terms: terms, Input: input, InputSha256: hash, Status: "pending_review", Version: 1, AuthorityEpoch: 1, RemoteEpoch: 0, PreparedBy: actor.Subject, PreparedStaffID: &actor.StaffID, CreatedAt: at, UpdatedAt: at}, nil
}
