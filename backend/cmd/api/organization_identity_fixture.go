package main

import (
	"errors"
	"net/http"

	"github.com/tesserix/helivanta/internal/config"
)

func organizationIdentityHTTP(cfg config.Config, identityFixture, mfaFixture string) (*http.Client, error) {
	if mfaFixture == "" {
		return http.DefaultClient, nil
	}
	if cfg.Env != "dev" || cfg.ZitadelIssuerURL != "http://auth.tesserix.localhost:20080" || identityFixture != "true" || mfaFixture != "true" {
		return nil, errors.New("native Staff MFA fixture requires isolated development identity")
	}
	return &http.Client{Transport: nativeOwnerFixtureTransport{next: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

const nativeFixtureOAuthIssuer = "https://auth.identity-fixture.invalid"

// Only exact fixture destinations are redirected; native tokens and user proofs remain real.
type nativeOwnerFixtureTransport struct{ next http.RoundTripper }

func (t nativeOwnerFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {

	if request == nil || request.URL == nil || t.next == nil {
		return nil, errors.New("native fixture owner destination refused")
	}
	origin := request.URL.Scheme + "://" + request.URL.Host
	if request.URL.User != nil || request.URL.Fragment != "" || request.URL.RawPath != "" {
		return nil, errors.New("native fixture owner destination refused")
	}
	clone := request.Clone(request.Context())
	value := *request.URL
	switch origin {
	case "http://onboarding-api.onboarding.svc.cluster.local:8080", "https://onboarding.identity-fixture.invalid":
		value.Scheme = "http"
		value.Host = "127.0.0.1:18787"
	case nativeFixtureOAuthIssuer:
		if request.Method != http.MethodPost || value.Path != "/oauth/v2/token" || value.RawQuery != "" || value.ForceQuery {
			return nil, errors.New("native fixture OAuth exchange refused")
		}
		value.Scheme = "http"
		value.Host = "auth.tesserix.localhost:20080"
	default:
		return nil, errors.New("native fixture owner destination refused")
	}
	clone.URL = &value
	clone.Host = value.Host
	return t.next.RoundTrip(clone)
}
