package iam

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestAssignmentListFailsClosedWithoutReleasePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery(), func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-owner"})
		c.Set("authz.permissions", authz.NewPermissionSet(PermAssignmentManage))
	})
	h := &assignmentProposalHandlers{}
	r.GET("/assignments", func(c *gin.Context) { _, _ = h.listFamily(c, pagination.Params{}, "leave") })
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/assignments", nil))
	require.Equal(t, 503, response.Code)
	require.Contains(t, response.Body.String(), "release_unavailable")
}

func TestEveryOwnerTemplateHasAnExplicitReleaseFamily(t *testing.T) {
	policy, err := release.DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true")
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	seen := map[string]bool{}
	for _, template := range staffAccessTemplates() {
		if seen[template.Family] {
			continue
		}
		seen[template.Family] = true
		t.Run(template.Family, func(t *testing.T) {
			_, err := policy.AssignmentEnabled(t.Context(), uuid.MustParse("11111111-1111-4111-8111-111111111111"), "synthetic-owner", template.Family)
			require.NoError(t, err, "every owner-resolved family must be classified")
		})
	}
}

func TestOperatingTemplateReleaseClassifiesEveryDeclaredDuty(t *testing.T) {
	policy, err := release.DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true")
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	for _, template := range operatingDutyTemplates {
		t.Run(template.Key, func(t *testing.T) {
			_, err := policy.OperatingTemplateEnabled(t.Context(), tenant, "synthetic-owner", template.Key)
			require.NoError(t, err)
		})
	}
}

func TestHospitalStocktakeAssignmentsRemainLockedWhenProviderEnablesThem(t *testing.T) {
	policy, err := release.New(release.Config{URL: "https://synthetic-release.invalid", ClientKey: "sdk-test", Client: &http.Client{Transport: assignmentReleaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"features":{"helivanta.release.team_accounts":{"defaultValue":true},"helivanta.release.pharmacy_setup":{"defaultValue":true},"helivanta.release.pharmacy_stocktake":{"defaultValue":true}}}`)), Header: http.Header{}}, nil
	})}})
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	enabled, err := policy.AssignmentEnabled(t.Context(), uuid.MustParse("11111111-1111-4111-8111-111111111111"), "synthetic-owner", "pharmacy_stocktake")
	require.NoError(t, err)
	require.False(t, enabled, "provider desired state cannot release unfinished hospital stocktakes")
}

func TestOperatingTemplateReleaseFailsClosedForLockedOrUnknownDuty(t *testing.T) {
	principal := authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-owner"}
	h := &operatingHandlers{}
	require.NoError(t, h.templateAvailability(t.Context(), principal, "pharmacy.inventory_receiver.v1"))
	require.ErrorIs(t, h.templateAvailability(t.Context(), principal, "pharmacy.stocktake_counter.v1"), authz.ErrScopedAuthorizationUnavailable)
	policy, err := release.New(release.Config{URL: "https://synthetic-release.invalid", ClientKey: "sdk-test", Client: &http.Client{Transport: assignmentReleaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"features":{"helivanta.release.team_accounts":{"defaultValue":true},"helivanta.release.pharmacy_setup":{"defaultValue":true},"helivanta.release.pharmacy_stocktake":{"defaultValue":true}}}`)), Header: http.Header{}}, nil
	})}})
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	h.releasePolicy = policy
	for _, template := range []string{"pharmacy.stocktake_reader.v1", "pharmacy.stocktake_counter.v1", "pharmacy.stocktake_reviewer.v1"} {
		require.ErrorIs(t, h.templateAvailability(t.Context(), principal, template), authz.ErrScopedCommandDenied)
	}
	require.ErrorIs(t, h.templateAvailability(t.Context(), principal, "pharmacy.unknown_future_duty.v1"), authz.ErrScopedAuthorizationUnavailable)
	require.NoError(t, h.templateAvailability(t.Context(), principal, "pharmacy.stock_fulfiller.v1"))
}

func TestUnreleasedAssignmentListStopsBeforeDatabaseAccess(t *testing.T) {
	policy, err := release.New(release.Config{URL: "https://synthetic-release.invalid", ClientKey: "sdk-test", Client: &http.Client{Transport: assignmentReleaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"features":{}}`)), Header: http.Header{}}, nil
	})}})
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	h := &assignmentProposalHandlers{releasePolicy: policy}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-owner"})
		c.Set("authz.permissions", authz.NewPermissionSet(PermAssignmentManage))
	})
	r.GET("/assignments", func(c *gin.Context) { _, _ = h.listFamily(c, pagination.Params{}, "supplier_access") })
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/assignments", nil))
	require.Equal(t, 404, response.Code)
}

type assignmentReleaseTransport func(*http.Request) (*http.Response, error)

func (f assignmentReleaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
