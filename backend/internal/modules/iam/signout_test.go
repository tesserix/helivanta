package iam

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// signoutTestTenantA/B mirror testutil.TenantA/TenantB's values. This
// file deliberately does not import internal/testutil: testutil imports
// internal/bootstrap, which imports this very package (internal/modules/iam)
// in production code, so a signout_test.go in package iam (not iam_test)
// importing testutil would be an import cycle in the test binary.
const (
	signoutTestTenantA = "11111111-1111-1111-1111-111111111111"
	signoutTestTenantB = "22222222-2222-2222-2222-222222222222"
)

// doRequest issues an authenticated JSON request against the harness
// router. A local copy of testutil.Do for the same import-cycle reason as
// the tenant constants above.
func doRequest(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// fixedVerifier maps bearer tokens to principals with an operator-chosen
// AuthTime, unlike testutil.StaticVerifier, which always stamps "now" at
// Verify time. The shared-checker proof (below) needs a token that keeps
// presenting the SAME auth_time on every use — the shape of a client
// replaying one still-open credential, not signing in again — and a
// "now" stamp on every call could never produce that.
type fixedVerifier map[string]authn.Principal

func (f fixedVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	if p, ok := f[raw]; ok {
		// #848: authn.Middleware refuses an unset IdleDeadline exactly
		// like an already-past one, so a principal declared without one
		// would 401 before reaching the sign-out route these tests are
		// about. Filled in here, on a copy, rather than at every literal
		// below, so this file stays about revocation — and only when the
		// caller left it unset, so a test that DOES want to exercise the
		// idle gate can still say so.
		if p.IdleDeadline.IsZero() {
			p.IdleDeadline = time.Now().Add(time.Hour)
		}
		return p, nil
	}
	return authn.Principal{}, errors.New("unknown token")
}

// stubResolver grants a fixed permission list per subject, mirroring
// testutil's unexported harnessResolver but usable from this internal
// test package.
type stubResolver map[string][]authz.Permission

func (s stubResolver) Resolve(_ context.Context, subject, _ string) (authz.PermissionSet, error) {
	return authz.NewPermissionSet(s[subject]...), nil
}

// fakeMembership answers per-subject membership from a fixed map,
// defaulting an unlisted subject to false (never a permissive default —
// tests that care about membership set every subject they exercise).
type fakeMembership map[string]bool

func (f fakeMembership) IsMember(_ context.Context, subject, _ string) (bool, error) {
	return f[subject], nil
}

// stubRoleLister answers ListRoles from a fixed subject->bindings map, so
// adminRevoke's cross-tenant gate can be tested without an OpenFGA
// container.
type stubRoleLister map[string][]authz.RoleBinding

func (s stubRoleLister) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	return s[subject], nil
}

// failingRoleLister always errors, for adminRevoke's fail-closed path:
// an authorization-infrastructure failure must answer 503, never fall
// through to treating "could not ask" as "not a member" (404).
type failingRoleLister struct{ err error }

func (f failingRoleLister) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return nil, f.err
}

type signoutHarnessConfig struct {
	verifier   fixedVerifier
	membership fakeMembership
	perms      stubResolver
	roles      platform.RoleLister
}

// newSignoutHarness builds the sign-out/admin-revoke routes wired exactly
// as cmd/api/main.go wires them: ONE *tenantdb.DB and ONE *RevocationChecker
// shared between authn.Middleware and the iam module's handlers. Building
// this by hand, rather than through testutil.NewHarness, is deliberate —
// NewHarness constructs opts.Modules before its own Postgres exists, so a
// module that needs the harness's own *tenantdb.DB at construction time
// (iam.New(checker) does, now) cannot be built that way. Replicating
// main.go's construction order here is also what makes this harness the
// right place to prove the module and the middleware share one checker
// instance (see TestModuleAndMiddlewareShareOneRevocationCheckerInstance).
func newSignoutHarness(t *testing.T, cfg signoutHarnessConfig) (*gin.Engine, *tenantdb.DB, *RevocationChecker, *events.Bus) {
	t.Helper()

	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, New(nil).Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "tenant tables must carry forced RLS")

	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	checker := NewRevocationChecker(db)

	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(requestid.Middleware())
	api := platform.NewRouter(e.Group("/v1",
		authn.Middleware(cfg.verifier, checker),
		requestid.PrincipalMiddleware(),
		authz.Middleware(cfg.perms),
	), cfg.membership)

	deps := platform.Deps{DB: db, Bus: bus, Roles: cfg.roles}
	m := New(checker)
	m.Routes(api, deps)
	require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	require.NoError(t, bus.StartBroadcasts(ctx, m.Broadcasts(deps)))
	go bus.RunDispatcher(ctx, db)

	return e, db, checker, bus
}

func watermarkFor(t *testing.T, db *tenantdb.DB, subject string) time.Time {
	t.Helper()
	var row revocationRow
	err := db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Where("subject = ?", subject).First(&row).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}
	}
	require.NoError(t, err)
	return row.RevokedAt
}

// revocationWatcher observes CredentialRevoked broadcasts as they arrive
// over NATS, standing in for a direct outbox_events read.
//
// Before #835 Task 1, this test read outbox_events directly under
// WithSystem to observe the write made inside the SAME transaction as the
// watermark. Since Task 1, that no longer works: SubjectCredentialRevoked
// publishes with no TenantID (revocation is subject-scoped, not
// tenant-scoped — see revoke()'s comment in signout.go), so its outbox
// row's tenant_id is NULL, and 0002_events_outbox_tenant's policy makes a
// NULL-tenant row readable by nobody under RLS — not even WithSystem —
// exactly as designed: only the dispatcher's WithAdmin read sees it.
// Watching the stream a message arrives on is the closest observation
// left to a test, without reaching for the same admin-pool bypass the
// dispatcher alone is allowed (withAdminAllowlist, internal/archtest/arch_test.go).
// A message reaching here still proves the write committed — it can only
// have been drained from a row the sign-out transaction actually inserted.
type revocationWatcher struct {
	mu      sync.Mutex
	targets []string
}

// startRevocationWatcher subscribes before the caller triggers a
// revocation, so no message can be published before the subscription is
// live and missed — the same reason dlq_test.go/panic_test.go subscribe
// to the DLQ subject before publishing.
func startRevocationWatcher(t *testing.T, ctx context.Context, bus *events.Bus) *revocationWatcher {
	t.Helper()
	w := &revocationWatcher{}
	require.NoError(t, bus.StartBroadcasts(ctx, []events.Broadcast{{
		Subject: iamcontract.SubjectCredentialRevoked,
		Handle: func(_ context.Context, evt events.Event) {
			var data iamcontract.CredentialRevokedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				return
			}
			w.mu.Lock()
			w.targets = append(w.targets, data.Subject)
			w.mu.Unlock()
		},
	}}))
	return w
}

func (w *revocationWatcher) saw(subject string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.targets {
		if s == subject {
			return true
		}
	}
	return false
}

// TestSignOutWritesTheWatermark used to also assert GIP was told
// (RevokeRefreshTokens) — #838 removed that call entirely: Helivanta stores no
// IdP credential to revoke (spec D4a), so the Helivanta watermark alone is now
// the whole of revocation. See TestModuleAndMiddlewareShareOneRevocationCheckerInstance
// below for the proof that the watermark alone is sufficient to refuse a
// live credential immediately, with no wait for GIP or any TTL.
func TestSignOutWritesTheWatermark(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	w := doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")
	require.Equal(t, http.StatusOK, w.Code)

	require.False(t, watermarkFor(t, db, "uid-nurse").IsZero(),
		"sign-out must write the watermark, not merely clear a cookie")
}

// TestSignOutWorksForARevokedMember is spec T4's sign-out counterpart:
// someone whose tenant membership was just revoked must still be able to
// end their own session. membership is false for every subject in this
// harness, so a 200 here is only possible because /me/sign-out is
// authz.NoTenantMembership and RequireMembership never runs for it.
func TestSignOutWorksForARevokedMember(t *testing.T) {
	r, _, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	require.Equal(t, http.StatusOK,
		doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "").Code)
}

func TestAdminRevokeRefusesASubjectOutsideTheActingTenant(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-admin": {Subject: "uid-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-admin": true},
		perms:      stubResolver{"uid-admin": {PermCredentialRevoke}},
		// uid-stranger is a member of tenant B only.
		roles: stubRoleLister{"uid-stranger": {{TenantID: signoutTestTenantB, Role: authz.RoleNurse}}},
	})

	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/uid-stranger/revoke", "tok-admin", "")

	require.Equal(t, http.StatusNotFound, w.Code,
		"a subject outside the acting tenant is answered 404, the cross-tenant answer")
	require.True(t, watermarkFor(t, db, "uid-stranger").IsZero(), "and no watermark is written")
}

func TestAdminRevokeRequiresThePermission(t *testing.T) {
	r, _, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": true},
		perms:      stubResolver{}, // holds nothing, in particular not PermCredentialRevoke
	})

	require.Equal(t, http.StatusForbidden,
		doRequest(r, http.MethodPost, "/v1/iam/subjects/uid-nurse/revoke", "tok-nurse", "").Code)
}

// TestAdminRevokeFailsClosedWhenRolesUnavailable closes a coverage gap:
// adminRevoke's ListRoles error path was previously unexercised. Without
// this test, a mutation that dropped the `if err != nil` check would
// still "deny" — hasBindingForTenant(nil, ...) is false, so the request
// falls through to 404 — but 404 is the wrong denial. An authorization-
// infrastructure failure must be indistinguishable from every OTHER such
// failure in this codebase (503 authz_unavailable), never silently
// reinterpreted as "the subject doesn't exist", which is a different
// claim the code has no basis to make when it could not even ask.
func TestAdminRevokeFailsClosedWhenRolesUnavailable(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-admin": {Subject: "uid-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-admin": true},
		perms:      stubResolver{"uid-admin": {PermCredentialRevoke}},
		roles:      failingRoleLister{err: errors.New("openfga unreachable")},
	})

	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/uid-target/revoke", "tok-admin", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a ListRoles failure must deny with 503 authz_unavailable, matching every other "+
			"authorization-infrastructure failure in this codebase — never fall through to 404")
	require.Contains(t, w.Body.String(), "authz_unavailable")
	require.True(t, watermarkFor(t, db, "uid-target").IsZero(), "and no watermark is written")
}

func TestRevocationPublishesInvalidationInTheSameTransaction(t *testing.T) {
	r, _, _, bus := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	watchCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	watcher := startRevocationWatcher(t, watchCtx, bus)

	doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")

	require.Eventually(t, func() bool {
		return watcher.saw("uid-nurse")
	}, 10*time.Second, 100*time.Millisecond,
		"the invalidation must go through the outbox, so it cannot commit without the watermark or vice versa")
}

// TestModuleAndMiddlewareShareOneRevocationCheckerInstance is the direct
// proof for the wiring hazard this task exists to close: Task 4 left
// cmd/api/main.go constructing a *RevocationChecker for authn.Middleware
// while internal/bootstrap/modules.go called iam.New() with no checker at
// all — two different objects, or (before this task even compiled) no
// shared object whatsoever. The module's sign-out handler invalidating a
// cache nothing on the request path reads is a defect no other test in
// this suite would catch, because every other test either exercises the
// handler alone or the middleware alone.
//
// The credential here is presented with a FIXED auth_time on every call
// (fixedVerifier), the shape of a client replaying one still-open
// credential rather than signing in again. If the module's checker and
// the middleware's checker were two separate instances, the middleware
// would keep answering from its own still-cached "not revoked" entry and
// the second request below would still succeed — wrongly serving a
// revoked credential until the 5-minute TTL, exactly the outage this
// feature exists to prevent.
func TestModuleAndMiddlewareShareOneRevocationCheckerInstance(t *testing.T) {
	signInTime := time.Now()
	r, _, checker, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier: fixedVerifier{
			"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: signInTime},
		},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	// Warm the middleware's cache with a negative entry for uid-nurse,
	// exactly as an ordinary request before sign-out would.
	w := doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "tok-nurse", "")
	require.Equal(t, http.StatusOK, w.Code, "precondition: the credential works before sign-out")
	require.Greater(t, checker.EntriesForTest(), 0,
		"precondition: the middleware's checker cached the negative lookup")

	w = doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")
	require.Equal(t, http.StatusOK, w.Code)

	// The SAME token, presented again immediately, with no wait for any
	// broadcast or TTL.
	w = doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "tok-nurse", "")
	require.Equal(t, http.StatusUnauthorized, w.Code,
		"the module's sign-out handler and authn.Middleware must consult the SAME RevocationChecker "+
			"instance; two separate instances would leave the middleware's cache uninvalidated until "+
			"the 5-minute TTL backstop")
}

// TestRevocationPropagatesToAnotherReplica is spec T9: two independently
// constructed checkers sharing one Postgres and one NATS — the shape of
// two API pods — must converge without waiting on the TTL. Unlike
// TestModuleAndMiddlewareShareOneRevocationCheckerInstance (one process,
// one shared object, immediate by construction), this is the case where
// two DIFFERENT checker instances legitimately exist and propagation must
// close the gap between them.
func TestRevocationPropagatesToAnotherReplica(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// The revocation table plus events.Migrations() (outbox_events, which
	// bus.Publish below writes into): this test needs no iam_roles/
	// iam_members, so it pulls in only what it exercises rather than the
	// full platform migration set. tenantdb.Migrations() IS still needed
	// (unlike the comment used to say) because outbox_events' own policy
	// (0002_events_outbox_tenant, #835 Task 1) calls hms_tenant_visible.
	iamMigs := New(nil).Migrations()
	var revocationMig tenantdb.Migration
	for _, m := range iamMigs {
		if m.ID == "0003_iam" {
			revocationMig = m
		}
	}
	require.NotEmpty(t, revocationMig.ID, "precondition: 0003_iam is the revocation table migration")
	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, revocationMig)
	require.NoError(t, db.Migrate(ctx, migs))

	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	go bus.RunDispatcher(ctx, db)

	replicaA := NewRevocationChecker(db)
	replicaB := NewRevocationChecker(db)

	moduleB := New(replicaB)
	require.NoError(t, bus.StartBroadcasts(ctx, moduleB.Broadcasts(platform.Deps{})))

	// B caches the negative answer.
	got, err := replicaB.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.True(t, got.IsZero())

	// A revokes.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := replicaA.RevokeTx(tx, "uid-nurse", time.Now().UTC(), "sign_out", "uid-nurse"); err != nil {
			return err
		}
		data, err := json.Marshal(iamcontract.CredentialRevokedData{Subject: "uid-nurse"})
		if err != nil {
			return err
		}
		return bus.Publish(tx, iamcontract.SubjectCredentialRevoked, events.Event{
			Type: "CredentialRevoked", Version: 1, Data: data,
		})
	}))

	require.Eventually(t, func() bool {
		w, err := replicaB.RevokedAfter(ctx, "uid-nurse")
		return err == nil && !w.IsZero()
	}, 15*time.Second, 100*time.Millisecond,
		"a revocation on one replica must invalidate the cache on another; without the broadcast "+
			"this only heals at the 5-minute TTL")
}
