package iam_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test imports its own module, not another clinical module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
)

type provisioningCaller struct{}

func (provisioningCaller) Verify(_ context.Context, header string) error {
	if header != "Bearer approved" {
		return errors.New("rejected")
	}
	return nil
}

type provisioningFGA struct {
	grants int
	fail   bool
}

func (f *provisioningFGA) GrantRole(context.Context, string, string, authz.Role) error {
	f.grants++
	if f.fail {
		return errors.New("unavailable")
	}
	return nil
}
func (*provisioningFGA) RevokeRole(context.Context, string, string, authz.Role) error { return nil }
func (*provisioningFGA) GrantPermission(context.Context, string, authz.Permission, authz.Role) error {
	return nil
}
func (*provisioningFGA) GrantTenantRole(context.Context, string, authz.Role) error { return nil }
func TestHospitalProvisioningPreservesExistingMembershipsAndRetriesFGA(t *testing.T) {
	_, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	fga := &provisioningFGA{fail: true}
	r := gin.New()
	r.POST("/onboard", iam.NewOnboardingHandler(platform.Deps{DB: db, Bus: bus, Authz: fga, Reconcile: func(context.Context, string) error { return nil }}, provisioningCaller{}))
	allowBootstrap := true
	call := func(token, subject string) int {
		req := httptest.NewRequest("POST", "/onboard", strings.NewReader(`{"allowBootstrap":`+fmt.Sprint(allowBootstrap)+`,"tenantId":"`+testutil.TenantA+`","organizationId":"org","initialAdminUserId":"`+subject+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, 401, call("Bearer foreign", "new-admin"))
	require.Zero(t, fga.grants)
	require.Equal(t, 503, call("Bearer approved", "new-admin"))
	fga.fail = false
	require.Equal(t, 200, call("Bearer approved", "new-admin"))
	require.Equal(t, 200, call("Bearer approved", "new-admin"))
	require.Equal(t, 409, call("Bearer approved", "different-admin"))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if e := tx.Table("iam_members").Where("subject=? AND role_key=?", "new-admin", "tenant_admin").Count(&count).Error; e != nil {
			return e
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
	allowBootstrap = false
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec("DELETE FROM iam_members").Error }))
	require.Equal(t, 409, call("Bearer approved", "new-admin"), "a completed onboarding must never recreate revoked membership")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		var count int64
		if e := tx.Table("iam_members").Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestHospitalProvisioningUsesDedicatedWorkloadHeader(t *testing.T) {
	for _, tc := range []struct {
		name, workload, authorization string
		status                        int
	}{
		{"dedicated header", "Bearer approved", "", 400},
		{"legacy direct header", "", "Bearer approved", 400},
		{"foreign workload", "Bearer foreign", "", 401},
		{"ambiguous credentials", "Bearer approved", "Bearer approved", 401},
		{"missing credentials", "", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/onboard", iam.NewOnboardingHandler(platform.Deps{}, provisioningCaller{}))
			req := httptest.NewRequest("POST", "/onboard", strings.NewReader(`{}`))
			if tc.workload != "" {
				req.Header.Set("X-Onboarding-Token", tc.workload)
			}
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
		})
	}
}

func TestHospitalProvisioningRejectsRepeatedCredentialHeaders(t *testing.T) {
	for _, header := range []string{"X-Onboarding-Token", "Authorization"} {
		t.Run(header, func(t *testing.T) {
			r := gin.New()
			r.POST("/onboard", iam.NewOnboardingHandler(platform.Deps{}, provisioningCaller{}))
			req := httptest.NewRequest("POST", "/onboard", strings.NewReader(`{}`))
			req.Header.Add(header, "Bearer approved")
			req.Header.Add(header, "Bearer approved")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, 401, w.Code)
		})
	}
}
