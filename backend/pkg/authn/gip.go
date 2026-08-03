package authn

import (
	"context"
	"errors"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
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
// enforcing that a tenant_id claim is present and is a string.
func principalFromToken(tok *auth.Token) (Principal, error) {
	tenantID, ok := tok.Claims["tenant_id"].(string)
	if !ok || tenantID == "" {
		return Principal{}, ErrNoTenantClaim
	}
	return Principal{Subject: tok.UID, TenantID: tenantID}, nil
}
