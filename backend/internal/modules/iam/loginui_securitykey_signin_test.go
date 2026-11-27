package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// registeredKeySignIn is a later sign-in by a user whose security key
// Helivanta registered: Zitadel holds it READY and the registry has it.
func registeredKeySignIn(t *testing.T) (*LoginUIHandlers, *gin.Engine, *zitadelEnrollmentFake) {
	t.Helper()
	fake := newZitadelEnrollmentFake(t)
	fake.keyReady.Store(true)
	h := newFactorTestHandlers(t, fake.client)
	h.SetWebAuthnRPID(securityKeyTestRPID)
	require.NoError(t, h.keys.Add(context.Background(), "user-enrol", securityKeyFakeID, securityKeyTestRPID))
	return h, newFactorRouter(h), fake
}

type factorRequiredWire struct {
	Factors     []string `json:"factor_required"`
	SecurityKey *struct {
		RequestOptions json.RawMessage `json:"request_options"`
	} `json:"security_key"`
}

func TestPasswordWithRegisteredKeyChallengesNatively(t *testing.T) {
	h, r, fake := registeredKeySignIn(t)
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body factorRequiredWire
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []string{"security_key"}, body.Factors)
	require.NotNil(t, body.SecurityKey)
	require.JSONEq(t, securityKeyFakeRequest, string(body.SecurityKey.RequestOptions))
	require.NotContains(t, rec.Body.String(), "security_key_required", "a registered key never leaves Helivanta's page")
	require.NotContains(t, rec.Body.String(), "callback_url")
	require.Equal(t, int32(1), fake.keyChallenges.Load())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.False(t, attempt.Enrolling)
	require.Equal(t, securityKeySignIn, attempt.SecurityKeyID, "the attempt must be marked so /factor admits the assertion")
	require.Equal(t, securityKeyChallengeToken, attempt.SessionToken, "the challenge's rotated token must be stored")

	w := doFactorKey(t, r, loginUITestAuthRequestID, goodKeyAssertion)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"callback_url"`)
	require.True(t, fake.finalized.Load())
}

func TestPasswordWithKeyAndTOTPOffersBoth(t *testing.T) {
	h, r, fake := registeredKeySignIn(t)
	fake.verified.Store(true) // TOTP enrolled too
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body factorRequiredWire
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []string{"security_key", "totp"}, body.Factors)
	require.NotNil(t, body.SecurityKey)

	// The TOTP stays usable on the same attempt.
	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.False(t, attempt.Enrolling)
	w := doFactor(t, r, loginUITestAuthRequestID, "totp", enrollmentGoodCode)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"callback_url"`)
}

// A key Helivanta did not register (a legacy key) keeps ADR-0069's
// continuation until slice 3, decided by comparing Zitadel's READY u2f ids
// against the registry: one of two keys registered is not enough.
func TestPasswordWithLegacyKeyKeepsADR0069Continuation(t *testing.T) {
	var challenges, searches atomic.Int32
	finalized := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"legacy-s","sessionToken":"private-legacy-token"}`))
	})
	mux.HandleFunc("GET /v2/sessions/legacy-s", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session":{"factors":{"user":{"id":"owner","organizationId":"owner-org","loginName":"owner@example.test"},"password":{"verifiedAt":"2026-11-11T07:00:00Z"}}}}`))
	})
	mux.HandleFunc("GET /v2/users/owner/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
	})
	mux.HandleFunc("POST /management/v1/users/owner/auth_factors/_search", func(w http.ResponseWriter, _ *http.Request) {
		searches.Add(1)
		_, _ = w.Write([]byte(`{"result":[{"state":"AUTH_FACTOR_STATE_READY","u2f":{"id":"helivanta-key"}},{"state":"AUTH_FACTOR_STATE_READY","u2f":{"id":"legacy-key"}}]}`))
	})
	mux.HandleFunc("PATCH /v2/sessions/legacy-s", func(w http.ResponseWriter, _ *http.Request) {
		challenges.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /v2/oidc/auth_requests/"+loginUITestAuthRequestID, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": loginUITestAuthRequestID, "clientId": "helivanta-web", "redirectUri": "https://helivanta.app/api/auth/callback"}})
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/"+loginUITestAuthRequestID, func(w http.ResponseWriter, _ *http.Request) {
		finalized = true
		w.WriteHeader(http.StatusInternalServerError)
	})
	native := httptest.NewTLSServer(mux)
	t.Cleanup(native.Close)

	h := newFactorTestHandlers(t, withNoEmailAccounts(loginclient.New(native.URL, "synthetic", native.Client())))
	h.SetNativeWebClientID("helivanta-web")
	h.SetWebAuthnRPID(securityKeyTestRPID)
	require.NoError(t, h.keys.Add(context.Background(), "owner", "helivanta-key", securityKeyTestRPID))
	r := newFactorRouter(h)

	rec := doPassword(t, r, loginUITestAuthRequestID, "owner@example.test", "synthetic-password")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"security_key_required"`)
	require.NotContains(t, rec.Body.String(), "factor_required")
	require.NotContains(t, rec.Body.String(), "request_options")
	require.Equal(t, int32(1), searches.Load(), "precondition: the decision read Zitadel's READY keys")
	require.Zero(t, challenges.Load(), "a legacy key must not get a native challenge")
	require.False(t, finalized)
}

func TestRegistryUnreadableIs503(t *testing.T) {
	h, r, fake := registeredKeySignIn(t)
	// DDL through Migrate (the schema owner's pool) — WithAdmin is archtest-banned.
	require.NoError(t, h.keys.db.Migrate(context.Background(), []tenantdb.Migration{{
		ID: "test_unreadable_security_keys", SQL: `ALTER TABLE iam_security_keys RENAME TO test_unreadable_security_keys`,
	}}))
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "request_options")
	require.NotContains(t, rec.Body.String(), "security_key_required")
	require.Zero(t, fake.keyChallenges.Load())
	require.False(t, fake.finalized.Load())
	_, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound, "no attempt is left behind")
}
