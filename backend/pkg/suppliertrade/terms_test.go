package suppliertrade

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func testTerms() Terms {
	return Terms{Seller: Endpoint{TenantID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Product: "supply", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111112"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111113"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111114"), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: Endpoint{TenantID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Product: "pharmacy", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111115"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111116"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111117"), LicenceVersion: 1, Jurisdiction: "IN-OD", RegistrationID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111119")), RegistrationVersion: ptr(int64(2)), HospitalID: ptr(uuid.MustParse("33333333-3333-4333-8333-333333333333")), BindingID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111118")), BindingVersion: ptr(int64(2))}, Category: "prescription", Currency: "INR", ValidFrom: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC), TermsReference: "TEST commercial terms revision 1"}
}
func ptr[T any](v T) *T { return &v }
func TestTermsRequireAnIndependentSellerAndAnExplicitHospitalBuyer(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for _, product := range []string{"supply", "pharmacy"} {
		in := testTerms()
		in.Seller.Product = product
		if product == "pharmacy" {
			in.Seller.RegistrationID = ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111120"))
			in.Seller.RegistrationVersion = ptr(int64(2))
		}
		require.NoError(t, ValidateTerms(in, now))
	}
	for name, change := range map[string]func(*Terms){
		"self trading":                             func(v *Terms) { v.Buyer.TenantID = v.Seller.TenantID },
		"missing hospital":                         func(v *Terms) { v.Buyer.HospitalID = nil },
		"missing exact binding":                    func(v *Terms) { v.Buyer.BindingID = nil },
		"missing binding revision":                 func(v *Terms) { v.Buyer.BindingVersion = nil },
		"supplier impersonates buyer":              func(v *Terms) { v.Buyer.Product = "supply" },
		"hospital impersonates independent seller": func(v *Terms) { v.Seller.HospitalID = v.Buyer.HospitalID },
		"stale source placeholder":                 func(v *Terms) { v.Seller.LocationVersion = 0 },
		"unselected category":                      func(v *Terms) { v.Category = "all" },
		"unsupported rollout state":                func(v *Terms) { v.Seller.Jurisdiction = "IN-DL" },
		"unbounded validity":                       func(v *Terms) { v.ExpiresAt = v.ValidFrom.Add(367 * 24 * time.Hour) },
		"expired intent":                           func(v *Terms) { v.ExpiresAt = now },
		"wrong currency":                           func(v *Terms) { v.Currency = "USD" },
		"control characters":                       func(v *Terms) { v.TermsReference = "TEST\nprivate" },
	} {
		t.Run(name, func(t *testing.T) {
			v := testTerms()
			change(&v)
			require.ErrorIs(t, ValidateTerms(v, now), ErrInvalid)
		})
	}
}
