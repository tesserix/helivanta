package authn

import (
	"context"
	"errors"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/google/uuid"
)

var ErrNoTenantClaim = errors.New("authn: token has no tenant_id claim")

type gipVerifier struct{ client *auth.Client }

// NewGIPVerifier verifies GIP/Firebase ID tokens. In dev it honors
// FIREBASE_AUTH_EMULATOR_HOST automatically (no credentials needed).
func NewGIPVerifier(ctx context.Context, projectID string) (TokenVerifier, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}
	return &gipVerifier{client: client}, nil
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
	return Principal{Subject: tok.UID, TenantID: tenantID.String()}, nil
}
