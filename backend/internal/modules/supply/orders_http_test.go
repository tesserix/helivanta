package supply_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's external test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPurchaseOrderRoutesAuthenticateBeforeBodyOrStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	router := platform.NewRouter(engine.Group("/v1"), &authz.Client{}).WithCursorSealer(testutil.CursorSealer(t))
	supply.New().Routes(router, platform.Deps{})
	buyer := "/v1/supply/hospitals/11111111-1111-4111-8111-111111111111/departments/019a55c0-1111-7111-8111-111111111112/outlets/019a55c0-1111-7111-8111-111111111113/purchase-orders/019a55c0-1111-7111-8111-111111111114"
	seller := "/v1/supply/supplier-orders/019a55c0-1111-7111-8111-111111111114"
	for _, tc := range []struct{ method, path string }{{"PUT", buyer}, {"GET", buyer}, {"GET", buyer + "/proposal-receipt"}, {"PUT", buyer + "/commands/019a55c0-1111-7111-8111-111111111115"}, {"GET", buyer + "/commands/019a55c0-1111-7111-8111-111111111115"}, {"GET", seller}, {"PUT", seller + "/commands/019a55c0-1111-7111-8111-111111111115"}, {"GET", seller + "/commands/019a55c0-1111-7111-8111-111111111115"}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"malformed":`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, 401, response.Code, response.Body.String())
		})
	}
}
