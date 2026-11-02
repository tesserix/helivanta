package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestDepartmentHandoffUsesBothOwnersAndRealOpenFGA(t *testing.T) {
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
	fga, err := authz.NewClient(ctx, server.URL, "department-handoff-"+uuid.NewString())
	require.NoError(t, err)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, fga.GrantTenantRole(ctx, tenant, authz.RoleTenantAdmin))
		for _, perm := range []authz.Permission{"workforce.hospital.manage", "workforce.master.manage", "organization.structure.manage", "organization.structure.read"} {
			require.NoError(t, fga.GrantPermission(ctx, tenant, perm, authz.RoleTenantAdmin))
		}
	}
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-manager", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantB, "user-foreign", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantTenantRole(ctx, testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-manager", authz.RoleStaffMember))
	var modules []platform.Module
	for _, m := range bootstrap.Modules(nil, nil) {
		if m.Name() == "workforce" || m.Name() == "organization" {
			modules = append(modules, m)
		}
	}
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"manager": testutil.TenantA, "foreign": testutil.TenantB}, Resolver: fga, Membership: fga, Writer: fga, Roles: fga, Modules: modules})
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if token == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	hospital, master := uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, call("PUT", base, "manager", `{"name":"Synthetic Canonical Department Handoff"}`).Code)
	require.Equal(t, 200, call("PUT", base+"/access/user-manager", "manager", `{"level":"manager"}`).Code)
	masterPath := base + "/hr/masters/" + master
	w := call("PUT", masterPath, "manager", `{"expected_version":0,"kind":"department","code":"SYN-PHARMACY","name":"Synthetic Legacy Pharmacy","active":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var source struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &source))
	registration := "/v1/organization/hospitals/" + hospital + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, call("PUT", registration, "manager", `{"reason":"Synthetic owner protocol"}`).Code)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.Contains(c, call("GET", registration, "manager", "").Body.String(), `"status":"verified"`)
	}, 10*time.Second, 50*time.Millisecond)
	node, revision := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	departmentPath := "/v1/organization/hospitals/" + hospital + "/departments/" + node + "/revisions/"
	departmentBody := `{"expected_version":0,"code":"SYN-PHARMA","name":"Synthetic Structural Pharmacy","kind":"department","status":"active","reason":"Synthetic source"}`
	w = call("PUT", departmentPath+revision, "manager", departmentBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	link := func(version int, rev string) (string, string) {
		return masterPath + "/department-links/" + uuid.Must(uuid.NewV7()).String(), fmt.Sprintf(`{"expected_version":%d,"master_revision_id":%q,"department_id":%q,"department_revision_id":%q,"reason":"Synthetic reviewed migration mapping"}`, version, source.RevisionID, node, rev)
	}
	path, body := link(0, revision)
	w = call("PUT", path, "manager", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.Contains(c, call("GET", path, "manager", "").Body.String(), `"status":"matched"`)
	}, 10*time.Second, 50*time.Millisecond)
	first := call("GET", path, "manager", "").Body.String()
	require.NotContains(t, first, "Synthetic reviewed migration mapping")
	require.Contains(t, first, `"observed_at":`)
	require.JSONEq(t, first, call("PUT", path, "manager", body).Body.String())
	require.Equal(t, 404, call("GET", path, "foreign", "").Code)
	newRevision := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, call("PUT", departmentPath+newRevision, "manager", strings.Replace(departmentBody, `"expected_version":0`, `"expected_version":1`, 1)).Code)
	stale, staleBody := link(1, revision)
	require.Equal(t, 200, call("PUT", stale, "manager", staleBody).Code)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.Contains(c, call("GET", stale, "manager", "").Body.String(), `"status":"rejected"`)
	}, 10*time.Second, 50*time.Millisecond)
	current, currentBody := link(2, newRevision)
	require.Equal(t, 200, call("PUT", current, "manager", currentBody).Code)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.Contains(c, call("GET", current, "manager", "").Body.String(), `"status":"matched"`)
	}, 10*time.Second, 50*time.Millisecond)
	require.JSONEq(t, first, call("GET", path, "manager", "").Body.String(), "later changes retain the original snapshot, not current authority")
	var persisted events.Event
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var raw []byte
		if err := tx.Raw(`SELECT payload FROM outbox_events WHERE subject='helivanta.in.organization.department_link_observed.v1' AND payload->'data'->>'request_id'=? ORDER BY created_at LIMIT 1`, strings.TrimPrefix(path, masterPath+"/department-links/")).Row().Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &persisted)
	}))
	require.NotContains(t, string(persisted.Data), "Synthetic")
	var observation events.Consumer
	for _, m := range modules {
		if m.Name() == "workforce" {
			for _, c := range m.Consumers(platform.Deps{Bus: bus}) {
				if c.Name == "workforce-department-link-observation" {
					observation = c
				}
			}
		}
	}
	require.NotNil(t, observation.Handle)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return observation.Handle(ctx, tx, persisted) }))
	var contradiction map[string]any
	require.NoError(t, json.Unmarshal(persisted.Data, &contradiction))
	contradiction["matches_current"] = false
	persisted.Data, err = json.Marshal(contradiction)
	require.NoError(t, err)
	require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return observation.Handle(ctx, tx, persisted) }))
	require.JSONEq(t, first, call("GET", path, "manager", "").Body.String())

	var rejectedEvent events.Event
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var raw []byte
		if err := tx.Raw(`SELECT payload FROM outbox_events WHERE subject='helivanta.in.organization.department_link_observed.v1' AND payload->'data'->>'request_id'=? ORDER BY created_at LIMIT 1`, strings.TrimPrefix(stale, masterPath+"/department-links/")).Row().Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &rejectedEvent)
	}))
	var rejectedFields map[string]any
	require.NoError(t, json.Unmarshal(rejectedEvent.Data, &rejectedFields))
	require.Equal(t, false, rejectedFields["matches_current"])
	delete(rejectedFields, "matches_current")
	rejectedEvent.Data, err = json.Marshal(rejectedFields)
	require.NoError(t, err)
	require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return observation.Handle(ctx, tx, rejectedEvent) }), "omitted false is not a retained negative observation")
	outage.Store(true)
	require.Equal(t, 503, call("GET", path, "manager", "").Code)
	require.Equal(t, 503, call("PUT", path, "manager", body).Code)
	outage.Store(false)
	require.JSONEq(t, first, call("GET", path, "manager", "").Body.String())
	require.NoError(t, fga.RevokeRole(ctx, testutil.TenantA, "user-manager", authz.RoleTenantAdmin))
	require.Equal(t, 403, call("PUT", path, "manager", body).Code)
	require.Equal(t, 403, call("GET", path, "manager", "").Code)
}
