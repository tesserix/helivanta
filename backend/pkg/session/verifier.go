package session

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrNoVerificationKey mirrors ErrNoSigningKey on the verify side: a
// Verifier built without a real Ed25519 public key must never be
// constructed, because "verification key absent" is exactly the state
// a forged token would want to run against.
var ErrNoVerificationKey = errors.New("session: no verification key configured")

// ErrInvalidToken is returned for every way a token can fail to be a
// genuine, current HMS session — bad signature, wrong/forged
// algorithm, wrong issuer, wrong kid, expired, malformed, or missing a
// required claim. Deliberately one error for all of these: a caller
// checking *why* a token was refused, in order to decide differently,
// is a caller finding a way to treat some refusals as more valid than
// others. There is no such thing here — every case means "not
// authenticated."
var ErrInvalidToken = errors.New("session: invalid token")

// Verifier checks HMS session tokens minted by the Signer holding the
// matching private key.
type Verifier struct {
	key    ed25519.PublicKey
	kid    string
	issuer string
}

// NewVerifier builds a Verifier bound to one Ed25519 public key, the
// kid it is expected to carry, and the issuer it must claim. As with
// NewSigner, every argument is validated up front rather than left to
// fail confusingly (or not at all) inside Verify.
func NewVerifier(key ed25519.PublicKey, kid, issuer string) (*Verifier, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: expected a %d-byte ed25519 public key, got %d bytes",
			ErrNoVerificationKey, ed25519.PublicKeySize, len(key))
	}
	if kid == "" {
		return nil, fmt.Errorf("%w: kid is required", ErrNoVerificationKey)
	}
	if issuer == "" {
		return nil, fmt.Errorf("%w: issuer is required", ErrNoVerificationKey)
	}
	return &Verifier{key: key, kid: kid, issuer: issuer}, nil
}

// Verify parses and validates raw, returning the claims it carried, or
// a wrapped ErrInvalidToken if raw is not a live, genuine HMS session
// signed by this Verifier's key.
//
// The accepted algorithm is pinned to EdDSA and never taken from the
// token's own header: the keyfunc below refuses to hand back a key at
// all unless token.Method is concretely *jwt.SigningMethodEd25519, and
// jwt.WithValidMethods pins the same set again at the parser level as
// defense in depth. This is what refuses both "alg: none" (Method is
// *jwt.signingMethodNone, not Ed25519 — the keyfunc never returns a
// key, so there is nothing for the empty signature to be checked
// against) and the classic algorithm-confusion attack of resigning
// with HS256 using the Ed25519 public key as an HMAC secret (Method is
// *jwt.SigningMethodHMAC, same refusal, and the public key bytes are
// never used as a MAC key by this code at all). A verifier that
// instead asked the keyfunc "what key matches the alg this token
// claims" would hand an attacker exactly the confusion this closes.
func (v *Verifier) Verify(raw string) (Claims, error) {
	var claims tokenClaims
	token, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if kid != v.kid {
			return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidToken, kid)
		}
		return v.key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !token.Valid {
		return Claims{}, ErrInvalidToken
	}
	if claims.Subject == "" || claims.TenantID == "" {
		return Claims{}, fmt.Errorf("%w: missing subject or tenant_id", ErrInvalidToken)
	}
	// Fail closed on a missing idle_deadline (spec D2/D3, #848): a
	// token minted before this claim existed must be refused, not
	// treated as "no idle limit" — that would be a class of session
	// this control can never reach. The cost, accepted deliberately,
	// is that every session minted before this deploys is invalidated
	// and everyone signs in once.
	if claims.IdleDeadline == 0 {
		return Claims{}, fmt.Errorf("%w: missing idle_deadline", ErrInvalidToken)
	}

	var issuedAt, expiresAt time.Time
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}

	return Claims{
		Subject:      claims.Subject,
		TenantID:     claims.TenantID,
		AuthTime:     time.Unix(claims.AuthTime, 0).UTC(),
		IdleDeadline: time.Unix(claims.IdleDeadline, 0).UTC(),
		IssuedAt:     issuedAt,
		ExpiresAt:    expiresAt,
		Issuer:       claims.Issuer,
	}, nil
}
