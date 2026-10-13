package workspace_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workspace" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"gorm.io/gorm"
)

func TestBrandingPersistsPerTenantAndPublishesOnlyPublicFields(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"owner": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"owner": {workspace.PermBrandingManage}},
		Modules: []platform.Module{workspace.New()},
	})
	r.GET("/branding/:tenant", workspace.PublicHandler(db, ratelimit.NewMemory(100)))
	path := "/v1/workspace/branding"
	body := `{"display_name":"YES HOSPITAL","welcome_text":"Care starts with your team.","logo_url":"https://storage.googleapis.com/example/logo.png","theme":"teal","login_layout":"split"}`
	require.Equal(t, 403, testutil.Do(r, "PUT", path, "reader", body).Code)
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, testutil.Do(r, "GET", path, "reader", "").Body.String(), "YES HOSPITAL")
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", body).Code)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workspace_branding_audit").Count(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count, "unchanged PUT does not duplicate audit")
		return nil
	}))
	require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec("DELETE FROM workspace_branding_audit").Error }))

	require.NotContains(t, testutil.Do(r, "GET", path, "other", "").Body.String(), "YES HOSPITAL")
	w = testutil.Do(r, "GET", "/branding/"+testutil.TenantA, "", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "YES HOSPITAL")
	require.NotContains(t, w.Body.String(), "user-owner")
	require.Equal(t, http.StatusNotFound, testutil.Do(r, "GET", "/branding/not-a-tenant", "", "").Code)
	for _, bad := range []string{
		`{"display_name":"X","welcome_text":"X","logo_url":"javascript:alert(1)","theme":"teal","login_layout":"split"}`,
		`{"display_name":"X","welcome_text":"X","logo_url":"https://127.0.0.1/a.png","theme":"teal","login_layout":"split"}`,
		`{"display_name":"X","welcome_text":"X","logo_url":"","theme":"arbitrary-css","login_layout":"split"}`,
		`{"display_name":"X","welcome_text":"X","logo_url":"","theme":"teal","login_layout":"split","tenant_id":"other"}`,
	} {
		require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", bad).Code)
	}
}
