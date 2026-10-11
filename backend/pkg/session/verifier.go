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
// genuine, current Helivanta session — bad signature, wrong/forged
// algorithm, wrong issuer, wrong kid, expired, malformed, or missing a
// required claim. Deliberately one error for all of these: a caller
// checking *why* a token was refused, in order to decide differently,
// is a caller finding a way to treat some refusals as more valid than
// others. There is no such thing here — every case means "not
// authenticated."
var ErrInvalidToken = errors.New("session: invalid token")

// Verifier checks Helivanta session tokens minted by the Signer holding the
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
// a wrapped ErrInvalidToken if raw is not a live, genuine Helivanta session
// signed by this Verifier's key.
//
// Verify does NOT enforce IdleDeadline — it only refuses a token that
// omits the claim (fail closed, #848) and returns whatever value the
// token carries, which may already be in the past. Checking IdleDeadline
// against time.Now() is authn.Middleware's job, the same way this
// package carries auth_time but the #781 revocation-watermark check is
// the caller's. A successful Verify is not proof the session is still
// within its idle window.
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
	claims, err := v.parse(raw, true)
	if err != nil {
		return Claims{}, err
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

// RenewalEvidence is what an EXPIRED session cookie may still tell the
// login exchange (#859): who it belonged to, when they last authenticated,
// and the idle deadline that browser was running against. Nothing else.
//
// It is deliberately NOT Claims and carries no tenant: authn.Principal
// cannot be built without one, so evidence read past exp cannot be turned
// into an authenticated request by a careless caller. internal/archtest
// pins every caller of Verifier.RenewalEvidence to an allowlist that holds
// only the login exchange (TestRenewalEvidenceIsOnlyReadByTheLoginExchange).
type RenewalEvidence struct {
	Subject      string
	AuthTime     time.Time
	IdleDeadline time.Time
}

// RenewalEvidence reads raw for the login exchange's idle-deadline decision
// (iam.LoginHandlers.idleDeadlineFor) WITHOUT refusing it for being
// expired. It is not an authentication check and must never be used as
// one; Verify is.
//
// Every other check Verify applies still applies, through the same code
// path: EdDSA only, this Verifier's kid, a valid signature, this issuer,
// exp and iat PRESENT (only their values are not judged), and sub,
// tenant_id and idle_deadline present. A token that is forged, from
// another issuer, or malformed is refused exactly as Verify refuses it.
//
// Why reading past exp is safe here, and why there is no bound on how far
// past (design spec D4, docs/superpowers/specs/2026-10-12-expired-cookie-
// keeps-idle-deadline-design.md): the caller only ever CARRIES this
// deadline forward, and a carried deadline can only keep or shorten the
// window a fresh login would open — every deadline Helivanta writes is
// now + IdleTimeout. An old token therefore never grants more than a fresh
// login would, while a bound would hand every token older than it a fresh
// window and reopen the exact hole #859 closes. The signature, which this
// still checks, is what stops a forged deadline.
func (v *Verifier) RenewalEvidence(raw string) (RenewalEvidence, error) {
	claims, err := v.parse(raw, false)
	if err != nil {
		return RenewalEvidence{}, err
	}
	return RenewalEvidence{
		Subject:      claims.Subject,
		AuthTime:     time.Unix(claims.AuthTime, 0).UTC(),
		IdleDeadline: time.Unix(claims.IdleDeadline, 0).UTC(),
	}, nil
}

// parse is the ONE implementation of "is this a genuine Helivanta session
// token", shared by Verify and RenewalEvidence so the two cannot drift.
// checkExpiry is the only difference between them: Verify judges exp and
// iat; RenewalEvidence requires both to be present but does not judge
// their values.
func (v *Verifier) parse(raw string, checkExpiry bool) (tokenClaims, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	}
	if !checkExpiry {
		// WithoutClaimsValidation turns off EVERY registered-claim check,
		// issuer included, not just exp. The issuer and the presence of
		// exp/iat are therefore re-checked by hand below rather than
		// silently dropped with the rest.
		opts = []jwt.ParserOption{
			jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
			jwt.WithoutClaimsValidation(),
		}
	}

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
	}, opts...)
	if err != nil {
		return tokenClaims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !token.Valid {
		return tokenClaims{}, ErrInvalidToken
	}
	if !checkExpiry {
		if claims.Issuer != v.issuer {
			return tokenClaims{}, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidToken, claims.Issuer)
		}
		if claims.ExpiresAt == nil || claims.IssuedAt == nil {
			return tokenClaims{}, fmt.Errorf("%w: missing exp or iat", ErrInvalidToken)
		}
	}
	if claims.Subject == "" || claims.TenantID == "" {
		return tokenClaims{}, fmt.Errorf("%w: missing subject or tenant_id", ErrInvalidToken)
	}
	// Fail closed on a missing idle_deadline (spec D2/D3, #848): a
	// token minted before this claim existed must be refused, not
	// treated as "no idle limit" — that would be a class of session
	// this control can never reach. The cost, accepted deliberately,
	// is that every session minted before this deploys is invalidated
	// and everyone signs in once.
	if claims.IdleDeadline == 0 {
		return tokenClaims{}, fmt.Errorf("%w: missing idle_deadline", ErrInvalidToken)
	}
	return claims, nil
}
