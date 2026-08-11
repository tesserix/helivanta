package testutil

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	TenantA = "11111111-1111-1111-1111-111111111111"
	TenantB = "22222222-2222-2222-2222-222222222222"
)

// StaticVerifier maps bearer tokens to tenant ids for tests.
type StaticVerifier map[string]string

func (s StaticVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if tenant, ok := s[raw]; ok {
		return authn.Principal{Subject: "user-" + raw, TenantID: tenant}, nil
	}
	return authn.Principal{}, context.DeadlineExceeded
}

// harnessResolver resolves from a static token→permissions map, so module
// tests need no OpenFGA container. Tests that must exercise real tuple
// resolution use the matrix suite instead.
type harnessResolver struct {
	tokens map[string]string
	perms  map[string][]authz.Permission
}

func (h harnessResolver) Resolve(_ context.Context, subject, _ string) (authz.PermissionSet, error) {
	return authz.NewPermissionSet(h.perms[strings.TrimPrefix(subject, "user-")]...), nil
}

// noopWriter discards tuple writes for modules that don't mutate
// authorization state, so ModuleHarness callers need no TupleWriter.
type noopWriter struct{}

func (noopWriter) GrantRole(context.Context, string, string, authz.Role) error  { return nil }
func (noopWriter) RevokeRole(context.Context, string, string, authz.Role) error { return nil }
func (noopWriter) GrantPermission(context.Context, string, authz.Permission, authz.Role) error {
	return nil
}

// noopRoleLister returns no bindings for modules that don't need to
// resolve cross-tenant membership, so ModuleHarness and
// ModuleHarnessWithAuthz callers need no RoleLister.
type noopRoleLister struct{}

func (noopRoleLister) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return nil, nil
}

// stubMinter mints a predictable, obviously-fake custom token so
// harnesses that don't care about token minting still exercise the
// switch route's success path. Tests that assert on minting (was it
// called, with what, and only after the gate) pass their own recorder
// via ModuleHarnessWithMinter.
type stubMinter struct{}

func (stubMinter) CustomTokenWithClaims(_ context.Context, uid string, claims map[string]interface{}) (string, error) {
	return fmt.Sprintf("stub-custom-token:%s:%v", uid, claims["tenant_id"]), nil
}

// ModuleHarness boots the full module stack (Postgres, NATS, routes,
// consumers, dispatcher) for the given modules. One call replaces the
// setup() previously copy-pasted per module test package. perms maps
// token → the permissions that token's caller holds.
func ModuleHarness(t *testing.T, tokens map[string]string, perms map[string][]authz.Permission, mods ...platform.Module) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	return moduleHarness(t, tokens, perms, noopWriter{}, noopRoleLister{}, stubMinter{}, mods...)
}

// ModuleHarnessWithAuthz is ModuleHarness plus a TupleWriter, for modules
// that mutate authorization state.
func ModuleHarnessWithAuthz(
	t *testing.T,
	tokens map[string]string,
	perms map[string][]authz.Permission,
	writer platform.TupleWriter,
	mods ...platform.Module,
) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	return moduleHarness(t, tokens, perms, writer, noopRoleLister{}, stubMinter{}, mods...)
}

// ModuleHarnessWithRoles is ModuleHarness plus a TupleWriter and a
// RoleLister, for modules (like iam) that resolve cross-tenant
// membership from OpenFGA rather than a single-tenant Postgres query.
func ModuleHarnessWithRoles(
	t *testing.T,
	tokens map[string]string,
	perms map[string][]authz.Permission,
	writer platform.TupleWriter,
	roles platform.RoleLister,
	mods ...platform.Module,
) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	return moduleHarness(t, tokens, perms, writer, roles, stubMinter{}, mods...)
}

// ModuleHarnessWithMinter is ModuleHarnessWithRoles plus a TokenMinter,
// for tests that assert on how the tenant-switch route mints — that it
// mints only after the membership gate passes, and what claims it puts
// in the token. Pass nil to simulate an unwired minter.
func ModuleHarnessWithMinter(
	t *testing.T,
	tokens map[string]string,
	perms map[string][]authz.Permission,
	writer platform.TupleWriter,
	roles platform.RoleLister,
	minter authn.TokenMinter,
	mods ...platform.Module,
) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	return moduleHarness(t, tokens, perms, writer, roles, minter, mods...)
}

func moduleHarness(
	t *testing.T,
	tokens map[string]string,
	perms map[string][]authz.Permission,
	writer platform.TupleWriter,
	roles platform.RoleLister,
	minter authn.TokenMinter,
	mods ...platform.Module,
) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := events.Migrations()
	for _, m := range mods {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "tenant tables must carry forced RLS")

	// Every test shares one NATS server, so each takes its own subject
	// namespace — otherwise one test's consumers would receive another's
	// events. t.Name() is unique per test by construction.
	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), t.Name())
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus, Authz: writer, Roles: roles, Tokens: minter}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	resolver := harnessResolver{tokens: tokens, perms: perms}
	api := platform.NewRouter(r.Group("/v1",
		authn.Middleware(StaticVerifier(tokens)),
		authz.Middleware(resolver)))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
	go bus.RunDispatcher(ctx, db)
	return r, db, bus, ctx
}

// Do issues an authenticated JSON request against the harness router.
func Do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
