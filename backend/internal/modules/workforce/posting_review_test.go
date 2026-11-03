package workforce_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPostingReviewCommandsFailClosedWithoutCurrentOwners(t *testing.T) {
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Perms["admin"] = append(opts.Perms["admin"], authz.Permission("workforce.posting.submit"), authz.Permission("workforce.posting.review"))
	})
	staff := h.staff("SYN-POSTING-REVIEW-SCOPE")
	proposal := uuid.Must(uuid.NewV7()).String()
	base := h.base + "/posting-staff/" + staff + "/posting-proposals/" + proposal
	for _, command := range []string{"commands", "decisions"} {
		t.Run(command, func(t *testing.T) {
			request := httptest.NewRequest("PUT", base+"/"+command+"/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer admin")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
			response := httptest.NewRecorder()
			h.handler.ServeHTTP(response, request)
			require.Equal(t, 503, response.Code, "broad permissions cannot replace unavailable current scope/source owners: %s", response.Body.String())
			require.NotContains(t, response.Body.String(), staff)
		})
	}
}

func TestPostingReviewDiscoveryFailsClosedWithoutCurrentOwners(t *testing.T) {
	h := newRequestHarness(t)
	request := httptest.NewRequest("GET", "/v1/workforce/posting-review-scopes", nil)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	require.Equal(t, 503, response.Code, "missing scope/source owners cannot be represented as verified empty discovery: %s", response.Body.String())
	require.NotContains(t, response.Body.String(), "data")
}
