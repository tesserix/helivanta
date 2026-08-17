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
// 4 proof for Finding 3, extended by #867 Task 4 to the fifth (factor)
// position: a route named in bootstrap.UnauthenticatedRoutes must
// actually be servable, never silently un-registered because a caller
// passed nil. Each of the five positions is exercised separately — a
// single passing case would not prove EVERY argument is checked, only
// that at least one is.
func TestMountUnauthenticatedPanicsOnNilHandler(t *testing.T) {
	cases := []struct {
		name                                          string
		login, authRequest, password, handoff, factor gin.HandlerFunc
	}{
		{"login", nil, stubHandler, stubHandler, stubHandler, stubHandler},
		{"authRequest", stubHandler, nil, stubHandler, stubHandler, stubHandler},
		{"password", stubHandler, stubHandler, nil, stubHandler, stubHandler},
		{"handoff", stubHandler, stubHandler, stubHandler, nil, stubHandler},
		{"factor", stubHandler, stubHandler, stubHandler, stubHandler, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			require.Panics(t, func() {
				bootstrap.MountUnauthenticated(e, tc.login, tc.authRequest, tc.password, tc.handoff, tc.factor)
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
		bootstrap.MountUnauthenticated(e, stubHandler, stubHandler, stubHandler, stubHandler, stubHandler)
	})

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodGet, "/v1/auth/login/request/abc"},
		{http.MethodPost, "/v1/auth/login/password"},
		{http.MethodPost, "/v1/auth/login/handoff/abc"},
		{http.MethodPost, "/v1/auth/login/factor"},
	} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s %s did not resolve to a registered route", tc.method, tc.path)
	}
}
