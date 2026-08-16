package testutil

import (
	"context"
	"crypto/ed25519"
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
	"github.com/tesserix/hms/pkg/session"
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
		// AuthTime is "now", as a real, freshly-issued token's would be.
		// Zero would silently compare as before any non-zero watermark a
		// test configures via HarnessOptions.Revocation, which would make
		// every such caller look already-revoked regardless of intent.
		//
		// IdleDeadline is an hour out for the same shape of reason
		// (#848): authn.Middleware refuses a zero deadline exactly like
		// an already-past one, so leaving it unset would 401 every
		// harness request regardless of what the test is about. An hour
		// is comfortably beyond any harness test's own runtime, so the
		// idle gate never fires incidentally; a test that actually cares
		// about the idle deadline supplies its own Verifier (see
		// HarnessOptions.Verifier and me_test.go's idleDeadlineVerifier).
		return authn.Principal{
			Subject:      "user-" + raw,
			TenantID:     tenant,
			AuthTime:     time.Now(),
			IdleDeadline: time.Now().Add(time.Hour),
		}, nil
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

// TestSessionKID/TestSessionIssuer/TestSessionTTL are fixed, obviously-fake
// values for NewSessionSignerForTest, mirroring login_test.go's
// loginTestKID/loginTestIssuer/loginTestTTL constants — kept here too so a
// module test that needs a working re-mint (the tenant-switch route) does
// not have to duplicate them.
const (
	TestSessionKID    = "hms-session-test"
	TestSessionIssuer = "https://hms.test"
	TestSessionTTL    = 15 * time.Minute
)

// NewSessionSignerForTest builds a real Ed25519 Signer/Verifier pair for
// tests that need a WORKING HMS session re-mint — the tenant-switch route
// (internal/modules/iam/me.go), specifically. It is deliberately not
// wired into NewHarness's defaults the way Writer/Roles/Membership are:
// HarnessOptions.SessionSigner left nil is exactly the deployment mistake
// (a half-wired entrypoint, or a test that never opted in) the switch
// route must refuse rather than silently issue nothing and claim success
// — see TestSwitchTenantFailsClosedWithoutASigner in
// internal/modules/iam/me_test.go. A caller that wants a working re-mint
// calls this explicitly and sets HarnessOptions.SessionSigner /
// SessionTTL from the result.
func NewSessionSignerForTest(t *testing.T) (*session.Signer, *session.Verifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, TestSessionKID, TestSessionIssuer, TestSessionTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, TestSessionKID, TestSessionIssuer)
	require.NoError(t, err)
	return signer, verifier
}

// alwaysMember is the harness's default MembershipChecker: every subject
// is a member of every tenant, in every tenant, unconditionally. This is
// what keeps HarnessOptions{}'s zero value usable — a test that has not
// opted into membership semantics (the overwhelming majority of module
// tests) should keep exercising the route it is actually about, not trip
// over an incidental 403 from a gate unrelated to what it tests.
type alwaysMember struct{}

func (alwaysMember) IsMember(context.Context, string, string) (bool, error) { return true, nil }

// neverRevoked is the harness's default RevocationChecker: every subject
// answers the zero watermark, unconditionally. This keeps
// HarnessOptions{}'s zero value usable — a test that has not opted into
// revocation semantics (the overwhelming majority of module tests)
// should keep exercising the route it is actually about, not trip over
// an incidental 401 from a gate unrelated to what it tests.
type neverRevoked struct{}

func (neverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

// HarnessOptions are the substitutable dependencies of a module harness.
//
// The zero value is valid: every dependency field falls back to a stub
// -- Writer to noopWriter, Roles to noopRoleLister, Membership to
// alwaysMember -- so a caller that only cares about, say, tenant
// isolation on a CRUD route can build a harness with just Tokens and
// Perms set and get sensible, permissive defaults everywhere else.
//
// SessionSigner is the one deliberate exception: it is NOT defaulted to
// a working signer. Left nil, deps.SessionSigner reaches the switch
// route nil on purpose, proving the "no session signer configured"
// failure path (see TestSwitchTenantFailsClosedWithoutASigner in
// internal/modules/iam). Auto-defaulting it here would make that case
// inexpressible again — a caller that wants a working re-mint sets
// SessionSigner/SessionTTL explicitly, typically from
// NewSessionSignerForTest.
type HarnessOptions struct {
	Tokens     map[string]string
	Perms      map[string][]authz.Permission
	Writer     platform.TupleWriter
	Roles      platform.RoleLister
	Membership authz.MembershipChecker
	// Revocation defaults to a checker that answers "never revoked" for
	// every subject — see neverRevoked's doc comment. Set it explicitly
	// to exercise the credential-revocation gate in authn.Middleware
	// (#781).
	Revocation authn.RevocationChecker
	// Verifier defaults to StaticVerifier(opts.Tokens), which always
	// stamps AuthTime as time.Now() at Verify time. Set it explicitly
	// when a test needs to control AuthTime itself — e.g. proving a
	// tenant-switch re-mint carries the ORIGINAL auth_time through
	// rather than resetting it: with the default verifier, "carried
	// through" and "reset to the mint time" are indistinguishable,
	// because the original auth_time is ALSO stamped moments before the
	// switch request and so is ALSO close to "now" by the time an
	// assertion runs.
	Verifier      authn.TokenVerifier
	SessionSigner *session.Signer
	// SessionTTL must be set alongside SessionSigner — it is the cookie
	// lifetime deps.SessionTTL carries into the switch route, exactly
	// like cfg.SessionTTL in cmd/api. Left at zero when SessionSigner is
	// nil, which is fine: nothing reads it before the signer-nil check.
	SessionTTL          time.Duration
	SessionSecureCookie bool
	Modules             []platform.Module
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
	revocation := opts.Revocation
	if revocation == nil {
		revocation = neverRevoked{}
	}
	verifier := opts.Verifier
	if verifier == nil {
		verifier = StaticVerifier(opts.Tokens)
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

	deps := platform.Deps{
		DB: db, Bus: bus, Authz: writer, Roles: roles,
		SessionSigner:       opts.SessionSigner,
		SessionTTL:          opts.SessionTTL,
		SessionSecureCookie: opts.SessionSecureCookie,
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Same middleware chain, same order, as cmd/api/main.go: requestid.Middleware()
	// at the engine level, then authn (which now also checks the credential
	// revocation watermark against Principal.AuthTime — Task 4, #781), then
	// requestid.PrincipalMiddleware() (which needs authn's principal already
	// set), then authz. Then, inside platform.Router itself
	// (internal/platform/router.go), authz.RequireMembership runs before
	// authz.Require on every route not marked authz.NoTenantMembership — the
	// membership gate that closes #781. Without this middleware order, module
	// integration tests exercise requestid.Logger(c) falling back to a bare
	// slog.Default() instead of the request-scoped logger every real request gets,
	// and nothing in the suite catches PrincipalMiddleware being dropped from
	// cmd/api, the membership gate being dropped from platform.Router, or the
	// revocation gate being dropped from authn.Middleware. See
	// TestRequestScopedLoggerCarriesCorrelationFields and
	// internal/modules/reference's membership tests below.
	r.Use(requestid.Middleware())
	resolver := harnessResolver{tokens: opts.Tokens, perms: opts.Perms}
	api := platform.NewRouter(r.Group("/v1",
		authn.Middleware(verifier, revocation),
		requestid.PrincipalMiddleware(),
		authz.Middleware(resolver)),
		membership)
	for _, m := range opts.Modules {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
		require.NoError(t, bus.StartBroadcasts(ctx, m.Broadcasts(deps)))
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
