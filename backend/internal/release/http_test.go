package release

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestCapabilityReadBindsAccountAndRefusesClientTargeting(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	payload := `{"features":{"helivanta.release.team_accounts":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	require.NoError(t, service.Refresh(t.Context()))
	const tenant = "018f8f5b-1111-7111-8111-111111111111"
	for _, tc := range []struct {
		name, query, expectedTenant, expectedSubject string
		authenticated                                bool
		want                                         int
	}{
		{"current_account", "", tenant, "owner", true, 200},
		{"missing_headers", "", "", "", true, 409},
		{"another_tenant", "", "018f8f5b-2222-7222-8222-222222222222", "owner", true, 409},
		{"another_account", "", tenant, "another-owner", true, 409},
		{"no_session", "", tenant, "owner", false, 401},
		{"caller_cannot_target_tenant", "?tenant_id=another", tenant, "owner", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			if tc.authenticated {
				engine.Use(func(c *gin.Context) {
					c.Set("authn.principal", authn.Principal{TenantID: tenant, Subject: "owner"})
					c.Next()
				})
			}
			engine.GET("/capabilities", service.Capabilities)
			request := httptest.NewRequest(http.MethodGet, "/capabilities"+tc.query, nil)
			request.Header.Set("X-Helivanta-Expected-Tenant", tc.expectedTenant)
			request.Header.Set("X-Helivanta-Expected-Subject", tc.expectedSubject)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, tc.want, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
			require.NotContains(t, response.Body.String(), "sdk-test")
			if tc.want == 200 {
				var snapshot ReleaseSnapshot
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &snapshot))
				require.Equal(t, tenant, snapshot.TenantID.String())
				require.Equal(t, "owner", snapshot.Subject)
				require.True(t, available(t, snapshot, "team_accounts"))
			} else {
				require.NotContains(t, response.Body.String(), `"capabilities"`)
			}
		})
	}
}

func TestDevelopmentReleaseFixtureCannotRunWithProductionIdentity(t *testing.T) {
	for _, config := range []struct{ env, issuer, enabled string }{
		{"prod", "http://auth.tesserix.localhost:20080", "true"},
		{"dev", "https://auth.tesserix.app", "true"},
		{"dev", "http://auth.tesserix.localhost:20080", "false"},
	} {
		_, err := DevelopmentFixture(config.env, config.issuer, config.enabled)
		require.Error(t, err)
	}
	fixture, err := DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true")
	require.NoError(t, err)
	require.NoError(t, fixture.Refresh(t.Context()))
}

func TestCompleteRouteValidationPermitsOnlyExplicitConditionalOwnerCallbacks(t *testing.T) {
	routes := gin.RoutesInfo{}
	for key := range routeCapabilities {
		parts := strings.SplitN(key, " ", 2)
		routes = append(routes, gin.RouteInfo{Method: parts[0], Path: parts[1]})
	}
	policy := &Service{}
	require.NoError(t, policy.ValidateCompleteRoutes(routes), "disabled internal integrations must not prevent startup")
	for key := range internalRouteCapabilities {
		parts := strings.SplitN(key, " ", 2)
		routes = append(routes, gin.RouteInfo{Method: parts[0], Path: parts[1]})
	}
	require.NoError(t, policy.ValidateCompleteRoutes(routes), "all production callbacks must be classified")
	routes = append(routes, gin.RouteInfo{Method: "POST", Path: "/internal/v1/unknown"})
	require.ErrorContains(t, policy.ValidateCompleteRoutes(routes), "unclassified release route")
	require.ErrorContains(t, policy.ValidateCompleteRoutes(nil), "missing release route")
}
