// Package session mints and verifies the HMS session token: the
// credential HMS itself issues once a Zitadel ID token has been
// verified at login (spec docs/superpowers/specs/2026-08-15-zitadel-auth-design.md,
// decision D2).
//
// This package is deliberately the whole of HMS's credential-issuing
// surface for the session token, and nothing else may sign or verify
// one. A defect here is an authentication bypass, not a bug: where a
// choice is between clever and obvious, this package takes obvious.
package session

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is exactly what authn.Principal needs and nothing else.
//
// No roles, deliberately: permissions resolve per request from
// OpenFGA, so a role baked into a token is a stale answer that
// survives until the token expires. Baking one in would mean a
// permission change takes effect at the next login instead of the
// next request.
type Claims struct {
	// Subject is the stable opaque subject identifier from Zitadel,
	// carried through unchanged.
	Subject string
	// TenantID is HMS's own fact, not Zitadel's — see spec D1. Set at
	// mint time by whichever caller has already resolved the tenant
	// (login, or tenant switch).
	TenantID string
	// AuthTime is when the human last authenticated, not when this
	// token was minted. It is carried through from mint to mint —
	// including across a tenant-switch re-mint — and NEVER reset to
	// time.Now(), because the #781 revocation watermark compares
	// against exactly this value. Resetting it on mint would let a
	// re-mint launder an old authentication into a fresh one and
	// quietly defeat revocation.
	AuthTime time.Time
	// IssuedAt and ExpiresAt are this token's own mint/expiry times,
	// distinct from AuthTime.
	IssuedAt  time.Time
	ExpiresAt time.Time
	// Issuer identifies HMS as the token's issuer (not Zitadel — this
	// is HMS's own credential, spec D1).
	Issuer string
}

// tokenClaims is the wire shape signed into the JWT. It embeds
// jwt.RegisteredClaims for sub/iss/iat/exp and adds exactly the two
// custom claims spec D2 allows: tenant_id and auth_time. AuthTime is
// carried as a Unix-seconds integer rather than jwt.NumericDate so its
// precision is explicit and symmetric between Mint and Verify.
type tokenClaims struct {
	TenantID string `json:"tenant_id"`
	AuthTime int64  `json:"auth_time"`
	jwt.RegisteredClaims
}
