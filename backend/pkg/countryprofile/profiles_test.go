package countryprofile_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"testing"
)

func TestSupportedCountriesNeverFallBackToAnotherCountry(t *testing.T) {
	for _, tc := range []struct{ code, currency, zone string }{{"IN", "INR", "Asia/Kolkata"}, {"PK", "PKR", "Asia/Karachi"}} {
		p, ok := countryprofile.Lookup(tc.code)
		require.True(t, ok)
		require.Equal(t, tc.currency, p.Currency)
		require.Equal(t, tc.zone, p.Timezone)
		require.True(t, countryprofile.ValidTimezone(tc.code, tc.zone))
	}
	p, ok := countryprofile.Lookup("ZZ")
	require.False(t, ok)
	require.Empty(t, p.Code)
	require.False(t, countryprofile.ValidTimezone("ZZ", "UTC"))
	require.False(t, countryprofile.ValidTimezone("PK", "Asia/Kolkata"))
	require.False(t, countryprofile.ValidTimezone("IN", "Asia/Karachi"))
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
	require.Len(t, doc.Countries, 2)
	for _, p := range doc.Countries {
		require.Equal(t, "review_required", p.Status)
	}
}
