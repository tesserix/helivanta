package iam

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/pagination"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGovernanceOperatingLocationChoicesNeverListsWithoutIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/choices", nil)
	h := &operatingHandlers{native: &operatingNativeRequest{}}
	rows, err := h.locationChoices(c, pagination.Params{Limit: 20})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestGovernanceOperatingLocationChoiceQueryIsClosed(t *testing.T) {
	for _, query := range []string{"?country=PK", "?country=IN&country=IN", "?limit=1&limit=20", "?cursor=", "?country=%zz", "?subject=another-account", "?"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
			require.False(t, operatingLocationChoiceQuery(c))
		})
	}
	for _, query := range []string{"", "?country=IN", "?country=IN&limit=20", "?cursor=sealed&limit=1"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
			require.True(t, operatingLocationChoiceQuery(c))
		})
	}
}
