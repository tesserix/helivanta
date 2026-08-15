package archtest

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/httpserver"
	"github.com/tesserix/hms/internal/platform"
)

// TestEveryEngineRouteIsDeclaredOrAllowlisted closes the one hole in the
// repository's strongest routing control.
//
// platform.Router makes an undeclared route inexpressible: every route
// through it names a permission, and gets membership, authorization and
// rate limiting from the /v1 chain. But `engine.POST(...)` bypasses all
// of that — the route declares nothing, appears in no Declared() list,
// and is reachable with no principal, no tenant and no permission check.
// Until this test, nothing distinguished a deliberate bypass (the login
// endpoint, which cannot require the session it creates) from an
// accidental one, and nothing would have failed if a module author
// reached for the engine because a route "didn't fit" the Router.
//
// So: enumerate what is ACTUALLY registered on the engine — gin's own
// Routes(), not a list we maintain — and require every entry to be
// either declared through platform.Router or named in
// bootstrap.UnauthenticatedRoutes with a reason.
func TestEveryEngineRouteIsDeclaredOrAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The same shape cmd/api builds: httpserver.New owns /healthz and
	// /readyz, every module registers through platform.Router on /v1, and
	// bootstrap.MountUnauthenticated adds the deliberate bypasses.
	srv := httpserver.New(nil)
	api := platform.NewRouter(srv.Engine.Group("/v1"), dryRouteRegistrationChecker{})
	for _, m := range allModules() {
		m.Routes(api, platform.Deps{})
	}
	bootstrap.MountUnauthenticated(srv.Engine, func(c *gin.Context) { c.Status(http.StatusOK) })

	declared := map[string]bool{}
	for _, dr := range api.Declared() {
		declared[dr.Method+" "+dr.Path] = true
	}

	for _, r := range srv.Engine.Routes() {
		key := r.Method + " " + r.Path
		if declared[key] {
			continue
		}
		reason, allowed := bootstrap.UnauthenticatedRoutes[key]
		require.Truef(t, allowed,
			"%s is registered on the gin engine but is neither declared through platform.Router "+
				"nor listed in bootstrap.UnauthenticatedRoutes. A route on the raw engine has no "+
				"permission declaration, no membership check and no rate limit — it is reachable by "+
				"anyone with no principal at all. Register it through platform.Router, or add it to "+
				"UnauthenticatedRoutes with the reason it cannot work any other way.", key)
		require.NotEmptyf(t, reason,
			"%s is allowlisted as unauthenticated with an empty reason; the reason is the review", key)
	}
}

// TestUnauthenticatedAllowlistHasNoDeadEntries is the reverse direction:
// an entry naming a route that no longer exists is stale permission
// nobody notices, and it makes the list read as larger and more
// frightening than it is.
func TestUnauthenticatedAllowlistHasNoDeadEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)

	srv := httpserver.New(nil)
	api := platform.NewRouter(srv.Engine.Group("/v1"), dryRouteRegistrationChecker{})
	for _, m := range allModules() {
		m.Routes(api, platform.Deps{})
	}
	bootstrap.MountUnauthenticated(srv.Engine, func(c *gin.Context) { c.Status(http.StatusOK) })

	onEngine := map[string]bool{}
	for _, r := range srv.Engine.Routes() {
		onEngine[r.Method+" "+r.Path] = true
	}

	for key, reason := range bootstrap.UnauthenticatedRoutes {
		require.Truef(t, onEngine[key],
			"bootstrap.UnauthenticatedRoutes names %q (%s) but no such route is registered — "+
				"remove it, or fix the typo that is silently exempting nothing", key, reason)
	}
}
