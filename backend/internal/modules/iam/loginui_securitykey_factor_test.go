package iam

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	goodKeyAssertion = `{"id":"good-assertion","type":"public-key","response":{"signature":"sig-secret"}}`
	badKeyAssertion  = `{"id":"forged-assertion","type":"public-key"}`
)

func doFactorKey(t *testing.T, r *gin.Engine, authRequestID, assertion string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"auth_request_id":"` + authRequestID + `","factor":"security_key","assertion":` + assertion + `}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/factor", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// enrolledKeyAttempt drives password → /enroll security_key, leaving the
// attempt at the factor stage with a WebAuthn challenge on the session.
func enrolledKeyAttempt(t *testing.T) (*LoginUIHandlers, *gin.Engine, *zitadelEnrollmentFake) {
	t.Helper()
	h, r, fake, _ := startSecurityKeyEnrolment(t)
	w := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return h, r, fake
}

func TestFactorSecurityKeyCompletes(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h, r, fake := enrolledKeyAttempt(t)
	w := doFactorKey(t, r, loginUITestAuthRequestID, goodKeyAssertion)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"callback_url"`)
	require.True(t, fake.finalized.Load(), "finalize must run (with the rotated token, or the fake refuses)")
	require.Equal(t, securityKeyChallengeToken, fake.keyCheckToken.Load(), "the assertion must carry the challenge's rotated token")
	require.Equal(t, int32(0), fake.sessionChecks.Load(), "no TOTP check was involved")

	_, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound, "a finished sign-in deletes its attempt")
	for _, secret := range []string{"good-assertion", "sig-secret", "assert-challenge", factorRotatedToken, securityKeyChallengeToken} {
		require.NotContains(t, logged.String(), secret)
	}
}

func TestFactorSecurityKeyWrongAssertionSpendsAttempt(t *testing.T) {
	h, r, fake := enrolledKeyAttempt(t)
	w := doFactorKey(t, r, loginUITestAuthRequestID, badKeyAssertion)
	wrongPassword := postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")
	require.Equal(t, wrongPassword.Code, w.Code, w.Body.String())
	require.Equal(t, wrongPassword.Body.String(), w.Body.String(), "a refused key must answer exactly like a wrong password")
	require.Equal(t, int32(1), fake.keyChecks.Load())
	require.False(t, fake.finalized.Load())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Equal(t, 1, attempt.FactorAttempts, "the refusal must spend a guess")
}

// Review Focus 4: an attempt no key challenge was issued to (an enrolled-TOTP
// factor step) is refused before any Zitadel call, like a wrong code.
func TestFactorSecurityKeyOnTOTPAttemptRefused(t *testing.T) {
	var patches atomic.Int32
	client := zitadelFactorRequired(t, func(w http.ResponseWriter, _ *http.Request) {
		patches.Add(1)
		_, _ = w.Write([]byte(`{"sessionToken":"` + factorRotatedToken + `"}`))
	}, finalizeRequireRotatedToken)
	h := newFactorTestHandlers(t, client)
	h.SetWebAuthnRPID(securityKeyTestRPID)
	r := newFactorRouter(h)
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"factor_required":["totp"]`)

	w := doFactorKey(t, r, loginUITestAuthRequestID, goodKeyAssertion)
	wrongPassword := postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")
	require.Equal(t, wrongPassword.Code, w.Code, w.Body.String())
	require.Equal(t, wrongPassword.Body.String(), w.Body.String())
	require.Equal(t, int32(0), patches.Load(), "the refusal must be decided before any Zitadel call")

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Equal(t, 1, attempt.FactorAttempts, "the refusal must spend a guess, as a wrong code does")
}

func TestFactorSecurityKeyZitadelDownIs503(t *testing.T) {
	h, r, fake := enrolledKeyAttempt(t)
	fake.keyCheckDown.Store(true)
	w := doFactorKey(t, r, loginUITestAuthRequestID, goodKeyAssertion)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.False(t, fake.finalized.Load())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Zero(t, attempt.FactorAttempts, "Zitadel being down is not a wrong key")
}

func TestFactorRejectsMalformedRequests(t *testing.T) {
	_, r, fake := enrolledKeyAttempt(t)
	for name, body := range map[string]string{
		"unknown factor":        `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"passkey","code":"123456"}`,
		"totp without code":     `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"totp"}`,
		"key without assertion": `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key"}`,
		"key with null":         `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key","assertion":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/factor", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	require.Equal(t, int32(0), fake.keyChecks.Load(), "a malformed request must not reach Zitadel")
}
