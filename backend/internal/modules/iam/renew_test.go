package iam

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/session"
)

// renewTestTenantA/B mirror testutil.TenantA/TenantB's values. This file
// deliberately does not import internal/testutil, for the same
// import-cycle reason signout_test.go's own tenant constants give:
// testutil imports internal/bootstrap, which imports this very package
// in production code.
const (
	renewTestTenantA = "11111111-1111-1111-1111-111111111111"
	renewTestTenantB = "22222222-2222-2222-2222-222222222222"
	renewTestKID     = "hms-session-renew-test"
	renewTestIssuer  = "https://hms.test"
	renewTestTTL     = 15 * time.Minute
)

// renewNeverRevoked stands in for authn.RevocationChecker; revocation is
// pkg/authn's own concern (#781), not what this file tests.
type renewNeverRevoked struct{}

func (renewNeverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

// stubUserState answers UserStateChecker.UserState from a fixed
// per-subject map, defaulting an unlisted subject to UserStateActive so
// a test that isn't specifically about this check doesn't have to
// declare it. Set err to force the fail-closed "unreadable" path — a
// stand-in for Task 1's *loginclient.Client hitting a transport error or
// a 5xx.
type stubUserState struct {
	states map[string]loginclient.UserState
	err    error
	calls  int
}

func (s *stubUserState) UserState(_ context.Context, id string) (loginclient.UserState, error) {
	s.calls++
	if s.err != nil {
		return loginclient.UserState(0), s.err
	}
	if st, ok := s.states[id]; ok {
		return st, nil
	}
	return loginclient.UserStateActive, nil
}

// renewEnv wires a real session.Signer/Verifier pair (so a renewal's
// re-minted cookie can actually be decoded and asserted on, not just
// checked non-empty) around renewalHandlers, mounted directly behind
// authn.Middleware — the SAME chain POST /v1/auth/renew sits behind in
// production (bootstrap.V1Chain + Module.registerRenewal), minus the DB
// and network calls neither renewalHandlers nor this file need. Built by
// hand rather than through testutil.NewHarness for the same reason
// signout_test.go's harness is: this package cannot import
// internal/testutil without an import cycle, and — the reason that
// matters most here — testutil.Do only ever sets an Authorization
// header, never a cookie, and this file's whole point (Review Round 1)
// is distinguishing cookie-authenticated from bearer-authenticated
// requests, which a harness that can only send one of the two shapes
// could never do.
//
// h is exposed directly (not just r) so tests can inject h.now for an
// EXACT RenewAt assertion — see TestRenewResponseRenewAtIsExactlyTTLOverThreeFromInjectedClock.
type renewEnv struct {
	r        *gin.Engine
	signer   *session.Signer
	verifier *session.Verifier
	h        *renewalHandlers
}

// newRenewEnv takes ttl explicitly (rather than a fixed constant) so
// TestRenewAtHasAFloorForATinySessionTTL can exercise a
// plausible-typo-sized SESSION_TTL without a second harness.
func newRenewEnv(t *testing.T, roles stubRoleLister, userState *stubUserState, ttl time.Duration) *renewEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, renewTestKID, renewTestIssuer, ttl)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, renewTestKID, renewTestIssuer)
	require.NoError(t, err)

	h := newRenewalHandlers(signer, roles, userState, ttl, true)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Same shape as bootstrap.V1Chain: authn first (cookie OR bearer
	// verification, #781 revocation, idle-deadline gate — ALL of which
	// must run before renewalHandlers.renew, see that method's own doc
	// comment), then requestid.PrincipalMiddleware() so
	// requestid.Logger(c) inside the handler carries subject/tenant like
	// it would in production.
	r.Use(requestid.Middleware())
	r.Use(authn.Middleware(authn.NewSessionVerifier(verifier), renewNeverRevoked{}))
	r.Use(requestid.PrincipalMiddleware())
	r.POST("/v1/auth/renew", h.renew)

	return &renewEnv{r: r, signer: signer, verifier: verifier, h: h}
}

// mint is a shorthand for signing a session cookie directly with the
// env's own signer — standing in for a prior login or renewal, without
// needing to drive POST /v1/auth/login to produce one.
func (e *renewEnv) mint(t *testing.T, subject, tenantID string, authTime, idleDeadline time.Time) string {
	t.Helper()
	tok, err := e.signer.Mint(subject, tenantID, authTime, idleDeadline)
	require.NoError(t, err)
	return tok
}

// doRenew sends the token as the helivanta_session cookie — the shape
// this endpoint is defined to serve.
func doRenew(r *gin.Engine, cookie string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/renew", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: cookie})
	}
	r.ServeHTTP(w, req)
	return w
}

// doRenewBearer sends the token as an Authorization: Bearer header
// instead of a cookie — the shape Review Round 1's CRITICAL finding
// proved this endpoint must NOT serve.
func doRenewBearer(r *gin.Engine, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/renew", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

// renewSessionCookie extracts the Helivanta session cookie value from a
// renewal response, reporting whether one was set at all — several
// tests below assert a REFUSAL sets none.
func renewSessionCookie(w *httptest.ResponseRecorder) (string, bool) {
	for _, c := range w.Result().Cookies() {
		if c.Name == authn.SessionCookie {
			return c.Value, true
		}
	}
	return "", false
}

// --- happy path ------------------------------------------------------

func TestRenewHappyPathReMintsAndSetsCookie(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	deadline := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	newCookie, ok := renewSessionCookie(w)
	require.True(t, ok, "a successful renewal must set a new session cookie")

	claims, err := env.verifier.Verify(newCookie)
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.Subject)
	require.Equal(t, renewTestTenantA, claims.TenantID)
	require.True(t, authTime.Equal(claims.AuthTime),
		"auth_time must be carried through unchanged, got %v want %v", claims.AuthTime, authTime)

	var body struct {
		TenantID string    `json:"tenant_id"`
		RenewAt  time.Time `json:"renew_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, renewTestTenantA, body.TenantID)
	require.False(t, body.RenewAt.IsZero(), "the response must tell the client when to renew next")
	require.Equal(t, 1, userState.calls, "renewal must re-check zitadel state on every call")
}

// --- spec D3/D4: idle_deadline is carried forward, never refreshed ---

// TestRenewCarriesIdleDeadlineForwardExactly is THIS endpoint's version
// of TestRenewalDoesNotMoveTheIdleDeadline (login_test.go): an untouched
// tab calling POST /v1/auth/renew every few minutes must not move its own
// idle window, or the #848 timeout never fires. The deadline minted below
// is deliberately not a round number (9m17s out, not 10m or 15m) so a bug
// that recomputes "now + idleTimeout" instead of carrying the old value
// through is unmistakable rather than accidentally close.
func TestRenewCarriesIdleDeadlineForwardExactly(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	original := time.Now().Add(9*time.Minute + 17*time.Second).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, original)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	newCookie, ok := renewSessionCookie(w)
	require.True(t, ok)

	claims, err := env.verifier.Verify(newCookie)
	require.NoError(t, err)
	require.True(t, claims.IdleDeadline.Equal(original),
		"renewal must carry idle_deadline forward UNCHANGED (got %v, want %v): "+
			"an untouched tab would renew itself forever and the #848 timeout would never fire",
		claims.IdleDeadline, original)
}

// TestRenewRefusesLapsedIdleDeadlineWithoutResurrectingIt proves the
// design brief's first refusal condition — "past idle_deadline" — AND
// that it is satisfied by this route's PLACEMENT inside the authenticated
// chain (authn.Middleware), not by code inside renewalHandlers.renew:
// userState.calls stays at 0, meaning the handler is never even invoked
// for an idle-expired session.
func TestRenewRefusesLapsedIdleDeadlineWithoutResurrectingIt(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Second)
	lapsed := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	stale := env.mint(t, "user-1", renewTestTenantA, authTime, lapsed)

	w := doRenew(env.r, stale)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok, "no session may be re-issued for an idle-expired session")
	require.Equal(t, 0, userState.calls,
		"an idle-expired session must be refused by authn.Middleware before the handler ever asks Zitadel")
}

// --- Review Round 1 CRITICAL: the cookie IS the credential -------------

// TestRenewViaBearerHeaderOnlyIsRefused is the covering test for Review
// Round 1's CRITICAL finding: authn.Middleware authenticates from
// Authorization: Bearer in PREFERENCE to the cookie, and accepts a
// Helivanta session JWT there too — so, before the fix, presenting a
// session token as a bearer header (no cookie at all) still produced a
// valid authn.Principal, and the OLD carry-forward logic (which
// independently re-read idle_deadline from c.Cookie) found no cookie and
// silently fell through to a FRESH now+idleTimeout window. Repeated on a
// timer, a single exfiltrated token — the "stolen cookie" case
// pkg/authn/authn.go's idle-timeout comment names explicitly — never
// expired. This test sends the token ONLY as a bearer header, with an
// idle window nearly out, and must see an outright refusal: the session
// cookie is this endpoint's credential by definition, and a bearer-only
// request is a shape it must not serve at all, fresh window or not.
func TestRenewViaBearerHeaderOnlyIsRefused(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	// Only 1 minute of idle window left: the exact shape the reviewer's
	// live probe used ("original idle_deadline=00:58:45 ... moved=19m0s").
	almostOut := time.Now().Add(1 * time.Minute).UTC().Truncate(time.Second)
	token := env.mint(t, "user-1", renewTestTenantA, authTime, almostOut)

	w := doRenewBearer(env.r, token)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok,
		"a bearer-only renewal must not be served at all: the session cookie IS this endpoint's credential, "+
			"and serving it (even without moving the deadline) is still the wrong answer")
	require.Equal(t, 0, userState.calls,
		"the bearer-only refusal must happen before any Zitadel call, not after")
}

// TestRenewRefusesABearerHeaderEvenAlongsideALiveCookie is the structural
// half of the shape control, and it is the assertion the final
// whole-branch review demanded be inverted.
//
// It used to assert a 200. The reasoning then was that a bearer-
// authenticated renewal was ACCEPTABLE so long as it carried the
// bearer principal's own deadline forward rather than minting a fresh
// window — which is true as far as correctness goes, and is still
// pinned by TestRenewCarriesIdleDeadlineForwardExactly below. But it
// meant the refusal one function up (TestRenewViaBearerHeaderOnlyIsRefused)
// rested on nothing but the ABSENCE of a cookie header, and the
// reviewer proved that a caller only had to attach any junk cookie
// alongside the bearer token to be served a re-minted session:
//
//	Authorization: Bearer <token>
//	Cookie: helivanta_session=not-a-token-at-all
//	=> 200, Set-Cookie: helivanta_session=<fresh>
//
// So renew.go now refuses on the bearer header itself, and this test
// asserts that refusal on the harder input: a bearer for tenant B
// alongside a GENUINE, still-live cookie for tenant A, same subject.
// authn.Middleware prefers the bearer (its own preference order), so
// this request authenticates perfectly well — and is still refused,
// because a bearer-authenticated renewal is not a shape this endpoint
// serves. Nothing about the cookie's validity rescues it.
func TestRenewRefusesABearerHeaderEvenAlongsideALiveCookie(t *testing.T) {
	roles := stubRoleLister{"user-1": {
		{TenantID: renewTestTenantA, Role: authz.RoleNurse},
		{TenantID: renewTestTenantB, Role: authz.RoleNurse},
	}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	almostOutA := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	cookieA := env.mint(t, "user-1", renewTestTenantA, authTime, almostOutA)
	freshB := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	bearerB := env.mint(t, "user-1", renewTestTenantB, authTime, freshB)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/renew", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: cookieA})
	req.Header.Set("Authorization", "Bearer "+bearerB)
	env.r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok,
		"a request carrying Authorization: Bearer must be refused outright — a present cookie header "+
			"says nothing about WHICH credential authenticated the request, which is exactly the hole "+
			"a cookie-presence check left open")
	require.Equal(t, 0, userState.calls,
		"the shape refusal must happen before any Zitadel call, not after")
}

// TestRenewOnTheCookiePathStillCarriesTheDeadlineForward is the other
// half, kept deliberately beside the refusal above: closing the bearer
// shape must not be mistaken for what makes this endpoint safe. The
// CORRECTNESS control is that the re-mint takes idle_deadline off the
// already-verified principal, so the window can never be extended by
// renewing — and that has to keep holding on the one path this endpoint
// does serve, with the same "nearly out" deadline the reviewer's probe
// used. TestRenewCarriesIdleDeadlineForwardExactly asserts the same
// property on a comfortable deadline; this one pins it at the boundary
// where extending it would be most valuable to an attacker.
func TestRenewOnTheCookiePathStillCarriesTheDeadlineForward(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	almostOut := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, almostOut)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	newCookie, ok := renewSessionCookie(w)
	require.True(t, ok)
	claims, err := env.verifier.Verify(newCookie)
	require.NoError(t, err)
	require.Equal(t, renewTestTenantA, claims.TenantID)
	require.True(t, claims.IdleDeadline.Equal(almostOut),
		"the re-minted session must carry the SAME idle_deadline (%s), not a fresh window — got %s",
		almostOut, claims.IdleDeadline)
}

// --- spec D3: fail closed on the Zitadel state check ------------------

func TestRenewRefusesInactiveZitadelSubject(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{states: map[string]loginclient.UserState{"user-1": loginclient.UserStateInactive}}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	deadline := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok, "no session may be re-issued for a subject Zitadel reports inactive")
}

// TestRenewFailsClosedWhenZitadelStateIsUnreadable is spec D3's central
// proof: "an unreadable answer is not evidence of 'active' any more than
// it is evidence of 'inactive'" — a transient Zitadel failure must refuse
// the renewal, not grant it.
func TestRenewFailsClosedWhenZitadelStateIsUnreadable(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{err: errors.New("zitadel unreachable")}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	deadline := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok, "an unreadable zitadel answer must FAIL CLOSED: no session may be re-issued")
}

// --- spec D4 point 2: membership is re-checked every renewal ----------

// TestRenewRefusesSinceRevokedMember mirrors
// TestLogin_RenewalForSinceRevokedMemberIsRefused (login_test.go:297-330):
// this endpoint must re-check OpenFGA membership on EVERY call, not trust
// the cookie's own tenant_id claim. user-1 keeps an unrelated membership
// in tenant B so bindings stays non-empty, isolating the per-tenant check
// (hasBindingForTenant) from the separate "no accessible tenant anywhere"
// gate.
func TestRenewRefusesSinceRevokedMember(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	deadline := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	// Membership in tenant A revoked out from under user-1 between mint
	// and renewal — the iam-fga-sync consumer applying a member_revoked
	// event, from this handler's point of view.
	roles["user-1"] = []authz.RoleBinding{{TenantID: renewTestTenantB, Role: authz.RoleNurse}}

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok, "no session may be re-issued for a since-revoked member")
}

// --- no credential at all ---------------------------------------------

func TestRenewWithNoCookieIsRefused(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	w := doRenew(env.r, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok)
	require.Equal(t, 0, userState.calls, "must never reach the handler with no credential presented at all")
}

// --- spec D5: the next-renew hint is derived from this server's TTL ---

// TestRenewResponseRenewAtIsExactlyTTLOverThreeFromInjectedClock uses
// renewalHandlers.now (Review Round 1, "Also fix": either use the
// injectable clock or drop it) to assert an EXACT RenewAt value, rather
// than one within a tolerance window — the same reason
// LoginHandlers.now/loginEnv.clock exist in login_test.go.
func TestRenewResponseRenewAtIsExactlyTTLOverThreeFromInjectedClock(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState, renewTestTTL)

	// Close to real time (not an arbitrary past date): idle_deadline
	// travels as a claim authn.Middleware checks against the REAL clock,
	// so a fixedNow far in the past would make the minted session look
	// already idle-expired before the handler ever runs.
	fixedNow := time.Now().UTC().Truncate(time.Second)
	env.h.now = func() time.Time { return fixedNow }

	authTime := fixedNow.Add(-2 * time.Minute)
	deadline := fixedNow.Add(10 * time.Minute)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	w := doRenew(env.r, current)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		RenewAt time.Time `json:"renew_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// The literal /3 here is deliberate, not renewalFraction: comparing
	// against the implementation's own constant would let this assertion
	// pass no matter what that constant was changed to, since both sides
	// would move together. A literal pins the documented ratio (renew.go's
	// renewalFraction doc comment) independently of the code under test.
	want := fixedNow.Add(renewTestTTL / 3)
	require.True(t, want.Equal(body.RenewAt),
		"renew_at must be EXACTLY now+ttl/3 off the injected clock (got %v, want %v)", body.RenewAt, want)
}

// TestRenewAtHasAFloorForATinySessionTTL covers renewAtFor's own floor.
// cmd/api can no longer boot with a TTL this small —
// config.RequireSessionTTL refuses anything under config.MinSessionTTL
// (#921) — so this is defence in depth: renewAtFor's contract is not to
// answer an un-throttled interval for ANY ttl it is handed, independent
// of how one binary happens to validate its configuration.
func TestRenewAtHasAFloorForATinySessionTTL(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	tinyTTL := 3 * time.Second // 3s/3 = 1s, well under renewAtFloor
	env := newRenewEnv(t, roles, userState, tinyTTL)

	authTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	deadline := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	current := env.mint(t, "user-1", renewTestTenantA, authTime, deadline)

	before := time.Now()
	w := doRenew(env.r, current)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		RenewAt time.Time `json:"renew_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	want := before.Add(renewAtFloor)
	require.WithinDuration(t, want, body.RenewAt, 5*time.Second,
		"renew_at must be floored at %v for a too-small SESSION_TTL (tinyTTL/3 = 1s here), "+
			"not left at an un-throttled ttl/3 value", renewAtFloor)
}

// The coupling config.MinSessionTTL's doc comment describes, enforced at
// COMPILE time (#921): the minimum is derived as renewalFraction ×
// renewAtFloor, and internal/config cannot import this package to state
// that itself. If renewAtFloor or renewalFraction is raised without
// raising config.MinSessionTTL, the constant below goes negative and
// converting it to uint64 is a compile error — so this test file, and
// make lint-go's type-check of it, refuse to build.
const _ = uint64(config.MinSessionTTL/renewalFraction - renewAtFloor)

// TestRenewAtAtMinimumSessionTTLIsProportional is the runtime half of the
// assertion above: at the smallest TTL cmd/api will boot with, renewAtFor
// answers its DESIGNED value, TTL/renewalFraction, and the floor does not
// engage. That is the whole reason the minimum is where it is.
func TestRenewAtAtMinimumSessionTTLIsProportional(t *testing.T) {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

	got := renewAtFor(now, config.MinSessionTTL)

	require.Equal(t, now.Add(config.MinSessionTTL/renewalFraction), got)
	require.True(t, got.Before(now.Add(config.MinSessionTTL)),
		"the first renewal must be scheduled before the session it renews expires")
}

// --- #916 Task 4, F3: the login/renew schedule coupling ------------------

// fixedClockVerifier is a TokenVerifier that accepts one canned raw
// token and answers with one canned Principal — enough to drive
// LoginHandlers.Login without a Zitadel, which is all the test below
// needs.
type fixedClockVerifier struct {
	subject  string
	authTime time.Time
}

func (v fixedClockVerifier) Verify(_ context.Context, _ string) (authn.Principal, error) {
	return authn.Principal{Subject: v.subject, AuthTime: v.authTime}, nil
}

// TestLoginAndRenewAgreeOnRenewAt is what makes spec D5's coupling
// STRUCTURAL rather than merely parallel (#916 Task 4, F3).
//
// D5's claim is that the browser renews on the SERVER's schedule rather
// than on a constant it invented. That held for renewals 2..n — POST
// /v1/auth/renew has always answered with `renew_at` — and was simply
// absent for renewal 1: POST /v1/auth/login returned only `tenant_id`,
// so apps/shell seeded its first timer from a hardcoded five-minute
// client constant. Any SESSION_TTL below ~5 minutes therefore expired
// every session before its first renewal, silently, and no test ran a
// short one. (config.RequireSessionTTL, #921, refuses only a TTL under
// config.MinSessionTTL — 90s — so 90s..5m is still a bootable range.)
//
// Login now answers with `renew_at` too. The risk that replaces the old
// one is DRIFT: two endpoints computing "when to renew next" that agree
// today and diverge the first time renewalFraction or renewAtFloor is
// tuned on one side only. So this test does not check that Login's value
// looks plausible, or that it equals a formula restated here — either
// would keep passing through exactly that drift. It drives BOTH REAL
// HANDLERS against ONE frozen clock and ONE TTL and asserts the two
// wire-format values are byte-identical.
//
// It can fail: point either handler at its own copy of the arithmetic
// and change one constant, and this goes red. Proven by mutation while
// writing it — see the task report.
func TestLoginAndRenewAgreeOnRenewAt(t *testing.T) {
	// Deliberately NOT time.Now(): a frozen, shared clock is the whole
	// point — two handlers reading real wall time would produce values
	// that differ by microseconds and force a tolerance window, and a
	// tolerance window is precisely what would hide a drift of less than
	// the tolerance.
	frozen := time.Date(2026, 8, 20, 4, 5, 6, 0, time.UTC)
	clock := func() time.Time { return frozen }

	// A TTL whose third is comfortably above renewAtFloor, so this test
	// pins the PROPORTIONAL branch of renewAtFor. The floor branch is
	// covered separately by TestRenewAtHasAFloorForATinySessionTTL.
	const ttl = 21 * time.Minute

	subject := "user-renew-at-agreement"
	authTime := frozen.Add(-time.Minute)

	// --- The renewal endpoint's answer ---
	roles := stubRoleLister{subject: {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	env := newRenewEnv(t, roles, &stubUserState{}, ttl)
	env.h.now = clock
	// The idle deadline comes from REAL wall time, not from `frozen`:
	// authn.Middleware's idle gate reads time.Now() and cannot be
	// injected, so a deadline derived from the frozen instant is only in
	// the future while real time happens to be inside `frozen`..`frozen+1h`.
	// It was — this test was written at 2026-08-20 ~04:10 UTC — and the
	// suite then started failing with "precondition: the renewal itself
	// must succeed" (401) the moment real time passed 05:05:06 UTC, from a
	// clock rather than from any change. The frozen clock is injected for
	// exactly one purpose (env.h.now, which is what renewAtFor reads) and
	// must not be reused for values a real, uninjectable clock judges.
	cookie := env.mint(t, subject, renewTestTenantA, authTime, time.Now().Add(time.Hour))
	renewRes := doRenew(env.r, cookie)
	require.Equal(t, http.StatusOK, renewRes.Code, "precondition: the renewal itself must succeed")

	var renewBody struct {
		RenewAt   string `json:"renew_at"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(renewRes.Body.Bytes(), &renewBody))
	require.NotEmpty(t, renewBody.RenewAt, "precondition: renew must actually emit renew_at")

	// --- The login endpoint's answer, same clock, same TTL ---
	// Its own key pair: Login only has to MINT here, and nothing in this
	// test decodes its cookie — the assertion is on the response body.
	loginPub, loginPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	loginSigner, err := session.NewSigner(loginPriv, renewTestKID, renewTestIssuer, ttl)
	require.NoError(t, err)
	loginVerifier, err := session.NewVerifier(loginPub, renewTestKID, renewTestIssuer)
	require.NoError(t, err)

	loginHandlers := NewLoginHandlers(LoginDeps{
		Zitadel:      fixedClockVerifier{subject: subject, authTime: authTime},
		Roles:        roles,
		Signer:       loginSigner,
		Sessions:     loginVerifier,
		TTL:          ttl,
		IdleTimeout:  time.Hour,
		SecureCookie: true,
		Now:          clock,
	})
	gin.SetMode(gin.TestMode)
	lr := gin.New()
	lr.POST("/v1/auth/login", loginHandlers.Login)

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"id_token":"any-token-the-fake-verifier-accepts"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	lr.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code,
		"precondition: the login itself must succeed; body=%s", loginRec.Body.String())

	var loginBody struct {
		RenewAt   string `json:"renew_at"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginBody))

	// Byte-identical on the WIRE, not merely equal as parsed time.Time:
	// the client reads the string, so a formatting divergence (a
	// different location, a truncated precision) is a real divergence
	// even when the instants match.
	require.Equal(t, renewBody.RenewAt, loginBody.RenewAt,
		"POST /v1/auth/login and POST /v1/auth/renew must answer the SAME renew_at for the "+
			"same clock and the same SESSION_TTL — they share renewAtFor precisely so a change "+
			"to renewalFraction or renewAtFloor cannot be applied to one and forgotten on the other")

	// expires_at (#941) is held to the same standard, and to its value:
	// frozen + ttl, whole seconds.
	require.Equal(t, renewBody.ExpiresAt, loginBody.ExpiresAt,
		"login and renew must answer the same expires_at for the same clock and SESSION_TTL")
	require.Equal(t, frozen.Add(ttl).UTC().Format(time.RFC3339Nano), renewBody.ExpiresAt)
}

// TestRenewExpiresAtIsNeverAfterTheCookiesExp pins expiresAtFor's safety
// claim against a REAL minted cookie on the real clock (#941): the browser
// bounds its retry cadence by expires_at, so a value later than the token's
// actual exp would let it wait past expiry. The token's exp is a JWT
// NumericDate truncated to whole seconds; an untruncated now+ttl with a
// sub-second part lands after it. Run repeatedly so a sub-second now is all
// but certain to occur.
func TestRenewExpiresAtIsNeverAfterTheCookiesExp(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	env := newRenewEnv(t, roles, &stubUserState{}, renewTestTTL)
	for i := range 20 {
		cookie := env.mint(t, "user-1", renewTestTenantA, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
		w := doRenew(env.r, cookie)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var body struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		claims, err := env.verifier.Verify(renewedCookieValue(t, w))
		require.NoError(t, err)

		require.False(t, body.ExpiresAt.After(claims.ExpiresAt),
			"iteration %d: expires_at %s is after the token's exp %s", i, body.ExpiresAt, claims.ExpiresAt)
		require.WithinDuration(t, claims.ExpiresAt, body.ExpiresAt, 2*time.Second)
		time.Sleep(37 * time.Millisecond)
	}
}

func renewedCookieValue(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == authn.SessionCookie {
			return c.Value
		}
	}
	t.Fatal("renew set no session cookie")
	return ""
}
