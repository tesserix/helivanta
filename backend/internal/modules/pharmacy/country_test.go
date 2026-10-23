package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCountryConfigurationIsIndiaOnly(t *testing.T) {
	r, _ := catalogueHarness(t)
	w := do(r, "GET", "/v1/pharmacy/configuration", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var config struct {
		Version   int `json:"schema_version"`
		Countries []struct {
			Code             string `json:"code"`
			Currency         string `json:"currency"`
			Timezone         string `json:"timezone"`
			RegulatoryStatus string `json:"regulatory_policy_status"`
			Locales          []struct {
				Code      string `json:"code"`
				Direction string `json:"direction"`
			}
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &config))
	require.Equal(t, 1, config.Version)
	// #1287: India is the only supported country.
	require.Len(t, config.Countries, 1)
	require.Equal(t, "IN", config.Countries[0].Code)
	require.Equal(t, "INR", config.Countries[0].Currency)
	require.Equal(t, "Asia/Kolkata", config.Countries[0].Timezone)
	require.Equal(t, "review_required", config.Countries[0].RegulatoryStatus)
	require.NotContains(t, w.Body.String(), `"PK"`)
	require.NotContains(t, w.Body.String(), "gst_rate")
	require.NotContains(t, w.Body.String(), "licence_approved")
	require.Equal(t, 403, do(r, "GET", "/v1/pharmacy/configuration", "denied", "").Code)
}
func TestOutletCountryRejectsUnsupportedAndMismatchedTimezone(t *testing.T) {
	r, _ := catalogueHarness(t)
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/"
	body := func(code, zone string) string {
		return fmt.Sprintf(`{"code":"TEST-COUNTRY-%s","name":"TEST Country Outlet","country_code":%q,"timezone":%q}`, uuid.Must(uuid.NewV7()).String()[24:], code, zone)
	}
	w := do(r, "PUT", root+uuid.Must(uuid.NewV7()).String(), "editor", body("IN", "Asia/Kolkata"))
	require.Equal(t, 200, w.Code, w.Body.String())
	// #1287: Pakistan is refused like any unsupported country.
	for _, invalid := range [][2]string{{"ZZ", "UTC"}, {"PK", "Asia/Karachi"}, {"PK", "Asia/Kolkata"}, {"IN", "Asia/Karachi"}, {"in", "Asia/Kolkata"}} {
		w := do(r, "PUT", root+uuid.Must(uuid.NewV7()).String(), "editor", body(invalid[0], invalid[1]))
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}
