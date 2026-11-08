// Package suppliertrade defines the canonical bilateral supplier exchange contract.
package suppliertrade

import (
	"errors"
	"github.com/google/uuid"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Terms = SupplierTradeTerms
type Endpoint = SupplierTradeEndpoint

var ErrInvalid = errors.New("invalid supplier trade intent")

func recordID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func endpointValid(e Endpoint) bool {
	if e.HospitalID != nil || e.BindingID != nil || e.BindingVersion != nil {
		if e.Product != "pharmacy" || e.HospitalID == nil || *e.HospitalID == uuid.Nil || e.BindingID == nil || !recordID(*e.BindingID) || e.BindingVersion == nil || *e.BindingVersion < 1 || *e.BindingVersion > 9007199254740991 {
			return false
		}
	}

	if e.Product == "pharmacy" && (e.RegistrationID == nil || !recordID(*e.RegistrationID) || e.RegistrationVersion == nil || *e.RegistrationVersion < 1 || *e.RegistrationVersion > 9007199254740991) {
		return false
	}
	if e.Product == "supply" && (e.RegistrationID != nil || e.RegistrationVersion != nil) {
		return false
	}
	return e.TenantID != uuid.Nil && (e.Product == "supply" || e.Product == "pharmacy") && recordID(e.OrganizationID) && recordID(e.LocationID) && recordID(e.LicenceID) && e.OrganizationVersion > 0 && e.OrganizationVersion <= 9007199254740991 && e.LocationVersion > 0 && e.LocationVersion <= 9007199254740991 && e.LicenceVersion > 0 && e.LicenceVersion <= 9007199254740991 && (e.Jurisdiction == "IN-MH" || e.Jurisdiction == "IN-OD")
}
func Text(value string, limit int) bool {
	return utf8.ValidString(value) && value != "" && strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

// ValidateTerms validates submitted intent; it never establishes source eligibility or consent.
func ValidateTerms(t Terms, now time.Time) error {
	if now.IsZero() || !endpointValid(t.Seller) || !endpointValid(t.Buyer) || t.Seller.TenantID == t.Buyer.TenantID || t.Buyer.Product != "pharmacy" || t.Buyer.HospitalID == nil || *t.Buyer.HospitalID == uuid.Nil || t.Buyer.BindingID == nil || !recordID(*t.Buyer.BindingID) || t.Buyer.BindingVersion == nil || *t.Buyer.BindingVersion < 1 || *t.Buyer.BindingVersion > 9007199254740991 || !slices.Contains(categories, t.Category) || t.Currency != "INR" || !Text(t.TermsReference, 200) || t.ValidFrom.IsZero() || !t.ExpiresAt.After(t.ValidFrom) || !t.ExpiresAt.After(now) || t.ExpiresAt.Sub(t.ValidFrom) > maximumValidityDays*24*time.Hour {
		return ErrInvalid
	}
	return nil
}

// ValidEndpointUse limits the source adapter to bulk supplier operations.
func ValidEndpointUse(e Endpoint, category, operation string) bool {
	return endpointValid(e) && slices.Contains(categories, category) && (operation == "listing" || operation == "order" || operation == "dispatch")
}

func ValidCategory(category string) bool { return slices.Contains(categories, category) }

func EndpointEqual(a, b Endpoint) bool { return reflect.DeepEqual(a, b) }
