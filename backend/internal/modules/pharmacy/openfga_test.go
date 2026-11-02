package pharmacy_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // External test importing its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestCatalogueRequiresRealOpenFGAAndCurrentOutletGrant(t *testing.T) {
	ctx := t.Context()
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
	fga, err := authz.NewClient(ctx, server.URL, "pharmacy-route-"+uuid.NewString())
	require.NoError(t, err)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		for _, grant := range pharmacy.New().Permissions() {
			for _, role := range grant.Roles {
				require.NoError(t, fga.GrantTenantRole(ctx, tenant, role))
				require.NoError(t, fga.GrantPermission(ctx, tenant, grant.Permission, role))
			}
		}
	}
	for subject, role := range map[string]authz.Role{
		"editor": authz.RolePharmacyOwner, "pharmacist": authz.RolePharmacist,
		"inventory": authz.RolePharmacyInventory, "sibling": authz.RolePharmacist,
	} {
		require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-"+subject, role))
	}
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantB, "user-foreign", authz.RolePharmacist))
	// Preserve tenant membership when the pharmacist's operational role is revoked.
	require.NoError(t, fga.GrantTenantRole(ctx, testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-pharmacist", authz.RoleStaffMember))
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA,
			"inventory": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB},
		Resolver: fga, Membership: fga, Writer: fga, Roles: fga,
		Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, subject := range []string{"editor", "pharmacist", "inventory"} {
		grantCatalogue(t, r, "user-"+subject, "editor")
	}
	id := uuid.Must(uuid.NewV7()).String()
	path := productPath(id)
	body := productBody(0, "Synthetic OpenFGA catalogue")
	w := do(r, "PUT", path, "pharmacist", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, token := range []string{"editor", "pharmacist", "inventory"} {
		require.Equal(t, 200, do(r, "GET", path, token, "").Code)
	}
	// An outlet editor grant cannot create a capability absent from OpenFGA.
	require.Equal(t, 403, do(r, "PUT", path, "inventory", productBody(1, "Forbidden inventory edit")).Code)
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "GET", path, token, "").Code)
		require.Equal(t, 404, do(r, "PUT", path, token, body).Code)
	}
	otherOutlet := strings.Replace(path, pharmaOutlet, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, 404, do(r, "GET", otherOutlet, "pharmacist", "").Code)
	prescriptions := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/prescriptions"
	require.Equal(t, 403, do(r, "GET", prescriptions, "editor", "").Code, "outlet ownership does not disclose prescriptions")
	require.Equal(t, 200, do(r, "GET", prescriptions, "pharmacist", "").Code)

	require.NoError(t, fga.RevokeRole(ctx, testutil.TenantA, "user-pharmacist", authz.RolePharmacist))
	require.Equal(t, 403, do(r, "GET", path, "pharmacist", "").Code)
	require.Equal(t, 403, do(r, "PUT", path, "pharmacist", body).Code, "an exact retry reauthorizes the retained identity")
	require.Equal(t, 403, do(r, "GET", prescriptions, "pharmacist", "").Code)
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-pharmacist", authz.RolePharmacist))
	require.Equal(t, 200, do(r, "GET", path, "pharmacist", "").Code)
	access := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/access/user-pharmacist"
	w = do(r, "DELETE", access, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, do(r, "GET", path, "pharmacist", "").Code)
	require.Equal(t, 404, do(r, "PUT", path, "pharmacist", body).Code)
	require.Equal(t, 404, do(r, "GET", prescriptions, "pharmacist", "").Code)
	grantCatalogue(t, r, "user-pharmacist", "editor")
	require.Equal(t, 200, do(r, "GET", path, "pharmacist", "").Code)

	outage.Store(true)
	require.Equal(t, 503, do(r, "GET", path, "pharmacist", "").Code)
	require.Equal(t, 503, do(r, "PUT", path, "pharmacist", body).Code)
	require.Equal(t, 503, do(r, "GET", prescriptions, "pharmacist", "").Code)
	outage.Store(false)
	require.Equal(t, 200, do(r, "PUT", path, "pharmacist", body).Code)
	var counts struct{ Products, Revisions, Audits, Events int64 }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, "user-editor", pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT
			(SELECT count(*) FROM pharmacy_catalogue_products) AS products,
			(SELECT count(*) FROM pharmacy_catalogue_revisions) AS revisions,
			(SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_catalogue_products') AS audits,
			(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.pharmacy.catalogue_updated.v1') AS events`).Scan(&counts).Error
	}))
	require.Equal(t, int64(1), counts.Products)
	require.Equal(t, int64(1), counts.Revisions)
	require.Equal(t, int64(1), counts.Audits)
	require.Equal(t, int64(1), counts.Events)
}
