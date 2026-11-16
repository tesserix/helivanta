package platform_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type releaseSDKTransport func(*http.Request) (*http.Response, error)

func (f releaseSDKTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReleaseAdmissionIsAdditionalToMembershipAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		enabled, permission, member, loaded bool
		want                                int
	}{
		{"not_released", false, true, true, true, 404},
		{"permission_still_required", true, false, true, true, 403},
		{"membership_still_required", true, true, false, true, 403},
		{"provider_unavailable", true, true, true, false, 503},
		{"released_and_authorized", true, true, true, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			payload := `{"features":{"helivanta.release.team_accounts":{"defaultValue":true},"helivanta.release.pharmacy_setup":{"defaultValue":` + map[bool]string{true: "true", false: "false"}[tc.enabled] + `}}}`
			service, err := release.New(release.Config{URL: "https://growthbook.test", ClientKey: "sdk-test", Client: &http.Client{Transport: releaseSDKTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}, nil
			})}})
			require.NoError(t, err)
			if tc.loaded {
				require.NoError(t, service.Refresh(t.Context()))
			}
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "owner", TenantID: "018f8f5b-1111-7111-8111-111111111111"})
				permissions := authz.NewPermissionSet()
				if tc.permission {
					permissions = authz.NewPermissionSet("pharmacy.catalogue.read")
				}
				c.Set("authz.permissions", permissions)
				c.Next()
			})
			var members authz.MembershipChecker = alwaysMember{}
			if !tc.member {
				members = neverMember{}
			}
			router := platform.NewRouter(engine.Group("/v1"), members).WithReleaseAdmission(service.Admission)
			called := false
			router.Group("/pharmacy").GET("/pharmacies", "pharmacy.catalogue.read", func(c *gin.Context) { called = true; c.Status(200) })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest("GET", "/v1/pharmacy/pharmacies", nil))
			require.Equal(t, tc.want, response.Code, response.Body.String())
			require.Equal(t, tc.want == 200, called)
		})
	}
}

func TestRuntimeRegistryRefusesUnknownRoutesAndKeepsRecoveryBaseline(t *testing.T) {
	service, err := release.New(release.Config{URL: "https://growthbook.test", ClientKey: "sdk-test"})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(principalStub())
	router := platform.NewRouter(engine.Group("/v1"), neverMember{}).WithReleaseAdmission(service.Admission)
	router.GET("/iam/me/profile", authz.NoTenantMembership, func(c *gin.Context) { c.Status(200) })
	require.NoError(t, service.ValidateRoutes(engine.Routes()))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/v1/iam/me/profile", nil))
	require.Equal(t, 200, response.Code)
	router.GET("/new-undeclared-business-route", authz.Public, func(c *gin.Context) { c.Status(200) })
	require.ErrorContains(t, service.ValidateRoutes(engine.Routes()), "unclassified")
}
