package iam_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestCurrentAuthorizationDeniesRemovedRoleDespiteStaleGraph(t *testing.T) {
	_, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, tenant))
		for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
			require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-current-admin',?)`, tenant, role).Error
			}))
			require.NoError(t, graph.GrantTenantRole(ctx, tenant, role))
			require.NoError(t, graph.GrantRole(ctx, tenant, "synthetic-current-admin", role))
		}
	}
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	signer, verifier := testutil.NewSessionSignerForTest(t)
	r := gin.New()
	api := platform.NewRouter(r.Group("/v1", authn.Middleware(authn.NewSessionVerifier(verifier), iam.NewRevocationChecker(db)), authz.Middleware(authority)), authority)
	iam.New(nil).Routes(api, platform.Deps{DB: db, Bus: bus})
	token, err := signer.Mint("synthetic-current-admin", testutil.TenantA, time.Now(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	read := func() int { return testutil.Do(r, http.MethodGet, "/v1/iam/members", token, "").Code }
	require.Equal(t, http.StatusOK, read())

	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject='synthetic-current-admin' AND role_key='tenant_admin'`, testutil.TenantA).Error
	}))
	stale, err := graph.Resolve(ctx, "synthetic-current-admin", testutil.TenantA)
	require.NoError(t, err)
	require.True(t, stale.Has(iam.PermMemberManage), "the fixture deliberately retains stale administrative graph authority")
	require.Equal(t, http.StatusForbidden, read(), "an old signed session and stale graph must not preserve a removed role")
	member, err := authority.IsMember(ctx, "synthetic-current-admin", testutil.TenantA)
	require.NoError(t, err)
	require.True(t, member, "removing one role must preserve the remaining staff membership")
	otherTenant, err := authority.Resolve(ctx, "synthetic-current-admin", testutil.TenantB)
	require.NoError(t, err)
	require.True(t, otherTenant.Has(iam.PermMemberManage), "the other tenant's administrative role remains usable")
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject='synthetic-current-admin'`, testutil.TenantA).Error
	}))
	member, err = authority.IsMember(ctx, "synthetic-current-admin", testutil.TenantA)
	require.NoError(t, err)
	require.False(t, member, "stale graph membership cannot retain final tenant membership")
	require.Equal(t, http.StatusForbidden, read())
	self := testutil.Do(r, http.MethodGet, "/v1/iam/me/permissions", token, "")
	require.Equal(t, http.StatusOK, self.Code, "an empty role set must preserve authenticated self-service recovery")
	require.NotContains(t, self.Body.String(), string(iam.PermMemberManage))
	require.NoError(t, graph.RevokeRole(ctx, testutil.TenantA, "synthetic-current-admin", authz.RoleTenantAdmin))
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "synthetic-current-admin", authz.RoleTenantAdmin))
	require.Equal(t, http.StatusForbidden, read(), "a late grant replay cannot restore removed IAM membership")
}

func TestCredentialRevocationScopeWithSignedSessionsAndRealGraph(t *testing.T) {
	_, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, tenant))
		require.NoError(t, graph.GrantTenantRole(ctx, tenant, authz.RoleStaffMember))
		require.NoError(t, graph.GrantRole(ctx, tenant, "synthetic-global-target", authz.RoleStaffMember))
	}
	require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "synthetic-current-admin", authz.RoleTenantAdmin))
	for _, member := range []struct {
		tenant, subject string
		role            authz.Role
	}{
		{testutil.TenantA, "synthetic-current-admin", authz.RoleTenantAdmin},
		{testutil.TenantB, "synthetic-global-target", authz.RoleStaffMember},
	} {
		require.NoError(t, db.WithTenant(ctx, member.tenant, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, member.tenant, member.subject, member.role).Error
		}))
	}
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	checker := iam.NewRevocationChecker(db)
	signer, verifier := testutil.NewSessionSignerForTest(t)
	r := gin.New()
	api := platform.NewRouter(r.Group("/v1", authn.Middleware(authn.NewSessionVerifier(verifier), checker), authz.Middleware(authority)), authority)
	iam.New(checker).Routes(api, platform.Deps{DB: db, Bus: bus, Roles: graph})
	actor, err := signer.Mint("synthetic-current-admin", testutil.TenantA, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	require.NoError(t, err)
	target, err := signer.Mint("synthetic-global-target", testutil.TenantB, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, testutil.Do(r, http.MethodGet, "/v1/iam/me/permissions", target, "").Code)
	denied := testutil.Do(r, http.MethodPost, "/v1/iam/subjects/synthetic-global-target/revoke", actor, "")
	require.Equal(t, http.StatusNotFound, denied.Code, denied.Body.String())
	require.Equal(t, http.StatusOK, testutil.Do(r, http.MethodGet, "/v1/iam/me/permissions", target, "").Code, "a stale graph target binding must not end the other tenant's signed session")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-global-target','staff_member')`, testutil.TenantA).Error
	}))
	accepted := testutil.Do(r, http.MethodPost, "/v1/iam/subjects/synthetic-global-target/revoke", actor, "")
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	require.Equal(t, http.StatusUnauthorized, testutil.Do(r, http.MethodGet, "/v1/iam/me/permissions", target, "").Code, "a current authorized explicit security action must retain its global effect")
}

func TestCurrentAuthorizationRequiresBothSourcesAndKnownRoles(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
	require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	for _, tc := range []struct {
		name      string
		source    authz.Role
		projected bool
		member    bool
		wantError bool
	}{
		{name: "graph_only", projected: true},
		{name: "source_only", source: authz.RoleTenantAdmin},
		{name: "current_staff_with_stale_admin_graph", source: authz.RoleStaffMember, projected: true, member: true},
		{name: "unknown_source_role", source: "unknown_role", projected: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.source != "" {
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, testutil.TenantA, tc.name, tc.source).Error
				}))
			}
			if tc.projected {
				require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, tc.name, authz.RoleTenantAdmin))
			}
			permissions, resolveErr := authority.Resolve(ctx, tc.name, testutil.TenantA)
			member, memberErr := authority.IsMember(ctx, tc.name, testutil.TenantA)
			if tc.wantError {
				require.Error(t, resolveErr)
				require.Error(t, memberErr)
			} else {
				require.NoError(t, resolveErr)
				require.NoError(t, memberErr)
			}
			require.Empty(t, permissions, "neither source alone nor an unknown role may authorize administrative access")
			require.Equal(t, tc.member, member)
		})
	}
}

type unavailableCurrentGraph struct{}

func (unavailableCurrentGraph) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return authz.NewPermissionSet(iam.PermMemberManage), errors.New("synthetic graph outage")
}

func (unavailableCurrentGraph) IsMember(context.Context, string, string) (bool, error) {
	return true, errors.New("synthetic graph outage")
}

func TestCurrentAuthorizationFailsClosedOnMissingDependenciesAndOutage(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	_, err := iam.NewCurrentAuthorization(nil, registry, unavailableCurrentGraph{})
	require.Error(t, err)
	_, err = iam.NewCurrentAuthorization(db, nil, unavailableCurrentGraph{})
	require.Error(t, err)
	_, err = iam.NewCurrentAuthorization(db, registry, nil)
	require.Error(t, err)
	authority, err := iam.NewCurrentAuthorization(db, registry, unavailableCurrentGraph{})
	require.NoError(t, err)
	permissions, err := authority.Resolve(ctx, "synthetic-admin", testutil.TenantA)
	require.Error(t, err)
	require.Empty(t, permissions)
	member, err := authority.IsMember(ctx, "synthetic-admin", testutil.TenantA)
	require.Error(t, err)
	require.False(t, member)
	r := gin.New()
	r.Use(authn.Middleware(testutil.StaticVerifier{"admin": testutil.TenantA}, iam.NewRevocationChecker(db)), authz.Middleware(authority))
	r.GET("/protected", authz.Require(iam.PermMemberManage), func(c *gin.Context) { c.Status(http.StatusOK) })
	response := testutil.Do(r, http.MethodGet, "/protected", "admin", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), `"error":"authz_unavailable"`)
}

func TestCurrentAuthorizationRechecksIAMAfterDelayedGraphResponse(t *testing.T) {
	for _, operation := range []string{"permissions", "membership"} {
		t.Run(operation, func(t *testing.T) {
			_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
			upstream, err := url.Parse(testinfra.StartOpenFGA(t))
			require.NoError(t, err)
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var intercept atomic.Bool
			suffix := "/list-objects"
			if operation == "membership" {
				suffix = "/check"
			}
			proxy.ModifyResponse = func(response *http.Response) error {
				if strings.HasSuffix(response.Request.URL.Path, suffix) && intercept.CompareAndSwap(true, false) {
					close(entered)
					select {
					case <-release:
					case <-response.Request.Context().Done():
						return response.Request.Context().Err()
					}
				}
				return nil
			}
			server := httptest.NewServer(proxy)
			t.Cleanup(func() { unblock(); server.Close() })
			graph, err := authz.NewClient(ctx, server.URL, testinfra.IsolationKey(t))
			require.NoError(t, err)
			registry := platform.NewRegistry()
			require.NoError(t, registry.Register(iam.New(nil)))
			require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
			require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
			require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "synthetic-delayed-admin", authz.RoleTenantAdmin))
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-delayed-admin','tenant_admin')`, testutil.TenantA).Error
			}))
			authority, err := iam.NewCurrentAuthorization(db, registry, graph)
			require.NoError(t, err)
			type result struct {
				permissions authz.PermissionSet
				member      bool
				err         error
			}
			finished := make(chan result, 1)
			intercept.Store(true)
			go func() {
				var answer result
				if operation == "permissions" {
					answer.permissions, answer.err = authority.Resolve(ctx, "synthetic-delayed-admin", testutil.TenantA)
				} else {
					answer.member, answer.err = authority.IsMember(ctx, "synthetic-delayed-admin", testutil.TenantA)
				}
				finished <- answer
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("graph response was not intercepted")
			}
			require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-owner"), testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject='synthetic-delayed-admin'`, testutil.TenantA).Error
			}))
			unblock()
			select {
			case answer := <-finished:
				require.NoError(t, answer.err)
				require.Empty(t, answer.permissions)
				require.False(t, answer.member, "a delayed graph allow must not bypass current membership removal")
			case <-ctx.Done():
				t.Fatal("current authorization did not finish")
			}
		})
	}
}

func TestCurrentAuthorizationDeniesWhenIAMDatabaseIsUnavailable(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
	require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "synthetic-source-outage", authz.RoleTenantAdmin))
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	permissions, err := authority.Resolve(ctx, "synthetic-source-outage", testutil.TenantA)
	require.Error(t, err)
	require.Empty(t, permissions, "a graph allow must not survive an unavailable authoritative source")
	member, err := authority.IsMember(ctx, "synthetic-source-outage", testutil.TenantA)
	require.Error(t, err)
	require.False(t, member)
}

func TestCurrentAuthorizationBoundsLookupInLargeTenant(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(iam.New(nil)))
	require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "synthetic-scale-admin", authz.RoleTenantAdmin))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) SELECT ?::uuid,'synthetic-scale-'||n,'staff_member' FROM generate_series(1,100000) AS n`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-scale-admin','tenant_admin')`, testutil.TenantA).Error
	}))
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	permissions, err := authority.Resolve(ctx, "synthetic-scale-admin", testutil.TenantA)
	require.NoError(t, err)
	require.True(t, permissions.Has(iam.PermMemberManage), "unrelated staff rows must not overflow or change this subject's bounded authority")
	var plan string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT role_key FROM iam_members WHERE tenant_id=? AND subject=? ORDER BY role_key LIMIT ?`, testutil.TenantA, "synthetic-scale-admin", len(authz.SystemRoles())+1).Scan(&plan).Error
	}))
	require.True(t, strings.Contains(plan, `"Node Type": "Index Only Scan"`) || strings.Contains(plan, `"Node Type": "Index Scan"`) || strings.Contains(plan, `"Node Type": "Bitmap Index Scan"`), plan)
	t.Logf("100,001-row current membership lookup: %s", plan)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT subject,role_key FROM iam_members
			WHERE tenant_id=? AND ((subject=? AND role_key=?) OR (subject=? AND role_key IN ?))
			ORDER BY subject,role_key LIMIT ? FOR SHARE`, testutil.TenantA, "synthetic-scale-admin", authz.RoleTenantAdmin,
			"synthetic-scale-99999", authz.SystemRoles(), len(authz.SystemRoles())+2).Scan(&plan).Error
	}))
	require.True(t, strings.Contains(plan, `"Node Type": "Index Scan"`) || strings.Contains(plan, `"Node Type": "Bitmap Index Scan"`), plan)
	t.Logf("100,001-row locked revocation membership lookup: %s", plan)
}
