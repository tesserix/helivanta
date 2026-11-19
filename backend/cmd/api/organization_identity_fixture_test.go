package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestNativeOwnerFixtureTransportOnlyForwardsTheExactRealOAuthExchange(t *testing.T) {
	called := 0
	transport := nativeOwnerFixtureTransport{next: ownerFixtureTransport(func(r *http.Request) (*http.Response, error) {
		called++
		require.Equal(t, "http://auth.tesserix.localhost:20080/oauth/v2/token", r.URL.String())
		client, secret, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "synthetic-client", client)
		require.Equal(t, "synthetic-secret", secret)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"POST", "/oauth/v2/token", true}, {"GET", "/oauth/v2/token", false},
		{"POST", "/oauth/v2/token?destination=foreign", false}, {"POST", "/v2/users", false},
	} {
		request, err := http.NewRequestWithContext(context.Background(), tc.method, "https://auth.identity-fixture.invalid"+tc.path, nil)
		require.NoError(t, err)
		request.SetBasicAuth("synthetic-client", "synthetic-secret")
		_, err = transport.RoundTrip(request)
		require.Equal(t, tc.allowed, err == nil)
	}
	require.Equal(t, 1, called)
}

func TestOrganisationIdentityNativeFixtureInitializesWithPreferredOAuthLogin(t *testing.T) {
	t.Setenv("HELIVANTA_IDENTITY_FIXTURE", "true")
	t.Setenv("HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE", "true")
	file := filepath.Join(t.TempDir(), "product-credential")
	require.NoError(t, os.WriteFile(file, []byte("synthetic-client-secret"), 0600))
	clients, err := newOrganizationIdentity(config.Config{
		Env: "dev", ZitadelIssuerURL: "http://auth.tesserix.localhost:20080",
		OrganizationIdentityURL:        "https://onboarding.identity-fixture.invalid",
		OrganizationIdentityProductKey: "helivanta", OrganizationIdentityTokenProjectID: "native-project",
		OrganizationIdentityClientID: "native-machine@platform.localhost", OrganizationIdentityClientSecretFile: file,
	})
	require.NoError(t, err)
	require.NotNil(t, clients.proof)
	require.NotNil(t, clients.staff)
}
