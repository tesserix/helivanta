package archtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/ratelimit"
)

// rlChainTenant is the fixed tenant every rlChainVerifier-issued principal
// belongs to. A distinct constant from seamTenant/tenantA/tenantB so this
// suite cannot collide with theirs if ever run against a shared OpenFGA
// store — though this suite never touches OpenFGA at all, only a fake
// Resolver, which is the whole point of T1.
const rlChainTenant = "55555555-5555-5555-5555-555555555555"

// rlChainVerifier is a minimal authn.TokenVerifier, the same shape as
// http_seam_test.go's seamVerifier: the raw bearer token IS the subject,
// always resolved into rlChainTenant. It stands in only for the identity
// provider; the seam under test is placement (authn -> ratelimit ->
// authz), not token verification.
type rlChainVerifier struct{}

func (rlChainVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	// IdleDeadline an hour out (#848): authn.Middleware refuses a zero
	// deadline exactly like an already-past one (fail closed), so
	// leaving it unset would 401 every request here and this seam test
	// would stop exercising the seam it is about. The idle gate itself
	// is pinned by pkg/authn's own tests.
	return authn.Principal{Subject: raw, TenantID: rlChainTenant, AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}, nil
}

// rlChainNeverRevoked stands in for iam.RevocationChecker; revocation is
// not what this seam tests.
type rlChainNeverRevoked struct{}

func (rlChainNeverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

// countingResolver wraps a real authz.Resolver and counts every call to
// Resolve — the one call in the whole chain that reaches OpenFGA.
type countingResolver struct {
	inner authz.Resolver
	n     *atomic.Int64
}

func (c countingResolver) Resolve(ctx context.Context, subject, tenantID string) (authz.PermissionSet, error) {
	c.n.Add(1)
	return c.inner.Resolve(ctx, subject, tenantID)
}

// allowAllResolver is the "real" authz.Resolver countingResolver wraps: it
// grants nothing and denies nothing, because this seam never calls
// authz.Require — only authz.Middleware, to prove whether Resolve ran at
// all.
type allowAllResolver struct{}

func (allowAllResolver) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return authz.PermissionSet{}, nil
}

// chainHarness mounts the REAL production chain — bootstrap.V1Chain, the
// same call cmd/api/main.go makes — with a static verifier standing in
// for GIP and a fake Resolver standing in for OpenFGA. One plain route is
// enough; this seam is about which middleware runs, not about
// authz.Require or a real handler.
//
// It builds from V1Chain rather than re-listing the middlewares on
// purpose. A hand-listed copy is a replica, and a replica keeps passing
// after production diverges from it: with the chain inline in main.go,
// deleting ratelimit.Middleware removed rate limiting from the API
// entirely and this whole suite stayed green.
//
// limits are applied via config, because V1Chain builds its
// ratelimit.Config from a config.Config through
// bootstrap.RateLimitConfig — there is deliberately no seam for injecting
// a pre-built policy, so the test exercises the real construction path
// including the Rate/6 burst arithmetic.
func chainHarness(t *testing.T, resolver authz.Resolver, cfg config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(bootstrap.V1Chain(rlChainVerifier{}, rlChainNeverRevoked{}, ratelimit.NewMemory(1000), cfg, resolver)...)
	e.GET("/v1/things", func(c *gin.Context) { c.Status(http.StatusOK) })
	return e
}

// doChain fires one request as subject. Every principal this harness
// issues belongs to rlChainTenant — see rlChainVerifier — so there is no
// tenant parameter to pass: one would be silently ignored, and a later
// test written against it would assert on tenant isolation it never
// actually exercised.
func doChain(e *gin.Engine, subject string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/things", nil)
	req.Header.Set("Authorization", "Bearer "+subject)
	e.ServeHTTP(w, req)
	return w
}

// TestThrottledRequestMakesNoOpenFGACall proves the middleware ORDER,
// which no unit test in pkg/ratelimit can: authz.Middleware calls
// OpenFGA on every request, and it is the most expensive step in the
// chain. If the limiter ever moves after it, a flood exhausts OpenFGA
// before anything is refused — the limiter would still return 429 and
// every other test in pkg/ratelimit would still pass, because none of
// them wire up authz at all.
//
// This is the load-bearing assertion the whole design (D3) rests on.
func TestThrottledRequestMakesNoOpenFGACall(t *testing.T) {
	var resolves atomic.Int64
	counting := countingResolver{inner: allowAllResolver{}, n: &resolves}

	// Principal rate 6/min gives a burst of exactly 1 through
	// RateLimitConfig's Rate/6 arithmetic, so the second request is
	// refused with no clock manipulation. Tenant is left at the
	// production default so only the principal bucket can be the one that
	// refuses — otherwise a tenant-bucket denial would satisfy this test
	// while saying nothing about the principal path.
	r := chainHarness(t, counting, config.Config{
		RateLimitTenantPerMin:    600,
		RateLimitPrincipalPerMin: 6,
	})

	require.Equal(t, http.StatusOK, doChain(r, "alice").Code)
	require.Equal(t, int64(1), resolves.Load())

	require.Equal(t, http.StatusTooManyRequests, doChain(r, "alice").Code)
	require.Equal(t, int64(1), resolves.Load(),
		"a throttled request must not reach authz: it would exhaust OpenFGA before the limiter refused anything")
}

// rateLimitExemptAllowlist is every route permitted to bypass rate
// limiting, with the reason. Adding an entry is a decision a reviewer
// sees: an exempt route is one an attacker may hammer without being
// refused, so the set must stay small and justified.
var rateLimitExemptAllowlist = map[string]string{
	"POST /v1/iam/me/sign-out":              "a clinician on a shared ward terminal must always be able to end their session",
	"POST /v1/iam/subjects/:subject/revoke": "incident response; an attacker looping requests is exactly what would trip the limiter",
}

// TestRateLimitExemptionsAreAllowlisted pins the exemption map exactly.
// config.Load() reads env with production defaults; only the Exempt map
// is under test here and it does not depend on the limit values
// themselves.
func TestRateLimitExemptionsAreAllowlisted(t *testing.T) {
	got := bootstrap.RateLimitConfig(config.Load()).Exempt
	require.Equal(t, rateLimitExemptAllowlist, got,
		"a route exempt from rate limiting must be added to rateLimitExemptAllowlist with a reason")
}

// allRegisteredV1Routes walks every module in allModules() (this file's
// approved-module list) through platform.NewRouter mounted at "/v1" —
// the same prefix cmd/api/main.go uses — and returns "METHOD /path" for
// every route it declares, WITHOUT dispatching a single request. It is
// the same registration-only walk arch_test.go's routesDeclaring and
// allDeclaredRoutes use, at the /v1 prefix so the keys line up exactly
// with ratelimit.Config's Exempt/Tight map keys.
func allRegisteredV1Routes(t *testing.T) map[string]bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	routes := map[string]bool{}
	for _, m := range allModules() {
		e := gin.New()
		r := platform.NewRouter(e.Group("/v1"), dryRouteRegistrationChecker{})
		m.Routes(r, platform.Deps{})
		for _, dr := range r.Declared() {
			routes[dr.Method+" "+dr.Path] = true
		}
	}
	return routes
}

// TestRateLimitPolicyRoutesAreRegistered is the improvement on
// TestRateLimitExemptionsAreAllowlisted the plan's version does not
// cover: that test only proves the Exempt map in
// bootstrap.RateLimitConfig matches a second hand-written map in this
// file — someone could transcribe a TYPO'D route key into both places
// and the test would stay green. A mistyped Exempt key
// ("/v1/iam/me/signout" instead of "/v1/iam/me/sign-out") means sign-out
// is silently rate limited during exactly the incident an attacker flood
// would cause, and no other test catches it: the route simply falls
// through to the default rule and still returns 200 or 429 like any
// other route, so nothing looks broken until an operator is trying to
// sign a compromised session out under load.
//
// This walks the REAL module registry via platform.Router.Declared() —
// the runtime source of truth for what routes exist — and requires
// every key in both Exempt and Tight to name one of them. Per
// docs/standards/engineering-principles.md §4, this converts "someone
// typo'd a route" from a silent gap into a CI failure naming the exact
// unmatched key.
func TestRateLimitPolicyRoutesAreRegistered(t *testing.T) {
	registered := allRegisteredV1Routes(t)
	cfg := bootstrap.RateLimitConfig(config.Load())

	for route, reason := range cfg.Exempt {
		require.Truef(t, registered[route],
			"rate limit Exempt route %q (%s) does not match any registered route — check for a typo", route, reason)
	}
	for route := range cfg.Tight {
		require.Truef(t, registered[route],
			"rate limit Tight route %q does not match any registered route — check for a typo", route)
	}
}
