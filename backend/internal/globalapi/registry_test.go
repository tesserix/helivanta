package globalapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/platform"
)

type noMembership struct{}

func (noMembership) IsMember(context.Context, string, string) (bool, error) { return false, nil }

func TestEveryRegisteredRouteReachesItsProductOwner(t *testing.T) {
	expectedOwners := map[string]string{"privacy": "platform", "iam": "platform", "workspace": "platform", "patientmaster": "platform", "reference": "platform", "drugmaster": "platform", "medicore": "medicore", "mediconnect": "mediconnect", "pharmacy": "pharmacy", "supply": "supply", "lab": "lab", "workforce": "workforce", "emergency": "emergency"}
	cfg := Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{}}
	for _, product := range expectedOwners {
		if _, exists := cfg.Upstreams[product]; exists {
			continue
		}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Test-Owner", product)
			w.WriteHeader(204)
		}))
		t.Cleanup(upstream.Close)
		cfg.Upstreams[product] = upstream.URL
	}
	gateway, err := New(cfg)
	require.NoError(t, err)
	for _, module := range bootstrap.Modules(nil, nil) {
		expected, exists := expectedOwners[module.Name()]
		require.True(t, exists, "new module needs owner review: %s", module.Name())
		router := platform.NewRouter(gin.New().Group("/v1"), noMembership{})
		module.Routes(router, platform.Deps{})
		for _, route := range router.Declared() {
			res := httptest.NewRecorder()
			gateway.ServeHTTP(res, httptest.NewRequest(route.Method, route.Path, nil))
			require.Equal(t, 204, res.Code, route.Path)
			require.Equal(t, expected, res.Header().Get("X-Test-Owner"), route.Path)
		}
	}
	for route := range bootstrap.UnauthenticatedRoutes {
		if !strings.Contains(route, " /v1/") {
			continue
		}
		// Public authentication routes are owned by platform, including POST variants.
		method, target, _ := strings.Cut(route, " ")
		res := httptest.NewRecorder()
		gateway.ServeHTTP(res, httptest.NewRequest(method, target, nil))
		require.Equal(t, 204, res.Code, target)
		require.Equal(t, "platform", res.Header().Get("X-Test-Owner"))
	}
}
