package pharmacy

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

type hospitalStocktakeScopeVerifier struct{ principal authn.Principal }

func (v hospitalStocktakeScopeVerifier) Verify(context.Context, string) (authn.Principal, error) {
	return v.principal, nil
}

func TestStocktakeHospitalCoordinatesPrecedeAuthority(t *testing.T) {
	const hospital = "0199e000-0000-7000-8000-000000000011"
	const department = "0199e000-0000-7000-8000-000000000012"
	const route = "/hospitals/:hospital/departments/:department/pharmacies/:pharmacy/outlets/:outlet/stocktakes/:stocktake"
	const target = "/hospitals/HOSPITAL/departments/DEPARTMENT/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stocktakes/0199e000-0000-7000-8000-000000000003"
	for _, scenario := range []struct {
		name, hospital, department string
		status                     int
	}{
		{"malformed hospital", "not-a-hospital", department, 404},
		{"nil hospital", "00000000-0000-0000-0000-000000000000", department, 404},
		{"malformed department", hospital, "not-a-department", 404},
		{"nil department", hospital, "00000000-0000-0000-0000-000000000000", 404},
		{"wrong department version", hospital, "0199e000-0000-4000-8000-000000000012", 404},
		{"current hospital needs authority", hospital, department, 503},
		{"legacy hospital needs authority", "0199e000-0000-4000-8000-000000000011", department, 503},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			router := gin.New()
			router.Use(authn.Middleware(hospitalStocktakeScopeVerifier{principal: authn.Principal{Subject: "TEST-actor", TenantID: "0199e000-0000-7000-8000-000000000001", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}}, stocktakeScopeRevocation{}))
			handler := catalogueHandlers{}
			router.GET(route, handler.getOperatingStocktake)
			path := strings.NewReplacer("HOSPITAL", scenario.hospital, "DEPARTMENT", scenario.department).Replace(target)
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("Authorization", "Bearer TEST")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, scenario.status, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "data")
		})
	}
}
