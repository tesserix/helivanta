package authn_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestExpectedTenantPrincipalChecksOnlyTheVerifiedTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const tenant = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, tc := range []struct {
		name   string
		values []string
		status int
	}{
		{"absent", nil, 200}, {"matching", []string{tenant}, 200}, {"equivalent UUID", []string{strings.ToUpper(tenant)}, 200},
		{"empty", []string{""}, 409}, {"malformed", []string{"not-a-tenant"}, 409}, {"zero", []string{"00000000-0000-0000-0000-000000000000"}, 409},
		{"mismatch", []string{"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}, 409}, {"duplicate", []string{tenant, tenant}, 409},
		{"conflicting duplicate", []string{tenant, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}, 409}, {"comma joined", []string{tenant + "," + tenant}, 409},
		{"whitespace", []string{" " + tenant}, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "verified-user", TenantID: tenant, IdleDeadline: time.Now().Add(time.Hour)}}, neverRevoked{}), func(c *gin.Context) {
				p, id, ok := authn.ExpectedTenantPrincipal(c)
				if !ok {
					return
				}
				require.Equal(t, "verified-user", p.Subject)
				c.JSON(200, gin.H{"tenant_id": id.String()})
			}, func(c *gin.Context) { require.NotEqual(t, 409, tc.status, "aborted requests must not continue") })
			req := httptest.NewRequest("GET", "/p", nil)
			req.Header.Set("Authorization", "Bearer good")
			for _, v := range tc.values {
				req.Header.Add("X-Helivanta-Expected-Tenant", v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				require.JSONEq(t, `{"tenant_id":"`+tenant+`"}`, w.Body.String())
			} else {
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.Contains(t, w.Body.String(), `"error":"session_changed"`)
				require.NotContains(t, w.Body.String(), tenant)
			}
		})
	}
}

func TestExpectedTenantPrincipalCannotAuthenticateAMissingOrInvalidPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tenant := range []string{"", "invalid"} {
		r := gin.New()
		r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "verified-user", TenantID: tenant, IdleDeadline: time.Now().Add(time.Hour)}}, neverRevoked{}), func(c *gin.Context) { _, _, ok := authn.ExpectedTenantPrincipal(c); require.False(t, ok) })
		req := httptest.NewRequest("GET", "/p", nil)
		req.Header.Set("Authorization", "Bearer good")
		req.Header.Set("X-Helivanta-Expected-Tenant", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 401, w.Code)
	}
	r := gin.New()
	r.GET("/p", func(c *gin.Context) { _, _, ok := authn.ExpectedTenantPrincipal(c); require.False(t, ok) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/p", nil))
	require.Equal(t, 401, w.Code)
}
