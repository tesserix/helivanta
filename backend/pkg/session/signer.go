package session

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrNoSigningKey is returned by NewSigner whenever it is asked to
// build a Signer that could mint without a real, fully-configured
// Ed25519 identity. Minting is a data/identity control (engineering
// principles §3) and fails closed: there is no such thing as a Signer
// that "mostly" has a key.
var ErrNoSigningKey = errors.New("session: no signing key configured")

// Signer mints HMS session tokens.
//
// Ed25519 (EdDSA) only, deliberately — spec D5: asymmetric signing so
// that a verifier, which only ever holds the public key, can never
// also mint. A shared HMAC secret would make anything that can verify
// a token also able to forge one, which becomes a privilege escalation
// the moment a service is extracted (ADR-0005). Pinning to a single
// algorithm also means there is no algorithm negotiation for a Signer
// to get wrong.
type Signer struct {
	key    ed25519.PrivateKey
	kid    string
	issuer string
	ttl    time.Duration
}

// NewSigner builds a Signer bound to one Ed25519 private key, one kid
// (carried in the token header from the start so key rotation is not
// precluded later — rotation itself is out of scope here), one issuer,
// and one token lifetime.
//
// Every argument is validated and a partially-configured Signer is
// refused outright: minting with no key must be an error, never a
// token, and the same holds for an empty kid, empty issuer or
// non-positive ttl — each is a way this Signer could later mint a
// credential nothing can meaningfully verify or expire.
func NewSigner(key ed25519.PrivateKey, kid, issuer string, ttl time.Duration) (*Signer, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: expected a %d-byte ed25519 private key, got %d bytes",
			ErrNoSigningKey, ed25519.PrivateKeySize, len(key))
	}
	if kid == "" {
		return nil, fmt.Errorf("%w: kid is required", ErrNoSigningKey)
	}
	if issuer == "" {
		return nil, fmt.Errorf("%w: issuer is required", ErrNoSigningKey)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: ttl must be positive, got %s", ErrNoSigningKey, ttl)
	}
	return &Signer{key: key, kid: kid, issuer: issuer, ttl: ttl}, nil
}

// Mint issues a new HMS session token for subject in tenantID.
//
// authTime is carried through into the token's auth_time claim
// UNMODIFIED — it is never set to time.Now() here. authTime means "when
// did this human last authenticate"; the #781 revocation watermark
// compares a token's auth_time against a per-subject revoked-after
// mark, and re-minting on tenant switch must not launder an old
// authentication into a fresh one by resetting it.
func (s *Signer) Mint(subject, tenantID string, authTime time.Time) (string, error) {
	if len(s.key) != ed25519.PrivateKeySize {
		return "", ErrNoSigningKey
	}
	if subject == "" {
		return "", errors.New("session: subject is required to mint")
	}
	if tenantID == "" {
		return "", errors.New("session: tenant_id is required to mint")
	}
	if authTime.IsZero() {
		return "", errors.New("session: auth_time is required to mint")
	}

	now := time.Now().UTC()
	claims := tokenClaims{
		TenantID: tenantID,
		AuthTime: authTime.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("session: sign token: %w", err)
	}
	return signed, nil
}
