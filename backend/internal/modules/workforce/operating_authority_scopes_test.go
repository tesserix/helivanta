package workforce

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestGovernanceOperatingDiscoveryNeverListsAnUnauthenticatedEmployer(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/workforce/operating-authority-scopes", nil)
	h := &handlers{}
	rows, err := h.operatingAuthorityScopeCandidates(c, pagination.Params{Limit: 20})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
