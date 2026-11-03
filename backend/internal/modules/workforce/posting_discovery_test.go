package workforce_test

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"strings"
	"testing"
)

type postingDiscoveryMember struct{}

func (postingDiscoveryMember) IsMember(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestPostingScopeDiscoveryRejectsMissingAccountIntentBeforeSourceReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "synthetic-reader", TenantID: "11111111-1111-4111-8111-111111111111"})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), postingDiscoveryMember{})
	workforce.New().Routes(r, platform.Deps{})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/workforce/posting-scopes", nil))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "session_changed")
}

func TestPostingWorkspacePagesFitWireBudget(t *testing.T) {
	targets := make([]workforce.StaffPostingTarget, 50)
	hospitals := make([]workforce.StaffPostingHospitalScope, 20)
	for i := range targets {
		targets[i] = workforce.StaffPostingTarget{ID: uuid.Must(uuid.NewV7()), EmployeeCode: strings.Repeat("<", 40), DisplayName: strings.Repeat("&", 150)}
	}
	for i := range hospitals {
		hospitals[i] = workforce.StaffPostingHospitalScope{HospitalID: uuid.Must(uuid.NewV7()), Name: strings.Repeat("&", 150), CanPrepare: true}
	}
	for _, rows := range []any{targets, hospitals} {
		payload, err := json.Marshal(map[string]any{"data": rows, "page": map[string]any{"has_more": true, "next_cursor": strings.Repeat("X", 2048)}})
		require.NoError(t, err)
		require.LessOrEqual(t, len(payload), 64*1024, "maximum escaped fields and continuation fit the documented wire budget")
	}
}
