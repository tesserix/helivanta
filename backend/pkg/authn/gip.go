package authn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/google/uuid"
)

var ErrNoTenantClaim = errors.New("authn: token has no tenant_id claim")

// ErrNoAuthTime is returned when a verified token carries no auth_time
// claim. A token with no auth_time cannot be evaluated against a
// revocation watermark, and a credential that cannot be evaluated is not
// one that can be trusted.
var ErrNoAuthTime = errors.New("authn: token has no auth_time claim")

type gipVerifier struct{ client *auth.Client }

type gipMinter struct{ client *auth.Client }

type gipRevoker struct{ client *auth.Client }

// NewGIPVerifier verifies GIP/Firebase ID tokens. It refuses to honor
// FIREBASE_AUTH_EMULATOR_HOST unless allowEmulator is true (dev only);
// see newAuthClient for why.
func NewGIPVerifier(ctx context.Context, projectID string, allowEmulator bool) (TokenVerifier, error) {
	client, err := newAuthClient(ctx, projectID, allowEmulator)
	if err != nil {
		return nil, err
	}
	return &gipVerifier{client: client}, nil
}

// NewGIPMinter mints GIP/Firebase custom tokens. It is a second, narrow
// view of the same Firebase auth client NewGIPVerifier wraps — separate
// constructors rather than one object exposing both, so a caller wired
// for minting cannot also verify (and vice versa), and so adding minting
// could not change verification. Like the verifier, it refuses to honor
// FIREBASE_AUTH_EMULATOR_HOST unless allowEmulator is true (dev only).
//
// Minting requires a service-account credential (the Admin SDK signs the
// token locally, or delegates to the IAM signBlob API). Against the auth
// emulator no signature is required, so dev and tests work with no
// credentials at all.
func NewGIPMinter(ctx context.Context, projectID string, allowEmulator bool) (TokenMinter, error) {
	client, err := newAuthClient(ctx, projectID, allowEmulator)
	if err != nil {
		return nil, err
	}
	return &gipMinter{client: client}, nil
}

// NewGIPRevoker revokes GIP/Firebase refresh tokens. A third narrow view
// of the same Firebase auth client NewGIPVerifier and NewGIPMinter wrap,
// for the same separation-of-capability reason: a caller wired to revoke
// cannot also verify or mint. Like both, it refuses to honor
// FIREBASE_AUTH_EMULATOR_HOST unless allowEmulator is true (dev only).
func NewGIPRevoker(ctx context.Context, projectID string, allowEmulator bool) (TokenRevoker, error) {
	client, err := newAuthClient(ctx, projectID, allowEmulator)
	if err != nil {
		return nil, err
	}
	return &gipRevoker{client: client}, nil
}

func newAuthClient(ctx context.Context, projectID string, allowEmulator bool) (*auth.Client, error) {
	// The Admin SDK reads this variable at construction. When it is set,
	// VerifyIDToken decodes the JWT and trusts it — no RSA signature
	// check at all. Reaching production, that turns "forge a token with
	// any tenant_id" into full access to every tenant's data, with a
	// clean boot and a green readiness probe. Refuse instead.
	if host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST"); host != "" && !allowEmulator {
		return nil, fmt.Errorf(
			"authn: FIREBASE_AUTH_EMULATOR_HOST=%q is set outside a dev environment; "+
				"the emulator makes ID token signature verification a no-op, so any "+
				"forged token would be accepted. Unset it, or set HMS_ENV=dev", host)
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}
	return client, nil
}

// CustomTokenWithClaims forwards to the Admin SDK. The wrapper exists so
// the concrete *auth.Client never escapes this package: a holder of the
// TokenMinter interface can mint, and can do nothing else.
func (g *gipMinter) CustomTokenWithClaims(ctx context.Context, uid string, claims map[string]interface{}) (string, error) {
	tok, err := g.client.CustomTokenWithClaims(ctx, uid, claims)
	if err != nil {
		return "", fmt.Errorf("mint custom token: %w", err)
	}
	return tok, nil
}

// RevokeRefreshTokens forwards to the Admin SDK, telling GIP to stop
// honoring refresh tokens issued to uid before now. It is called AFTER
// the HMS watermark commits, never before: GIP is not transactional, and
// a GIP failure must not roll back a revocation HMS already decided on
// (see revocationHandlers.revoke in internal/modules/iam/signout.go).
func (g *gipRevoker) RevokeRefreshTokens(ctx context.Context, uid string) error {
	if err := g.client.RevokeRefreshTokens(ctx, uid); err != nil {
		return fmt.Errorf("revoke refresh tokens for %s: %w", uid, err)
	}
	return nil
}

func (g *gipVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := g.client.VerifyIDToken(ctx, raw)
	if err != nil {
		return Principal{}, err
	}
	return principalFromToken(tok)
}

// principalFromToken maps a verified GIP/Firebase token to a Principal,
// enforcing that a tenant_id claim is present, is a string, and parses as
// a UUID. The canonical uuid.String() lowercase form is stored on
// Principal.TenantID rather than the raw claim, so every downstream
// consumer (Resolve's object-id prefix matching, the iam-fga-sync
// consumer, tenant comparisons) sees one consistent casing regardless of
// how the issuer rendered the claim. This is also why Resolve's
// "perm:"+tenantID+"/" prefix match is safe: a canonical UUID string
// cannot contain "/", so it can never be mistaken for a prefix of a
// different tenant's object id.
func principalFromToken(tok *auth.Token) (Principal, error) {
	raw, ok := tok.Claims["tenant_id"].(string)
	if !ok || raw == "" {
		return Principal{}, ErrNoTenantClaim
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		return Principal{}, ErrNoTenantClaim
	}
	// A token with no auth_time claim cannot be evaluated against a
	// revocation watermark, and a credential that cannot be evaluated is
	// not one that can be trusted.
	if tok.AuthTime == 0 {
		return Principal{}, ErrNoAuthTime
	}
	return Principal{
		Subject:  tok.UID,
		TenantID: tenantID.String(),
		AuthTime: time.Unix(tok.AuthTime, 0).UTC(),
	}, nil
}
