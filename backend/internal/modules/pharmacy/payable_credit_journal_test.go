package pharmacy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestPayableCreditJournalRejectsChangedActorBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/payables/:id/supplier-credit-allocations"
	for _, route := range registered.Routes() {
		if route.Method == "GET" && route.Path == path {
			r.GET(path, route.HandlerFunc)
		}
	}
	request := httptest.NewRequest("GET", "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/accounts/payables/0199e000-0000-7000-8000-000000000011/supplier-credit-allocations", nil)
	request.Header.Set("Authorization", "Bearer TEST")
	request.Header.Set("X-Helivanta-Expected-Subject", "TEST-other-actor")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"session_changed"`)
}
