package iam

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// These tests drive #856's HTTP half end to end — Password, Factor and
// PasswordChange over a real Postgres login_attempt table and a fake Zitadel
// that remembers whether the password was changed. See
// docs/superpowers/specs/2026-10-08-password-change-required-design.md.

const (
	pcTyped       = "Typed-at-sign-in-1"
	pcReplacement = "Fresh-choice-77"
)

// pcFixture is a fake Zitadel for a user whose password must change. A
// successful POST /v2/users/{id}/password clears the requirement (unless
// stayDue), exactly as Zitadel's changeRequired:false does, so the gate's
// re-read after the change is a real observation, not a constant.
type pcFixture struct {
	totp    bool
	stayDue bool

	setPasswordStatus int
	setPasswordBody   string
	finalizeStatus    int

	mu               sync.Mutex
	changeRequired   bool
	setPasswordCalls int
	patchCalls       int
	finalizeCalls    int
	lastSetPassword  map[string]any
}

func (f *pcFixture) counts() (setPassword, patch, finalize int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setPasswordCalls, f.patchCalls, f.finalizeCalls
}

func (f *pcFixture) client(t *testing.T) *loginclient.Client {
	t.Helper()
	f.changeRequired = true
	methods := `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`
	if f.totp {
		methods = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"sess-pc","sessionToken":"tok-pc"}`))
	})
	mux.HandleFunc("GET /v2/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session":{"id":"sess-pc","factors":{"user":{"id":"user-pc","organizationId":"org-pc"},` +
			`"password":{"verifiedAt":"t"},"totp":{"verifiedAt":"t"}}}}`))
	})
	mux.HandleFunc("GET /v2/users/{id}/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":` + methods + `}`))
	})
	mux.HandleFunc("GET /v2/users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		required := f.changeRequired
		f.mu.Unlock()
		human := `"passwordChanged":"2026-01-01T00:00:00Z"`
		if required {
			human += `,"passwordChangeRequired":true`
		}
		_, _ = w.Write([]byte(`{"user":{"state":"USER_STATE_ACTIVE","human":{` + human + `}}}`))
	})
	mux.HandleFunc("GET /v2/settings/password/expiry", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"settings":{}}`))
	})
	mux.HandleFunc("GET /v2/settings/password/complexity", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"settings":{"minLength":"12","requiresUppercase":true,"requiresLowercase":true,` +
			`"requiresNumber":true,"requiresSymbol":true}}`))
	})
	registerZitadelPolicyOK(mux)
	mux.HandleFunc("PATCH /v2/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.patchCalls++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"sessionToken":"tok-pc-rotated"}`))
	})
	mux.HandleFunc("POST /v2/users/{id}/password", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.setPasswordCalls++
		f.lastSetPassword = nil
		_ = json.Unmarshal(raw, &f.lastSetPassword)
		if f.setPasswordStatus != 0 {
			w.WriteHeader(f.setPasswordStatus)
			_, _ = w.Write([]byte(f.setPasswordBody))
			return
		}
		if !f.stayDue {
			f.changeRequired = false
		}
		_, _ = w.Write([]byte(`{"details":{}}`))
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.finalizeCalls++
		f.mu.Unlock()
		if f.finalizeStatus != 0 {
			w.WriteHeader(f.finalizeStatus)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"callbackUrl":"https://hms.test/api/auth/callback?code=pc&state=pc"}`))
	})
	return newZitadelTestClient(t, mux)
}

func newPCRouter(h *LoginUIHandlers) *gin.Engine {
	r := newFactorRouter(h)
	r.POST("/v1/auth/login/password-change", h.PasswordChange)
	return r
}

func doPasswordChange(t *testing.T, r *gin.Engine, authRequestID, current, next string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"auth_request_id": authRequestID, "current_password": current, "new_password": next,
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password-change", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// requireErrorCode decodes respond.Error's envelope and asserts its code.
func requireErrorCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) string {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var body struct {
		Code    string `json:"error"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	require.Equal(t, code, body.Code, w.Body.String())
	return body.Message
}

func requireAttemptStage(t *testing.T, h *LoginUIHandlers, stage attemptStage) {
	t.Helper()
	_, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stage)
	require.NoError(t, err, "expected a login_attempt row at stage %q", stage)
}

func requireNoAttempt(t *testing.T, h *LoginUIHandlers) {
	t.Helper()
	for _, stage := range []attemptStage{stageFactor, stagePasswordChange} {
		_, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stage)
		require.ErrorIs(t, err, errAttemptNotFound, "a login_attempt row survived at stage %q", stage)
	}
}

func TestPasswordAnswersPasswordChangeRequiredAndHoldsTheSession(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)

	w := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"password_change_required":{"reason":"required","policy":{"min_length":12,`+
		`"requires_uppercase":true,"requires_lowercase":true,"requires_number":true,"requires_symbol":true}}}`,
		w.Body.String())
	requireAttemptStage(t, h, stagePasswordChange)
	_, _, finalize := f.counts()
	require.Zero(t, finalize, "finalize was called for a user whose password must change: #856's bypass")
}

func TestPasswordChangeCompletesTheLogin(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"callback_url":"https://hms.test/api/auth/callback?code=pc&state=pc"}`, w.Body.String())

	setPassword, _, finalize := f.counts()
	require.Equal(t, 1, setPassword)
	require.Equal(t, 1, finalize)
	require.Equal(t, map[string]any{
		"newPassword":     map[string]any{"password": pcReplacement, "changeRequired": false},
		"currentPassword": pcTyped,
	}, f.lastSetPassword)
	requireNoAttempt(t, h)
}

// The headline TOTP case: the change is offered only AFTER the code, the
// same row carries on (advanced), and the change then completes.
func TestPasswordChangeAfterAVerifiedTOTP(t *testing.T) {
	f := &pcFixture{totp: true}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)

	w := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"factor_required":["totp"]}`, w.Body.String(), "a change was offered before the TOTP code")

	w = doFactor(t, r, loginUITestAuthRequestID, "totp", "123456")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"password_change_required"`)
	require.NotContains(t, w.Body.String(), "callback_url")
	requireAttemptStage(t, h, stagePasswordChange)

	w = doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "callback_url")
	requireNoAttempt(t, h)
}

// The security property of the stage column: a session whose TOTP is still
// unproven can never have its password changed, and answers exactly like a
// missing attempt.
func TestPasswordChangeRefusesASessionStillWaitingForItsFactor(t *testing.T) {
	f := &pcFixture{totp: true}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)
	requireAttemptStage(t, h, stageFactor)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	requireErrorCode(t, w, http.StatusBadRequest, "auth_request_invalid")
	setPassword, _, finalize := f.counts()
	require.Zero(t, setPassword, "a password was changed for a session whose TOTP was never proven")
	require.Zero(t, finalize)
	requireAttemptStage(t, h, stageFactor)
}

func TestFactorRefusesASessionWaitingForAPasswordChange(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doFactor(t, r, loginUITestAuthRequestID, "totp", "123456")
	requireErrorCode(t, w, http.StatusBadRequest, "auth_request_invalid")
	_, patch, _ := f.counts()
	require.Zero(t, patch, "Factor resumed a session written for the password-change step")
}

// Refused before Zitadel is called: Zitadel answers it with a 500 and a
// lockout strike (loginclient.ErrPasswordUnchanged).
func TestPasswordChangeRefusesAnUnchangedPassword(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcTyped)
	msg := requireErrorCode(t, w, http.StatusUnprocessableEntity, passwordUnchangedCode)
	require.Equal(t, passwordUnchangedMessage, msg)
	setPassword, _, _ := f.counts()
	require.Zero(t, setPassword, "Zitadel was called with an unchanged password")
	requireAttemptStage(t, h, stagePasswordChange)
}

// A policy refusal names the rule and keeps the attempt, so the user can try
// again — and the retry completes.
func TestPasswordChangeRejectedByPolicyCanBeRetried(t *testing.T) {
	f := &pcFixture{
		setPasswordStatus: http.StatusBadRequest,
		setPasswordBody:   `{"message":"Errors.User.PasswordComplexityPolicy.HasSymbol (DOMAIN-ZDLwA)","details":[{"id":"DOMAIN-ZDLwA"}]}`,
	}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, "NoSymbolsHere12")
	msg := requireErrorCode(t, w, http.StatusUnprocessableEntity, passwordRejectedCode)
	require.Equal(t, passwordRuleMessages[loginclient.PasswordRuleSymbol], msg)
	requireAttemptStage(t, h, stagePasswordChange)

	f.mu.Lock()
	f.setPasswordStatus = 0
	f.mu.Unlock()
	w = doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "callback_url")
}

func TestPasswordChangeWithAWrongCurrentPasswordEndsTheAttempt(t *testing.T) {
	f := &pcFixture{
		setPasswordStatus: http.StatusBadRequest,
		setPasswordBody:   `{"message":"Errors.User.Password.Invalid (COMMAND-3M0fs)","details":[{"id":"COMMAND-3M0fs"}]}`,
	}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, "not-what-was-typed", pcReplacement)
	requireErrorCode(t, w, http.StatusForbidden, refusalCodeIncomplete)
	requireNoAttempt(t, h)
	_, _, finalize := f.counts()
	require.Zero(t, finalize)
}

func TestPasswordChangeWhenZitadelIsDownKeepsTheAttempt(t *testing.T) {
	f := &pcFixture{setPasswordStatus: http.StatusServiceUnavailable, setPasswordBody: `{}`}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	requireErrorCode(t, w, http.StatusServiceUnavailable, "zitadel_unavailable")
	requireAttemptStage(t, h, stagePasswordChange)
}

// After Zitadel accepted the new password, no answer may invite a resubmit
// (it would send the old password as current and be refused): the attempt
// ends and the user is told the change took effect.
func TestPasswordChangedButCompletionFailedTellsTheUserToSignInAgain(t *testing.T) {
	f := &pcFixture{finalizeStatus: http.StatusServiceUnavailable}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	msg := requireErrorCode(t, w, http.StatusServiceUnavailable, refusalCodePasswordChangedRestart)
	require.Equal(t, refusalMessagePasswordChangedRestart, msg)
	requireNoAttempt(t, h)
}

// If the re-read still says due after Zitadel accepted the change, the login
// is refused — never finalized, and never a second prompt that could loop.
func TestPasswordChangeThatDidNotRegisterIsRefused(t *testing.T) {
	f := &pcFixture{stayDue: true}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)

	w := doPasswordChange(t, r, loginUITestAuthRequestID, pcTyped, pcReplacement)
	requireErrorCode(t, w, http.StatusForbidden, refusalCodeIncomplete)
	_, _, finalize := f.counts()
	require.Zero(t, finalize)
	requireNoAttempt(t, h)
}

// Like Factor, an unknown attempt answers attempt-expired only after the
// failure floor, so the clock says nothing about which ids exist.
func TestPasswordChangeUnknownAttemptIsExpiredAfterTheFloor(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)

	start := time.Now()
	w := doPasswordChange(t, r, "V2_never_existed", pcTyped, pcReplacement)
	elapsed := time.Since(start)
	requireErrorCode(t, w, http.StatusBadRequest, "auth_request_invalid")
	require.GreaterOrEqual(t, elapsed, MinFailedLoginDuration, "attempt-expired answered before the failure floor")
	setPassword, _, _ := f.counts()
	require.Zero(t, setPassword)
}

func TestPasswordChangeRejectsAMalformedBody(t *testing.T) {
	f := &pcFixture{}
	h := newFactorTestHandlers(t, f.client(t))
	r := newPCRouter(h)
	w := doPasswordChange(t, r, loginUITestAuthRequestID, "", pcReplacement)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPasswordChangeRefusesOverBudgetOnItsOwnBucket(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	migrateLoginAttempt(t, db)
	f := &pcFixture{}
	limiter := ratelimit.NewMemory(100)
	h := NewLoginUIHandlers(f.client(t), db, limiter, ratelimit.Rule{Rate: 1, Burst: 1, Per: time.Minute}, ratelimit.Rule{})
	r := newPCRouter(h)

	// Spend this IP's password-change budget.
	doPasswordChange(t, r, "V2_budget", pcTyped, pcTyped)
	w := doPasswordChange(t, r, "V2_budget", pcTyped, pcTyped)
	require.Equal(t, http.StatusTooManyRequests, w.Code)

	// Its own bucket: the password step's budget is untouched.
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", pcTyped).Code)
}

// #948 meets #856: a just-enrolled TOTP proves the factor; only then is the
// change asked for, on the same row — advanced out of the enrolment step so
// neither Enroll nor Factor can resume it — and the change then completes.
func TestPasswordChangeAfterANativeEnrolment(t *testing.T) {
	fake := newZitadelEnrollmentFake(t)
	fake.passwordChangeRequired.Store(true)
	h := newFactorTestHandlers(t, fake.client)
	r := newPCRouter(h)

	w := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"enrollment_required"`, "a change was offered before the factor was enrolled")

	w = doEnroll(t, r, loginUITestAuthRequestID, "totp", enrollmentGoodCode)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"password_change_required"`)
	require.False(t, fake.finalized.Load(), "finalize was called after enrolment for a user whose password must change")
	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID, stagePasswordChange)
	require.NoError(t, err)
	require.False(t, attempt.Enrolling, "an advanced row must not stay marked as enrolling")
	_, err = h.store.Get(context.Background(), loginUITestAuthRequestID, stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound)
}
