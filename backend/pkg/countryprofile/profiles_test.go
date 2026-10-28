package countryprofile_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"regexp"
	"testing"
)

func TestSupportedCountriesNeverFallBackToAnotherCountry(t *testing.T) {
	for _, tc := range []struct{ code, currency, zone string }{{"IN", "INR", "Asia/Kolkata"}} {
		p, ok := countryprofile.Lookup(tc.code)
		require.True(t, ok)
		require.Equal(t, tc.currency, p.Currency)
		require.Equal(t, tc.zone, p.Timezone)
		require.True(t, countryprofile.ValidTimezone(tc.code, tc.zone))
	}
	// A country without a profile is an unknown code like any other.
	for _, code := range []string{"ZZ", "US"} {
		p, ok := countryprofile.Lookup(code)
		require.False(t, ok, code)
		require.Empty(t, p.Code)
	}
	require.False(t, countryprofile.ValidTimezone("ZZ", "UTC"))
	require.False(t, countryprofile.ValidTimezone("US", "America/New_York"))
	require.False(t, countryprofile.ValidTimezone("IN", "America/New_York"))
	require.True(t, countryprofile.ValidTimezone("IN", "Asia/Calcutta"))
}
func TestCountryMetadataCannotBeMutatedByCallerAndDoesNotAuthorizeTrading(t *testing.T) {
	first := countryprofile.Configuration()
	first[0] = '!'
	second := countryprofile.Configuration()
	require.Equal(t, byte('{'), second[0])
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		Countries     []struct {
			Code   string
			Status string `json:"regulatory_policy_status"`
		}
	}
	require.NoError(t, json.Unmarshal(second, &doc))
	require.Equal(t, 1, doc.SchemaVersion)
	require.Len(t, doc.Countries, 1)
	for _, p := range doc.Countries {
		require.Equal(t, "review_required", p.Status)
	}
}

func TestTaxIdentifierKindsDoNotCrossCountryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		country, kind string
		valid         bool
	}{
		{"IN", "gstin", true}, {"IN", "pan", true}, {"IN", "ein", false}, {"IN", "vat", false}, {"US", "ein", false}, {"US", "other", false}, {"IN", "other", true}, {"ZZ", "other", false}, {"IN", "unknown", false},
	} {
		require.Equal(t, tc.valid, countryprofile.ValidTaxIdentifierKind(tc.country, tc.kind), tc.country+":"+tc.kind)
	}
}

func TestIndiaTaxSchemeSplitsStatesFromUnionTerritories(t *testing.T) {
	s, ok := countryprofile.Tax("IN")
	require.True(t, ok)
	require.Equal(t, "gst", s.Scheme)
	require.Equal(t, "gstin", s.RegistrationKind)
	require.Equal(t, "HSN", s.ClassificationName)
	require.True(t, countryprofile.ValidTaxIdentifierKind("IN", s.RegistrationKind))
	// Delhi, Puducherry and Jammu & Kashmir have legislatures and levy SGST;
	// the six in UTGST Act s.2(8) levy UTGST.
	for code, kind := range map[string]string{
		"01": "state", "07": "state", "27": "state", "34": "state", "37": "state",
		"04": "union_territory", "26": "union_territory", "31": "union_territory",
		"35": "union_territory", "38": "union_territory", "97": "union_territory",
	} {
		j, ok := s.Jurisdiction(code)
		require.True(t, ok, code)
		require.Equal(t, kind, j.Kind, code)
	}
	// Retired (25 merged into 26, 28 replaced by 37) or not an outlet location.
	for _, code := range []string{"25", "28", "96", "99", "00", "7"} {
		_, ok := s.Jurisdiction(code)
		require.False(t, ok, code)
	}
	seen := map[string]bool{}
	for _, j := range s.Jurisdictions {
		require.False(t, seen[j.Code], j.Code)
		seen[j.Code] = true
	}
	require.Len(t, s.Jurisdictions, 37)
	hsn := regexp.MustCompile(s.ClassificationPattern)
	for _, v := range []string{"3004", "300490", "30049099"} {
		require.True(t, hsn.MatchString(v), v)
	}
	for _, v := range []string{"300", "30049", "3004909", "300490991", "30a4", ""} {
		require.False(t, hsn.MatchString(v), v)
	}
	require.NotEmpty(t, s.Sources)
}

func TestCountryWithoutTaxSchemeRefusesTaxSetup(t *testing.T) {
	_, ok := countryprofile.Tax("US")
	require.False(t, ok)
	_, ok = countryprofile.Tax("ZZ")
	require.False(t, ok)
}

// An amount is in a supported country's currency, and no other.
func TestSupportedCurrencyIsAProfileCurrency(t *testing.T) {
	require.True(t, countryprofile.SupportedCurrency("INR"))
	for _, code := range []string{"USD", "EUR", "inr", ""} {
		require.False(t, countryprofile.SupportedCurrency(code), code)
	}
}
