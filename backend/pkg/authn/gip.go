// Package authn's GIP support is DYING CODE, kept alive only for the two
// capabilities Zitadel does not replace at the token-verification layer:
// minting a custom token for tenant switching (TokenMinter) and revoking
// refresh tokens at the identity provider (TokenRevoker). Both are used
// by internal/modules/iam (me.go's switchTenant, signout.go/revoke), and
// deleting this file before those call sites are replaced would not
// compile — see the "Sequencing corrected" note in
// docs/superpowers/plans/2026-08-15-zitadel-auth.md Task 3.
//
// Task 5 of that plan removes TokenMinter/TokenRevoker's last GIP callers
// (replacing them with an HMS-session re-mint and the #781 watermark
// alone), and Task 7 deletes this file and the Firebase dependency
// entirely. Do not add anything new here, and do not resurrect
// NewGIPVerifier/gipVerifier/principalFromToken — Task 3 removed the GIP
// *verifier* path for good; backend/pkg/authn/zitadel.go is what verifies
// an ID token now.
package authn

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

type gipMinter struct{ client *auth.Client }

type gipRevoker struct{ client *auth.Client }

// NewGIPMinter mints GIP/Firebase custom tokens. Separate constructors
// rather than one object exposing both mint and revoke, so a caller
// wired for minting cannot also revoke (and vice versa), and so adding
// one capability could not change the other. It refuses to honor
// FIREBASE_AUTH_EMULATOR_HOST unless allowEmulator is true (dev only) —
// see newAuthClient.
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

// NewGIPRevoker revokes GIP/Firebase refresh tokens. A second narrow
// view of the same Firebase auth client NewGIPMinter wraps, for the same
// separation-of-capability reason: a caller wired to revoke cannot also
// mint. Like NewGIPMinter, it refuses to honor FIREBASE_AUTH_EMULATOR_HOST
// unless allowEmulator is true (dev only).
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
