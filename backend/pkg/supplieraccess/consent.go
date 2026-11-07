package supplieraccess

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

var ErrInvalid = errors.New("invalid supplier grant")
var ErrConflict = errors.New("supplier grant state changed")
var ErrIneligible = errors.New("supplier grant is not currently eligible")
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const maximumVersion = int64(9007199254740991)

type Template struct {
	Permission    string
	GroupPurposes []string
}

func TemplateFor(key string) (Template, bool) {
	value, ok := templates[key]
	value.GroupPurposes = slices.Clone(value.GroupPurposes)
	return value, ok
}
func v7(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func subject(value string) bool {
	return suppliertrade.Text(value, 200) && !strings.ContainsAny(value, "*#:") && strings.IndexFunc(value, unicode.IsSpace) == -1
}
func normalized(input SupplierGrantInput) SupplierGrantInput {
	input.ValidFrom = input.ValidFrom.UTC().Truncate(time.Microsecond)
	input.ExpiresAt = input.ExpiresAt.UTC().Truncate(time.Microsecond)
	if input.RecipientSubject != nil {
		value := *input.RecipientSubject
		input.RecipientSubject = &value
	}
	if input.RecipientGroupID != nil {
		value := *input.RecipientGroupID
		input.RecipientGroupID = &value
	}
	return input
}
func ValidInput(input SupplierGrantInput) bool {
	_, ok := TemplateFor(input.TemplateKey)
	if !ok || !v7(input.RelationshipID) || !digestPattern.MatchString(input.TermsSha256) || !suppliertrade.Text(input.Reason, 500) || input.ValidFrom.IsZero() || !input.ExpiresAt.After(input.ValidFrom) || input.ExpiresAt.Sub(input.ValidFrom) > maximumValidityDays*24*time.Hour {
		return false
	}
	switch input.RecipientKind {
	case "subject":
		return input.RecipientSubject != nil && subject(*input.RecipientSubject) && input.RecipientGroupID == nil
	case "group":
		return input.RecipientSubject == nil && input.RecipientGroupID != nil && v7(*input.RecipientGroupID)
	default:
		return false
	}
}
func validScope(scope SupplierGrantScope) bool {
	return scope.SourceTenantID != uuid.Nil && scope.RecipientTenantID != uuid.Nil && scope.SourceTenantID != scope.RecipientTenantID && scope.HospitalID != uuid.Nil && (scope.SourceProduct == "supply" || scope.SourceProduct == "pharmacy") && v7(scope.SourceOrganizationID) && v7(scope.SourceLocationID)
}
func InputHash(scope SupplierGrantScope, input SupplierGrantInput) (string, error) {
	if !validScope(scope) || !ValidInput(input) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Scope SupplierGrantScope `json:"scope"`
		Input SupplierGrantInput `json:"input"`
	}{scope, normalized(input)})
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func scopeFor(relationship suppliertrade.Relationship) (SupplierGrantScope, error) {
	terms := relationship.Terms
	if terms.Buyer.HospitalID == nil {
		return SupplierGrantScope{}, ErrInvalid
	}
	return SupplierGrantScope{SourceTenantID: terms.Seller.TenantID, RecipientTenantID: terms.Buyer.TenantID, HospitalID: *terms.Buyer.HospitalID, SourceProduct: terms.Seller.Product, SourceOrganizationID: terms.Seller.OrganizationID, SourceLocationID: terms.Seller.LocationID}, nil
}
func Prepare(id uuid.UUID, relationship suppliertrade.Relationship, input SupplierGrantInput, actor string, at time.Time) (SupplierGrant, error) {
	input = normalized(input)
	if !v7(id) || !subject(actor) || at.IsZero() || !ValidInput(input) {
		return SupplierGrant{}, ErrInvalid
	}
	if relationship.Side != "seller" || relationship.OwnerTenantID != relationship.Terms.Seller.TenantID || !suppliertrade.RelationshipCurrent(relationship, at) || suppliertrade.ValidateTerms(relationship.Terms, at) != nil {
		return SupplierGrant{}, ErrIneligible
	}
	if input.RelationshipID != relationship.ID || input.TermsSha256 != relationship.TermsSha256 || input.ValidFrom.Before(relationship.Terms.ValidFrom) || input.ExpiresAt.After(relationship.Terms.ExpiresAt) || !input.ExpiresAt.After(at) {
		return SupplierGrant{}, ErrInvalid
	}
	scope, err := scopeFor(relationship)
	if err != nil {
		return SupplierGrant{}, err
	}
	digest, err := InputHash(scope, input)
	if err != nil {
		return SupplierGrant{}, err
	}
	stamp := at.UTC().Truncate(time.Microsecond)
	return SupplierGrant{ID: id, OwnerTenantID: scope.SourceTenantID, Scope: scope, Input: input, InputSha256: digest, Side: "source", Status: "pending_review", Version: 1, AuthorityEpoch: 1, PreparedBy: actor, CreatedAt: stamp, UpdatedAt: stamp}, nil
}
func validState(r SupplierGrant) bool {
	hash, err := InputHash(r.Scope, r.Input)
	return err == nil && hash == r.InputSha256 && v7(r.ID) && r.Version >= 1 && r.Version <= maximumVersion && r.AuthorityEpoch >= 1 && r.AuthorityEpoch <= maximumVersion && r.RemoteEpoch >= 0 && r.RemoteEpoch <= maximumVersion && !r.CreatedAt.IsZero() && !r.UpdatedAt.Before(r.CreatedAt) && ((r.Side == "source" && r.OwnerTenantID == r.Scope.SourceTenantID) || (r.Side == "hospital" && r.OwnerTenantID == r.Scope.RecipientTenantID))
}
func exchange(r SupplierGrant, phase, actor string) *SupplierGrantExchange {
	return &SupplierGrantExchange{GrantID: r.ID, Scope: r.Scope, Input: normalized(r.Input), InputSha256: r.InputSha256, SenderTenantID: r.OwnerTenantID, SenderEpoch: r.AuthorityEpoch, Phase: phase, SignatorySubject: actor, HomeBasisSha256: r.HomeBasisSha256}
}
func Decide(current SupplierGrant, expected int64, operation, actor, homeBasis string, at time.Time) (SupplierGrant, *SupplierGrantExchange, error) {
	if !validState(current) || !subject(actor) || at.IsZero() {
		return SupplierGrant{}, nil, ErrInvalid
	}
	if expected != current.Version || current.Version >= maximumVersion || current.AuthorityEpoch >= maximumVersion {
		return SupplierGrant{}, nil, ErrConflict
	}
	r := current
	r.Input = normalized(r.Input)
	phase := ""
	switch operation {
	case "approve":
		if !r.Input.ExpiresAt.After(at) || actor == r.PreparedBy || (r.Input.RecipientSubject != nil && *r.Input.RecipientSubject == actor) {
			return SupplierGrant{}, nil, ErrConflict
		}
		if r.Side == "source" && r.Status == "pending_review" {
			if homeBasis != "" {
				return SupplierGrant{}, nil, ErrInvalid
			}
			r.Status = "offered"
			phase = "offered"
		} else if r.Side == "hospital" && r.Status == "incoming" {
			if !digestPattern.MatchString(homeBasis) {
				return SupplierGrant{}, nil, ErrInvalid
			}
			r.HomeBasisSha256 = &homeBasis
			r.Status = "accepted_pending_ack"
			phase = "accepted"
		} else {
			return SupplierGrant{}, nil, ErrConflict
		}
		r.SignedBy = &actor
	case "reject":
		if r.Status != "pending_review" && r.Status != "incoming" {
			return SupplierGrant{}, nil, ErrConflict
		}
		if actor == r.PreparedBy {
			return SupplierGrant{}, nil, ErrConflict
		}
		r.Status = "rejected"
		if r.Side == "hospital" {
			phase = "rejected"
		}
	case "suspend":
		if !slices.Contains([]string{"offered", "accepted_pending_ack", "approved"}, r.Status) {
			return SupplierGrant{}, nil, ErrConflict
		}
		r.Status = "suspended"
		phase = "suspended"
	default:
		return SupplierGrant{}, nil, ErrInvalid
	}
	r.Version++
	r.AuthorityEpoch++
	stamp := at.UTC().Truncate(time.Microsecond)
	if stamp.After(r.UpdatedAt) {
		r.UpdatedAt = stamp
	}
	if phase == "" {
		return r, nil, nil
	}
	return r, exchange(r, phase, actor), nil
}
func Apply(current *SupplierGrant, e SupplierGrantExchange, destination uuid.UUID, at time.Time) (SupplierGrant, *SupplierGrantExchange, error) {
	empty := SupplierGrant{}
	hash, err := InputHash(e.Scope, e.Input)
	if err != nil || hash != e.InputSha256 || !v7(e.GrantID) || !subject(e.SignatorySubject) || e.SenderEpoch < 1 || e.SenderEpoch > maximumVersion || at.IsZero() || !slices.Contains([]string{"offered", "accepted", "acknowledged", "rejected", "suspended"}, e.Phase) {
		return empty, nil, ErrInvalid
	}
	side := "source"
	sender := e.Scope.RecipientTenantID
	if destination == e.Scope.RecipientTenantID {
		side = "hospital"
		sender = e.Scope.SourceTenantID
	} else if destination != e.Scope.SourceTenantID {
		return empty, nil, ErrInvalid
	}
	if sender != e.SenderTenantID {
		return empty, nil, ErrInvalid
	}
	var r SupplierGrant
	if current == nil {
		if side != "hospital" || !slices.Contains([]string{"offered", "rejected", "suspended"}, e.Phase) {
			return empty, nil, ErrInvalid
		}
		stamp := at.UTC().Truncate(time.Microsecond)
		r = SupplierGrant{ID: e.GrantID, OwnerTenantID: destination, Scope: e.Scope, Input: normalized(e.Input), InputSha256: hash, Side: side, Status: "incoming", Version: 1, AuthorityEpoch: 1, PreparedBy: "system:iam-supplier-grants", CreatedAt: stamp, UpdatedAt: stamp}
	} else {
		r = *current
		r.Input = normalized(r.Input)
		if !validState(r) || r.ID != e.GrantID || r.OwnerTenantID != destination || r.Side != side || r.InputSha256 != hash || r.Scope != e.Scope {
			return empty, nil, ErrInvalid
		}
		if e.SenderEpoch < r.RemoteEpoch {
			return r, nil, nil
		}
		if e.SenderEpoch == r.RemoteEpoch {
			if side != "hospital" || e.Phase != "acknowledged" || r.Status != "accepted_pending_ack" {
				return r, nil, nil
			}
			if r.RemoteSignedBy == nil || *r.RemoteSignedBy != e.SignatorySubject {
				return empty, nil, ErrInvalid
			}
		}
		if r.Version >= maximumVersion {
			return empty, nil, ErrConflict
		}
		r.Version++
	}
	terminal := r.Status == "rejected" || r.Status == "suspended"
	var reply *SupplierGrantExchange
	if !terminal {
		switch e.Phase {
		case "offered":
			if side != "hospital" || r.Status != "incoming" || e.HomeBasisSha256 != nil {
				return empty, nil, ErrInvalid
			}
		case "accepted":
			if side != "source" || r.Status != "offered" || r.SignedBy == nil || *r.SignedBy == e.SignatorySubject || e.HomeBasisSha256 == nil || !digestPattern.MatchString(*e.HomeBasisSha256) {
				return empty, nil, ErrInvalid
			}
			basis := *e.HomeBasisSha256
			r.HomeBasisSha256 = &basis
			r.Status = "approved"
			reply = exchange(r, "acknowledged", *r.SignedBy)
		case "acknowledged":
			if side != "hospital" || r.Status != "accepted_pending_ack" || r.SignedBy == nil || *r.SignedBy == e.SignatorySubject || r.HomeBasisSha256 == nil || e.HomeBasisSha256 == nil || *r.HomeBasisSha256 != *e.HomeBasisSha256 {
				return empty, nil, ErrInvalid
			}
			r.Status = "approved"
		case "rejected":
			r.Status = "rejected"
		case "suspended":
			r.Status = "suspended"
		}
	}
	r.RemoteEpoch = e.SenderEpoch
	signatory := e.SignatorySubject
	r.RemoteSignedBy = &signatory
	stamp := at.UTC().Truncate(time.Microsecond)
	if stamp.After(r.UpdatedAt) {
		r.UpdatedAt = stamp
	}
	return r, reply, nil
}
func Current(grant SupplierGrant, relationship suppliertrade.Relationship, at time.Time) bool {
	scope, err := scopeFor(relationship)
	return err == nil && validState(grant) && suppliertrade.RelationshipCurrent(relationship, at) && grant.Scope == scope && grant.Input.RelationshipID == relationship.ID && grant.Input.TermsSha256 == relationship.TermsSha256 && grant.Status == "approved" && grant.SignedBy != nil && subject(*grant.SignedBy) && grant.RemoteSignedBy != nil && subject(*grant.RemoteSignedBy) && *grant.SignedBy != *grant.RemoteSignedBy && grant.HomeBasisSha256 != nil && digestPattern.MatchString(*grant.HomeBasisSha256) && !at.Before(grant.Input.ValidFrom) && at.Before(grant.Input.ExpiresAt)
}
