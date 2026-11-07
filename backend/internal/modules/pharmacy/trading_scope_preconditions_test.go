package pharmacy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestTradingCommandsRejectChangedTenantBeforeBodyValidation(t *testing.T) {
	h := &tradingScopeHandlers{}
	for name, handler := range map[string]gin.HandlerFunc{
		"prepare":            h.prepare,
		"review":             h.decide,
		"licence revocation": h.revokeLicence,
	} {
		t.Run(name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "user-reviewer", TenantID: "11111111-1111-1111-1111-111111111111"})
			})
			r.PUT("/:id/:command/:revocation", handler)
			req := httptest.NewRequest(http.MethodPut, "/0199e000-0000-7000-8000-000000000099/0199e000-0000-7000-8000-000000000098/0199e000-0000-7000-8000-000000000097", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Helivanta-Expected-Tenant", "22222222-2222-2222-2222-222222222222")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			require.JSONEq(t, `{"error":"session_changed","message":"Your workspace changed. Refresh the page before continuing."}`, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		})
	}
}
