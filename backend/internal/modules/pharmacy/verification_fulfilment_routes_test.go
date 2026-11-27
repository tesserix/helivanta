package pharmacy_test

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"strings"
	"testing"
)

func TestVerificationFulfilmentDeclaresProtectedReadProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := platform.NewRouter(gin.New().Group("/v1"), policyDraftRouteMembership{}).WithCursorSealer(testutil.CursorSealer(t))
	pharmacy.New().Routes(router, platform.Deps{})
	found := 0
	for _, route := range router.Declared() {
		if strings.HasSuffix(route.Path, "/fulfilment-review-context") {
			found++
			require.Equal(t, "POST", route.Method)
			require.Equal(t, pharmacy.PermPrescriptionRead, route.Permission)
		}
	}
	require.Equal(t, 1, found)
}
