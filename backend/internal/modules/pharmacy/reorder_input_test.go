package pharmacy

import (
	"encoding/json"
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
				var in reorderLevelRequest
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

func TestReorderReceiptDecodesCanonicalMetadata(t *testing.T) {
	var receipt ReorderLevelReceipt
	err := json.Unmarshal([]byte(`{"id":"0199e000-0000-7000-8000-000000000003","outlet_id":"0199e000-0000-7000-8000-000000000001","product_id":"0199e000-0000-7000-8000-000000000002","minimum_units":null,"expected_version":0,"product_version":1,"version":1,"unit_of_issue":"capsule","recorded_at":"2026-11-20T00:00:00Z"}`), &receipt)
	require.NoError(t, err)
	require.Equal(t, "0199e000-0000-7000-8000-000000000003", receipt.ID.String())
	require.Equal(t, int64(1), receipt.Version)
	require.Nil(t, receipt.MinimumUnits)
	require.Equal(t, int64(0), *receipt.ExpectedVersion)
}
