package organization

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestAssignmentDepartmentChoicesRequireAnAuthenticatedAccount(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/choices", nil)
	rows, err := (&handlers{}).listAssignmentDepartmentChoices(c, pagination.Params{Limit: 20})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestAssignmentDepartmentChoiceQueryRejectsCallerFilters(t *testing.T) {
	for _, query := range []string{"?status=pending", "?status=", "?status=active&status=active", "?subject=other", "?status=retired", "?limit=1&limit=20", "?cursor=", "?limit=%zz", "?limit=20;status=active", "?"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
		require.False(t, assignmentDepartmentChoiceQuery(c), query)
	}
	for _, query := range []string{"", "?limit=20", "?status=active", "?cursor=opaque&limit=1"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
		require.True(t, assignmentDepartmentChoiceQuery(c), query)
	}
}
