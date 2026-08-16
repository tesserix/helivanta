package iam_test

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

	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/ratelimit"
	"github.com/tesserix/hms/pkg/session"
)

const (
	loginTestIssuer = "https://hms.test"
	loginTestKID    = "hms-session-v1"
	loginTestTTL    = 15 * time.Minute

	tenantA = testutil.TenantA
	tenantB = testutil.TenantB
)

// fakeZitadelVerifier stands in for authn.NewZitadelVerifier: raw ==
// "good" verifies to the fixed principal below, anything else fails.
// Verifying real OIDC discovery/JWKS/RS256 is pkg/authn/zitadel_test.go's
// job; this test's job is what Login does ONCE it holds a verified
// Principal — resolving membership and minting, or refusing to.
type fakeZitadelVerifier struct {
	subject  string
	authTime time.Time
}

func (f fakeZitadelVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	if raw != "good" {
		return authn.Principal{}, errors.New("invalid zitadel token")
	}
	return authn.Principal{Subject: f.subject, AuthTime: f.authTime}, nil
}

// fakeRoleListerLogin mirrors fakeRoleLister in me_test.go (kept
// separate/renamed to avoid a name collision within the same test
// binary/package) — a static subject -> bindings map standing in for
// OpenFGA's ListRoles, the exact call meHandlers.tenants and
// meHandlers.switchTenant already use.
type fakeRoleListerLogin struct {
	bindings map[string][]authz.RoleBinding
	err      error
	calls    int
}

func (f *fakeRoleListerLogin) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.bindings[subject], nil
}

// loginHarness wires a real session.Signer/Verifier pair (so minted
// tokens can actually be checked, not just asserted non-empty) around a
// fakeZitadelVerifier and fakeRoleListerLogin, exactly mirroring what
// cmd/api/main.go wires in production minus the network calls. limiter is
// nil — every test in this file except the #841 rate-limit tests below is
// about what Login does once it holds a verified Principal, not about the
// budget, and a nil limiter fails open (Login admits unconditionally),
// which is exactly "no rate limiting" for these tests' purposes.
func loginHarness(t *testing.T, zv authn.TokenVerifier, roles *fakeRoleListerLogin) (*gin.Engine, *session.Verifier) {
	t.Helper()
	return loginHarnessRL(t, zv, roles, nil, ratelimit.Rule{})
}

// loginHarnessRL is loginHarness plus an explicit limiter/rule, for the
// #841 rate-limit tests that need to control the budget directly.
func loginHarnessRL(t *testing.T, zv authn.TokenVerifier, roles *fakeRoleListerLogin, limiter ratelimit.Limiter, rule ratelimit.Rule) (*gin.Engine, *session.Verifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, loginTestKID, loginTestIssuer, loginTestTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, loginTestKID, loginTestIssuer)
	require.NoError(t, err)

	// Sessions is the SAME verifier the assertions below decode minted
	// cookies with, exactly as cmd/api/main.go passes the SAME
	// sessionVerifier the /v1 chain uses: it is what lets Login recognise
	// its own previously-minted session on a renewal (spec D3).
	// IdleTimeout comes from config.Load() rather than a literal so these
	// tests exercise the real default (15m) and would follow it if it
	// ever changed.
	h := iam.NewLoginHandlers(iam.LoginDeps{
		Zitadel:      zv,
		Roles:        roles,
		Signer:       signer,
		Sessions:     verifier,
		TTL:          loginTestTTL,
		IdleTimeout:  config.Load().IdleTimeout,
		SecureCookie: true,
		Limiter:      limiter,
		Limit:        rule,
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Mounted directly on the engine, exactly as cmd/api/main.go mounts
	// it — outside any authn.Middleware/authz gate, because there is no
	// HMS session yet for either of those to check.
	r.POST("/v1/auth/login", h.Login)
	return r, verifier
}

func doLogin(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// sessionCookie extracts the HMS session cookie value from a login
// response, failing the test if it is not present — every successful
// login must set one.
func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == authn.SessionCookie {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie set; headers: %v", authn.SessionCookie, w.Header())
	return ""
}

// --- 1. a valid Zitadel token for a member yields a session with the
//        right subject, tenant and auth_time ---------------------------

func TestLogin_MintsSessionWithRightSubjectTenantAndAuthTime(t *testing.T) {
	authTime := time.Date(2026, 3, 4, 9, 30, 12, 0, time.UTC)
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, verifier := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: authTime}, roles)

	w := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, tenantA, body.TenantID)

	claims, err := verifier.Verify(sessionCookie(t, w))
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.Subject)
	require.Equal(t, tenantA, claims.TenantID)
	require.True(t, authTime.Equal(claims.AuthTime),
		"auth_time must be the Zitadel token's, got %v want %v", claims.AuthTime, authTime)
}

// --- 2. auth_time is the Zitadel token's, not the mint time -------------
//
// A separate, narrower test from #1 above: it uses an auth_time far
// enough in the past that a reset-to-now bug is unmistakable even under
// a coarse comparison, and it is the one MUTATION 1 (below) targets
// directly.

func TestLogin_AuthTimeIsNotMintTime(t *testing.T) {
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, verifier := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: longAgo}, roles)

	w := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	claims, err := verifier.Verify(sessionCookie(t, w))
	require.NoError(t, err)
	require.Equal(t, longAgo.Unix(), claims.AuthTime.Unix(),
		"auth_time must be carried through from the Zitadel token, not reset to the mint time")
}

// --- 3. a subject in no tenant is refused, without disclosing whether
//        the account exists ----------------------------------------------

func TestLogin_RefusesSubjectWithNoTenantMembership(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{}}
	r, _ := loginHarness(t, fakeZitadelVerifier{subject: "ghost", authTime: time.Now()}, roles)

	w := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Empty(t, sessionCookies(w), "no session cookie may be set on a refused login")
}

// TestLogin_RefusalBodyDoesNotDistinguishNoAccountFromNoMembership is the
// explicit non-disclosure proof: two subjects that ListRoles answers
// identically for (empty bindings, whether that is because HMS has never
// heard of the subject or because every membership was revoked — a
// distinction this handler structurally cannot see, since ListRoles is
// its only signal) must produce byte-identical response bodies. If a
// future change added a second lookup (e.g. "does this subject appear in
// iam_members at all") that branched the message, this test is what
// would catch it.
func TestLogin_RefusalBodyDoesNotDistinguishNoAccountFromNoMembership(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{}}

	r1, _ := loginHarness(t, fakeZitadelVerifier{subject: "never-seen", authTime: time.Now()}, roles)
	w1 := doLogin(t, r1, `{"id_token":"good"}`)

	r2, _ := loginHarness(t, fakeZitadelVerifier{subject: "revoked-everywhere", authTime: time.Now()}, roles)
	w2 := doLogin(t, r2, `{"id_token":"good"}`)

	require.Equal(t, http.StatusNotFound, w1.Code)
	require.Equal(t, w1.Code, w2.Code)
	require.Equal(t, w1.Body.String(), w2.Body.String(),
		"the refusal must not disclose which of the two cases occurred")
}

// --- 4. a subject is refused a tenant they are not a member of ----------

func TestLogin_RefusesRequestedTenantCallerIsNotMemberOf(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, _ := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	w := doLogin(t, r, `{"id_token":"good","tenant_id":"`+tenantB+`"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Empty(t, sessionCookies(w), "no session cookie may be set when the requested tenant is refused")
}

// TestLogin_HonoursExplicitTenantWhenCallerIsAMember proves the
// tenant_id field is not just validated-and-ignored: naming a real
// membership among several must select THAT tenant, not silently fall
// back to bindings[0].
func TestLogin_HonoursExplicitTenantWhenCallerIsAMember(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {
			{TenantID: tenantA, Role: authz.RoleNurse},
			{TenantID: tenantB, Role: authz.RoleDoctor},
		},
	}}
	r, verifier := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	w := doLogin(t, r, `{"id_token":"good","tenant_id":"`+tenantB+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	claims, err := verifier.Verify(sessionCookie(t, w))
	require.NoError(t, err)
	require.Equal(t, tenantB, claims.TenantID)
}

// --- 5. renewal (spec D4a) re-checks membership on every call, not only
//        at first login ---------------------------------------------------

// TestLogin_RenewalForSinceRevokedMemberIsRefused is the D4a proof: this
// endpoint IS the renewal mechanism (spec D4a — "renewal is the login
// exchange, run again with a fresh Zitadel token"; there is no separate
// renewal endpoint), so it must re-check membership on EVERY call, not
// only the first one a session is minted from. A first call succeeds
// while user-1 is a member of tenantA; membership is then revoked FROM
// TENANT A SPECIFICALLY, while user-1 keeps an unrelated membership in
// tenantB — bindings is non-empty, so this exercises hasBindingForTenant's
// per-tenant check, not the separate "len(bindings) == 0" no-account gate
// (an earlier version of this test cleared bindings to nil entirely and
// passed even with the per-tenant check removed, because the empty-bindings
// gate caught it first for the wrong reason; see the task report's
// mutation-testing section for the reproduction). A second call — same
// subject, same tenant_id, the shape of a browser's silent prompt=none
// renewal naming the tenant it is currently in — must be refused exactly
// like a first-time non-member of that tenant would be. If this endpoint
// only checked membership once (e.g. by caching the bindings, or by
// trusting a previously-issued session instead of the fresh Zitadel
// token), a revoked member would keep renewing indefinitely and the TTL
// bound spec D4/D4a promises would be false.
func TestLogin_RenewalForSinceRevokedMemberIsRefused(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, verifier := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	// The original sign-in: user-1 is a member of tenantA, and succeeds.
	w1 := doLogin(t, r, `{"id_token":"good","tenant_id":"`+tenantA+`"}`)
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())
	_, err := verifier.Verify(sessionCookie(t, w1))
	require.NoError(t, err)
	require.Equal(t, 1, roles.calls, "precondition: the first login consulted OpenFGA")

	// Membership in tenantA is revoked out from under user-1 between the
	// original sign-in and the renewal — the iam-fga-sync consumer
	// applying a member_revoked event, from this handler's point of view.
	// user-1 keeps an unrelated membership in tenantB, so bindings stays
	// non-empty: this isolates the per-tenant membership check from the
	// separate "no accessible tenant anywhere" gate above it.
	roles.bindings["user-1"] = []authz.RoleBinding{{TenantID: tenantB, Role: authz.RoleNurse}}

	// The silent renewal: the browser re-authenticated against Zitadel
	// (prompt=none) and posts a FRESH ID token — fakeZitadelVerifier
	// still accepts raw=="good" for this test's purposes, standing in for
	// a genuinely fresh Zitadel token — naming the SAME tenant (A) it was
	// last in, exactly as D4a describes.
	w2 := doLogin(t, r, `{"id_token":"good","tenant_id":"`+tenantA+`"}`)
	require.Equal(t, http.StatusNotFound, w2.Code,
		"a renewal for a since-revoked member must be refused, the same as any other non-member")
	require.Empty(t, sessionCookies(w2), "no session may be re-issued for a revoked member")
	require.Equal(t, 2, roles.calls,
		"the renewal must consult OpenFGA again, not trust a cached or previously-issued answer")
}

// --- invalid credentials never reach the membership gate ----------------

func TestLogin_RefusesInvalidZitadelToken(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, _ := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	w := doLogin(t, r, `{"id_token":"evil"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Zero(t, roles.calls, "an unverified token must never reach the membership lookup")
	require.Empty(t, sessionCookies(w))
}

func TestLogin_RejectsMalformedRequestBody(t *testing.T) {
	roles := &fakeRoleListerLogin{}
	r, _ := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	w := doLogin(t, r, `not json`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// --- #841: rate limit between token verification and ListRoles ----------

// TestLogin_RefusesOverBudget proves the budget actually refuses: rate 6
// with burst 1 (mirroring internal/archtest/ratelimit_test.go's
// TestThrottledRequestMakesNoOpenFGACall arithmetic) admits exactly one
// request and 429s the second, with a Retry-After header a client can
// act on.
func TestLogin_RefusesOverBudget(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	r, _ := loginHarnessRL(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, limiter, rule)

	w1 := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	w2 := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
	require.NotEmpty(t, w2.Header().Get("Retry-After"),
		"a refused login must tell the client when to retry")
	require.Empty(t, sessionCookies(w2), "no session may be issued for a throttled login")
}

// TestLoginThrottledMakesNoRolesListCall is this endpoint's equivalent of
// internal/archtest/ratelimit_test.go's
// TestThrottledRequestMakesNoOpenFGACall: the budget must be checked
// BEFORE ListRoles, not after, or a flood exhausts OpenFGA before
// anything is refused. roles.calls is asserted, not just the status
// code, because a check placed after ListRoles would ALSO return 429 on
// the second call — the status code alone cannot distinguish correct
// placement from the exact bug this test exists to catch. See this
// task's report for the observed failure when the check is moved after
// ListRoles.
func TestLoginThrottledMakesNoRolesListCall(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	r, _ := loginHarnessRL(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, limiter, rule)

	require.Equal(t, http.StatusOK, doLogin(t, r, `{"id_token":"good"}`).Code)
	require.Equal(t, 1, roles.calls, "the first, admitted login must have consulted OpenFGA")

	require.Equal(t, http.StatusTooManyRequests, doLogin(t, r, `{"id_token":"good"}`).Code)
	require.Equal(t, 1, roles.calls,
		"a throttled login must not reach OpenFGA: it would exhaust ListRoles before the limiter refused anything")
}

// TestLogin_BudgetIsPerSubject proves the bucket is keyed on the verified
// subject, not shared globally: a second subject exhausting its own
// budget must not affect a third subject's ability to log in, and the
// throttled subject retrying under a different (fresh) principal is not
// how a real attacker would behave — but the isolation itself is what a
// shared/global bucket would break, silently throttling every hospital's
// logins over one caller's flood.
func TestLogin_BudgetIsPerSubject(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
		"user-2": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}

	r1, _ := loginHarnessRL(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, limiter, rule)
	require.Equal(t, http.StatusOK, doLogin(t, r1, `{"id_token":"good"}`).Code)
	require.Equal(t, http.StatusTooManyRequests, doLogin(t, r1, `{"id_token":"good"}`).Code,
		"precondition: user-1's budget is exhausted")

	r2, _ := loginHarnessRL(t, fakeZitadelVerifier{subject: "user-2", authTime: time.Now()}, roles, limiter, rule)
	require.Equal(t, http.StatusOK, doLogin(t, r2, `{"id_token":"good"}`).Code,
		"user-2's login must not be affected by user-1 exhausting its own budget")
}

// TestLoginAdmitsWhenLimiterUnavailable is D3's fail-open proof: a nil
// limiter (the shape an unwired dependency takes) must never block a
// login that would otherwise succeed — denying here, per
// docs/standards/engineering-principles.md §3, is the worse outage,
// since it would time out every open tab's silent renewal within one
// SessionTTL. loginHarness (not loginHarnessRL) is used deliberately: it
// is the nil-limiter case by construction.
func TestLoginAdmitsWhenLimiterUnavailable(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	r, _ := loginHarness(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles)

	// Many calls in a row: a real (non-nil) limiter at any sane budget
	// would have refused well before this many. A nil limiter must
	// admit every single one.
	for i := 0; i < 20; i++ {
		w := doLogin(t, r, `{"id_token":"good"}`)
		require.Equal(t, http.StatusOK, w.Code, "attempt %d: nil limiter must fail open, not deny", i+1)
	}
}

// --- #848 spec D3: the idle deadline, and what may move it -------------

// mutableZitadelVerifier is fakeZitadelVerifier with an auth_time the
// test can move. It exists because auth_time is what distinguishes a
// silent renewal from a human signing in again: Zitadel returns the
// ORIGINAL auth_time on a prompt=none re-authorization, so a renewal's
// token carries the same auth_time as the session it renews, while a
// genuine re-authentication carries a later one. A fixed-auth_time fake
// can express only the first of those.
type mutableZitadelVerifier struct {
	subject  string
	authTime *time.Time
}

func (m mutableZitadelVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	if raw != "good" {
		return authn.Principal{}, errors.New("invalid zitadel token")
	}
	return authn.Principal{Subject: m.subject, AuthTime: *m.authTime}, nil
}

// loginEnv is the fixture for the idle-deadline tests.
//
// It builds its own signer/verifier pair rather than going through
// loginHarness, because these tests need to mint PRIOR sessions the HTTP
// surface cannot produce — one whose deadline has already lapsed, one
// belonging to another subject. Those tokens must be signed with the
// SAME key the handler verifies with, or they would simply fail
// verification and every such test would pass for the wrong reason (the
// "no usable cookie" branch) while proving nothing about carry-forward.
// env.signer is therefore deliberately the harness's own signer.
type loginEnv struct {
	cfg      config.Config
	r        *gin.Engine
	signer   *session.Signer
	verifier *session.Verifier
	roles    *fakeRoleListerLogin
	authTime time.Time
	// zitadelAuthTime is what the fake Zitadel verifier reports;
	// reauthenticate moves it.
	zitadelAuthTime *time.Time
	// clock is the handler's own clock. renewWith advances it five
	// minutes per call, the real renewal cadence (spec D4a) — and, more
	// importantly, the thing that makes the D3 assertion able to fail at
	// all: idle_deadline travels as a Unix timestamp, so a login and a
	// renewal in the same wall-clock second compute the IDENTICAL
	// "now + IdleTimeout" and a reset-on-renewal implementation passes.
	// That was observed against this very test before the clock was
	// injected (see the task report), which is exactly the class of
	// prove-nothing test this project keeps finding.
	clock *time.Time
}

func newLoginEnv(t *testing.T) *loginEnv {
	t.Helper()
	// An auth_time in the past, not time.Now(): every renewal below
	// re-presents it, and a fixed, distant value makes "carried through"
	// unmistakably different from "reset to the mint time".
	authTime := time.Date(2026, 3, 4, 9, 30, 12, 0, time.UTC)
	zitadelAuthTime := authTime

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, loginTestKID, loginTestIssuer, loginTestTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, loginTestKID, loginTestIssuer)
	require.NoError(t, err)

	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	cfg := config.Load()
	clock := time.Now()
	h := iam.NewLoginHandlers(iam.LoginDeps{
		Zitadel:      mutableZitadelVerifier{subject: "user-1", authTime: &zitadelAuthTime},
		Roles:        roles,
		Signer:       signer,
		Sessions:     verifier,
		TTL:          loginTestTTL,
		IdleTimeout:  cfg.IdleTimeout,
		SecureCookie: true,
		Now:          func() time.Time { return clock },
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login", h.Login)

	return &loginEnv{
		cfg: cfg, r: r, signer: signer, verifier: verifier, roles: roles,
		authTime: authTime, zitadelAuthTime: &zitadelAuthTime, clock: &clock,
	}
}

// freshWindow is what a genuine login mints RIGHT NOW, on the handler's
// own clock: the value the carry-forward branch must NOT produce, and the
// one the genuine-login branch must.
func (e *loginEnv) freshWindow() time.Time { return e.clock.Add(e.cfg.IdleTimeout) }

// reauthenticate moves the auth_time the fake Zitadel verifier reports,
// standing in for the human signing in again at the terminal.
func (e *loginEnv) reauthenticate(at time.Time) { *e.zitadelAuthTime = at }

// login is a GENUINE sign-in: no HMS session cookie on the request, which
// is exactly how the handler tells it apart from a renewal.
func (e *loginEnv) login(t *testing.T) string {
	t.Helper()
	w := doLogin(t, e.r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return sessionCookie(t, w)
}

// renewWith is spec D4a's silent renewal: the SAME endpoint, a fresh
// Zitadel ID token, and — the part that matters — the session cookie the
// browser attaches automatically, carrying the deadline this session is
// already running against.
func (e *loginEnv) renewWith(t *testing.T, current string) string {
	t.Helper()
	// Five minutes later, the real renewal cadence. Without this the
	// handler's clock never moves and the assertion cannot fail — see
	// loginEnv.clock.
	*e.clock = e.clock.Add(5 * time.Minute)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"id_token":"good"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: current})
	e.r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return sessionCookie(t, w)
}

// deadlineOf decodes the idle_deadline out of a minted session token with
// a REAL verifier — the claim as it actually travels on the wire, not a
// value the test kept on the side.
func (e *loginEnv) deadlineOf(t *testing.T, token string) time.Time {
	t.Helper()
	claims, err := e.verifier.Verify(token)
	require.NoError(t, err)
	return claims.IdleDeadline
}

// TestRenewalDoesNotMoveTheIdleDeadline is THE test for spec D3. An
// untouched tab renews every 5 minutes; if renewal moved the deadline,
// the timeout would never fire and every other test would still pass.
//
// Observed failing (see the task report) against an implementation that
// minted time.Now().Add(cfg.IdleTimeout) unconditionally.
func TestRenewalDoesNotMoveTheIdleDeadline(t *testing.T) {
	env := newLoginEnv(t)
	first := env.login(t) // genuine login, sets deadline
	original := env.deadlineOf(t, first)

	for i := 0; i < 3; i++ {
		next := env.renewWith(t, first) // POST /v1/auth/login carrying the session cookie
		got := env.deadlineOf(t, next)
		if !got.Equal(original) {
			t.Fatalf("renewal %d moved the idle deadline: got %v, want %v unchanged — "+
				"an untouched tab would renew itself forever and the timeout would never fire",
				i+1, got, original)
		}
		first = next
	}
}

func TestGenuineLoginSetsAFreshIdleDeadline(t *testing.T) {
	env := newLoginEnv(t)
	tok := env.login(t) // no session cookie on the request
	got := env.deadlineOf(t, tok)
	want := time.Now().Add(env.cfg.IdleTimeout)
	if got.Before(want.Add(-30*time.Second)) || got.After(want.Add(30*time.Second)) {
		t.Errorf("idle deadline = %v, want ~%v for a genuine login", got, want)
	}
}

// TestRenewalDoesNotResurrectAnAlreadyLapsedSession closes the second
// half of D3, and the one place this implementation is stricter than the
// task brief. A tab whose deadline has already lapsed is STILL renewing
// every five minutes. If a lapsed deadline earned a fresh window, the
// session would come back to life at the next renewal — dead for a few
// minutes, alive again after, forever — which is the same failure D3
// exists to prevent, just delayed.
//
// The lapsed session here is minted directly (not slept into existence)
// so the test is deterministic and fast; carrying a past deadline is
// explicitly allowed by session.Signer.Mint for exactly this flow.
func TestRenewalDoesNotResurrectAnAlreadyLapsedSession(t *testing.T) {
	env := newLoginEnv(t)
	lapsed := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	stale, err := env.signer.Mint("user-1", tenantA, env.authTime, lapsed)
	require.NoError(t, err)

	renewed := env.renewWith(t, stale)
	got := env.deadlineOf(t, renewed)
	require.True(t, got.Equal(lapsed),
		"a renewal of a lapsed session must carry the lapsed deadline through (got %v, want %v): "+
			"granting it a fresh window would let an untouched tab resurrect itself every five minutes", got, lapsed)
	require.True(t, got.Before(time.Now()),
		"precondition: the re-minted session must still be idle-expired, so authn.Middleware refuses it")
}

// TestGenuineReAuthenticationAfterALapsedSessionGetsAFreshWindow is the
// other side of the test above, and what keeps it from being a lockout: a
// human who signs in again — a NEW Zitadel authentication, so a strictly
// later auth_time — gets a full window even though their browser still
// holds the lapsed cookie. The discriminator is the new auth_time, not
// the absence of a cookie, because a browser that never cleared its
// cookie is the normal case at a shared terminal.
func TestGenuineReAuthenticationAfterALapsedSessionGetsAFreshWindow(t *testing.T) {
	env := newLoginEnv(t)
	lapsed := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	stale, err := env.signer.Mint("user-1", tenantA, env.authTime, lapsed)
	require.NoError(t, err)

	// A NEW authentication: auth_time strictly after the one the stale
	// session carries. Everything else about the request is identical to
	// a renewal, cookie included — which is the point.
	env.reauthenticate(env.authTime.Add(time.Hour))

	renewed := env.renewWith(t, stale)
	got := env.deadlineOf(t, renewed)
	// Against the HANDLER's clock, which renewWith has just advanced —
	// comparing against the test process's own time.Now() would be off by
	// exactly that advance and would assert the wrong thing.
	want := env.freshWindow()
	require.WithinDuration(t, want, got, 30*time.Second,
		"a human who authenticated again must get a fresh idle window, not inherit the lapsed one")
}

// TestGenuineLoginByADifferentSubjectDoesNotInheritTheOtherSessionsDeadline
// is the issue's own scenario: the previous clinician never signed out,
// so their cookie is still on the terminal when the next person signs in.
// The new person must get their own window rather than the remainder of a
// stranger's.
func TestGenuineLoginByADifferentSubjectDoesNotInheritTheOtherSessionsDeadline(t *testing.T) {
	env := newLoginEnv(t)
	// user-2's session, nearly out of idle window, left behind on the
	// terminal. user-1 is the subject this env's Zitadel verifier
	// authenticates.
	nearlyOut := time.Now().Add(20 * time.Second).UTC().Truncate(time.Second)
	othersCookie, err := env.signer.Mint("user-2", tenantA, env.authTime, nearlyOut)
	require.NoError(t, err)

	tok := env.renewWith(t, othersCookie) // same request shape; different subject's cookie
	got := env.deadlineOf(t, tok)
	want := env.freshWindow()
	require.WithinDuration(t, want, got, 30*time.Second,
		"a different subject signing in must get their own idle window, not the remainder of the session left on the terminal")
}

// TestLoginFailsClosedWithoutASessionVerifier guards the deployment
// mistake, mirroring TestSwitchTenantFailsClosedWithoutASigner: without
// the verifier this handler cannot tell a renewal from a genuine login,
// so every renewal would re-open the idle window and #848 would be
// silently absent. Refusing to mint is the only safe answer.
func TestLoginFailsClosedWithoutASessionVerifier(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NotNil(t, pub)
	signer, err := session.NewSigner(priv, loginTestKID, loginTestIssuer, loginTestTTL)
	require.NoError(t, err)
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{
		"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}},
	}}
	// Sessions deliberately left unset — its zero value (nil) is what
	// this test exercises.
	h := iam.NewLoginHandlers(iam.LoginDeps{
		Zitadel: fakeZitadelVerifier{subject: "user-1", authTime: time.Now()},
		Roles:   roles, Signer: signer, TTL: loginTestTTL,
		IdleTimeout: 15 * time.Minute, SecureCookie: true,
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login", h.Login)

	w := doLogin(t, r, `{"id_token":"good"}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Empty(t, sessionCookies(w),
		"no session may be minted when the renewal discriminator is unwired")
}

func sessionCookies(w *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == authn.SessionCookie {
			out = append(out, c)
		}
	}
	return out
}
