package pharmacy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
)

func TestWardIndentPharmacyRoutesRequireAuthentication(t *testing.T) {
	router := gin.New()
	wardIndentPharmacyRoutes(platform.NewRouter(router.Group("/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet"), stockHoldRegistrationOnly{}), &catalogueHandlers{})
	base := "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000001/outlets/0199e000-0000-7000-8000-000000000002/ward-indents"
	id := "/0199e000-0000-7000-8000-000000000003"
	for method, paths := range map[string][]string{http.MethodGet: {base, base + id}, http.MethodPut: {base + id + "/closure"}} {
		for _, path := range paths {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			require.Equal(t, 401, response.Code, method+" "+path)
			require.NotContains(t, response.Body.String(), "data")
		}
	}
}

func TestSaleDigestSeparatesWardIndentIssues(t *testing.T) {
	product, occupancy, indent := uuid.New(), uuid.New(), uuid.New()
	base := saleInput{Lines: []saleLineInput{{ProductID: product, QuantityUnits: 2}}, Admission: &saleAdmission{OccupancyID: occupancy}}
	issued := base
	issued.WardIndentID = &indent
	require.NotEqual(t, base.digest(), issued.digest())
	other := uuid.New()
	changed := base
	changed.WardIndentID = &other
	require.NotEqual(t, issued.digest(), changed.digest())
	require.Equal(t, issued.digest(), issued.digest())
}

func TestWardIndentCloseReasonMustMatchOutstanding(t *testing.T) {
	require.Contains(t, errWardIndentReason{outstanding: true}.Error(), "close reason")
	for _, ok := range []string{"stock-out until Friday", "supplier delay"} {
		require.True(t, prescriptionText(ok, 200), ok)
	}
	for _, bad := range []string{"", " padded ", "tab\there"} {
		require.False(t, prescriptionText(bad, 200), bad)
	}
}
