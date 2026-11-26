package pharmacy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
)

const wardIndentBase = "/v1/pharmacy/hospitals/0199e000-0000-4000-8000-000000000001/departments/0199e000-0000-7000-8000-000000000002"

func TestWardIndentRoutesRequireAuthentication(t *testing.T) {
	router := gin.New()
	wardIndentRoutes(platform.NewRouter(router.Group("/v1/pharmacy"), stockHoldRegistrationOnly{}), &catalogueHandlers{})
	outlet := "/outlets/0199e000-0000-7000-8000-000000000003"
	indent := outlet + "/ward-indents/0199e000-0000-7000-8000-000000000004"
	for method, paths := range map[string][]string{
		http.MethodGet: {"/ward-indent-outlet-choices", outlet + "/ward-indent-products", outlet + "/ward-indent-occupant?bed_code=B1", outlet + "/ward-indents", indent},
		http.MethodPut: {indent, indent + "/cancellation"},
	} {
		for _, path := range paths {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, wardIndentBase+path, nil))
			require.Equal(t, 401, response.Code, method+" "+path)
			require.NotContains(t, response.Body.String(), "data")
		}
	}
}

func TestWardIndentLocationCodeShape(t *testing.T) {
	for _, ok := range []string{"B1", "ICU-04", "A"} {
		require.True(t, wardIndentLocation.MatchString(ok), ok)
	}
	for _, bad := range []string{"", "b1", "-B1", "B 1", "B1;DROP", "ABCDEFGHIJKLMNOPQRSTU"} {
		require.False(t, wardIndentLocation.MatchString(bad), bad)
	}
}

func TestWardIndentNameRejectsUntrimmedOrOversized(t *testing.T) {
	require.True(t, wardIndentName("City Hospital"))
	require.False(t, wardIndentName(" City Hospital"))
	require.False(t, wardIndentName(""))
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	require.False(t, wardIndentName(string(long)))
}
