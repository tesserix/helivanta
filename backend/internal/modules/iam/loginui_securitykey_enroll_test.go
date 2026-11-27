package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	securityKeyFakeID         = "key-enrol"
	securityKeyFakeCreation   = `{"publicKey":{"attestation":"none","challenge":"create-challenge","rp":{"id":"helivanta.test","name":"Helivanta"}}}`
	securityKeyFakeRequest    = `{"publicKey":{"challenge":"assert-challenge","rpId":"helivanta.test"}}`
	securityKeyChallengeToken = "token-after-webauthn-challenge"
	securityKeyTestRPID       = "helivanta.test"
	goodKeyCredential         = `{"id":"good-credential","type":"public-key"}`
	badKeyCredential          = `{"id":"forged-credential","type":"public-key"}`
)

// startSecurityKeyEnrolment drives the password step to enrollment_required
// on a deployment with a validated relying party.
func startSecurityKeyEnrolment(t *testing.T) (*LoginUIHandlers, *gin.Engine, *zitadelEnrollmentFake, *httptest.ResponseRecorder) {
	t.Helper()
	fake := newZitadelEnrollmentFake(t)
	h := newFactorTestHandlers(t, fake.client)
	h.SetWebAuthnRPID(securityKeyTestRPID)
	r := newFactorRouter(h)
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return h, r, fake, rec
}

func doEnrollKey(t *testing.T, r *gin.Engine, authRequestID, credential string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"auth_request_id":"` + authRequestID + `","factor":"security_key","credential":` + credential + `}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/enroll", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func countSecurityKeys(t *testing.T, h *LoginUIHandlers) int64 {
	t.Helper()
	var n int64
	require.NoError(t, h.keys.db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Table("iam_security_keys").Count(&n).Error
	}))
	return n
}

func TestPasswordEnrollmentOffersSecurityKey(t *testing.T) {
	h, _, _, rec := startSecurityKeyEnrolment(t)
	var body struct {
		Factors []string `json:"enrollment_required"`
		TOTP    struct {
			Secret string `json:"secret"`
		} `json:"totp"`
		SecurityKey struct {
			CreationOptions json.RawMessage `json:"creation_options"`
		} `json:"security_key"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []string{"totp", "security_key"}, body.Factors)
	require.Equal(t, enrollmentFakeSecret, body.TOTP.Secret, "the TOTP option must stay on offer")
	require.JSONEq(t, securityKeyFakeCreation, string(body.SecurityKey.CreationOptions))

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.True(t, attempt.Enrolling)
	require.Equal(t, securityKeyFakeID, attempt.SecurityKeyID)
}

// Without a validated relying party the key is not offered at all: TOTP
// only, and no key id on the attempt (fail closed).
func TestPasswordEnrollmentWithoutRelyingPartyOffersTOTPOnly(t *testing.T) {
	fake := newZitadelEnrollmentFake(t)
	h := newFactorTestHandlers(t, fake.client)
	r := newFactorRouter(h)
	rec := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "security_key")
	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Empty(t, attempt.SecurityKeyID)

	w := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, int32(0), fake.keyVerifies.Load())
}

func TestEnrollSecurityKeyRecordsThenChallenges(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h, r, fake, _ := startSecurityKeyEnrolment(t)
	var recordedBeforeChallenge atomic.Bool
	hook := func() {
		ok, err := h.keys.Contains(context.Background(), "user-enrol", []string{securityKeyFakeID})
		recordedBeforeChallenge.Store(err == nil && ok)
	}
	fake.onChallenge.Store(&hook)

	w := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		SecurityKey struct {
			RequestOptions json.RawMessage `json:"request_options"`
		} `json:"security_key"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.JSONEq(t, securityKeyFakeRequest, string(body.SecurityKey.RequestOptions))
	require.NotContains(t, w.Body.String(), "callback_url", "a key enrolment must not finalize the login")
	require.Equal(t, int32(1), fake.keyVerifies.Load())
	require.Equal(t, int32(1), fake.keyChallenges.Load())
	require.True(t, recordedBeforeChallenge.Load(), "the registry row must exist before the challenge is issued")
	require.False(t, fake.finalized.Load())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err, "the attempt must stay at the factor stage for the assertion")
	require.Equal(t, securityKeyChallengeToken, attempt.SessionToken, "the challenge's rotated token must be stored")
	require.Equal(t, securityKeyFakeID, attempt.SecurityKeyID)

	for _, secret := range []string{"good-credential", "create-challenge", "assert-challenge", securityKeyChallengeToken, factorSessionCreationToken} {
		require.NotContains(t, logged.String(), secret)
	}
}

func TestEnrollSecurityKeyTwiceRegistersOnce(t *testing.T) {
	h, r, fake, _ := startSecurityKeyEnrolment(t)
	first := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	second := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Contains(t, second.Body.String(), "request_options")

	require.Equal(t, int32(1), fake.keyVerifies.Load(), "a recorded key must not be verified again")
	require.Equal(t, int32(2), fake.keyChallenges.Load(), "each call answers a fresh challenge")
	require.Equal(t, int64(1), countSecurityKeys(t, h))
	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Zero(t, attempt.FactorAttempts, "a retry is not a refusal and spends no guess")
}

func TestEnrollSecurityKeyRegistryFailureIs503(t *testing.T) {
	h, r, fake, _ := startSecurityKeyEnrolment(t)
	// Reads work, inserts fail: the failure lands after Zitadel verified the key.
	require.NoError(t, h.keys.db.WithAdmin(context.Background(), func(tx *gorm.DB) error {
		return tx.Exec(`
			CREATE FUNCTION test_refuse_security_key() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'registry unavailable'; END $$;
			CREATE TRIGGER test_refuse_security_key BEFORE INSERT ON iam_security_keys
			FOR EACH ROW EXECUTE FUNCTION test_refuse_security_key();`).Error
	}))

	w := doEnrollKey(t, r, loginUITestAuthRequestID, goodKeyCredential)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "request_options")
	require.Equal(t, int32(1), fake.keyVerifies.Load(), "precondition: the failure came after the verify")
	require.Equal(t, int32(0), fake.keyChallenges.Load(), "no challenge may be issued for an unrecorded key")
	require.Equal(t, int64(0), countSecurityKeys(t, h))
}

func TestEnrollSecurityKeyWrongCredentialSpendsAttempt(t *testing.T) {
	h, r, fake, _ := startSecurityKeyEnrolment(t)
	w := doEnrollKey(t, r, loginUITestAuthRequestID, badKeyCredential)
	wrongPassword := postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")
	require.Equal(t, wrongPassword.Code, w.Code, w.Body.String())
	require.Equal(t, wrongPassword.Body.String(), w.Body.String(), "a refused key must answer exactly like a wrong password")

	require.Equal(t, int32(1), fake.keyVerifies.Load())
	require.Equal(t, int32(0), fake.keyChallenges.Load())
	require.Equal(t, int64(0), countSecurityKeys(t, h))
	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err)
	require.Equal(t, 1, attempt.FactorAttempts, "the refusal must spend a guess")
}

func TestEnrollRejectsUnknownFactor(t *testing.T) {
	_, r, fake, _ := startSecurityKeyEnrolment(t)
	for name, body := range map[string]string{
		"unknown factor":           `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"passkey","code":"123456","credential":{"id":"x"}}`,
		"totp without code":        `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"totp"}`,
		"key without credential":   `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key"}`,
		"key with null credential": `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key","credential":null}`,
		"key with control in name": `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key","credential":{"id":"good-credential"},"name":"key\u0007"}`,
		"key with overlong name":   `{"auth_request_id":"` + loginUITestAuthRequestID + `","factor":"security_key","credential":{"id":"good-credential"},"name":"` + strings.Repeat("k", 65) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/enroll", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	require.Equal(t, int32(0), fake.keyVerifies.Load(), "a malformed request must not reach Zitadel")
}

func TestSecurityKeyNameIsNormalised(t *testing.T) {
	for raw, want := range map[string]string{
		"":                       defaultSecurityKeyName,
		"   ":                    defaultSecurityKeyName,
		"  Ward 4 key ":          "Ward 4 key",
		strings.Repeat("é", 64):  strings.Repeat("é", 64),
		"Clé de sécurité (Asha)": "Clé de sécurité (Asha)",
	} {
		got, err := securityKeyName(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got)
	}
	for _, raw := range []string{strings.Repeat("é", 65), "a\nb", "tab\there", "nul\x00", "\x7f", "bad\xff"} {
		_, err := securityKeyName(raw)
		require.Error(t, err, "%q", raw)
	}
}
