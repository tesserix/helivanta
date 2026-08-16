package loginclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// countingZitadel serves a login policy and records whether finalize was
// called. It answers the session/authentication-methods routes
// HasEnrolledFactor needs with a PASSWORD-ONLY user by default, so every
// existing forceMfa-focused test in this file keeps exercising exactly
// the policy branch it is named for rather than accidentally tripping
// the enrolled-factor check added in #854 Task 8. Tests that need to
// exercise the enrolled-factor branch itself use
// countingZitadelWithFactors below.
func countingZitadel(t *testing.T, policyJSON string, finalized *atomic.Bool) *Client {
	t.Helper()
	return countingZitadelWithFactors(t, policyJSON, `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`, finalized)
}

// countingZitadelWithFactors is countingZitadel with the
// authentication_methods response under caller control, so tests can
// assert HasEnrolledFactor's own branch of CompleteIfSufficient.
// authMethodTypesJSON is the JSON array countingZitadel's fake
// GET /v2/users/{id}/authentication_methods answers with — the ACTUAL
// wire shape confirmed live 2026-08-16 against v4.15.3 (spike, "Per-user
// enrolled factors" §): {"authMethodTypes":[...]}.
func countingZitadelWithFactors(t *testing.T, policyJSON, authMethodTypesJSON string, finalized *atomic.Bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			w.Write([]byte(policyJSON))
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":` + authMethodTypesJSON + `}`))
		case r.Method == http.MethodPost && len(r.URL.Path) > len("/v2/oidc/auth_requests/"):
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "pat", srv.Client())
}

// THE test this whole spec exists for. Zitadel will happily finalize a
// password-only session under forceMfa (spike §2) — HMS must not ask it to.
func TestCompleteIfSufficientDoesNotFinalizeWhenForceMFA(t *testing.T) {
	var finalized atomic.Bool
	// passwordCheckLifetime is the anchor LoginPolicy's doc comment
	// describes (client.go): without it this fixture would exercise the
	// fail-closed "unrecognized policy" branch instead of the genuine
	// forceMfa=true branch this test is named for — both currently reach
	// OutcomeHandoff, so that mistake would pass silently.
	c := countingZitadel(t, `{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called under forceMfa: this is an MFA bypass")
	}
	if got.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, want empty on handoff", got.CallbackURL)
	}
}

func TestCompleteIfSufficientFinalizesWhenNoMFARequired(t *testing.T) {
	var finalized atomic.Bool
	// passwordCheckLifetime is the anchor LoginPolicy's doc comment
	// describes (client.go) — real Zitadel elides forceMfa entirely when
	// it is false, so this fixture, like the real wire response, carries
	// no explicit forceMfa at all.
	c := countingZitadel(t, `{"policy":{"passwordCheckLifetime":"864000s"}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeComplete {
		t.Fatalf("Outcome = %v, want OutcomeComplete", got.Outcome)
	}
	if !finalized.Load() {
		t.Error("finalize was not called for a sufficient session")
	}
	if got.CallbackURL == "" {
		t.Error("CallbackURL is empty on a completed login")
	}
}

// The fail-closed guarantee at the level that matters: not "LoginPolicy
// returns an error" but "no authorization code is issued". Every body here
// is a 200 — no error path is involved anywhere — and each one previously
// produced outcome=complete with finalize actually called, because a
// missing forceMfa field decoded to false. This asserts on the call
// counter, which is the only thing that distinguishes a working login from
// an MFA bypass.
func TestCompleteIfSufficientHandsOffWhenPolicyShapeIsUnrecognised(t *testing.T) {
	bodies := map[string]string{
		"empty object":                   `{}`,
		"null body":                      `null`,
		"forceMfa un-nested":             `{"forceMfa":true}`,
		"forceMfa renamed to snake_case": `{"policy":{"force_mfa":true}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var finalized atomic.Bool
			c := countingZitadel(t, body, &finalized)

			got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
			if err != nil {
				t.Fatalf("CompleteIfSufficient() error = %v, want a handoff not an error", err)
			}
			if finalized.Load() {
				t.Fatal("finalize was called for a policy body HMS could not understand: a 200 that did not say 'MFA off' was read as if it had")
			}
			if got.Outcome != OutcomeHandoff {
				t.Errorf("Outcome = %v, want OutcomeHandoff", got.Outcome)
			}
		})
	}
}

// Fail closed: an unreadable policy must hand off, never complete.
func TestCompleteIfSufficientHandsOffWhenPolicyUnreadable(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/management/v1/policies/login" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		finalized.Store(true)
		w.Write([]byte(`{"callbackUrl":"x"}`))
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v, want a handoff not an error", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff when the policy cannot be read", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called while the policy was unknown: fails open")
	}
}

// TestCompleteIfSufficientHandsOffWhenUserHasEnrolledFactor is #854
// Task 8's proven-to-fail test: the org does NOT force MFA (same policy
// body as TestCompleteIfSufficientFinalizesWhenNoMFARequired, which DOES
// finalize), but the user has voluntarily enrolled TOTP — HasEnrolledFactor
// verified live 2026-08-16 against v4.15.3 that
// GET /v2/users/{id}/authentication_methods reflects exactly this shape
// for a user with a second factor. Before this task's change to
// CompleteIfSufficient, this test failed: the org-policy-only check had
// no way to see the user's own factor and finalized anyway, which is
// precisely the bypass spec D4/Task 8's KNOWN LIMITATIONS §1 (now
// resolved) warned about.
func TestCompleteIfSufficientHandsOffWhenUserHasEnrolledFactor(t *testing.T) {
	var finalized atomic.Bool
	c := countingZitadelWithFactors(t,
		`{"policy":{"passwordCheckLifetime":"864000s"}}`,
		`["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`,
		&finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff: the user enrolled a factor a password-only session cannot satisfy", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called for a user with an enrolled second factor the org policy alone would have missed: this is the bypass Task 8 closes")
	}
}

// TestCompleteIfSufficientHandsOffWhenEnrolledFactorCheckUnreadable pins
// the fail-closed direction for HasEnrolledFactor itself, the same way
// TestCompleteIfSufficientHandsOffWhenPolicyUnreadable pins it for
// LoginPolicy: an unreadable answer must never be mistaken for "no
// factor found".
func TestCompleteIfSufficientHandsOffWhenEnrolledFactorCheckUnreadable(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s"}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v, want a handoff not an error", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff when the enrolled-factor check cannot be read", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called while the enrolled-factor check was unreadable: fails open")
	}
}
