package loginclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// servePasswordCurrent answers the two reads the #856 password gate makes —
// GET /v2/users/{id} and GET /v2/settings/password/expiry — for a user whose
// password is NOT due to change: no passwordChangeRequired, and an org with
// no expiry policy. Every fixture in this file calls it first, so each test
// keeps exercising the branch it is named for; the gate itself is pinned in
// passwordchange_test.go. It answers false for every other request.
func servePasswordCurrent(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.Count(r.URL.Path, "/") == 3:
		w.Write([]byte(userPasswordCurrentJSON))
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/v2/settings/password/expiry":
		w.Write([]byte(noPasswordExpiryJSON))
		return true
	}
	return false
}

// userPasswordCurrentJSON is GET /v2/users/{id} for a human user with a
// recorded password change and no change required — the shape Zitadel
// sends, with passwordChangeRequired elided because it is false.
const userPasswordCurrentJSON = `{"user":{"userId":"u1","state":"USER_STATE_ACTIVE","human":{"passwordChanged":"2026-01-01T00:00:00Z"}}}`

// noPasswordExpiryJSON is GET /v2/settings/password/expiry for an org with
// no expiry policy: maxAgeDays is zero, so proto3 JSON elides it.
const noPasswordExpiryJSON = `{"settings":{"resourceOwnerType":"RESOURCE_OWNER_TYPE_INSTANCE"}}`

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
		if servePasswordCurrent(w, r) {
			return
		}
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

// TestCompleteIfSufficient_ForceMFAWithNoEnrolledFactorAsksForEnrolment is
// the companion case to the one above: a password-only session under
// forceMfa, with NOTHING enrolled that Helivanta could natively prompt
// for, answers OutcomeEnrollmentRequired for TOTP (#948, spec D1) — the
// outcome that replaced the #947 refusal. Finding 1's reorder must not
// turn this into a completion or a factor-required prompt for a factor
// the user never configured, and enrolment-required must never finalize.
func TestCompleteIfSufficient_ForceMFAWithNoEnrolledFactorAsksForEnrolment(t *testing.T) {
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, forceMFAPolicy, []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD"})
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeEnrollmentRequired, res.Outcome, "forceMfa with nothing enrolled must ask for enrolment: %v/%v", res.Outcome, res.Reason)
	require.Equal(t, []string{"totp"}, res.Factors)
	require.Empty(t, res.CallbackURL)
	require.False(t, finalized.Load(), "finalize was called under forceMfa with no factor to offer: this is an MFA bypass")
}

// Enrolment is offered ONLY when nothing is enrolled: forceMfa plus an
// uncollectible factor stays a RefusalFactorUnsupported (spec D6), because
// enrolling a TOTP would not make the other factor collectible.
func TestCompleteIfSufficient_ForceMFAWithUnsupportedFactorIsStillRefusedNotEnrolled(t *testing.T) {
	methods := []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_OTP_EMAIL"}
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, forceMFAPolicy, methods)
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.False(t, finalized.Load())
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
	// real forceMfa with nothing enrolled is, since #948, an
	// OutcomeEnrollmentRequired), and the assertions below pin which one
	// this is.
	c := countingZitadel(t, `{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeEnrollmentRequired {
		t.Errorf("Result = %v/%v, want enrollment_required", got.Outcome, got.Reason)
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
		if servePasswordCurrent(w, r) {
			return
		}
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
// Verification: before #917, mutating CompleteIfSufficient to call the
// unscoped InstanceLoginPolicyForDisplay(ctx) instead of
// c.LoginPolicyForOrg(ctx, subject.OrgID) made gotOrgHeader empty and this
// test failed. #917 deleted that reader and made loginPolicy take a required
// org id, so the unscoped read is no longer expressible; this test still
// fails if the org id passed is anything but the session's own.
func TestCompleteIfSufficientScopesThePolicyReadToTheSessionsOrg(t *testing.T) {
	const orgIDFromSession = "org-289838195028398512-distinctive"
	var gotOrgHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if servePasswordCurrent(w, r) {
			return
		}
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

// A linked external identity provider is an ALTERNATIVE first factor, not a
// second factor Zitadel demands on top of a password (#950, spec D1). An
// account carrying PASSWORD + IDP must complete exactly like a password-only
// account: finalize called, callback returned. Observed in production on
// 2026-10-08 — the first morning after #949 — refusing instead.
func TestCompleteIfSufficient_IdpLinkCompletesLikePasswordOnly(t *testing.T) {
	methods := []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_IDP"}
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, nonForceMFAPolicy, methods)
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome, "a password + linked-IdP account was not completed: %v/%v", res.Outcome, res.Reason)
	require.NotEmpty(t, res.CallbackURL)
	require.True(t, finalized.Load(), "finalize was not called for a password + linked-IdP account")
}

// PASSWORD + IDP + TOTP must ask for TOTP natively, the same answer TOTP
// alone gets (#950, spec D1): the IdP link neither blocks the prompt nor
// skips it.
func TestCompleteIfSufficient_IdpLinkWithTOTPAsksForTheFactor(t *testing.T) {
	methods := []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_IDP", "AUTHENTICATION_METHOD_TYPE_TOTP"}
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, nonForceMFAPolicy, methods)
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome, "password + IdP + TOTP did not ask for the factor: %v/%v", res.Outcome, res.Reason)
	require.Equal(t, []string{"totp"}, res.Factors)
	require.False(t, finalized.Load(), "finalize was called before the TOTP code was collected")
}

// The IdP link is neutral, not a licence: a factor Helivanta still cannot
// collect, enrolled alongside it, refuses exactly as before (#950, spec D1).
func TestCompleteIfSufficient_IdpLinkDoesNotExcuseAnUnsupportedFactor(t *testing.T) {
	methods := []string{"AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_IDP", "AUTHENTICATION_METHOD_TYPE_OTP_EMAIL"}
	c, finalized := clientWithEnrolledMethodsAndFinalizeTracking(t, nonForceMFAPolicy, methods)
	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.Equal(t, methods, res.EnrolledMethods)
	require.False(t, finalized.Load(), "finalize was called with OTP_EMAIL enrolled: the IdP link must not excuse an uncollectible factor")
}

// afterFactorZitadel serves a session with TOTP genuinely verified and the
// caller's enrolled-method list, and records whether finalize was called —
// the fixture the two #950 CompleteAfterFactor tests below share.
func afterFactorZitadel(t *testing.T, authMethodTypesJSON string) (*Client, *atomic.Bool) {
	t.Helper()
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if servePasswordCurrent(w, r) {
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			w.Write([]byte(`{"authMethodTypes":` + authMethodTypesJSON + `}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/oidc/auth_requests/"):
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "pat", srv.Client()), &finalized
}

// After a verified TOTP code, PASSWORD + IDP + TOTP completes (#950, spec
// D1): the IdP link must not turn a correct code into a refusal.
func TestCompleteAfterFactor_IdpLinkWithVerifiedTOTPCompletes(t *testing.T) {
	c, finalized := afterFactorZitadel(t, `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_IDP","AUTHENTICATION_METHOD_TYPE_TOTP"]`)
	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, got.Outcome, "password + IdP + verified TOTP was not completed: %v/%v", got.Outcome, got.Reason)
	require.NotEmpty(t, got.CallbackURL)
	require.True(t, finalized.Load(), "finalize was not called after a verified TOTP on a password + IdP + TOTP account")
}

// The uncollectible check CompleteAfterFactor re-runs (#867 Finding 2) is
// untouched by the IdP link: OTP_EMAIL alongside it still refuses and still
// never finalizes (#950, spec D1).
func TestCompleteAfterFactor_IdpLinkDoesNotExcuseAnUnsupportedFactor(t *testing.T) {
	c, finalized := afterFactorZitadel(t, `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_IDP","AUTHENTICATION_METHOD_TYPE_TOTP","AUTHENTICATION_METHOD_TYPE_OTP_EMAIL"]`)
	got, err := c.CompleteAfterFactor(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, got.Outcome)
	require.Equal(t, RefusalFactorUnsupported, got.Reason)
	require.Contains(t, got.EnrolledMethods, "AUTHENTICATION_METHOD_TYPE_OTP_EMAIL")
	require.False(t, finalized.Load(), "finalize was called with OTP_EMAIL enrolled: the IdP link must not excuse an uncollectible factor")
}

// RegisterTOTP reads the user id off the session and decodes Zitadel's
// {uri, secret} answer (#948, spec D2). The fixture is the wire shape
// observed live 2026-10-08.
func TestRegisterTOTPDecodesURIAndSecret(t *testing.T) {
	var registered atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/users/u1/totp":
			registered.Store(true)
			w.Write([]byte(`{"details":{"sequence":"4"},"uri":"otpauth://totp/ZITADEL:x@helivanta.dev?secret=ABCD&issuer=ZITADEL","secret":"ABCD"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.RegisterTOTP(context.Background(), "1")
	require.NoError(t, err)
	require.True(t, registered.Load(), "POST /v2/users/{id}/totp was not called")
	require.Equal(t, "ABCD", got.Secret)
	require.Equal(t, "otpauth://totp/ZITADEL:x@helivanta.dev?secret=ABCD&issuer=ZITADEL", got.URI)
}

// A 200 without a usable uri/secret, and a 409 (verified TOTP already
// exists, COMMAND-do9se), are both retryable failures, never an enrolment
// with a blank secret.
func TestRegisterTOTPFailsClosedOnBadAnswers(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"missing secret": func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"uri":"otpauth://x"}`)) },
		"409 already set up": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"code":6,"message":"already set up","details":[{"id":"COMMAND-do9se"}]}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v2/sessions/") {
					w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
					return
				}
				handler(w, r)
			}))
			t.Cleanup(srv.Close)
			c := New(srv.URL, "pat", srv.Client())
			got, err := c.RegisterTOTP(context.Background(), "1")
			require.ErrorIs(t, err, ErrUnavailable)
			require.Empty(t, got.Secret)
		})
	}
}

// VerifyTOTPEnrollment posts the code to /totp/verify (NOT /totp/_verify,
// which 404s — MFA spike §5) and maps a 400 to ErrBadCredentials so the
// handler can answer it as a wrong code (#948, spec D3).
func TestVerifyTOTPEnrollmentPostsCodeAndMapsWrongCode(t *testing.T) {
	var gotBody string
	wrong := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/users/u1/totp/verify":
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			if wrong {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"code":3,"message":"Invalid code (EVENT-8isk2)","details":[{"id":"EVENT-8isk2"}]}`))
				return
			}
			w.Write([]byte(`{"details":{"sequence":"6"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	require.NoError(t, c.VerifyTOTPEnrollment(context.Background(), "1", "123456"))
	require.JSONEq(t, `{"code":"123456"}`, gotBody)

	wrong = true
	err := c.VerifyTOTPEnrollment(context.Background(), "1", "000000")
	require.ErrorIs(t, err, ErrBadCredentials)
}
