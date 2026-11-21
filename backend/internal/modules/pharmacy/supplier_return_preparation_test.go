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

func TestSupplierReturnPreparationRejectsInvalidReadBeforeResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns/preparation/:payable"
	for _, route := range registered.Routes() {
		if route.Method == "GET" && route.Path == path {
			r.GET(path, route.HandlerFunc)
		}
	}
	base := "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/accounts/supplier-returns/preparation/0199e000-0000-7000-8000-000000000011"
	for _, tc := range []struct {
		name, query, body, subject string
		status                     int
	}{
		{"actor fence", "", "", "TEST-other", 409},
		{"query", "?limit=1000", "", "TEST-actor", 400},
		{"body", "", "{}", "TEST-actor", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", base+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer TEST")
			req.Header.Set("X-Helivanta-Expected-Subject", tc.subject)
			req.Header.Set("X-Helivanta-Expected-Tenant", "0199e000-0000-7000-8000-000000000001")
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, tc.status, res.Code, res.Body.String())
		})
	}
}
