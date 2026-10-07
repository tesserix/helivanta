package loginclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingZitadel serves a login policy and records whether finalize was
// called. It answers the session/authentication-methods routes
// classifyEnrolledMethods needs with a PASSWORD-ONLY user by default, so
// every existing forceMfa-focused test in this file keeps exercising
// exactly the policy branch it is named for rather than accidentally
// tripping the enrolled-factor check added in #854 Task 8. Tests that
// need to exercise the enrolled-factor branch itself use
// countingZitadelWithFactors below.
func countingZitadel(t *testing.T, policyJSON string, finalized *atomic.Bool) *Client {
	t.Helper()
	return countingZitadelWithFactors(t, policyJSON, `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`, finalized)
}

// countingZitadelWithFactors is countingZitadel with the
// authentication_methods response under caller control, so tests can
// assert classifyEnrolledMethods's own branch of CompleteIfSufficient.
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
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
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

// clientWithEnrolledMethods wraps countingZitadelWithFactors with a
// caller-supplied policy and authMethodTypes list, for tests that
// exercise Task 3's TOTP-classification branch of CompleteIfSufficient
// rather than needing to track finalize calls directly (though see
// clientWithEnrolledMethodsAndFinalizeTracking below for the tests that
// do). It discards the finalize-call tracking countingZitadelWithFactors
// takes — most of these tests assert on Result's
// Outcome/Factors/CallbackURL instead, which already pin finalize having
// been called or not.
func clientWithEnrolledMethods(t *testing.T, policyJSON string, methods []string) *Client {
	t.Helper()
	c, _ := clientWithEnrolledMethodsAndFinalizeTracking(t, policyJSON, methods)
	return c
}

// clientWithEnrolledMethodsAndFinalizeTracking is
// clientWithEnrolledMethods plus the finalize-call counter, for the tests
// that assert directly on whether finalize ran (the same reason
// countingZitadel exposes one) rather than relying solely on Outcome.
func clientWithEnrolledMethodsAndFinalizeTracking(t *testing.T, policyJSON string, methods []string) (*Client, *atomic.Bool) {
	t.Helper()
	encoded, err := json.Marshal(methods)
	require.NoError(t, err)
	var finalized atomic.Bool
	c := countingZitadelWithFactors(t, policyJSON, string(encoded), &finalized)
	return c, &finalized
}

// nonForceMFAPolicy is the passwordCheckLifetime-anchored, forceMfa-absent
// policy body most of this file's TOTP-classification tests use — see
// LoginPolicy's doc comment (client.go) for why the anchor field must be
// present for a fixture to exercise the intended branch rather than the
// unrecognized-policy fail-closed path.
const nonForceMFAPolicy = `{"policy":{"passwordCheckLifetime":"864000s"}}`

// forceMFAPolicy is the same anchor with forceMfa explicit, for the tests
// pinning #867 fix round 1's Finding 1: TOTP-only enrollment must ask for
// the factor NATIVELY even when the org forces MFA — CompleteIfSufficient
// must not need to reach (or care about) this policy at all in that case.
const forceMFAPolicy = `{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`

// A user with TOTP enrolled must now be PROMPTED, not handed off — that is
// the whole point of this spec. Previously this handed off.
func TestCompleteIfSufficient_TOTPEnrolledAsksForTheFactor(t *testing.T) {
	c := clientWithEnrolledMethods(t, nonForceMFAPolicy, []string{"AUTHENTICATION_METHOD_TYPE_TOTP", "AUTHENTICATION_METHOD_TYPE_PASSWORD"})
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome)
	require.Equal(t, []string{"totp"}, res.Factors)
	require.Empty(t, res.CallbackURL, "nothing to redirect to until the factor is verified")
}

// TestCompleteIfSufficient_ForceMFAWithTOTPEnrolledStillAsksForTheFactor
// is #867 fix round 1's Finding 1: an EARLIER version of
// CompleteIfSufficient checked policy.ForceMFA before classifying
// enrolled methods, so this exact case — the headline case the whole spec
// exists for — silently fell through to a handoff instead of
// prompting natively. It failed closed (no bypass), but the feature
// never fired for an org that actually forces MFA, which is presumably
// most orgs that bother enrolling TOTP in the first place.
func TestCompleteIfSufficient_ForceMFAWithTOTPEnrolledStillAsksForTheFactor(t *testing.T) {
	c := clientWithEnrolledMethods(t, forceMFAPolicy, []string{"AUTHENTICATION_METHOD_TYPE_TOTP", "AUTHENTICATION_METHOD_TYPE_PASSWORD"})
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome, "TOTP-only enrollment must ask for the factor even when the org forces MFA")
	require.Equal(t, []string{"totp"}, res.Factors)
	require.Empty(t, res.CallbackURL)
}

// TestCompleteIfSufficient_ForceMFAWithNoEnrolledFactorIsRefused is the
// companion case to the one above: a password-only session under
// forceMfa, with NOTHING enrolled that Helivanta could natively prompt
// for, is refused with RefusalMFAEnrollmentRequired (#947; native
// enrolment, #948, replaces it). Finding 1's reorder must not turn this
// into a completion or a factor-required prompt for a factor the user
// never configured.
func TestCompleteIfSufficient_ForceMFAWithNoEnrolledFactorIsRefused(t *testing.T) {
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, forceMFAPolicy, []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD"})
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalMFAEnrollmentRequired, res.Reason)
	require.Empty(t, res.Factors)
	require.Empty(t, res.CallbackURL)
	require.False(t, finalized.Load(), "finalize was called under forceMfa with no factor to offer: this is an MFA bypass")
}

// A factor Helivanta cannot collect is refused (spec D1; #947), and the
// refusal carries what the account has enrolled so the handler can log it.
func TestCompleteIfSufficient_OtpEmailIsRefusedAsUnsupported(t *testing.T) {
	methods := []string{"AUTHENTICATION_METHOD_TYPE_OTP_EMAIL", "AUTHENTICATION_METHOD_TYPE_PASSWORD"}
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, nonForceMFAPolicy, methods)
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.Equal(t, methods, res.EnrolledMethods)
	require.False(t, finalized.Load())
	require.Empty(t, res.Factors)
	require.Empty(t, res.CallbackURL)
}

// OutcomeRefused must remain the zero value: a forgotten assignment must
// fail closed. A zero Result is therefore a refusal with an unspecified
// reason, which the handlers answer as a refusal and log as a defect.
func TestOutcomeRefusedIsZero(t *testing.T) {
	var r Result
	require.Equal(t, OutcomeRefused, r.Outcome)
	require.Equal(t, RefusalUnspecified, r.Reason)
}

// TestCompleteAfterFactor_FinalizesOnlyWhenSessionFactorsReportTOTP pins
// the second half of the OutcomeFactorRequired flow: CompleteAfterFactor
// must re-read the session's own verified factors and finalize only when
// TOTP is actually true there — never on the caller's say-so.
func TestCompleteAfterFactor_FinalizesOnlyWhenSessionFactorsReportTOTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// CompleteAfterFactor now re-runs classifyEnrolledMethods
			// before trusting SessionFactors (#867 fix round 1, Finding
			// 2), so this single GET /v2/sessions/{id} response has to
			// satisfy BOTH callers that hit it: sessionUserID (needs only
			// factors.user.id) and SessionFactors (needs
			// factors.password/totp). One fixture body does both.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/oidc/auth_requests/"):
			w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, got.Outcome)
	require.NotEmpty(t, got.CallbackURL)
}

// TestCompleteAfterFactor_RefusesWhenTOTPNotVerified pins the fail-closed
// direction: a session that has not actually verified TOTP (whatever the
// caller believes happened) must never be finalized. The enrolled-method
// fixture still enrolls TOTP so this test genuinely exercises the
// SessionFactors branch rather than failing closed one step earlier for
// an unrelated reason (no TOTP enrolled at all).
func TestCompleteAfterFactor_RefusesWhenTOTPNotVerified(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, got.Outcome)
	require.Equal(t, RefusalFactorNotVerified, got.Reason)
	require.False(t, finalized.Load(), "finalize was called without TOTP actually verified on the session: this is an MFA bypass")
}

// TestCompleteAfterFactor_UnavailableWhenSessionFactorsUnreadable pins the
// fail-closed direction for an unreadable SessionFactors answer, the same
// way CompleteIfSufficient's own tests pin it for LoginPolicy and the
// enrolled-methods check: an unreadable answer must never be mistaken for
// "TOTP verified". classifyEnrolledMethods and SessionFactors both GET
// /v2/sessions/{id}, so a request counter lets the FIRST hit (classifying
// enrollment) succeed while the SECOND (SessionFactors itself) fails —
// otherwise an unconditional failure on that path would trip
// classification first and this test would no longer be exercising the
// branch it is named for.
func TestCompleteAfterFactor_UnavailableWhenSessionFactorsUnreadable(t *testing.T) {
	var finalized atomic.Bool
	var sessionGETs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			if sessionGETs.Add(1) == 1 {
				w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.ErrorIs(t, err, ErrUnavailable, "an unreadable answer is a retryable error, never a refusal (#947 spec D1)")
	require.NotEqual(t, OutcomeComplete, got.Outcome)
	require.False(t, finalized.Load(), "finalize was called while session factors were unreadable: fails open")
}

// TestCompleteAfterFactor_OtpEmailAlsoEnrolledIsRefused is #867 fix
// round 2's Finding A(1): the re-reviewer verified — with a throwaway,
// deliberately-discarded test — that CompleteAfterFactor's re-run of
// classifyEnrolledMethods (fix round 1, Finding 2) actually catches a
// session that has TOTP genuinely verified but ALSO has an uncollectible
// factor (OTP_EMAIL) enrolled. That exact case is spec D1's reason for
// existing: Helivanta can only collect one of the two factors the user
// configured, so finalizing on TOTP's strength alone would silently skip
// OTP_EMAIL. This test lands what the re-reviewer's throwaway probe
// proved, so the behavior stays pinned rather than reverting silently the
// next time someone touches this file. It asserts on the finalize call
// counter, not merely the Outcome, per the coordinator's explicit
// instruction — Outcome alone would not distinguish "correctly refused to
// finalize" from "finalized, then also happened to report the wrong
// Outcome by mistake".
func TestCompleteAfterFactor_OtpEmailAlsoEnrolledIsRefused(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// TOTP genuinely verified on the session — if
			// CompleteAfterFactor looked only at SessionFactors.TOTP, this
			// fixture alone would finalize.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP","AUTHENTICATION_METHOD_TYPE_OTP_EMAIL"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, got.Outcome)
	require.Equal(t, RefusalFactorUnsupported, got.Reason)
	require.Contains(t, got.EnrolledMethods, "AUTHENTICATION_METHOD_TYPE_OTP_EMAIL")
	require.False(t, finalized.Load(), "finalize was called for a session with TOTP verified AND an uncollectible factor (OTP_EMAIL) also enrolled: this is the exact D1 bypass Finding 2 exists to close")
}

// TestCompleteAfterFactor_UnavailableWhenEnrolledMethodsUnreadable pins
// Finding A(2): an unreadable enrolled-methods answer inside
// CompleteAfterFactor must fail closed as ErrUnavailable, the same direction
// CompleteIfSufficient already has pinned
// (TestCompleteIfSufficientUnavailableWhenEnrolledFactorCheckUnreadable) —
// now needed a second time because CompleteAfterFactor re-runs the same
// check (fix round 1, Finding 2) rather than trusting the first call ever
// ran.
func TestCompleteAfterFactor_UnavailableWhenEnrolledMethodsUnreadable(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// TOTP genuinely verified here on purpose: if the
			// enrolled-methods-unreadable guard were ever accidentally
			// removed, CompleteAfterFactor would fall through to
			// SessionFactors, see TOTP true, and finalize — this fixture
			// is what makes that mutation observable rather than the test
			// passing by accident because SessionFactors also happened to
			// report no TOTP.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotEqual(t, OutcomeComplete, got.Outcome)
	require.False(t, finalized.Load(), "finalize was called while the enrolled-methods check was unreadable: fails open")
}

// TestCompleteAfterFactor_RefusesWhenTOTPNoLongerEnrolled pins Finding
// A(3): the re-reviewer named the !totpEnrolled branch as untested too.
// This is the "enrollment changed between the two calls, or this path was
// reached without CompleteIfSufficient ever running" case CompleteAfterFactor's
// own doc comment describes — TOTP is no longer among the enrolled
// methods at all, so there is nothing here to natively verify against
// regardless of what SessionFactors would say.
func TestCompleteAfterFactor_RefusesWhenTOTPNoLongerEnrolled(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// TOTP genuinely verified here on purpose — see the sibling
			// unreadable-enrolled-methods test above for why: it makes
			// removing the !totpEnrolled guard observable instead of the
			// test passing by accident.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, got.Outcome)
	require.Equal(t, RefusalFactorNotVerified, got.Reason)
	require.False(t, finalized.Load(), "finalize was called for a session with no TOTP enrolled at all: nothing to natively verify")
}

// THE test this whole spec exists for. Zitadel will happily finalize a
// password-only session under forceMfa (spike §2) — Helivanta must not ask it to.
func TestCompleteIfSufficientDoesNotFinalizeWhenForceMFA(t *testing.T) {
	var finalized atomic.Bool
	// passwordCheckLifetime is the anchor LoginPolicy's doc comment
	// describes (client.go): without it this fixture would exercise the
	// fail-closed "unrecognized policy" branch instead of the genuine
	// forceMfa=true branch this test is named for. Since #947 the two are
	// distinguishable (an unrecognised policy is an ErrUnavailable error; a
	// real forceMfa is a RefusalMFAEnrollmentRequired), and the assertions
	// below pin which one this is.
	c := countingZitadel(t, `{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeRefused || got.Reason != RefusalMFAEnrollmentRequired {
		t.Errorf("Result = %v/%v, want refused/mfa_enrollment_required", got.Outcome, got.Reason)
	}
	if finalized.Load() {
		t.Fatal("finalize was called under forceMfa: this is an MFA bypass")
	}
	if got.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, want empty on a refusal", got.CallbackURL)
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
func TestCompleteIfSufficientUnavailableWhenPolicyShapeIsUnrecognised(t *testing.T) {
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
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("CompleteIfSufficient() error = %v, want ErrUnavailable (#947: unreadable is retryable, never a refusal)", err)
			}
			if finalized.Load() {
				t.Fatal("finalize was called for a policy body Helivanta could not understand: a 200 that did not say 'MFA off' was read as if it had")
			}
			if got.Outcome == OutcomeComplete {
				t.Errorf("Outcome = %v alongside an error", got.Outcome)
			}
		})
	}
}

// Fail closed: an unreadable policy is ErrUnavailable, never a completion
// and never a refusal (#947 spec D1).
func TestCompleteIfSufficientUnavailableWhenPolicyUnreadable(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// Classification now runs BEFORE the policy read (#867 fix
			// round 1, Finding 1), so this fixture must answer a
			// password-only enrollment here to actually reach — and
			// exercise — the policy-unreadable branch this test is named
			// for, rather than failing closed one step earlier for an
			// unrelated reason.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CompleteIfSufficient() error = %v, want ErrUnavailable when the policy cannot be read", err)
	}
	if got.Outcome == OutcomeComplete {
		t.Errorf("Outcome = %v alongside an error", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called while the policy was unknown: fails open")
	}
}

// TestCompleteIfSufficientRefusesWhenUserHasEnrolledFactor is #854
// Task 8's proven-to-fail test: the org does NOT force MFA (same policy
// body as TestCompleteIfSufficientFinalizesWhenNoMFARequired, which DOES
// finalize), but the user has voluntarily enrolled a factor Helivanta
// cannot collect — verified live 2026-08-16 against v4.15.3 that
// GET /v2/users/{id}/authentication_methods reflects exactly this shape
// for a user with a second factor (the read this task's
// classifyEnrolledMethods, sufficiency.go, is built on). Before this task's change
// to CompleteIfSufficient, this test failed: the org-policy-only check
// had no way to see the user's own factor and finalized anyway, which is
// precisely the bypass spec D4/Task 8's KNOWN LIMITATIONS §1 (now
// resolved) warned about.
//
// Task 3 (#867) narrowed this: TOTP alone now asks for the factor instead
// of refusing (TestCompleteIfSufficient_TOTPEnrolledAsksForTheFactor),
// so this fixture enrolls TOTP ALONGSIDE an uncollectible factor
// (OTP_EMAIL) — the case spec D1 calls out by name: Helivanta can only
// collect one of the two, so completing on the strength of TOTP alone
// would silently skip the OTP_EMAIL factor the user also configured.
func TestCompleteIfSufficientRefusesWhenUserHasEnrolledFactor(t *testing.T) {
	var finalized atomic.Bool
	c := countingZitadelWithFactors(t,
		`{"policy":{"passwordCheckLifetime":"864000s"}}`,
		`["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP","AUTHENTICATION_METHOD_TYPE_OTP_EMAIL"]`,
		&finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeRefused || got.Reason != RefusalFactorUnsupported {
		t.Errorf("Result = %v/%v, want refused/factor_unsupported: the user enrolled a factor a password-only session cannot satisfy, even alongside TOTP", got.Outcome, got.Reason)
	}
	if finalized.Load() {
		t.Fatal("finalize was called for a user with an enrolled second factor the org policy alone would have missed: this is the bypass Task 8 closes")
	}
}

// TestCompleteIfSufficientUnavailableWhenEnrolledFactorCheckUnreadable pins
// the fail-closed direction for classifyEnrolledMethods itself, the same
// way TestCompleteIfSufficientUnavailableWhenPolicyUnreadable pins it for
// LoginPolicy: an unreadable answer must never be mistaken for "no
// factor found".
func TestCompleteIfSufficientUnavailableWhenEnrolledFactorCheckUnreadable(t *testing.T) {
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
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CompleteIfSufficient() error = %v, want ErrUnavailable when the enrolled-factor check cannot be read", err)
	}
	if got.Outcome == OutcomeComplete {
		t.Errorf("Outcome = %v alongside an error", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called while the enrolled-factor check was unreadable: fails open")
	}
}

// TestCompleteIfSufficientUnavailableWhenSessionHasNoOrgID is #913 Task 2's
// central proof: a session whose factors.user carries no organizationId
// at all must never reach a completed login. This is the case KNOWN
// LIMITATIONS §2 (now deleted from CompleteIfSufficient's doc comment)
// used to warn about — before this task, a missing org id simply meant
// the unscoped InstanceLoginPolicyForDisplay read ran instead, silently
// judging this user by whichever org the login client PAT's own resource
// owner happened to be. Now LoginPolicyForOrg refuses an empty org id
// BEFORE issuing any HTTP request (spec D2) rather than falling back to
// an unscoped read, so this test also asserts the policy endpoint is
// never even hit — not merely that the outcome happens to be a non-completion.
//
// Verification: mutating sessionSubject to default a missing
// organizationId to some non-empty value (e.g. the login client's own
// resource owner, or a hardcoded org id) would make LoginPolicyForOrg
// stop refusing, the policy endpoint assertion below would fire, and this
// test would fail — reintroducing #913 for exactly the session shape
// hardest to notice.
func TestCompleteIfSufficientUnavailableWhenSessionHasNoOrgID(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			t.Fatal("policy endpoint hit with no org id to scope the request: LoginPolicyForOrg must refuse before ever issuing this request (spec D2)")
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			// No organizationId at all on factors.user — the shape this
			// test is named for. Password-only enrollment so the flow
			// reaches the policy-scoping decision rather than failing
			// closed one step earlier for an unrelated reason.
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
		default:
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"x"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CompleteIfSufficient() error = %v, want ErrUnavailable when the session carries no organizationId", err)
	}
	if got.Outcome == OutcomeComplete {
		t.Errorf("Outcome = %v alongside an error", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called for a session with no organizationId: this is the #913 bypass")
	}
}

// TestCompleteIfSufficientScopesThePolicyReadToTheSessionsOrg is spec D5's
// unit-level proof that the org id LoginPolicyForOrg puts on the wire is
// the one the SESSION response reported, not a value this package might
// otherwise be tempted to hardcode (e.g. the login client's own resource
// owner, or a constant left over from an earlier draft). orgIDFromSession
// is deliberately distinctive — nothing else in this file's fixtures uses
// it — so a hardcoded org id anywhere on this call path makes gotOrgHeader
// wrong rather than accidentally matching.
//
// Verification: mutating CompleteIfSufficient to call
// c.InstanceLoginPolicyForDisplay(ctx) (the pre-#913-fix unscoped read)
// instead of c.LoginPolicyForOrg(ctx, subject.OrgID) makes gotOrgHeader
// empty — InstanceLoginPolicyForDisplay sets no x-zitadel-orgid header at
// all — and this test fails.
func TestCompleteIfSufficientScopesThePolicyReadToTheSessionsOrg(t *testing.T) {
	const orgIDFromSession = "org-289838195028398512-distinctive"
	var gotOrgHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			gotOrgHeader = r.Header.Get("x-zitadel-orgid")
			w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s"}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"` + orgIDFromSession + `"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
		default:
			w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeComplete {
		t.Fatalf("Outcome = %v, want OutcomeComplete", got.Outcome)
	}
	if gotOrgHeader != orgIDFromSession {
		t.Errorf("x-zitadel-orgid = %q, want %q: the policy request must be scoped to the org the SESSION reported, not a hardcoded value", gotOrgHeader, orgIDFromSession)
	}
}
