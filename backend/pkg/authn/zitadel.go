package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// zitadelClaims is the subset of a Zitadel ID token's payload this
// package reads beyond what go-oidc's oidc.IDToken already parses
// (issuer, audience, subject, expiry, issued-at — all checked by
// oidc.IDTokenVerifier.Verify itself). auth_time is Zitadel/OIDC-specific
// and not one of go-oidc's built-in fields, so it is read back out of the
// token's raw claims via oidc.IDToken.Claims.
type zitadelClaims struct {
	AuthTime int64 `json:"auth_time"`
}

type zitadelVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewZitadelVerifier builds a TokenVerifier that verifies Zitadel-issued
// ID tokens through standard OIDC — real discovery against issuerURL,
// real JWKS fetch, real RS256 signature check, issuer and audience
// pinned to issuerURL/clientID. No vendor SDK: this is exactly the
// go-oidc usage independently proved against a live Zitadel in
// docs/superpowers/spikes/2026-08-15-zitadel-spike.md P0-4, including its
// two negative cases (wrong audience, tampered signature).
//
// issuerURL and clientID are both required and construction refuses
// outright without them, rather than building a Verifier that would
// silently check tokens against an empty audience (which oidc.Config
// would treat as "no real token can ever match" — safe, but a
// configuration defect masquerading as an auth failure with no signal at
// boot; refusing to construct at all is the honest failure, matching
// engineering-principles.md §4's boot-failure tier).
func NewZitadelVerifier(ctx context.Context, issuerURL, clientID string) (TokenVerifier, error) {
	if issuerURL == "" {
		return nil, errors.New("authn: zitadel issuer URL is required")
	}
	if clientID == "" {
		return nil, errors.New("authn: zitadel client ID is required")
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("authn: zitadel discovery at %s: %w", issuerURL, err)
	}
	// ClientID pins the audience check; SupportedSigningAlgs is left at
	// its default (the provider's advertised
	// id_token_signing_alg_values_supported, RS256 for Zitadel per the
	// spike's discovery document) rather than accepting whatever alg a
	// token's own header asks for — go-oidc still only honors an
	// algorithm from that list, never the token's say-so alone.
	return &zitadelVerifier{
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

// Verify checks raw as a Zitadel ID token and returns who authenticated
// and when. It deliberately returns a Principal with an EMPTY TenantID —
// see Principal.TenantID's doc comment for why that is safe, not a
// loosening.
func (z *zitadelVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	idToken, err := z.verifier.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("authn: verify zitadel token: %w", err)
	}
	var claims zitadelClaims
	if err := idToken.Claims(&claims); err != nil {
		return Principal{}, fmt.Errorf("authn: parse zitadel claims: %w", err)
	}
	// A token with no auth_time claim cannot be evaluated against a
	// revocation watermark, and a credential that cannot be evaluated is
	// not one that can be trusted. Carried over unchanged from GIP's
	// contract (docs/superpowers/spikes/2026-08-15-zitadel-spike.md
	// confirms auth_time is present and non-zero on every real Zitadel
	// token observed) — #781 depends on it regardless of issuer.
	if claims.AuthTime == 0 {
		return Principal{}, ErrNoAuthTime
	}
	return Principal{
		Subject:  idToken.Subject,
		AuthTime: time.Unix(claims.AuthTime, 0).UTC(),
	}, nil
}
