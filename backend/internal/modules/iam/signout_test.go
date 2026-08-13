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

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
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

// recordingRevoker records every subject GIP was asked to revoke, so a
// test can assert not just that sign-out succeeded but that the identity
// provider was actually told (#781 D1: HMS-initiated revocations must not
// leave GIP quietly disagreeing).
type recordingRevoker struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingRevoker) RevokeRefreshTokens(_ context.Context, uid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, uid)
	return nil
}

func (r *recordingRevoker) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
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
func newSignoutHarness(t *testing.T, cfg signoutHarnessConfig) (*gin.Engine, *tenantdb.DB, *RevocationChecker, *recordingRevoker) {
	t.Helper()

	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, New(nil).Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "tenant tables must carry forced RLS")

	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), t.Name())
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	checker := NewRevocationChecker(db)
	revoker := &recordingRevoker{}

	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(requestid.Middleware())
	api := platform.NewRouter(e.Group("/v1",
		authn.Middleware(cfg.verifier, checker),
		requestid.PrincipalMiddleware(),
		authz.Middleware(cfg.perms),
	), cfg.membership)

	deps := platform.Deps{DB: db, Bus: bus, Roles: cfg.roles, TokenRevoker: revoker}
	m := New(checker)
	m.Routes(api, deps)
	require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	require.NoError(t, bus.StartBroadcasts(ctx, m.Broadcasts(deps)))
	go bus.RunDispatcher(ctx, db)

	return e, db, checker, revoker
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

// outboxContains reports whether an outbox row published on subject
// carries a CredentialRevokedData payload naming targetSubject. It reads
// outbox_events directly rather than through the dispatcher, so it
// observes the write made inside the SAME transaction as the watermark —
// exactly the property TestRevocationPublishesInvalidationInTheSameTransaction
// exists to prove.
func outboxContains(t *testing.T, db *tenantdb.DB, subject, targetSubject string) bool {
	t.Helper()
	var payloads [][]byte
	err := db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload FROM outbox_events WHERE subject = ?`, subject).Scan(&payloads).Error
	})
	require.NoError(t, err)
	for _, raw := range payloads {
		var evt events.Event
		if err := json.Unmarshal(raw, &evt); err != nil {
			continue
		}
		var data CredentialRevokedData
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			continue
		}
		if data.Subject == targetSubject {
			return true
		}
	}
	return false
}

func TestSignOutWritesTheWatermarkAndRevokesAtGIP(t *testing.T) {
	r, db, _, revoker := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	w := doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")
	require.Equal(t, http.StatusOK, w.Code)

	require.False(t, watermarkFor(t, db, "uid-nurse").IsZero(),
		"sign-out must write the watermark, not merely clear a cookie")
	require.Equal(t, []string{"uid-nurse"}, revoker.Calls(),
		"GIP must be told, so the identity provider does not disagree with us")
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
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"tok-nurse": {Subject: "uid-nurse", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"uid-nurse": false},
		perms:      stubResolver{},
	})

	doRequest(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")

	require.Eventually(t, func() bool {
		return outboxContains(t, db, SubjectCredentialRevoked, "uid-nurse")
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
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// The revocation table plus events.Migrations() (outbox_events, which
	// bus.Publish below writes into): this test needs no iam_roles/
	// iam_members and no hms_tenant_visible, so it pulls in only what it
	// exercises rather than the full platform migration set.
	iamMigs := New(nil).Migrations()
	migs := append(events.Migrations(), iamMigs[len(iamMigs)-1])
	require.NoError(t, db.Migrate(ctx, migs))

	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), t.Name())
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
		data, err := json.Marshal(CredentialRevokedData{Subject: "uid-nurse"})
		if err != nil {
			return err
		}
		return bus.Publish(tx, SubjectCredentialRevoked, events.Event{
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
