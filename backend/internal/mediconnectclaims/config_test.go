package mediconnectclaims_test

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/mediconnectclaims"
	"testing"
)

func TestOwnerConfigurationRequiresExplicitCompleteTransport(t *testing.T) {
	cases := []struct {
		name             string
		values           map[string]string
		enabled, invalid bool
	}{
		{name: "disabled"},
		{name: "partial token", values: map[string]string{"MEDICONNECT_CLAIM_TOKEN_FILE": "/var/run/secrets/claim/token"}, invalid: true},
		{name: "unsafe cluster", values: map[string]string{"MEDICONNECT_CLAIM_ORIGIN": "http://api.helivanta.svc.cluster.local", "MEDICONNECT_CLAIM_TOKEN_FILE": "/var/run/secrets/claim/token"}, invalid: true},
		{name: "explicit cluster mtls", values: map[string]string{"MEDICONNECT_CLAIM_ORIGIN": "http://api.helivanta.svc.cluster.local", "MEDICONNECT_CLAIM_TOKEN_FILE": "/var/run/secrets/claim/token", "MEDICONNECT_CLAIM_TRANSPORT": "istio-mtls"}, enabled: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, err := mediconnectclaims.FromEnv(func(key string) string { return tt.values[key] })
			if tt.invalid {
				require.Error(t, err)
				require.Nil(t, client)
				return
			}
			require.NoError(t, err)
			if tt.enabled {
				require.NotNil(t, client)
			} else {
				require.Nil(t, client)
			}
		})
	}
}
