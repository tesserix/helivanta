package iam_test

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestPartnerMemberCollectionsDoNotFallbackToLegacyAuthority(t *testing.T) {
	router, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"member": testutil.TenantA}, Modules: []platform.Module{iam.New(nil)}})
	hospital := uuid.NewString()
	group := uuid.Must(uuid.NewV7()).String()
	for _, path := range []string{"/v1/iam/hospitals/" + hospital + "/partner-member-capabilities", "/v1/iam/hospitals/" + hospital + "/partner-member-candidates?limit=1", "/v1/iam/hospitals/" + hospital + "/partner-groups?limit=1", "/v1/iam/hospitals/" + hospital + "/partner-groups/" + group + "/member-proposals?limit=1"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("Authorization", "Bearer member")
			request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			request.Header.Set("X-Helivanta-Expected-Subject", "user-member")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 503, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "partner_group_unavailable")
		})
	}
}
