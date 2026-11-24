package release

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentFixtureIsolatesLowStockWithoutReleasingOtherWorkflows(t *testing.T) {
	service, err := DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true", "team_accounts", "pharmacy_setup", "pharmacy_low_stock")
	require.NoError(t, err)
	require.NoError(t, service.Refresh(t.Context()))
	snapshot, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-1111-7111-8111-111111111111"), "TEST-owner")
	require.NoError(t, err)
	for _, capability := range snapshot.Capabilities {
		want := capability.Key == "team_accounts" || capability.Key == "pharmacy_setup" || capability.Key == "pharmacy_low_stock"
		require.Equal(t, want, capability.Available, capability.Key)
	}
	for _, test := range []struct{ environment, issuer, key string }{
		{"prod", "http://auth.tesserix.localhost:20080", "pharmacy_low_stock"},
		{"dev", "https://auth.tesserix.app", "pharmacy_low_stock"},
		{"dev", "http://auth.tesserix.localhost:20080", "unknown"},
		{"dev", "http://auth.tesserix.localhost:20080", "baseline"},
	} {
		_, err := DevelopmentFixture(test.environment, test.issuer, "true", test.key)
		require.Error(t, err)
	}
}
