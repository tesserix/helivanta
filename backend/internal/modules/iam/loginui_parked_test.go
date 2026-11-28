package iam

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const parkedTestPassword = "Setup-password-1778"

func parkedTestHandlers(t *testing.T) (*LoginUIHandlers, *gin.Engine, *zitadelEnrollmentFake) {
	t.Helper()
	fake := newZitadelEnrollmentFake(t)
	h := newFactorTestHandlers(t, fake.client)
	h.SetNativeWebClientID("helivanta-web")
	r := newFactorRouter(h)
	r.POST("/v1/auth/login/resume", h.Resume)
	return h, r, fake
}

func doResume(t *testing.T, r *gin.Engine, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/resume", strings.NewReader(`{"auth_request_id":"`+loginUITestAuthRequestID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	r.ServeHTTP(w, req)
	return w
}

func countAttempts(t *testing.T, h *LoginUIHandlers, key string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, h.store.db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Table("login_attempt").Where("auth_request_id = ?", key).Count(&n).Error
	}))
	return n
}

// #1778 spec D1/D3/D4: a parked session resumes exactly once, into the same
// enrolment step a password answer gives, and never finalises on the password.
func TestParkedSetupSessionResumesOnceIntoEnrolment(t *testing.T) {
	h, r, fake := parkedTestHandlers(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cookie, err := h.ParkSetupSession(context.Background(), "user-enrol", "org-1", parkedTestPassword)
	require.NoError(t, err)
	require.Equal(t, SetupResumeCookie, cookie.Name)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.LessOrEqual(t, cookie.MaxAge, int(loginAttemptTTL.Seconds()))
	require.Len(t, cookie.Value, 43)
	require.Zero(t, countAttempts(t, h, cookie.Value), "only the handle's hash is stored")
	require.EqualValues(t, 1, countAttempts(t, h, parkedKey(cookie.Value)))
	_, err = h.store.Get(context.Background(), parkedKey(cookie.Value), stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound, "no factor step can read a parked session")

	w := doResume(t, r, cookie)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"enrollment_required"`)
	require.Contains(t, w.Header().Get("Set-Cookie"), SetupResumeCookie+"=;")
	require.False(t, fake.finalized.Load(), "a password-only session never finalises under forceMfa")
	_, err = h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.NoError(t, err, "the enrolment continues on the real auth request")
	require.Zero(t, countAttempts(t, h, parkedKey(cookie.Value)))

	require.Equal(t, http.StatusNotFound, doResume(t, r, cookie).Code, "a handle resumes once")
	for _, secret := range []string{parkedTestPassword, cookie.Value, factorSessionCreationToken} {
		require.NotContains(t, logs.String(), secret)
	}
}

func TestParkSetupSessionRefusesAnotherAccount(t *testing.T) {
	h, _, _ := parkedTestHandlers(t)
	for _, account := range [][2]string{{"user-other", "org-1"}, {"user-enrol", "org-other"}, {"", "org-1"}, {"user-enrol", ""}} {
		cookie, err := h.ParkSetupSession(context.Background(), account[0], account[1], parkedTestPassword)
		require.Error(t, err, "%v", account)
		require.Nil(t, cookie)
	}
	var n int64
	require.NoError(t, h.store.db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Table("login_attempt").Where("stage = ?", stageParked).Count(&n).Error
	}))
	require.Zero(t, n, "nothing is parked for a session of another account")
}

func TestResumeFallsBackWithoutAUsableHandle(t *testing.T) {
	h, r, fake := parkedTestHandlers(t)
	require.Equal(t, http.StatusNotFound, doResume(t, r, nil).Code, "no cookie")
	require.Equal(t, http.StatusNotFound, doResume(t, r, &http.Cookie{Name: SetupResumeCookie, Value: "unknown"}).Code)

	expired, err := h.ParkSetupSession(context.Background(), "user-enrol", "org-1", parkedTestPassword)
	require.NoError(t, err)
	require.NoError(t, h.store.db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE login_attempt SET expires_at = ? WHERE auth_request_id = ?`, time.Now().Add(-time.Second), parkedKey(expired.Value)).Error
	}))
	require.Equal(t, http.StatusNotFound, doResume(t, r, expired).Code, "an expired handle")

	const own, other = "helivanta-11111111-1111-4111-8111-111111111111", "helivanta-22222222-2222-4222-8222-222222222222"
	fake.sessionOrg.Store(own)
	foreign, err := h.ParkSetupSession(context.Background(), "user-enrol", own, parkedTestPassword)
	require.NoError(t, err)
	fake.requestOrg.Store(other)
	require.Equal(t, http.StatusNotFound, doResume(t, r, foreign).Code, "an auth request for another organisation")
	require.Zero(t, countAttempts(t, h, parkedKey(foreign.Value)), "a refused handle is still spent")
	require.Zero(t, countAttempts(t, h, loginUITestAuthRequestID), "a refused resume starts no step")
	require.False(t, fake.finalized.Load())

	// An unscoped request (the single email card) resumes for the parked account.
	fake.sessionOrg.Store("")
	fake.requestOrg.Store("")
	matching, err := h.ParkSetupSession(context.Background(), "user-enrol", "org-1", parkedTestPassword)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, doResume(t, r, matching).Code)
	require.False(t, fake.finalized.Load())
}
