package iam

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOwnerSecurityRejectsChangedAccountAndCrossOriginBeforeDependencies(t *testing.T) {
	h := &ownerSecurityHandlers{}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		handler := h.read
		if method == http.MethodPost {
			handler = h.reconcile
		}
		w := boundaryRequest(t, method, "/iam/me/security", "{}", handler, func(r *http.Request) {
			r.Header.Set("X-Helivanta-Expected-Tenant", "11111111-1111-4111-8111-111111111111")
		})
		require.Equal(t, 409, w.Code)
	}
	w := boundaryRequest(t, http.MethodPost, "/iam/me/security", "{}", h.reconcile, func(r *http.Request) { r.Header.Set("Origin", "https://other.example") })
	require.Equal(t, 403, w.Code)
}

func TestOwnerSecurityRequiresRecentAuthenticationForReconciliation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	identity := boundaryIdentity{authn.Principal{Subject: "owner", TenantID: boundaryHospital, AuthTime: now.Add(-6 * time.Minute), IdleDeadline: now.Add(time.Hour)}}
	router := gin.New()
	router.Use(authn.Middleware(identity, identity))
	h := &ownerSecurityHandlers{}
	router.POST("/iam/me/security", h.reconcile)
	request := httptest.NewRequest(http.MethodPost, "/iam/me/security", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer session")
	request.Header.Set("Origin", "https://helivanta.app")
	request.Header.Set("X-Helivanta-Expected-Tenant", boundaryHospital)
	request.Header.Set("X-Helivanta-Expected-Subject", "owner")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 401, response.Code)
	require.Contains(t, response.Body.String(), "reauthentication_required")
}
