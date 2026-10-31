package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type refreshVerifier struct {
	principal Principal
	err       error
	observed  []string
}

func (v *refreshVerifier) Verify(_ context.Context, raw string) (Principal, error) {
	v.observed = append(v.observed, raw)
	return v.principal, v.err
}

type refreshRevocation struct {
	at  time.Time
	err error
}

func (r *refreshRevocation) RevokedAfter(context.Context, string) (time.Time, error) {
	return r.at, r.err
}

func TestRefreshOriginalCredentialAndCurrentChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*gin.Context, *refreshVerifier, *refreshRevocation)
		status int
	}{
		{"current", func(*gin.Context, *refreshVerifier, *refreshRevocation) {}, 200},
		{"replacement_header_not_borrowed", func(c *gin.Context, _ *refreshVerifier, _ *refreshRevocation) {
			c.Request.Header.Set("Authorization", "Bearer replacement")
		}, 200},
		{"revoked", func(_ *gin.Context, v *refreshVerifier, r *refreshRevocation) { r.at = v.principal.AuthTime }, 401},
		{"revocation_unavailable", func(_ *gin.Context, _ *refreshVerifier, r *refreshRevocation) {
			r.err = errors.New("source unavailable")
		}, 503},
		{"verification_expired", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) { v.err = errors.New("expired") }, 401},
		{"idle_expired", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) {
			v.principal.IdleDeadline = time.Now().Add(-time.Second)
		}, 401},
		{"subject_changed", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) { v.principal.Subject = "other" }, 401},
		{"tenant_changed", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) {
			v.principal.TenantID = "22222222-2222-2222-2222-222222222222"
		}, 401},
		{"authentication_time_changed", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) {
			v.principal.AuthTime = v.principal.AuthTime.Add(time.Second)
		}, 401},
		{"idle_deadline_extended", func(_ *gin.Context, v *refreshVerifier, _ *refreshRevocation) {
			v.principal.IdleDeadline = v.principal.IdleDeadline.Add(time.Hour)
		}, 401},
		{"identity_denied", func(c *gin.Context, _ *refreshVerifier, _ *refreshRevocation) {
			BindIdentityRefresh(c, func() bool { c.AbortWithStatusJSON(401, gin.H{"error": "inactive"}); return false })
		}, 401},
		{"identity_unavailable", func(c *gin.Context, _ *refreshVerifier, _ *refreshRevocation) {
			BindIdentityRefresh(c, func() bool { c.AbortWithStatusJSON(503, gin.H{"error": "identity_unavailable"}); return false })
		}, 503},
		{"identity_missing_result", func(c *gin.Context, _ *refreshVerifier, _ *refreshRevocation) {
			BindIdentityRefresh(c, func() bool { return false })
		}, 503},
		{"identity_missing_check", func(c *gin.Context, _ *refreshVerifier, _ *refreshRevocation) { BindIdentityRefresh(c, nil) }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := Principal{Subject: "staff-user", TenantID: testTenant, AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
			verifier := &refreshVerifier{principal: original}
			rev := &refreshRevocation{}
			router := gin.New()
			entered := false
			router.GET("/private", Middleware(verifier, rev), func(c *gin.Context) {
				tc.change(c, verifier, rev)
				if !Refresh(c) {
					return
				}
				entered = true
				principal, ok := PrincipalFrom(c)
				require.True(t, ok)
				require.Equal(t, original, principal)
				c.JSON(200, gin.H{"subject": principal.Subject})
			})
			request := httptest.NewRequest("GET", "/private", nil)
			request.Header.Set("Authorization", "Bearer original")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.status == 200, entered)
			require.Equal(t, []string{"original", "original"}, verifier.observed)
			require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
			if tc.status != 200 {
				require.NotContains(t, response.Body.String(), `"subject"`)
			}
		})
	}
}

func TestRefreshWithoutEntryCapabilityFailsClosed(t *testing.T) {
	router := gin.New()
	router.GET("/private", func(c *gin.Context) {
		if Refresh(c) {
			c.Status(http.StatusOK)
		}
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/private", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "authz_unavailable")
}
