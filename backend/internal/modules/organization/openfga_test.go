package organization_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestOrganizationRoutesUseRealOpenFGAAndFailClosedAfterRevocationOrOutage(t *testing.T) {
	ctx := context.Background()
	upstream, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var outage atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outage.Load() {
			http.Error(w, "synthetic authorization outage", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	fga, err := authz.NewClient(ctx, server.URL, "organization-route-"+uuid.NewString())
	require.NoError(t, err)
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember, authz.RolePharmacist} {
		require.NoError(t, fga.GrantTenantRole(ctx, testutil.TenantA, role))
	}
	require.NoError(t, fga.GrantPermission(ctx, testutil.TenantA, organization.PermStructureManage, authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantPermission(ctx, testutil.TenantA, organization.PermStructureRead, authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantPermission(ctx, testutil.TenantA, organization.PermStructureRead, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-manager", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-manager", authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-pharmacist", authz.RolePharmacist))
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"manager": testutil.TenantA, "pharmacist": testutil.TenantA}, Resolver: fga, Membership: fga, Writer: fga, Roles: fga, Modules: []platform.Module{organization.New()}})
	hospital := uuid.NewString()
	requestID := uuid.Must(uuid.NewV7())
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + requestID.String()
	body := `{"reason":"Synthetic real OpenFGA registration"}`
	require.Equal(t, 200, organizationCall(r, "PUT", path, "manager", testutil.TenantA, body).Code)
	for _, consumer := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if consumer.Name != "organization-hospital-observation" {
			continue
		}
		raw, e := json.Marshal(map[string]any{"request_id": requestID, "hospital_id": hospital, "exists": true})
		require.NoError(t, e)
		evt := events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: raw}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
	}
	department := departmentPath(hospital, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
	departmentBody := departmentIntent(0, "PHARMA", "Synthetic FGA Pharmacy", "department", "", "active")
	require.Equal(t, 200, organizationCall(r, "PUT", department, "manager", testutil.TenantA, departmentBody).Code)
	require.Equal(t, 403, organizationCall(r, "PUT", department, "pharmacist", testutil.TenantA, departmentBody).Code)

	require.Equal(t, 403, organizationCall(r, "PUT", path, "pharmacist", testutil.TenantA, body).Code, "a pharmacy role grants no structure management")
	require.Equal(t, 403, organizationCall(r, "GET", path, "pharmacist", testutil.TenantA, "").Code)
	require.NoError(t, fga.RevokeRole(ctx, testutil.TenantA, "user-manager", authz.RoleTenantAdmin))
	require.Equal(t, 403, organizationCall(r, "PUT", department, "manager", testutil.TenantA, departmentBody).Code)
	require.Equal(t, 200, organizationCall(r, "GET", department, "manager", testutil.TenantA, "").Code)
	require.Equal(t, 403, organizationCall(r, "PUT", path, "manager", testutil.TenantA, body).Code, "the same authenticated identity cannot replay after its manager relationship is revoked")
	require.Equal(t, 200, organizationCall(r, "GET", path, "manager", testutil.TenantA, "").Code, "the independent reader relationship remains effective")
	outage.Store(true)
	require.Equal(t, 503, organizationCall(r, "GET", department, "manager", testutil.TenantA, "").Code)
	require.Equal(t, 503, organizationCall(r, "PUT", department, "manager", testutil.TenantA, departmentBody).Code)
	require.Equal(t, 503, organizationCall(r, "GET", path, "manager", testutil.TenantA, "").Code, "a previous successful resolution does not authorize an outage read")
	require.Equal(t, 503, organizationCall(r, "PUT", path, "manager", testutil.TenantA, body).Code)
	outage.Store(false)
	require.Equal(t, 200, organizationCall(r, "GET", path, "manager", testutil.TenantA, "").Code)
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("organization_hospital_requests").Count(&count).Error }))
	require.EqualValues(t, 1, count, "refused attempts cannot create retained commands")
}
