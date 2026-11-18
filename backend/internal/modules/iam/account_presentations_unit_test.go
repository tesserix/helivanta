package iam

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountPresentationRequiresAuthenticationBeforeDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &memberPresentationHandlers{}
	r.POST("/account-presentations", h.readAccounts)
	request := httptest.NewRequest(http.MethodPost, "/account-presentations", strings.NewReader(`{"subjects":["synthetic-account"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
