package testutil

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBrowserReleaseSnapshotRequiresTheExactFixtureAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	MountBrowserReleaseCapabilities(t, engine, StaticVerifier{"owner": TenantA})
	for _, tc := range []struct {
		token, tenant, subject string
		want                   int
	}{
		{"owner", TenantA, "user-owner", 200},
		{"owner", TenantB, "user-owner", 409},
		{"owner", TenantA, "another-user", 409},
		{"unknown", TenantA, "user-owner", 401},
	} {
		req := httptest.NewRequest("GET", "/v1/workspace/release-capabilities", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-Helivanta-Expected-Tenant", tc.tenant)
		req.Header.Set("X-Helivanta-Expected-Subject", tc.subject)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		require.Equal(t, tc.want, response.Code, response.Body.String())
		if tc.want == 200 {
			require.Contains(t, response.Body.String(), `"key":"emergency","available":true`)
		}
	}
}
