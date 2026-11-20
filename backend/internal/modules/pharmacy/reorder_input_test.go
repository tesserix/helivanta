package pharmacy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func TestReorderLevelHTTPInput(t *testing.T) {
	for _, tt := range []struct {
		body   string
		status int
	}{
		{`{"minimum_units":0,"expected_version":0,"product_version":1}`, 200},
		{`{"minimum_units":null,"expected_version":0,"product_version":1}`, 200},
		{`{"minimum_units":-1,"expected_version":0,"product_version":1}`, 400},
		{`{"minimum_units":1.5,"expected_version":0,"product_version":1}`, 400},
		{`{"minimum_units":1000000001,"expected_version":0,"product_version":1}`, 400},
		{`{"expected_version":0,"product_version":1}`, 400},
		{`{"minimum_units":1,"product_version":1}`, 400},
		{`{"minimum_units":1,"expected_version":0}`, 400},
		{`{"minimum_units":1,"expected_version":0,"product_version":1,"tenant_id":"forged"}`, 400},
		{`{"minimum_units":1,"expected_version":0,"product_version":1} {}`, 400},
	} {
		t.Run(tt.body, func(t *testing.T) {
			r := gin.New()
			r.PUT("/", func(c *gin.Context) {
				var in reorderLevelInput
				if decodeCatalogue(c, &in) {
					respond.OK(c, in)
				}
			})
			req := httptest.NewRequest("PUT", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code, w.Body.String())
		})
	}
}
