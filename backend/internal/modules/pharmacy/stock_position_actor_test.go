package pharmacy

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStockPositionReadHonorsExplicitActorFenceBeforeFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/stock/positions"
	for _, route := range registered.Routes() {
		if route.Method == "GET" && (route.Path == path || route.Path == path+".csv") {
			r.GET(route.Path, route.HandlerFunc)
		}
	}
	for _, suffix := range []string{"", ".csv"} {
		req := httptest.NewRequest("GET", "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stock/positions"+suffix+"?as_of=invalid", nil)
		req.Header.Set("Authorization", "Bearer TEST")
		req.Header.Set("X-Helivanta-Expected-Subject", "TEST-other")
		req.Header.Set("X-Helivanta-Expected-Tenant", "0199e000-0000-7000-8000-000000000001")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		require.Equal(t, 409, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"error":"session_changed"`)
	}
}
