package bootstrap_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/bootstrap"
)

func stubHandler(c *gin.Context) { c.Status(http.StatusOK) }

// TestMountUnauthenticatedPanicsOnNilHandler is the fix-round #854 Task
// 4 proof for Finding 3, extended by #867 Task 4 to the factor
// position: a route named in bootstrap.UnauthenticatedRoutes must
// actually be servable, never silently un-registered because a caller
// passed nil. Each of the six positions (#948 added enroll, #856 passwordChange) is exercised separately — a
// single passing case would not prove EVERY argument is checked, only
// that at least one is.
func TestMountUnauthenticatedPanicsOnNilHandler(t *testing.T) {
	cases := []struct {
		name                                                         string
		login, authRequest, password, factor, enroll, passwordChange gin.HandlerFunc
	}{
		{"login", nil, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler},
		{"authRequest", stubHandler, nil, stubHandler, stubHandler, stubHandler, stubHandler},
		{"password", stubHandler, stubHandler, nil, stubHandler, stubHandler, stubHandler},
		{"factor", stubHandler, stubHandler, stubHandler, nil, stubHandler, stubHandler},
		{"enroll", stubHandler, stubHandler, stubHandler, stubHandler, nil, stubHandler},
		{"passwordChange", stubHandler, stubHandler, stubHandler, stubHandler, stubHandler, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			require.Panics(t, func() {
				bootstrap.MountUnauthenticated(e, tc.login, tc.authRequest, tc.password, tc.factor, tc.enroll, tc.passwordChange)
			}, "a nil %s handler must panic at boot, not silently leave the route unregistered", tc.name)
		})
	}
}

// TestMountUnauthenticatedRegistersAllFiveRoutesWhenGivenRealHandlers is
// the positive case: with every handler non-nil, all five routes named
// in UnauthenticatedRoutes (minus /healthz and /readyz, owned by
// httpserver.New rather than this function) actually respond, proving
// the panic guard above did not also break the working path — this is
// exactly the check Finding 3 said the arch test's own harness could
// not make mean anything on its own, so it is pinned directly here too.
func TestMountUnauthenticatedRegistersAllFiveRoutesWhenGivenRealHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	require.NotPanics(t, func() {
		bootstrap.MountUnauthenticated(e, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler)
	})

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodGet, "/v1/auth/login/request/abc"},
		{http.MethodPost, "/v1/auth/login/password"},
		{http.MethodPost, "/v1/auth/login/factor"},
		{http.MethodPost, "/v1/auth/login/enroll"},
		{http.MethodPost, "/v1/auth/login/password-change"},
	} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s %s did not resolve to a registered route", tc.method, tc.path)
	}
}

// TestHostedLoginHandoffRouteIsGone pins #947: Helivanta never hands a sign-in
// to Zitadel's hosted login, so the route that built that redirect must not
// exist on the engine at all — not merely be unused. A reintroduced handoff
// route would have to delete this test to land.
func TestHostedLoginHandoffRouteIsGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	bootstrap.MountUnauthenticated(e, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/auth/login/handoff/abc", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	_, declared := bootstrap.UnauthenticatedRoutes["POST /v1/auth/login/handoff/:id"]
	require.False(t, declared, "the handoff route must not be allowlisted either")
}
