package loginclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	keyOnlyMethods   = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_U2F"]`
	keyAndTOTP       = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	keyAndPasskey    = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_PASSKEY"]`
	oneReadyKey      = `{"result":[{"state":"AUTH_FACTOR_STATE_READY","u2f":{"id":"k1"}},{"state":"AUTH_FACTOR_STATE_NOT_READY","u2f":{"id":"k2"}}]}`
	noReadyKey       = `{"result":[{"state":"AUTH_FACTOR_STATE_NOT_READY","u2f":{"id":"k2"}}]}`
	verifiedFactorAt = `{"verifiedAt":"2026-11-27T00:00:00Z"}`
)

// keyZitadel serves a user whose enrolled methods, verified session factors
// and READY u2f ids the test chooses (#1898), with a policy that does NOT
// force MFA: the case where a forgotten key check finalizes password-only.
type keyZitadel struct {
	methods   string
	readyKeys string
	webAuthN  bool
	totp      bool
	searches  atomic.Int32
	finalized atomic.Bool
}

func (z *keyZitadel) client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if servePasswordCurrent(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			_, _ = w.Write([]byte(nonForceMFAPolicy))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			factors := `"user":{"id":"u1","organizationId":"o1"},"password":` + verifiedFactorAt
			if z.webAuthN {
				factors += `,"webAuthN":` + verifiedFactorAt
			}
			if z.totp {
				factors += `,"totp":` + verifiedFactorAt
			}
			_, _ = w.Write([]byte(`{"session":{"id":"1","factors":{` + factors + `}}}`))
		case strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			_, _ = w.Write([]byte(`{"authMethodTypes":` + z.methods + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/management/v1/users/u1/auth_factors/_search":
			z.searches.Add(1)
			if r.Header.Get("x-zitadel-orgid") != "o1" {
				t.Errorf("auth factors read in org %q, want o1", r.Header.Get("x-zitadel-orgid"))
			}
			_, _ = w.Write([]byte(z.readyKeys))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/oidc/auth_requests/"):
			z.finalized.Store(true)
			_, _ = w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "pat", srv.Client())
}

// fakeRegistry answers ok/err and records what it was asked.
type fakeRegistry struct {
	ok      bool
	err     error
	calls   int
	subject string
	keyIDs  []string
}

func (f *fakeRegistry) contains(_ context.Context, subject string, keyIDs []string) (bool, error) {
	f.calls++
	f.subject, f.keyIDs = subject, keyIDs
	return f.ok, f.err
}

var keySession = Session{ID: "1", Token: "t"}

func TestCompleteIfSufficient_RegisteredKeyAsksForTheKey(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
	reg := &fakeRegistry{ok: true}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, reg.contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome, res.Reason.String())
	require.Equal(t, []string{"security_key"}, res.Factors)
	require.Equal(t, "u1", reg.subject, "the registry must be asked about the session's own user")
	require.Equal(t, []string{"k1"}, reg.keyIDs, "only READY keys are compared")
	require.False(t, z.finalized.Load())
}

func TestCompleteIfSufficient_RegisteredKeyAndTOTPOffersBoth(t *testing.T) {
	z := &keyZitadel{methods: keyAndTOTP, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome)
	require.Equal(t, []string{"security_key", "totp"}, res.Factors)
}

// A key Helivanta did not register (a legacy key) stays uncollectible, so the
// ADR-0069 continuation still sees RefusalFactorUnsupported.
func TestCompleteIfSufficient_UnregisteredKeyStaysUnsupported(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: false}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.Contains(t, res.EnrolledMethods, "AUTHENTICATION_METHOD_TYPE_U2F")
}

// No registry (no validated relying party): U2F is uncollectible, exactly as
// before #1898, and Zitadel's key list is not even read.
func TestCompleteIfSufficient_KeyWithoutRegistryStaysUnsupported(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, nil)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.Zero(t, z.searches.Load())
}

// U2F listed but no READY key: nothing Helivanta could challenge, so it is
// uncollectible without asking the registry ("no keys" is never "all known").
func TestCompleteIfSufficient_KeyWithNoReadyIDsStaysUnsupported(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: noReadyKey}
	reg := &fakeRegistry{ok: true}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, reg.contains)
	require.NoError(t, err)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.Zero(t, reg.calls)
}

func TestCompleteIfSufficient_KeyAndPasskeyStaysUnsupported(t *testing.T) {
	z := &keyZitadel{methods: keyAndPasskey, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
}

func TestCompleteIfSufficient_KeyRegistryOrZitadelUnreadableIsUnavailable(t *testing.T) {
	t.Run("registry", func(t *testing.T) {
		z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
		res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, (&fakeRegistry{err: errors.New("db down")}).contains)
		require.ErrorIs(t, err, ErrUnavailable)
		require.Equal(t, Result{}, res)
		require.False(t, z.finalized.Load())
	})
	t.Run("zitadel key list", func(t *testing.T) {
		z := &keyZitadel{methods: keyOnlyMethods, readyKeys: `not json`}
		res, err := z.client(t).CompleteIfSufficient(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
		require.ErrorIs(t, err, ErrUnavailable)
		require.Equal(t, Result{}, res)
		require.False(t, z.finalized.Load())
	})
}

// The bypass the Task 2 review named: once U2F is collectible, a U2F-only
// user (no ForceMFA) must not finalize on a password-only session.
func TestCompleteAfterFactor_KeyOnlyPasswordOnlySessionRefused(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteAfterFactor(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorNotVerified, res.Reason)
	require.False(t, z.finalized.Load(), "a password-only session finalized for a security-key user: MFA bypass")
}

func TestCompleteAfterFactor_KeyOnlyWithVerifiedKeyCompletes(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey, webAuthN: true}
	res, err := z.client(t).CompleteAfterFactor(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome, res.Reason.String())
	require.True(t, z.finalized.Load())
}

func TestCompleteAfterFactor_UnregisteredKeyRefusedEvenWhenVerified(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey, webAuthN: true}
	res, err := z.client(t).CompleteAfterFactor(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: false}).contains)
	require.NoError(t, err)
	require.Equal(t, RefusalFactorUnsupported, res.Reason)
	require.False(t, z.finalized.Load())
}

func TestCompleteAfterPasswordChange_KeyOnlyPasswordOnlySessionRefused(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey}
	res, err := z.client(t).CompleteAfterPasswordChange(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorNotVerified, res.Reason)
	require.False(t, z.finalized.Load(), "a password-only session finalized for a security-key user: MFA bypass")
}

func TestCompleteAfterPasswordChange_KeyOnlyWithVerifiedKeyCompletes(t *testing.T) {
	z := &keyZitadel{methods: keyOnlyMethods, readyKeys: oneReadyKey, webAuthN: true}
	res, err := z.client(t).CompleteAfterPasswordChange(context.Background(), "V2_1", keySession, (&fakeRegistry{ok: true}).contains)
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome, res.Reason.String())
	require.True(t, z.finalized.Load())
}
