package pharmacy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
)

func TestHospitalStocktakeOutletDiscoveryRequiresAuthenticatedMembership(t *testing.T) {
	router := gin.New()
	operatingStocktakeRoutes(platform.NewRouter(router.Group("/v1/pharmacy"), stockHoldRegistrationOnly{}), &catalogueHandlers{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/pharmacy/hospitals/0199e000-0000-4000-8000-000000000001/departments/0199e000-0000-7000-8000-000000000002/stocktake-outlet-choices", nil))
	require.Equal(t, 401, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "data")
}
