package supply

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"net/http/httptest"
	"testing"
)

func TestMalformedSourceScopeRetainsOneNotFoundResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Params = gin.Params{{Key: "product", Value: "supply"}, {Key: "organization", Value: "not-an-id"}, {Key: "location", Value: "not-an-id"}}
	_, ok := sourcePathScope(c)
	require.False(t, ok)
	original := response.Body.String()
	purchaseOrderError(c, supplierorder.ErrInvalid)
	require.Equal(t, 404, response.Code)
	require.True(t, json.Valid(response.Body.Bytes()), response.Body.String())
	require.Equal(t, original, response.Body.String())
}
