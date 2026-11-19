package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/config"
)

type ownerFixtureTransport func(*http.Request) (*http.Response, error)

func (f ownerFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeOwnerFixtureTransportRequiresAllIsolatedGuards(t *testing.T) {
	for _, tc := range []struct {
		env, issuer, identity, mfa string
		ok                         bool
	}{
		{"dev", "http://auth.tesserix.localhost:20080", "true", "true", true},
		{"prod", "http://auth.tesserix.localhost:20080", "true", "true", false},
		{"dev", "https://auth.tesserix.app", "true", "true", false},
		{"dev", "http://auth.tesserix.localhost:20080", "", "true", false},
		{"dev", "http://auth.tesserix.localhost:20080", "true", "yes", false},
	} {
		_, err := organizationIdentityHTTP(config.Config{Env: tc.env, ZitadelIssuerURL: tc.issuer}, tc.identity, tc.mfa)
		require.Equal(t, tc.ok, err == nil)
	}
}
func TestNativeOwnerFixtureTransportPinsDestinationAndPreservesTheRealMachineToken(t *testing.T) {
	called := 0
	transport := nativeOwnerFixtureTransport{next: ownerFixtureTransport(func(r *http.Request) (*http.Response, error) {
		called++
		require.Equal(t, "http://127.0.0.1:18787/v1/orgs/tenant/staff-mfa-recoveries/command?actor_subject=actor", r.URL.String())
		require.Equal(t, "Bearer synthetic-native-jwt", r.Header.Get("Authorization"))
		require.Equal(t, "helivanta", r.Header.Get("X-Expected-Product-Key"))
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	for _, origin := range []string{"http://onboarding-api.onboarding.svc.cluster.local:8080", "https://onboarding.identity-fixture.invalid"} {
		request, err := http.NewRequestWithContext(context.Background(), "GET", origin+"/v1/orgs/tenant/staff-mfa-recoveries/command?actor_subject=actor", nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer synthetic-native-jwt")
		request.Header.Set("X-Expected-Product-Key", "helivanta")
		_, err = transport.RoundTrip(request)
		require.NoError(t, err)
		require.Equal(t, origin+"/v1/orgs/tenant/staff-mfa-recoveries/command?actor_subject=actor", request.URL.String(), "caller request must not be rewritten")
	}
	require.Equal(t, 2, called)
	for _, destination := range []string{
		"https://onboard.tesserix.app/v1/orgs/tenant",
		"http://127.0.0.1:18787/v1/orgs/tenant",
		"http://other.svc:8080/v1/orgs/tenant",
		"https://onboarding-api.onboarding.svc.cluster.local:8080/v1/orgs/tenant",
		"https://auth.tesserix.app/v1/orgs/tenant",
		"http://onboarding.identity-fixture.invalid/v1/orgs/tenant",
		"https://onboarding.identity-fixture.invalid:443/v1/orgs/tenant",
		"https://onboarding.identity-fixture.invalid.evil/v1/orgs/tenant",
		"https://user@onboarding.identity-fixture.invalid/v1/orgs/tenant",
		"https://onboarding.identity-fixture.invalid/v1/%2forgs/tenant",
		"https://onboarding.identity-fixture.invalid/v1/orgs/tenant#fragment",
	} {
		bad, err := http.NewRequestWithContext(context.Background(), "GET", destination, nil)
		require.NoError(t, err)
		_, err = transport.RoundTrip(bad)
		require.Error(t, err)
		require.Equal(t, 2, called)
	}
	_, err := transport.RoundTrip(nil)
	require.Error(t, err)
	require.Equal(t, 2, called)
}
