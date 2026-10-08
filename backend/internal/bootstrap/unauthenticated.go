package bootstrap

import (
	"github.com/gin-gonic/gin"
)

// UnauthenticatedRoutes is every route deliberately mounted OUTSIDE the
// authenticated /v1 chain, keyed "METHOD /path", with the reason it is
// outside.
//
// This list exists because `srv.Engine.POST(...)` is a hole in the
// repository's strongest routing control. `platform.Router` makes an
// undeclared route inexpressible — every route through it must name a
// permission, and gets membership, rate limiting and authorization for
// free. A route registered directly on the *gin.Engine* gets none of
// that, declares nothing, and appears in no `Declared()` list. Nothing
// distinguished a deliberate bypass from an accidental one until this
// list, and TestEveryEngineRouteIsDeclaredOrAllowlisted, existed.
//
// The bar for an entry is high: a route here is reachable by anyone on
// the network, with no principal, no tenant and no permission check. It
// must be a route that *cannot* work any other way.
var UnauthenticatedRoutes = map[string]string{
	// Creates the Helivanta session, so it cannot require one: it verifies a
	// caller-presented Zitadel ID token, not the Helivanta session the /v1
	// chain checks (#838, spec D1).
	"POST /v1/auth/login": "creates the Helivanta session and therefore cannot require one",

	// #854 Task 4: Helivanta's own login form drives these directly
	// against Zitadel's login-client API, all before any Helivanta session
	// exists — the same reason POST /v1/auth/login above is here.
	"GET /v1/auth/login/request/:id": "renders the login form before any Helivanta session exists",
	"POST /v1/auth/login/password":   "checks the credential that creates the session, so it cannot require one",

	// #867 Task 4: the native-MFA factor check. Reached only after
	// Password answers factor_required — there is still no Helivanta
	// session at this point, only a server-held Zitadel session the
	// browser never sees (spec D2) — so it cannot require one either,
	// same as its siblings above. (A fourth route, a hosted-login handoff,
	// was deleted by #947: Helivanta never sends a user to Zitadel's UI.)
	"POST /v1/auth/login/factor": "checks the second factor that creates the session, so it cannot require one",

	// #948: native TOTP enrolment. Reached only after Password answers
	// enrollment_required (forceMfa, nothing enrolled) — again no
	// Helivanta session yet, only the server-held Zitadel session — and
	// it confirms the factor that then creates the session.
	"POST /v1/auth/login/enroll": "confirms the newly enrolled second factor that creates the session, so it cannot require one",

	// #856: a password that must change is changed here, after every factor
	// is proven but still before any Helivanta session exists — the same
	// server-held Zitadel session as the factor step, at its own stage.
	"POST /v1/auth/login/password-change": "completes a sign-in whose password must change first, so it cannot require a session",

	// Liveness and readiness. Deliberately outside /v1 and unlimited: a
	// throttled or authenticated probe takes a healthy replica out of
	// service, which is the failure mode these exist to prevent.
	"GET /healthz": "liveness probe; an authenticated probe cannot report health before auth works",
	"GET /readyz":  "readiness probe; same reason, and it must answer during a dependency outage",
}

// MountUnauthenticated registers the routes that cannot sit behind the
// authenticated chain. It is a function rather than a literal in
// cmd/api/main.go for the same reason V1Chain is: run() opens a
// database, NATS and an OIDC provider before it builds a router, so nothing
// mounted there is reachable from a test, and a bypass nobody can
// enumerate is a bypass nobody reviews.
//
// /healthz and /readyz are not mounted here — httpserver.New owns them —
// but they are listed in UnauthenticatedRoutes because the arch test
// enumerates the whole engine and must account for every route on it.
//
// authRequest, password, factor, enroll and passwordChange (#854 Task 4 /
// #867 Task 4 / #948 / #856, iam.LoginUIHandlers' five methods) are
// accepted as plain
// gin.HandlerFunc rather than a concrete *iam.LoginUIHandlers, the same
// way login is — this package stays agnostic of any one module's types.
// cmd/api/main.go constructs the real loginclient.Client and wires all
// four from there.
//
// Every argument is required — a nil PANICS at boot, deliberately. An
// earlier version of this function accepted nil and simply skipped
// registering that route, so a route named in UnauthenticatedRoutes
// (claiming it is reachable, with no principal, by design) could
// silently NOT be mounted at all: TestUnauthenticatedAllowlistHasNoDeadEntries
// could not tell the difference, because its own harness always passes
// non-nil stubs regardless of what production actually wires. That is
// exactly the gap this control exists to prevent: a route that is
// declared but not served is not a smaller version of the bypass, it is
// login silently broken. A nil here — whether main.go has not been
// updated yet for a route this list already promises, or a wiring typo
// drops one of them by accident — surfaces as a boot failure
// instead, which per this repo's enforcement ladder (compile error >
// boot failure > CI failure > documented convention) is the correct
// rung: loud and at start-up, not silent and per-request.
func MountUnauthenticated(e *gin.Engine, login, authRequest, password, factor, enroll, passwordChange gin.HandlerFunc) {
	mustHandler("POST /v1/auth/login", login)
	mustHandler("GET /v1/auth/login/request/:id", authRequest)
	mustHandler("POST /v1/auth/login/password", password)
	mustHandler("POST /v1/auth/login/factor", factor)
	mustHandler("POST /v1/auth/login/enroll", enroll)
	mustHandler("POST /v1/auth/login/password-change", passwordChange)

	e.POST("/v1/auth/login", login)
	e.GET("/v1/auth/login/request/:id", authRequest)
	e.POST("/v1/auth/login/password", password)
	e.POST("/v1/auth/login/factor", factor)
	e.POST("/v1/auth/login/enroll", enroll)
	e.POST("/v1/auth/login/password-change", passwordChange)
}

// mustHandler panics naming which UnauthenticatedRoutes entry a nil
// handler would have silently left unregistered. The route string, not
// just "handler was nil", is what makes the panic actionable — this
// function has five call sites, and a bare nil-pointer-shaped panic
// would leave whoever hits it grepping the diff to find out which one.
func mustHandler(route string, h gin.HandlerFunc) {
	if h == nil {
		panic("bootstrap.MountUnauthenticated: nil handler for " + route +
			" — every route in UnauthenticatedRoutes must actually be mounted, never silently skipped")
	}
}
