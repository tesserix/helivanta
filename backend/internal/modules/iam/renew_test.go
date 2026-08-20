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
	// renewTestTTL and renewTestIdleTimeout are deliberately DIFFERENT
	// values (15m vs 20m) so a test that asserts something is derived
	// from one cannot pass by accident against the other.
	renewTestTTL         = 15 * time.Minute
	renewTestIdleTimeout = 20 * time.Minute
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
// matters here specifically — testutil.Do only ever sets an Authorization
// header, never a cookie, while resolveIdleDeadline (login.go, shared
// verbatim with renew.go) reads the idle_deadline carry-forward
// EXCLUSIVELY from c.Cookie(authn.SessionCookie). A harness that could
// only send bearer tokens could never exercise the carry-forward branch
// this file's most important test is about.
type renewEnv struct {
	r        *gin.Engine
	signer   *session.Signer
	verifier *session.Verifier
}

func newRenewEnv(t *testing.T, roles stubRoleLister, userState *stubUserState) *renewEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, renewTestKID, renewTestIssuer, renewTestTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, renewTestKID, renewTestIssuer)
	require.NoError(t, err)

	h := newRenewalHandlers(verifier, signer, roles, userState, renewTestTTL, renewTestIdleTimeout, true)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Same shape as bootstrap.V1Chain: authn first (cookie verification,
	// #781 revocation, idle-deadline gate — ALL of which must run before
	// renewalHandlers.renew, see that method's own doc comment), then
	// requestid.PrincipalMiddleware() so requestid.Logger(c) inside the
	// handler carries subject/tenant like it would in production.
	r.Use(requestid.Middleware())
	r.Use(authn.Middleware(authn.NewSessionVerifier(verifier), renewNeverRevoked{}))
	r.Use(requestid.PrincipalMiddleware())
	r.POST("/v1/auth/renew", h.renew)

	return &renewEnv{r: r, signer: signer, verifier: verifier}
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

func doRenew(r *gin.Engine, cookie string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/renew", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: cookie})
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
	env := newRenewEnv(t, roles, userState)

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
	env := newRenewEnv(t, roles, userState)

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
	env := newRenewEnv(t, roles, userState)

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

// --- spec D3: fail closed on the Zitadel state check ------------------

func TestRenewRefusesInactiveZitadelSubject(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{states: map[string]loginclient.UserState{"user-1": loginclient.UserStateInactive}}
	env := newRenewEnv(t, roles, userState)

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
	env := newRenewEnv(t, roles, userState)

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
	env := newRenewEnv(t, roles, userState)

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
	env := newRenewEnv(t, roles, userState)

	w := doRenew(env.r, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, ok := renewSessionCookie(w)
	require.False(t, ok)
	require.Equal(t, 0, userState.calls, "must never reach the handler with no credential presented at all")
}

// --- spec D5: the next-renew hint is derived from this server's TTL ---

func TestRenewResponseRenewAtIsDerivedFromSessionTTL(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	userState := &stubUserState{}
	env := newRenewEnv(t, roles, userState)

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
	// The literal /3 here is deliberate, not renewalFraction: comparing
	// against the implementation's own constant would let this assertion
	// pass no matter what that constant was changed to, since both sides
	// would move together. A literal pins the documented ratio (renew.go's
	// renewalFraction doc comment) independently of the code under test.
	want := before.Add(renewTestTTL / 3)
	require.WithinDuration(t, want, body.RenewAt, 5*time.Second,
		"renew_at must be derived from THIS SERVER'S OWN SessionTTL (%v/3), not a client-invented constant",
		renewTestTTL)
}
