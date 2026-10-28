package iam

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/session"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type identityGuardEnv struct {
	db       *tenantdb.DB
	bus      *events.Bus
	checker  *RevocationChecker
	signer   *session.Signer
	verifier authn.TokenVerifier
}

func newIdentityGuardEnv(t *testing.T, withOutbox bool) *identityGuardEnv {
	t.Helper()
	db, ctx := revocationHarness(t)
	migrations := tenantdb.Migrations()
	if withOutbox {
		migrations = append(migrations, events.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migrations))
	for _, migration := range New(nil).Migrations() {
		if migration.ID == "0013_iam_identity_guard" {
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{migration}))
		}
	}
	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	pub, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(key, "guard-test", "https://guard.test", time.Hour)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, "guard-test", "https://guard.test")
	require.NoError(t, err)
	return &identityGuardEnv{db: db, bus: bus, checker: NewRevocationChecker(db), signer: signer, verifier: authn.NewSessionVerifier(verifier)}
}

func (h *identityGuardEnv) token(t *testing.T, subject string, at time.Time) string {
	t.Helper()
	token, err := h.signer.Mint(subject, "11111111-1111-1111-1111-111111111111", at, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return token
}

func (h *identityGuardEnv) request(provider UserStateChecker, cached *RevocationChecker, token string) *httptest.ResponseRecorder {
	return h.serve(cached, token, NewIdentityGuard(h.db, h.bus, h.checker, provider))
}

func (h *identityGuardEnv) serve(cached *RevocationChecker, token string, guard gin.HandlerFunc) *httptest.ResponseRecorder {
	e := gin.New()
	e.GET("/staff-data", authn.Middleware(h.verifier, cached), guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/staff-data", nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
	}
	e.ServeHTTP(w, r)
	return w
}

type guardUserStateFunc func(context.Context, string) (loginclient.UserState, error)

func (f guardUserStateFunc) UserState(ctx context.Context, subject string) (loginclient.UserState, error) {
	return f(ctx, subject)
}

func TestIdentityGuardRejectsExistingCredentialAfterDisablement(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := &stubUserState{states: map[string]loginclient.UserState{"staff-user": loginclient.UserStateActive}}
	request := func() int { return h.request(provider, h.checker, token).Code }
	require.Equal(t, http.StatusOK, request())
	provider.states["staff-user"] = loginclient.UserStateInactive
	require.Equal(t, http.StatusUnauthorized, request(), "ordinary traffic must check disablement without relying on renewal")
	require.False(t, watermarkFor(t, h.db, "staff-user").IsZero())
	provider.states["staff-user"] = loginclient.UserStateActive
	require.Equal(t, http.StatusUnauthorized, request(), "reactivation must not restore the old credential")

}

func TestIdentityGuardRejectsRevocationCommittedDuringIdentityLookup(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := guardUserStateFunc(func(ctx context.Context, subject string) (loginclient.UserState, error) {
		err := h.db.WithSystem(ctx, func(tx *gorm.DB) error {
			return h.checker.RevokeTx(tx, subject, time.Now(), "admin_revoke", "reviewer")
		})
		return loginclient.UserStateActive, err
	})
	w := h.request(provider, h.checker, token)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestIdentityGuardUnavailableStatePreservesCredential(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	for _, state := range []guardUserStateFunc{
		func(context.Context, string) (loginclient.UserState, error) {
			return loginclient.UserStateActive, errors.New("provider unreachable")
		},
		func(context.Context, string) (loginclient.UserState, error) { return loginclient.UserState(0), nil },
	} {
		w := h.request(state, h.checker, token)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
		require.True(t, watermarkFor(t, h.db, "staff-user").IsZero(), "unknown state must not permanently invalidate the credential")
	}
}

func TestIdentityGuardSeesWatermarkDespiteMissedBroadcast(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	other := NewRevocationChecker(h.db)
	provider := &stubUserState{}
	require.Equal(t, http.StatusOK, h.request(provider, other, token).Code)
	revokeNow(t, h.db, h.checker, "staff-user", time.Now(), "admin_revoke", "reviewer")
	stale, err := other.RevokedAfter(t.Context(), "staff-user")
	require.NoError(t, err)
	require.True(t, stale.IsZero(), "precondition: this replica missed invalidation")
	w := h.request(provider, other, token)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	otherToken := h.token(t, "other-staff", time.Now().Add(-time.Hour))
	require.Equal(t, http.StatusOK, h.request(provider, other, otherToken).Code)
}

func TestIdentityGuardRollsBackRevocationWhenOutboxUnavailable(t *testing.T) {
	h := newIdentityGuardEnv(t, false)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := &stubUserState{states: map[string]loginclient.UserState{"staff-user": loginclient.UserStateInactive}}
	w := h.request(provider, h.checker, token)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, h.db, "staff-user").IsZero(), "outbox failure must roll back the watermark")
}

func TestIdentityGuardDispatchesCommittedInvalidation(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	watcher := startRevocationWatcher(t, t.Context(), h.bus)
	go h.bus.RunDispatcher(t.Context(), h.db)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := &stubUserState{states: map[string]loginclient.UserState{"staff-user": loginclient.UserStateInactive}}
	w := h.request(provider, h.checker, token)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Eventually(t, func() bool { return watcher.saw("staff-user") }, 10*time.Second, 10*time.Millisecond)
	var row revocationRow
	require.NoError(t, h.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Where("subject=?", "staff-user").Take(&row).Error
	}))
	require.Equal(t, "identity_inactive", row.Reason)
	require.Equal(t, "system:identity-state", row.Actor)
}

func TestIdentityGuardKeepsSelfSignOutAvailableDuringIdentityOutage(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := &stubUserState{err: errors.New("identity provider unavailable")}
	for _, route := range []string{"/v1/iam/me/sign-out"} {
		e := gin.New()
		e.Use(authn.Middleware(h.verifier, h.checker), NewIdentityGuard(h.db, h.bus, h.checker, provider))
		e.POST(route, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, route, nil)
		r.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
		e.ServeHTTP(w, r)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	}
	require.Zero(t, provider.calls, "self sign-out must not depend on the identity service")
}

func TestIdentityGuardObservationDoesNotRevokeLaterAuthentication(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	observedAt := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	provider := &stubUserState{states: map[string]loginclient.UserState{"staff-user": loginclient.UserStateInactive}}
	guard := newIdentityGuard(h.db, h.bus, h.checker, provider, func() time.Time { return observedAt })
	old := h.token(t, "staff-user", observedAt.Add(-time.Hour))
	require.Equal(t, http.StatusUnauthorized, h.serve(h.checker, old, guard).Code)
	require.Equal(t, observedAt.UTC(), watermarkFor(t, h.db, "staff-user").UTC())
	provider.states["staff-user"] = loginclient.UserStateActive
	fresh := h.token(t, "staff-user", time.Now())
	require.Equal(t, http.StatusOK, h.request(provider, h.checker, fresh).Code)
	require.Equal(t, http.StatusUnauthorized, h.request(provider, h.checker, old).Code)
}

func TestIdentityGuardChecksActualProviderForSignedSubject(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	state := "USER_STATE_ACTIVE"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/users/staff-user", r.URL.Path)
		_, err := w.Write([]byte(`{"user":{"state":"` + state + `"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(provider.Close)
	client := loginclient.New(provider.URL, "synthetic-test-pat", provider.Client())
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	require.Equal(t, http.StatusOK, h.request(client, h.checker, token).Code)
	state = "USER_STATE_LOCKED"
	require.Equal(t, http.StatusUnauthorized, h.request(client, h.checker, token).Code)
	state = "USER_STATE_ACTIVE"
	require.Equal(t, http.StatusUnauthorized, h.request(client, NewRevocationChecker(h.db), token).Code)
}

func TestIdentityGuardMissingAuthorityFailsClosed(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	provider := &stubUserState{}
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	for _, guard := range []gin.HandlerFunc{
		NewIdentityGuard(nil, h.bus, h.checker, provider),
		NewIdentityGuard(h.db, nil, h.checker, provider),
		NewIdentityGuard(h.db, h.bus, nil, provider),
		NewIdentityGuard(h.db, h.bus, h.checker, nil),
		NewIdentityGuard(h.db, h.bus, h.checker, (*loginclient.Client)(nil)),
	} {
		w := h.serve(h.checker, token, guard)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	}
	guard := NewIdentityGuard(h.db, h.bus, h.checker, provider)
	e := gin.New()
	e.GET("/no-principal", guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/no-principal", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestIdentityGuardFreshWatermarkUnavailableFailsClosedAfterWarmCache(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	token := h.token(t, "staff-user", time.Now().Add(-time.Hour))
	provider := &stubUserState{}
	require.Equal(t, http.StatusOK, h.request(provider, h.checker, token).Code)
	require.NoError(t, h.db.Close())
	w := h.request(provider, h.checker, token)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "identity_unavailable", "must reach the fresh IAM gate despite the warm outer cache")
}

func TestIdentityGuardInactiveAdminCannotRevokeAnotherSubject(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	provider := &stubUserState{states: map[string]loginclient.UserState{"admin": loginclient.UserStateInactive}}
	token := h.token(t, "admin", time.Now().Add(-time.Hour))
	e := gin.New()
	e.Use(authn.Middleware(h.verifier, h.checker), NewIdentityGuard(h.db, h.bus, h.checker, provider))
	reached := false
	e.POST("/v1/iam/subjects/:subject/revoke", func(c *gin.Context) { reached = true; c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/iam/subjects/another-user/revoke", nil)
	r.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
	e.ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.False(t, reached, "a disabled admin must not change another user's credentials")
	require.True(t, watermarkFor(t, h.db, "another-user").IsZero())
}

func TestIdentityGuardAdminRevokeUnavailableFailsClosed(t *testing.T) {
	h := newIdentityGuardEnv(t, true)
	provider := &stubUserState{err: errors.New("provider unavailable")}
	token := h.token(t, "admin", time.Now().Add(-time.Hour))
	e := gin.New()
	e.Use(authn.Middleware(h.verifier, h.checker), NewIdentityGuard(h.db, h.bus, h.checker, provider))
	e.POST("/v1/iam/subjects/:subject/revoke", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/iam/subjects/another-user/revoke", nil)
	r.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
	e.ServeHTTP(w, r)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, h.db, "admin").IsZero())
	require.True(t, watermarkFor(t, h.db, "another-user").IsZero())
}
