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
	// Creates the HMS session, so it cannot require one: it verifies a
	// caller-presented Zitadel ID token, not the HMS session the /v1
	// chain checks (#838, spec D1).
	"POST /v1/auth/login": "creates the HMS session and therefore cannot require one",

	// #854 Task 4: HMS's own login form drives these three directly
	// against Zitadel's login-client API, all before any HMS session
	// exists — the same reason POST /v1/auth/login above is here.
	"GET /v1/auth/login/request/:id":  "renders the login form before any HMS session exists",
	"POST /v1/auth/login/password":    "checks the credential that creates the session, so it cannot require one",
	"POST /v1/auth/login/handoff/:id": "hands an auth request to the hosted login when HMS cannot complete it",

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
// authRequest, password and handoff (#854 Task 4, iam.LoginUIHandlers'
// three methods) are accepted as plain gin.HandlerFunc rather than a
// concrete *iam.LoginUIHandlers, the same way login is — this package
// stays agnostic of any one module's types. Task 5 constructs the real
// loginclient.Client and wires all three from cmd/api/main.go; until
// then a nil is accepted and the corresponding route is simply not
// registered, so this signature can land (and the routes can be pinned
// in UnauthenticatedRoutes and exercised by the arch test's own harness,
// which always passes non-nil stubs) without main.go having to grow the
// PAT plumbing Task 5 owns.
func MountUnauthenticated(e *gin.Engine, login, authRequest, password, handoff gin.HandlerFunc) {
	e.POST("/v1/auth/login", login)
	if authRequest != nil {
		e.GET("/v1/auth/login/request/:id", authRequest)
	}
	if password != nil {
		e.POST("/v1/auth/login/password", password)
	}
	if handoff != nil {
		e.POST("/v1/auth/login/handoff/:id", handoff)
	}
}
