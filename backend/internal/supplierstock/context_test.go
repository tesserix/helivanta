package supplierstock

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestChangedWorkspaceUsesSharedSessionErrorBeforeOwnerAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, name := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/allocations", nil)
			c.Set("authn.principal", authn.Principal{TenantID: "0199e000-0000-7000-8000-000000000001", Subject: "TEST-reader"})
			c.Set("authn.entry_refresh", func() bool { return true })
			c.Request.Header.Set("X-Helivanta-Expected-Tenant", "0199e000-0000-7000-8000-000000000001")
			c.Request.Header.Set("X-Helivanta-Expected-Subject", "TEST-reader")
			c.Request.Header.Set(name, "changed")
			_, _, _, ok := (HTTP{}).request(c)
			require.False(t, ok)
			require.Equal(t, 409, response.Code)
			require.JSONEq(t, `{"error":"session_changed","message":"The workspace or account changed. Refresh before continuing."}`, response.Body.String())
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}
