package pharmacy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

type policyDraftRouteMembership struct{}

func (policyDraftRouteMembership) IsMember(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestVerificationPolicyPreparationDeclaresOnlyAdministrativeRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := platform.NewRouter(gin.New().Group("/v1"), policyDraftRouteMembership{}).WithCursorSealer(testutil.CursorSealer(t))
	pharmacy.New().Routes(router, platform.Deps{})
	count := 0
	for _, route := range router.Declared() {
		if !strings.Contains(route.Path, "/verification-policy-drafts") {
			continue
		}
		count++
		require.Equal(t, pharmacy.PermAccessManage, route.Permission)
		require.False(t, strings.Contains(route.Path, "approve") || strings.Contains(route.Path, "activate"))
		if route.Method == "GET" && !strings.Contains(route.Path, ":revision") {
			require.True(t, route.Paginated)
		}
	}
	require.Equal(t, 4, count)
}
