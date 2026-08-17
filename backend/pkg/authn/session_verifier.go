package authn

import (
	"context"
	"fmt"

	"github.com/tesserix/helivanta/pkg/session"
)

// sessionTokenVerifier adapts *session.Verifier to the TokenVerifier
// interface Middleware expects, translating session.Claims into a
// Principal.
//
// This is what ends the interim state the Zitadel verifier left behind
// (see zitadel.go's Principal.TenantID doc comment, and the plan's
// "Interim-state note" after Task 3): from here on, the authenticated
// /v1 chain verifies the HMS session HMS itself minted at login (plan
// Task 4, spec D1), never a raw Zitadel ID token — so Principal.TenantID
// is HMS's own fact again, not the empty placeholder the Zitadel
// verifier had to leave it as.
type sessionTokenVerifier struct {
	verifier *session.Verifier
}

// NewSessionVerifier builds the TokenVerifier bootstrap.V1Chain mounts
// in front of every ordinary /v1 route.
//
// A raw Zitadel ID token presented here is refused, structurally rather
// than by a check this code has to remember to make: v.Verify pins both
// the signing algorithm (Ed25519 only — session/verifier.go) and the
// issuer (cfg.SessionIssuer, HMS's own, never Zitadel's), and a Zitadel
// ID token is signed RS256 by Zitadel's key under Zitadel's issuer. It
// fails signature verification (wrong algorithm entirely) before the
// issuer mismatch is ever reached.
func NewSessionVerifier(v *session.Verifier) TokenVerifier {
	return &sessionTokenVerifier{verifier: v}
}

func (s *sessionTokenVerifier) Verify(_ context.Context, raw string) (Principal, error) {
	claims, err := s.verifier.Verify(raw)
	if err != nil {
		return Principal{}, fmt.Errorf("authn: verify hms session: %w", err)
	}
	return Principal{
		Subject:      claims.Subject,
		TenantID:     claims.TenantID,
		AuthTime:     claims.AuthTime,
		IdleDeadline: claims.IdleDeadline,
	}, nil
}
