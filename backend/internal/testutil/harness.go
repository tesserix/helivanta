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

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
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
// authorization state, so a harness caller that doesn't set Writer needs
// no TupleWriter of its own.
type noopWriter struct{}

func (noopWriter) GrantRole(context.Context, string, string, authz.Role) error  { return nil }
func (noopWriter) RevokeRole(context.Context, string, string, authz.Role) error { return nil }
func (noopWriter) GrantPermission(context.Context, string, authz.Permission, authz.Role) error {
	return nil
}
func (noopWriter) GrantTenantRole(context.Context, string, authz.Role) error { return nil }

// noopRoleLister returns no bindings for modules that don't need to
// resolve cross-tenant membership, so a harness caller that doesn't set
// Roles needs no RoleLister of its own.
type noopRoleLister struct{}

func (noopRoleLister) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return nil, nil
}

// StubMinter mints a predictable, obviously-fake custom token. It is NOT
// the default for HarnessOptions.Minter — see that field's doc comment
// — but is exported for a test that wants a working mint without
// writing its own recorder.
type StubMinter struct{}

func (StubMinter) CustomTokenWithClaims(_ context.Context, uid string, claims map[string]interface{}) (string, error) {
	return fmt.Sprintf("stub-custom-token:%s:%v", uid, claims["tenant_id"]), nil
}

// alwaysMember is the harness's default MembershipChecker: every subject
// is a member of every tenant, in every tenant, unconditionally. This is
// what keeps HarnessOptions{}'s zero value usable — a test that has not
// opted into membership semantics (the overwhelming majority of module
// tests) should keep exercising the route it is actually about, not trip
// over an incidental 403 from a gate unrelated to what it tests.
type alwaysMember struct{}

func (alwaysMember) IsMember(context.Context, string, string) (bool, error) { return true, nil }

// HarnessOptions are the substitutable dependencies of a module harness.
//
// The zero value is valid: every dependency field falls back to a stub
// -- Writer to noopWriter, Roles to noopRoleLister, Membership to
// alwaysMember -- so a caller that only cares about, say, tenant
// isolation on a CRUD route can build a harness with just Tokens and
// Perms set and get sensible, permissive defaults everywhere else.
//
// Minter is the one deliberate exception: it is NOT defaulted to a
// working stub. ModuleHarnessWithMinter used to exist specifically so a
// caller could pass nil on purpose and prove the tenant-switch route's
// "no minter configured" failure path (see
// TestSwitchTenantFailsClosedWithoutAMinter in internal/modules/iam).
// Auto-defaulting Minter here would make that case inexpressible again
// — a caller that wants a working mint sets Minter explicitly (a
// *recordingMinter test double, or the exported StubMinter above).
type HarnessOptions struct {
	Tokens     map[string]string
	Perms      map[string][]authz.Permission
	Writer     platform.TupleWriter
	Roles      platform.RoleLister
	Minter     authn.TokenMinter
	Membership authz.MembershipChecker
	Modules    []platform.Module
}

// NewHarness boots the full module stack (Postgres, NATS, routes,
// consumers, dispatcher) for opts.Modules. One call replaces the four
// near-identical ModuleHarness* constructors this harness used to
// export, collapsed here because Task 3 (#781) added Membership as a
// fifth substitutable dependency and a sixth is coming — four
// near-identical variants was already one too many, and a fifth or
// sixth would only make that worse.
func NewHarness(t *testing.T, opts HarnessOptions) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()

	writer := opts.Writer
	if writer == nil {
		writer = noopWriter{}
	}
	roles := opts.Roles
	if roles == nil {
		roles = noopRoleLister{}
	}
	membership := opts.Membership
	if membership == nil {
		membership = alwaysMember{}
	}

	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := bootstrap.PlatformMigrations()
	for _, m := range opts.Modules {
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

	deps := platform.Deps{DB: db, Bus: bus, Authz: writer, Roles: roles, Tokens: opts.Minter}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Same middleware chain, same order, as cmd/api/main.go: requestid.Middleware()
	// at the engine level, then authn, then requestid.PrincipalMiddleware() (which
	// needs authn's principal already set), then authz. Then, inside platform.Router
	// itself (internal/platform/router.go), authz.RequireMembership runs before
	// authz.Require on every route not marked authz.NoTenantMembership — the
	// membership gate that closes #781. Without this middleware order, module
	// integration tests exercise requestid.Logger(c) falling back to a bare
	// slog.Default() instead of the request-scoped logger every real request gets,
	// and nothing in the suite catches PrincipalMiddleware being dropped from
	// cmd/api, or the membership gate being dropped from platform.Router. See
	// TestRequestScopedLoggerCarriesCorrelationFields and
	// internal/modules/reference's membership tests below.
	r.Use(requestid.Middleware())
	resolver := harnessResolver{tokens: opts.Tokens, perms: opts.Perms}
	api := platform.NewRouter(r.Group("/v1",
		authn.Middleware(StaticVerifier(opts.Tokens)),
		requestid.PrincipalMiddleware(),
		authz.Middleware(resolver)),
		membership)
	for _, m := range opts.Modules {
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
