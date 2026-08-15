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

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
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
// cmd/api/main.go wires in production minus the network calls.
func loginHarness(t *testing.T, zv authn.TokenVerifier, roles *fakeRoleListerLogin) (*gin.Engine, *session.Verifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, loginTestKID, loginTestIssuer, loginTestTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, loginTestKID, loginTestIssuer)
	require.NoError(t, err)

	h := iam.NewLoginHandlers(zv, roles, signer, loginTestTTL, true)

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

func sessionCookies(w *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == authn.SessionCookie {
			out = append(out, c)
		}
	}
	return out
}
