package iam

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

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
		"the missing-cookie refusal must happen before any Zitadel call, not after")
}

// TestRenewIgnoresAStaleCookieWhenBearerAuthenticates closes the related
// case the reviewer named: a cookie for tenant A alongside a bearer
// header for tenant B, same subject. authn.Middleware authenticates from
// the bearer (its own preference order), so the principal is tenant B's
// — the re-minted session must carry B's OWN deadline, never inherit
// A's stale cookie value. This is what proves the fix is "read the
// deadline off the already-verified principal", not merely "require a
// cookie header to be present somewhere on the request".
func TestRenewIgnoresAStaleCookieWhenBearerAuthenticates(t *testing.T) {
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

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	newCookie, ok := renewSessionCookie(w)
	require.True(t, ok)
	claims, err := env.verifier.Verify(newCookie)
	require.NoError(t, err)
	require.Equal(t, renewTestTenantB, claims.TenantID,
		"must renew the BEARER-authenticated tenant (the one that actually authenticated the request), not the stale cookie's")
	require.True(t, claims.IdleDeadline.Equal(freshB),
		"must carry forward tenant B's own deadline, not tenant A's stale cookie value")
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

// TestRenewAtHasAFloorForATinySessionTTL is the covering test for Review
// Round 1's "Also fix" floor: SESSION_TTL has no boot-time floor
// (config.go), so a plausible typo (SESSION_TTL=3s for "3m") must not
// leave renew_at effectively un-throttled — every connected client
// would otherwise poll this endpoint, and therefore Zitadel's core API,
// roughly once a second, forever.
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
