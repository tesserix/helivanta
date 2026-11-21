package pharmacy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPharmacyFinancialCursorReadRejectsUnknownDuplicateAndBodyInputs(t *testing.T) {
	for _, tc := range []struct {
		query, body string
		status      int
	}{
		{"", "", 200}, {"?limit=50&cursor=TEST-reference", "", 200},
		{"?all=true", "", 400}, {"?limit=50&limit=1", "", 400}, {"?cursor=", "", 400},
		{"?cursor=%xx", "", 400}, {"?limit=50", "{}", 400},
	} {
		t.Run(tc.query+tc.body, func(t *testing.T) {
			r := gin.New()
			r.GET("/records", func(c *gin.Context) {
				if pharmacyFinancialCursorRead(c) {
					c.Status(http.StatusOK)
				}
			})
			request := httptest.NewRequest("GET", "/records"+tc.query, strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
		})
	}
}
