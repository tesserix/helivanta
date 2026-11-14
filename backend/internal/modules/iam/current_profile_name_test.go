package iam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type currentProfileFixture struct {
	profile         loginclient.UserProfile
	tenant, subject string
}

func (f currentProfileFixture) Verify(context.Context, string) (authn.Principal, error) {
	return authn.Principal{Subject: f.subject, TenantID: f.tenant, AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}, nil
}
func (currentProfileFixture) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}
func (f currentProfileFixture) UserProfile(_ context.Context, tenant, subject string) (loginclient.UserProfile, error) {
	if tenant != f.tenant || subject != f.subject {
		return loginclient.UserProfile{}, loginclient.ErrUnavailable
	}
	return f.profile, nil
}
func (f currentProfileFixture) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return []authz.RoleBinding{{TenantID: f.tenant, Role: authz.RoleTenantAdmin}, {TenantID: "22222222-2222-4222-8222-222222222222", Role: authz.RoleDoctor}}, nil
}

func TestCurrentProfileNamesOnlyTheAuthenticatedTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := currentProfileFixture{
		tenant: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner",
		profile: loginclient.UserProfile{DisplayName: "Synthetic Owner", Email: "owner@example.test", TenantName: "Yes Hospital"},
	}
	r := gin.New()
	r.Use(authn.Middleware(f, f))
	h := meHandlers{roles: f, profiles: f}
	r.GET("/me/profile", h.profile)
	req := httptest.NewRequest(http.MethodGet, "/me/profile?tenant_id=22222222-2222-4222-8222-222222222222", nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	req.Header.Set("X-Helivanta-Expected-Tenant", "22222222-2222-4222-8222-222222222222")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"subject":"synthetic-owner","tenant_id":"11111111-1111-4111-8111-111111111111","tenant_name":"Yes Hospital","display_name":"Synthetic Owner","email":"owner@example.test","roles":[{"key":"tenant_admin","label":"Tenant Admin"}]}`, w.Body.String())
}
