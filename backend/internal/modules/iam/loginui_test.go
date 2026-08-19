package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	// loginUITestHostedLoginBaseURL stands in for
	// config.ZitadelHostedLoginURL (wired for real in Task 5) — an
	// origin+path with no query string, matching Task 1's finding that
	// Zitadel APPENDS to whatever baseUri is configured.
	loginUITestHostedLoginBaseURL = "http://zitadel.test/ui/v2/login"
	// loginUITestAuthRequestID is a fixed, realistic-looking (Task 1's
	// spike observed the "V2_" prefix) auth request id every
	// postPassword call in this file uses — its exact value is never
	// asserted on, only that it round-trips into the handoff_url and the
	// log line.
	loginUITestAuthRequestID = "V2_test_auth_request"
)

// registerZitadelPolicyOK registers a forceMfa=false login policy
// response on mux — GET /v1/auth/login/request/:id (#867 Task 4, spec
// D5) now reads the login policy on every call, not just Password, so
// every AuthRequest-driving test's fake Zitadel needs a policy route
// too, even ones that predate that read and only ever cared about the
// auth-request shape. Mirrors zitadelHappyPath's own policy fixture
// (forceMfa omitted, matching what the real dev Zitadel actually sends).
func registerZitadelPolicyOK(mux *http.ServeMux) {
	mux.HandleFunc("GET /management/v1/policies/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s"}}`))
	})
}

// newZitadelTestClient starts an httptest.Server serving mux, points a
// real loginclient.Client at it, and registers the server's teardown —
// the same shape every zitadel*(t) helper below builds around a
// different mux.
func newZitadelTestClient(t *testing.T, mux *http.ServeMux) *loginclient.Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return loginclient.New(server.URL, "test-login-client-pat", server.Client())
}

// zitadelWrongPassword answers POST /v2/sessions with the spike's exact
// wrong-password shape (HTTP 400, COMMAND-3M0fs) and sleeps first to
// imitate the real cost of Zitadel actually computing the password hash
// (spike §3: 0.72-0.78s observed) — TestPasswordFailureTimingIsEqualised
// relies on this path being naturally slow, and the unknown-user path
// below being naturally fast, to prove the FLOOR closes the gap between
// them rather than merely happening to land above both by accident.
func zitadelWrongPassword(t *testing.T) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(750 * time.Millisecond)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid credentials","details":[{"id":"COMMAND-3M0fs","failedAttempts":1}]}`))
	})
	return newZitadelTestClient(t, mux)
}

// zitadelUnknownUser answers POST /v2/sessions with the spike's exact
// unknown-user shape (HTTP 404, QUERY-Dfbg2), instantly — no hash is
// ever computed for a loginName Zitadel does not recognize.
func zitadelUnknownUser(t *testing.T) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"user not found","details":[{"id":"QUERY-Dfbg2"}]}`))
	})
	return newZitadelTestClient(t, mux)
}

// zitadelHappyPath answers a session, a forceMfa=false policy, and a
// finalize call returning a callback URL — the full OutcomeComplete
// path. The policy fixture omits forceMfa entirely rather than sending
// an explicit false, matching what the real dev Zitadel actually sends
// (loginclient.LoginPolicy's doc comment) — passwordCheckLifetime is the
// anchor that fixture relies on to tell this apart from an unrecognized
// body.
func zitadelHappyPath(t *testing.T) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"sess-1","sessionToken":"tok-1"}`))
	})
	mux.HandleFunc("GET /management/v1/policies/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s"}}`))
	})
	// GET /v2/sessions/{id} and GET /v2/users/{id}/authentication_methods
	// back CompleteIfSufficient's per-user enrolled-factor check (#854
	// Task 8) — a PASSWORD-ONLY user here, matching this fixture's name
	// ("happy path": nothing Helivanta cannot handle), so the login still
	// completes.
	mux.HandleFunc("GET /v2/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session":{"id":"sess-1","factors":{"user":{"id":"user-1"}}}}`))
	})
	mux.HandleFunc("GET /v2/users/{id}/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"callbackUrl":"https://hms.test/api/auth/callback?code=abc&state=xyz"}`))
	})
	return newZitadelTestClient(t, mux)
}

// zitadelForceMFA answers a session and a forceMfa=true policy. Its
// finalize route deliberately fails the test outright if ever hit: under
// forceMfa, CompleteIfSufficient must hand off BEFORE calling finalize
// (loginclient's sufficiency.go), so this route being reached at all is
// itself the MFA bypass spec D4 exists to prevent — asserting only on
// the handler's response body would let a regression that also broke
// the mock go unnoticed.
func zitadelForceMFA(t *testing.T) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"sess-2","sessionToken":"tok-2"}`))
	})
	mux.HandleFunc("GET /management/v1/policies/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`))
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("finalize called (%s %s) under a forceMfa policy: this is an MFA bypass", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	return newZitadelTestClient(t, mux)
}

// zitadelDown is a server closed before any request reaches it, so every
// call this client makes fails at the transport level (connection
// refused) — loginclient.do maps that to ErrUnavailable, never one of
// the credential sentinels.
func zitadelDown(t *testing.T) *loginclient.Client {
	t.Helper()
	server := httptest.NewServer(http.NewServeMux())
	server.Close()
	return loginclient.New(server.URL, "test-login-client-pat", server.Client())
}

// postPassword builds a Gin context around a fresh LoginUIHandlers wired
// to client, and POSTs {auth_request_id, login_name, password} to
// Password. loginUITestAuthRequestID is used for every call — this
// file's tests only vary loginName/password/the Zitadel fixture, never
// the auth request id itself.
func postPassword(t *testing.T, client *loginclient.Client, loginName, password string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)

	body := `{"auth_request_id":"` + loginUITestAuthRequestID +
		`","login_name":"` + loginName + `","password":"` + password + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// Spec D5: a wrong password and an unknown user must be indistinguishable
// by status, by body, AND by timing. Mapping the status alone leaves the
// ~55x timing oracle the spike measured (0.72s vs 0.013s) fully intact.
func TestPasswordFailuresAreIdenticalForWrongPasswordAndUnknownUser(t *testing.T) {
	wrong := postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")
	unknown := postPassword(t, zitadelUnknownUser(t), "nobody@helivanta.dev", "nope")

	if wrong.Code != unknown.Code {
		t.Errorf("status differs: wrong=%d unknown=%d", wrong.Code, unknown.Code)
	}
	if wrong.Body.String() != unknown.Body.String() {
		t.Errorf("body differs:\n wrong=%s\n unknown=%s", wrong.Body.String(), unknown.Body.String())
	}
	if strings.Contains(wrong.Body.String(), "failedAttempts") {
		t.Error("response leaks failedAttempts to the browser")
	}
}

// timingToleranceForEqualisedFailures bounds how far apart the two
// failure paths' measured latencies may land in this test. It is
// deliberately generous (test-scheduling jitter on a shared CI runner is
// real) while still being far tighter than the ~55x gap spec D5 exists
// to close — see TestPasswordFailureTimingIsEqualised's doc comment for
// what it is actually there to catch.
const timingToleranceForEqualisedFailures = 150 * time.Millisecond

// TestPasswordFailureTimingIsEqualised is spec D5's timing proof, and it
// must check BOTH sides plus the gap between them — not just that the
// FAST path (unknown user) got slowed down to the floor.
//
// An earlier version of this test asserted only unknownElapsed >=
// MinFailedLoginDuration. That is insufficient: a regression that
// applied the floor to ONLY ONE of the two ErrBadCredentials/
// ErrUserNotFound branches in Password (e.g. an errors.Is check that
// silently stopped matching one of them) would leave wrong-password
// answering at its own natural ~830ms while unknown-user sat pinned at
// the floor — a real, if smaller, enumeration oracle — and the
// single-sided assertion could not see it: it only ever looks at the
// side that was already slow by construction of the FAKE SERVER, not by
// construction of the HANDLER under test. See this task's fix-round
// report for the mutation that demonstrates this concretely (the floor
// applied to only the unknown-user branch): the single-sided assertion
// passes it, this one does not.
func TestPasswordFailureTimingIsEqualised(t *testing.T) {
	// The fake unknown-user server answers instantly; the fake
	// wrong-password server sleeps to imitate the real hash cost
	// (809-845ms measured against the real dev Zitadel, see
	// MinFailedLoginDuration's doc comment) — both must still clear the
	// floor, and land close to each other, regardless of how differently
	// they started.
	wrongStart := time.Now()
	postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")
	wrongElapsed := time.Since(wrongStart)

	unknownStart := time.Now()
	postPassword(t, zitadelUnknownUser(t), "nobody@helivanta.dev", "nope")
	unknownElapsed := time.Since(unknownStart)

	if wrongElapsed < MinFailedLoginDuration {
		t.Errorf("wrong-password path returned in %v, faster than the %v floor: timing oracle intact",
			wrongElapsed, MinFailedLoginDuration)
	}
	if unknownElapsed < MinFailedLoginDuration {
		t.Errorf("unknown-user path returned in %v, faster than the %v floor: timing oracle intact",
			unknownElapsed, MinFailedLoginDuration)
	}

	gap := wrongElapsed - unknownElapsed
	if gap < 0 {
		gap = -gap
	}
	if gap > timingToleranceForEqualisedFailures {
		t.Errorf("wrong-password (%v) and unknown-user (%v) differ by %v, over the %v tolerance: "+
			"the floor is not applying equally to both failure paths",
			wrongElapsed, unknownElapsed, gap, timingToleranceForEqualisedFailures)
	}
}

func TestPasswordSuccessReturnsCallbackURL(t *testing.T) {
	rec := postPassword(t, zitadelHappyPath(t), "test@helivanta.dev", "HmsDev123!")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/api/auth/callback") {
		t.Errorf("body = %s, want a callback_url", rec.Body.String())
	}
}

func TestPasswordUnderForceMFAReturnsHandoffNotSession(t *testing.T) {
	rec := postPassword(t, zitadelForceMFA(t), "test@helivanta.dev", "HmsDev123!")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "handoff_url") {
		t.Errorf("body = %s, want a handoff_url", body)
	}
	if strings.Contains(body, "callback_url") {
		t.Error("a forceMfa login returned a callback_url: MFA bypass")
	}
}

func TestPasswordWhenZitadelIsDownReturns503NotBadCredentials(t *testing.T) {
	rec := postPassword(t, zitadelDown(t), "test@helivanta.dev", "HmsDev123!")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — never a credentials error when the IdP is down", rec.Code)
	}
}

// --- coverage beyond the brief's pinned five --------------------------

// TestPasswordHandoffURLCarriesTheAuthRequestID proves the handoff_url a
// forceMfa login answers with actually resumes the SAME auth request
// Zitadel's own /oauth/v2/authorize redirect started — not a bare
// redirect to the hosted login's home page, which would drop the OIDC
// client/redirect/scope context entirely.
func TestPasswordHandoffURLCarriesTheAuthRequestID(t *testing.T) {
	rec := postPassword(t, zitadelForceMFA(t), "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "authRequest="+loginUITestAuthRequestID)
	require.Contains(t, rec.Body.String(), loginUITestHostedLoginBaseURL)
}

// TestPasswordRejectsMalformedBody mirrors login_test.go's
// TestLogin_RejectsMalformedRequestBody: a body that does not even parse
// must never reach Zitadel.
func TestPasswordRejectsMalformedBody(t *testing.T) {
	h := NewLoginUIHandlers(zitadelHappyPath(t), loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPasswordRefusesOverBudget proves the Password endpoint's own
// budget actually refuses, keyed independently of LoginHandlers.Login's
// "login:" bucket (login_test.go's TestLogin_RefusesOverBudget is this
// test's sibling for the other endpoint).
func TestPasswordRefusesOverBudget(t *testing.T) {
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	client := zitadelHappyPath(t)

	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, limiter, rule, ratelimit.Rule{})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)

	post := func() *httptest.ResponseRecorder {
		body := `{"auth_request_id":"` + loginUITestAuthRequestID + `","login_name":"test@helivanta.dev","password":"HmsDev123!"}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:12345"
		r.ServeHTTP(w, req)
		return w
	}

	w1 := post()
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	w2 := post()
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
	require.NotEmpty(t, w2.Header().Get("Retry-After"))
}

// TestAuthRequestRefusesOverBudget and TestHandoffRefusesOverBudget are
// spec D2's "all three sit behind the existing unauthenticated limiter"
// for the two routes that had NO budget at all before this branch's
// final review round. Both are mounted on the raw gin engine
// (bootstrap.MountUnauthenticated), outside V1Chain, so
// ratelimit.Middleware never runs for them — an unlimited
// GET /v1/auth/login/request/:id in particular makes an unauthenticated
// Zitadel round trip per call on the INSTANCE-LEVEL login-client PAT,
// spending a budget shared with every other Tesserix product.
//
// Both were proven to fail against the pre-fix handlers (200 on the
// second call, where a 429 is required); see the branch's final-fixes
// report for the failure output.
func TestAuthRequestRefusesOverBudget(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc","clientId":"cid-1","redirectUri":"https://hms.test/cb","scope":["openid"]}}`))
	})
	registerZitadelPolicyOK(mux)
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, limiter, rule, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/V2_abc", nil)
		req.RemoteAddr = "203.0.113.9:12345"
		r.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusOK, get().Code)

	w2 := get()
	require.Equal(t, http.StatusTooManyRequests, w2.Code,
		"GET /v1/auth/login/request/:id is unauthenticated and makes a Zitadel round trip on the "+
			"instance-level login-client PAT; it must be budgeted (spec D2)")
	require.NotEmpty(t, w2.Header().Get("Retry-After"))
}

func TestHandoffRefusesOverBudget(t *testing.T) {
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(nil, loginUITestHostedLoginBaseURL, nil, limiter, rule, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/handoff/:id", h.Handoff)

	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/handoff/"+loginUITestAuthRequestID, nil)
		req.RemoteAddr = "203.0.113.11:12345"
		r.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusOK, post().Code)

	w2 := post()
	require.Equal(t, http.StatusTooManyRequests, w2.Code,
		"POST /v1/auth/login/handoff/:id is reachable by anyone with no principal; it must be budgeted (spec D2)")
	require.NotEmpty(t, w2.Header().Get("Retry-After"))
}

// TestLoginUIRoutesDoNotShareEachOthersBudget proves the three routes
// key into DIFFERENT buckets off the one shared limiter: exhausting
// Password's budget from an IP must not refuse that same IP's
// AuthRequest or Handoff call. A single shared key would let a
// credential-guessing flood lock a legitimate clinician out of even
// LOADING the login form.
func TestLoginUIRoutesDoNotShareEachOthersBudget(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc"}}`))
	})
	registerZitadelPolicyOK(mux)
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"user not found","details":[{"id":"QUERY-Dfbg2"}]}`))
	})
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, limiter, rule, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)
	r.POST("/v1/auth/login/handoff/:id", h.Handoff)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.13:12345"
		r.ServeHTTP(w, req)
		return w
	}

	// Drain Password's bucket for this IP (burst 1, so the second call
	// is refused). The credential is deliberately wrong — this test is
	// about budgets, not outcomes.
	pwBody := `{"auth_request_id":"` + loginUITestAuthRequestID + `","login_name":"nobody@helivanta.dev","password":"wrong"}`
	require.Equal(t, http.StatusUnauthorized, do(http.MethodPost, "/v1/auth/login/password", pwBody).Code)
	require.Equal(t, http.StatusTooManyRequests, do(http.MethodPost, "/v1/auth/login/password", pwBody).Code,
		"precondition: Password's own bucket must be drained for this IP")

	require.Equal(t, http.StatusOK, do(http.MethodGet, "/v1/auth/login/request/V2_abc", "").Code,
		"a drained password budget must not refuse the auth-request read from the same IP")
	require.Equal(t, http.StatusOK, do(http.MethodPost, "/v1/auth/login/handoff/"+loginUITestAuthRequestID, "").Code,
		"a drained password budget must not refuse the handoff call from the same IP")
}

// TestAuthRequestAndHandoffAdmitWhenLimiterUnavailable extends
// TestPasswordAdmitsWhenLimiterUnavailable to the other two routes: a
// nil limiter must fail OPEN on all three, never take sign-in down.
func TestAuthRequestAndHandoffAdmitWhenLimiterUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc"}}`))
	})
	registerZitadelPolicyOK(mux)
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)
	r.POST("/v1/auth/login/handoff/:id", h.Handoff)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/V2_abc", nil))
		require.Equal(t, http.StatusOK, w.Code, "auth request attempt %d: nil limiter must fail open", i+1)

		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/auth/login/handoff/"+loginUITestAuthRequestID, nil))
		require.Equal(t, http.StatusOK, w.Code, "handoff attempt %d: nil limiter must fail open", i+1)
	}
}

// TestPasswordAdmitsWhenLimiterUnavailable is this endpoint's version of
// login_test.go's TestLoginAdmitsWhenLimiterUnavailable: a nil limiter
// must fail OPEN, not block sign-in.
func TestPasswordAdmitsWhenLimiterUnavailable(t *testing.T) {
	for i := 0; i < 5; i++ {
		rec := postPassword(t, zitadelHappyPath(t), "test@helivanta.dev", "HmsDev123!")
		require.Equal(t, http.StatusOK, rec.Code, "attempt %d: nil limiter must fail open, not deny", i+1)
	}
}

// TestAuthRequest_ReturnsAuthRequestFields proves GET
// /v1/auth/login/request/:id passes through what the login form needs
// (client id, redirect uri, scope) rather than Zitadel's raw wire shape.
func TestAuthRequest_ReturnsAuthRequestFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc","clientId":"cid-1","redirectUri":"https://hms.test/cb","scope":["openid","profile"]}}`))
	})
	registerZitadelPolicyOK(mux)
	client := newZitadelTestClient(t, mux)
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/V2_abc", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"client_id":"cid-1"`)
	require.Contains(t, w.Body.String(), `"redirect_uri":"https://hms.test/cb"`)
}

// TestAuthRequest_InvalidIDReturns400 proves an unrecognized/expired auth
// request id gets a distinct, non-credential refusal.
func TestAuthRequest_InvalidIDReturns400(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found","details":[{"id":"QUERY-x"}]}`))
	})
	client := newZitadelTestClient(t, mux)
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/stale", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandoff_ReturnsURLCarryingTheID proves POST
// /v1/auth/login/handoff/:id builds a handoff_url without needing any
// Zitadel call of its own.
func TestHandoff_ReturnsURLCarryingTheID(t *testing.T) {
	h := NewLoginUIHandlers(nil, loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/handoff/:id", h.Handoff)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/auth/login/handoff/"+loginUITestAuthRequestID, nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "authRequest="+loginUITestAuthRequestID)
	require.Contains(t, w.Body.String(), loginUITestHostedLoginBaseURL)
}

// --- POST /v1/auth/login/factor (#867 Task 4) ------------------------

// newFactorTestHandlers boots a real Postgres with only the
// login_attempt table migrated (mirroring loginattempt_test.go's
// newTestLoginAttemptStore) and wires it into a fresh LoginUIHandlers —
// every test in this section needs a REAL store, not a nil one, because
// Password and Factor must see the SAME row: Password writes it,
// Factor reads, bumps and deletes it, and a nil store (fine for every
// OTHER test in this file, which never reaches OutcomeFactorRequired)
// would panic the moment either method touched it.
func newFactorTestHandlers(t *testing.T, client *loginclient.Client) *LoginUIHandlers {
	t.Helper()
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	migs := New(nil).Migrations()
	var loginAttemptMig *tenantdb.Migration
	for i := range migs {
		if migs[i].ID == "0004_iam" {
			loginAttemptMig = &migs[i]
		}
	}
	require.NotNil(t, loginAttemptMig, "precondition: 0004_iam is the login_attempt migration")
	require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{*loginAttemptMig}))

	return NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, db, nil, ratelimit.Rule{}, ratelimit.Rule{})
}

// newFactorRouter mounts Password and Factor on a fresh gin.Engine — the
// two routes every test below needs together, since Factor only makes
// sense after a Password call has left a row for it to resume.
func newFactorRouter(h *LoginUIHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)
	r.POST("/v1/auth/login/factor", h.Factor)
	return r
}

func doPassword(t *testing.T, r *gin.Engine, authRequestID, loginName, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"auth_request_id":"` + authRequestID + `","login_name":"` + loginName + `","password":"` + password + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func doFactor(t *testing.T, r *gin.Engine, authRequestID, factor, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"auth_request_id":"` + authRequestID + `","factor":"` + factor + `","code":"` + code + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/factor", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// factorSessionCreationToken/factorRotatedToken are the two tokens every
// zitadelFactorRequired-based fixture below uses: the token session
// creation (POST /v2/sessions) hands out, and the DIFFERENT token a
// successful PATCH (VerifyTOTP) rotates to. Named constants rather than
// inline literals so the finalize gate below (which must accept
// EXACTLY factorRotatedToken, never factorSessionCreationToken) and
// every test's assertions on h.store's persisted token all read the
// same two strings — a copy-paste slip between "tok-mfa" and
// "tok-mfa-rotated" typed independently in three or four places would
// silently defeat the whole point of this fixture.
const (
	factorSessionCreationToken = "tok-mfa"
	factorRotatedToken         = "tok-mfa-rotated"
)

// zitadelFactorRequired answers a password session for a user enrolled
// in BOTH password and TOTP (spec D1's headline case) — Password's
// CompleteIfSufficient therefore answers OutcomeFactorRequired rather
// than completing or handing off. patchSessions serves PATCH
// /v2/sessions/{id} (loginclient.VerifyTOTP) — callers below supply
// either a success or a wrong-code response, the only thing that
// differs between "good code" and "wrong code" test fixtures.
// finalizeOutcome serves POST /v2/oidc/auth_requests/{id} — see
// finalizeRequireRotatedToken below for the ONE variant used by every
// caller in this file: it does not just answer a canned callback URL,
// it INSPECTS the request body and refuses unless the session token
// finalize was actually called with is factorRotatedToken.
//
// # Why finalize must inspect the token, not just answer unconditionally
//
// #867 fix round 1, Finding 1: an EARLIER version of this fixture's
// finalize and GET /v2/sessions/{id} handlers ignored *http.Request
// entirely — the same shape TestFactorGoodCodeReturnsCallbackAndDeletesRow
// still calls "the D3 regression test" in its own doc comment, except
// with a fixture that could not have proven any such thing: a reviewer
// mutated Factor to finalize with the STALE, pre-verification token
// (loginAttemptStore's own, never updated) and the test PASSED, because
// nothing in the fixture ever looked at which token arrived. A test that
// cannot fail against the exact defect it claims to guard is not a
// guard. finalizeRequireRotatedToken exists so that mutation now fails
// this test — see TestFactorGoodCodeReturnsCallbackAndDeletesRow's own
// doc comment for the actual FAIL output this task's report records.
func zitadelFactorRequired(t *testing.T, patchSessions, finalizeOutcome http.HandlerFunc) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"sess-mfa","sessionToken":"` + factorSessionCreationToken + `"}`))
	})
	// Read by classifyEnrolledMethods' sessionUserID (both during
	// Password's CompleteIfSufficient and, later, Factor's
	// CompleteAfterFactor) AND by SessionFactors after a successful
	// VerifyTOTP — the SAME body serves all three reads, carrying both
	// factors.user.id and a pre-populated factors.totp.verifiedAt: this
	// is a stateless fixture, not a real Zitadel session, so it does not
	// need to track whether verification "really" happened yet by the
	// time of any individual read. Not token-gated: unlike finalize and
	// VerifyTOTP, this GET is authenticated with the login-client PAT
	// (the Authorization header this test's http.Client always sends),
	// never the Zitadel SESSION token, so there is no session-token
	// argument for this fixture to check here.
	mux.HandleFunc("GET /v2/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session":{"id":"sess-mfa","factors":{"user":{"id":"user-mfa"},"totp":{"verifiedAt":"2026-01-01T00:00:00Z"}}}}`))
	})
	mux.HandleFunc("GET /v2/users/{id}/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]}`))
	})
	mux.HandleFunc("PATCH /v2/sessions/{id}", patchSessions)
	mux.HandleFunc("POST /v2/oidc/auth_requests/{id}", finalizeOutcome)
	return newZitadelTestClient(t, mux)
}

// finalizeRequireRotatedToken decodes finalize's request body (the SAME
// shape loginclient.Client.finalize sends: {"session":{"sessionId",
// "sessionToken"}}) and refuses with a 400 UNLESS sessionToken is
// EXACTLY factorRotatedToken — answering the canned callback URL only
// when the caller actually threaded the rotated token through. This is
// what makes TestFactorGoodCodeReturnsCallbackAndDeletesRow able to fail
// against the D3 stale-token defect (#867 fix round 1, Finding 1): a
// Factor implementation that finalizes with attempt.SessionToken (the
// ORIGINAL, pre-verification token) sends factorSessionCreationToken
// here instead, which this handler now REJECTS.
func finalizeRequireRotatedToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session struct {
			SessionID    string `json:"sessionId"`
			SessionToken string `json:"sessionToken"`
		} `json:"session"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Session.SessionToken != factorRotatedToken {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"session token is stale (not the rotated token)","details":[{"id":"COMMAND-stale-token"}]}`))
		return
	}
	_, _ = w.Write([]byte(`{"callbackUrl":"https://hms.test/api/auth/callback?code=mfa&state=mfa"}`))
}

// finalizeUnavailable always answers 503 regardless of body — used by
// zitadelFactorGoodCodeFinalizeUnavailable so a test can observe the
// STORE's persisted token directly (Finding 1's second, independent
// check) without finalize ever succeeding or deleting the row.
func finalizeUnavailable(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"message":"internal","details":[{"id":"UNAVAIL-1"}]}`))
}

// zitadelFactorGoodCode's PATCH answers success with factorRotatedToken
// — deliberately DIFFERENT from factorSessionCreationToken, the token
// session creation handed out — and its finalize
// (finalizeRequireRotatedToken) REJECTS any other token. This proves
// spec D3 end to end: TestFactorGoodCodeReturnsCallbackAndDeletesRow
// fails if Factor persists or forwards the WRONG token to
// CompleteAfterFactor, because finalize would then be called with
// factorSessionCreationToken and this fixture's finalize refuses it —
// see zitadelFactorRequired's own doc comment for the mutation this
// closes.
func zitadelFactorGoodCode(t *testing.T) *loginclient.Client {
	t.Helper()
	return zitadelFactorRequired(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionToken":"` + factorRotatedToken + `"}`))
	}, finalizeRequireRotatedToken)
}

// zitadelFactorGoodCodeFinalizeUnavailable is zitadelFactorGoodCode's
// PATCH (a correct code, rotating to factorRotatedToken) paired with a
// finalize that ALWAYS answers 503 — independent of which token it
// receives. This lets a test observe h.store directly after Factor's
// call: UpdateToken must have already persisted the rotated token
// BEFORE CompleteAfterFactor/finalize ever runs (loginui.go's own doc
// comment on Factor: "Persisted BEFORE CompleteAfterFactor is called"),
// and finalize failing for an UNRELATED reason (Zitadel unavailable)
// must not roll that back or prevent it from having happened. This is
// Finding 1's second, independent proof — it does not rely on
// finalizeRequireRotatedToken's gate at all, so it catches the D3 defect
// even in a hypothetical world where that gate had a bug of its own.
func zitadelFactorGoodCodeFinalizeUnavailable(t *testing.T) *loginclient.Client {
	t.Helper()
	return zitadelFactorRequired(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionToken":"` + factorRotatedToken + `"}`))
	}, finalizeUnavailable)
}

// zitadelFactorBadCode's PATCH answers the spike's wrong-credential
// shape (HTTP 400) every time — loginclient.VerifyTOTP maps this to
// ErrBadCredentials, the same sentinel a wrong password produces.
// finalize is never reached on this path (VerifyTOTP always fails
// first), so its outcome does not matter; finalizeRequireRotatedToken is
// reused anyway rather than inventing a third finalize fixture.
func zitadelFactorBadCode(t *testing.T) *loginclient.Client {
	t.Helper()
	return zitadelFactorRequired(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid credentials","details":[{"id":"COMMAND-totp"}]}`))
	}, finalizeRequireRotatedToken)
}

// TestPasswordFactorRequiredRespondsWithFactorsOnly pins the FIRST half
// of spec D8's three-way outcome: a password check against a
// TOTP-enrolled user answers 200 with factor_required, and — this is the
// part a naive implementation gets wrong — carries NEITHER callback_url
// NOR handoff_url. Spec D8: "the page must treat factor_required as
// neither success nor failure", and a response that also happened to
// carry one of its siblings' fields would make that impossible to tell
// apart structurally.
func TestPasswordFactorRequiredRespondsWithFactorsOnly(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	r := newFactorRouter(h)

	w := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"factor_required":["totp"]`)
	require.NotContains(t, w.Body.String(), "callback_url", "factor_required must not also carry a callback_url")
	require.NotContains(t, w.Body.String(), "handoff_url", "factor_required must not also carry a handoff_url")
}

// TestPasswordFactorRequiredWritesLoginAttemptRow pins spec D2/D3: the
// Zitadel session (id AND token) Password just created must be stashed
// server-side, keyed by the auth_request_id the browser has, so Factor
// can resume it. Read directly off h.store (this file is package iam,
// not iam_test, precisely so tests like this one can reach in) rather
// than inferring the row exists indirectly — this is the fact the row
// EXISTS AND HOLDS THE RIGHT SESSION, not just that Password answered
// 200.
func TestPasswordFactorRequiredWritesLoginAttemptRow(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	r := newFactorRouter(h)

	w := doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID)
	require.NoError(t, err, "Password must have written a login_attempt row for this auth_request_id")
	require.Equal(t, "sess-mfa", attempt.SessionID)
	require.Equal(t, "tok-mfa", attempt.SessionToken)
}

// TestFactorGoodCodeReturnsCallbackAndDeletesRow is the D3 regression
// test the spec explicitly calls for: password → factor → finalize
// succeeds. zitadelFactorGoodCode's PATCH deliberately rotates to
// factorRotatedToken (different from factorSessionCreationToken, the
// token session creation returned), and its finalize
// (finalizeRequireRotatedToken) REJECTS any request whose sessionToken
// is not EXACTLY factorRotatedToken.
//
// #867 fix round 1, Finding 1: an earlier version of this test's
// fixture did NOT actually enforce that — both GET /v2/sessions/{id}
// and finalize ignored the request body entirely — so this test PASSED
// even when a reviewer mutated Factor to finalize with the stale,
// pre-verification token. That is fixed now: this test is PROVEN able
// to catch that exact mutation (see this task's report for the FAIL
// output, produced by temporarily reverting Factor to finalize with
// `loginclient.Session{attempt.SessionID, attempt.SessionToken}` instead
// of `verified`, then reverting).
//
// The row must also be gone afterwards: a finalized attempt has nothing
// left to resume. TestFactorPersistsRotatedTokenBeforeFinalizeAttempt
// below is this test's independent second proof — it observes h.store
// directly rather than relying on finalizeRequireRotatedToken's gate.
func TestFactorGoodCodeReturnsCallbackAndDeletesRow(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	r := newFactorRouter(h)

	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)

	w := doFactor(t, r, loginUITestAuthRequestID, "totp", "123456")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "/api/auth/callback")

	_, err := h.store.Get(context.Background(), loginUITestAuthRequestID)
	require.ErrorIs(t, err, errAttemptNotFound, "a finalized attempt's row must be deleted")
}

// TestFactorPersistsRotatedTokenBeforeFinalizeAttempt is Finding 1's
// SECOND, independent proof of spec D3 — deliberately NOT relying on
// finalizeRequireRotatedToken's gate at all, so it still catches the D3
// stale-token defect even in a hypothetical world where that gate had a
// bug of its own. zitadelFactorGoodCodeFinalizeUnavailable's PATCH
// rotates the token exactly like zitadelFactorGoodCode's does, but its
// finalize always answers 503 regardless of which token it receives —
// so Factor takes the "complete_after_factor" error path
// (h.respondLoginClientError) rather than deleting the row, and this
// test can read h.store directly to assert the token UpdateToken
// actually persisted is factorRotatedToken, never
// factorSessionCreationToken. loginui.go's own doc comment on Factor
// states the ordering this pins: "Persisted BEFORE CompleteAfterFactor
// is called" — this test is what proves that claim rather than merely
// asserting it in prose.
func TestFactorPersistsRotatedTokenBeforeFinalizeAttempt(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCodeFinalizeUnavailable(t))
	r := newFactorRouter(h)

	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)

	w := doFactor(t, r, loginUITestAuthRequestID, "totp", "123456")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	attempt, err := h.store.Get(context.Background(), loginUITestAuthRequestID)
	require.NoError(t, err, "finalize failing for an unrelated reason must not delete the row")
	require.Equal(t, factorRotatedToken, attempt.SessionToken,
		"the store must hold the ROTATED token, not the stale session-creation one, even when finalize itself fails")
}

// TestFactorWrongCodeMatchesWrongPasswordByteForByte is spec D5 extended
// to the factor step: a wrong TOTP code must be BYTE-IDENTICAL to a
// wrong password, not merely "similarly worded" — asserting on the exact
// bytes, rather than eyeballing the strings, is what
// TestFactorWrongCodeProofCanFail (below) proves actually holds this
// test to something.
func TestFactorWrongCodeMatchesWrongPasswordByteForByte(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorBadCode(t))
	r := newFactorRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)

	wrongCode := doFactor(t, r, loginUITestAuthRequestID, "totp", "000000")
	wrongPassword := postPassword(t, zitadelWrongPassword(t), "test@helivanta.dev", "nope")

	require.Equal(t, wrongPassword.Code, wrongCode.Code, "status must match the shared refusal")
	require.Equal(t, wrongPassword.Body.String(), wrongCode.Body.String(),
		"a wrong TOTP code and a wrong password must answer byte-identically (spec D5)")
}

// TestFactorFiveWrongCodesExhausts pins spec D6 end to end: four wrong
// codes each answer the shared refusal, the FIFTH answers
// attempt-expired instead (distinct status/body from the refusal) — and
// the row is actually gone, not merely reported missing (mirrors
// loginattempt_test.go's TestLoginAttempt_ExpiredRowIsActuallyDeleted
// reasoning: BumpAndGet's own exhaustion delete could silently roll
// back, and a Get-based check alone cannot tell that apart from a
// genuine delete).
func TestFactorFiveWrongCodesExhausts(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorBadCode(t))
	r := newFactorRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)

	for i := 1; i <= 4; i++ {
		w := doFactor(t, r, loginUITestAuthRequestID, "totp", "000000")
		require.Equal(t, http.StatusUnauthorized, w.Code, "wrong code %d must answer the shared refusal", i)
		require.Contains(t, w.Body.String(), passwordFailureMessage)
	}

	fifth := doFactor(t, r, loginUITestAuthRequestID, "totp", "000000")
	require.Equal(t, http.StatusBadRequest, fifth.Code,
		"the fifth wrong code must answer attempt-expired, distinct from the refusal")
	require.Contains(t, fifth.Body.String(), authRequestExpiredMessage)
	require.NotContains(t, fifth.Body.String(), passwordFailureMessage)

	var n int64
	require.NoError(t, h.store.db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Table("login_attempt").Where("auth_request_id = ?", loginUITestAuthRequestID).Count(&n).Error
	}))
	require.Equal(t, int64(0), n, "the row, and the Zitadel session token it carries, must actually be deleted")
}

// TestFactorUnknownAuthRequestIDReturnsAttemptExpiredNotRefusal is the
// enumeration-resistance half of spec D5 for this endpoint: an
// auth_request_id this server never saw (or already forgot) must answer
// EXACTLY the same attempt-expired shape as an exhausted one, never the
// credential refusal — a refusal would tell a prober "this id existed
// and had a pending attempt", which an id it invented never did.
func TestFactorUnknownAuthRequestIDReturnsAttemptExpiredNotRefusal(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	r := newFactorRouter(h)

	w := doFactor(t, r, "V2_never_had_a_password_step", "totp", "000000")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), authRequestExpiredMessage)
	require.NotContains(t, w.Body.String(), passwordFailureMessage,
		"an unknown auth_request_id must never answer the credential refusal")
}

// TestFactorAttemptExpiredTimingIsEqualised is Finding I4's proof (#867
// fix round 2): the SAME shape as TestPasswordFailureTimingIsEqualised,
// aimed at respondAttemptExpired instead of respondEqualisedFailure. An
// EARLIER version of this file added waitUntilFailureFloor to
// respondAttemptExpired (Finding/Minor 3, fix round 1) purely on the
// strength of a doc-comment argument — no test anywhere read elapsed
// time on ANY respondAttemptExpired path, so deleting that call passed
// the entire suite. This test closes that gap the same way
// TestPasswordFailureTimingIsEqualised closes it for
// respondEqualisedFailure: it measures BOTH sides and the GAP between
// them, not just that one side cleared the floor — see that test's own
// doc comment for why a single-sided assertion is insufficient.
//
// The two paths compared:
//   - unknown: store.Get misses immediately — no Zitadel round trip at
//     all (zitadelFactorGoodCode's client is passed but never reached).
//   - exhausted: reached only after four prior wrong codes, then a REAL
//     VerifyTOTP round trip plus a real BumpAndGet on the fifth — only
//     the FIFTH call's own latency is measured (exhaustedStart is taken
//     immediately before it), so the first four calls' own floored
//     waits do not inflate this test's measurement of the fifth.
func TestFactorAttemptExpiredTimingIsEqualised(t *testing.T) {
	hUnknown := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	rUnknown := newFactorRouter(hUnknown)
	unknownStart := time.Now()
	doFactor(t, rUnknown, "V2_never_existed_timing_test", "totp", "000000")
	unknownElapsed := time.Since(unknownStart)

	hExhausted := newFactorTestHandlers(t, zitadelFactorBadCode(t))
	rExhausted := newFactorRouter(hExhausted)
	require.Equal(t, http.StatusOK,
		doPassword(t, rExhausted, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)
	for i := 1; i <= 4; i++ {
		w := doFactor(t, rExhausted, loginUITestAuthRequestID, "totp", "000000")
		require.Equal(t, http.StatusUnauthorized, w.Code, "precondition: wrong code %d must be the shared refusal", i)
	}

	exhaustedStart := time.Now()
	fifth := doFactor(t, rExhausted, loginUITestAuthRequestID, "totp", "000000")
	exhaustedElapsed := time.Since(exhaustedStart)
	require.Equal(t, http.StatusBadRequest, fifth.Code, "precondition: the fifth wrong code must exhaust the attempt")

	if unknownElapsed < MinFailedLoginDuration {
		t.Errorf("unknown-id attempt-expired returned in %v, faster than the %v floor: timing oracle intact",
			unknownElapsed, MinFailedLoginDuration)
	}
	if exhaustedElapsed < MinFailedLoginDuration {
		t.Errorf("exhausted-attempt attempt-expired returned in %v, faster than the %v floor: timing oracle intact",
			exhaustedElapsed, MinFailedLoginDuration)
	}

	gap := unknownElapsed - exhaustedElapsed
	if gap < 0 {
		gap = -gap
	}
	if gap > timingToleranceForEqualisedFailures {
		t.Errorf("unknown-id (%v) and exhausted-attempt (%v) differ by %v, over the %v tolerance: "+
			"the floor is not applying equally to both attempt-expired paths",
			unknownElapsed, exhaustedElapsed, gap, timingToleranceForEqualisedFailures)
	}
}

// TestAuthRequestPoliciesReflectForceMFA pins spec D5's one
// enforcer-linked field: require_mfa mirrors LoginPolicy.ForceMFA
// exactly, read the SAME way loginclient's own sufficiency decision
// reads it.
func TestAuthRequestPoliciesReflectForceMFA(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc"}}`))
	})
	mux.HandleFunc("GET /management/v1/policies/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s","forceMfa":true}}`))
	})
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/V2_abc", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"require_mfa":true`)
}

// TestAuthRequestDoesNotExposeRequireMFALocalOnly pins spec D5's
// deliberate omission: even under a policy that sets
// forceMfaLocalOnly (folded into ForceMFA in Go, per
// loginclient.LoginPolicy's own doc comment), the response carries NO
// key spelling out that Zitadel-specific field at all — not merely that
// it is false. Rendering from a value the enforcer never separately
// consults is exactly what D5 forbids.
func TestAuthRequestDoesNotExposeRequireMFALocalOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authRequest":{"id":"V2_abc"}}`))
	})
	mux.HandleFunc("GET /management/v1/policies/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"policy":{"passwordCheckLifetime":"864000s","forceMfaLocalOnly":true}}`))
	})
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/auth/login/request/:id", h.AuthRequest)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/auth/login/request/V2_abc", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// require_mfa still reflects the fold (ForceMFA is true because
	// forceMfaLocalOnly is true) — this is not a test that MFA became
	// invisible, only that the SEPARATE, Zitadel-specific key never
	// crosses the wire.
	require.Contains(t, w.Body.String(), `"require_mfa":true`)
	require.NotContains(t, strings.ToLower(w.Body.String()), "requiremfalocalonly")
	require.NotContains(t, w.Body.String(), "require_mfa_local_only")
}

// TestNonNilFactorsNeverReturnsNil pins nonNilFactors directly (#867 fix
// round 1, Minor 4): a nil input must come back as a non-nil, empty
// slice, and a real slice must pass through unchanged.
func TestNonNilFactorsNeverReturnsNil(t *testing.T) {
	got := nonNilFactors(nil)
	require.NotNil(t, got, "nonNilFactors(nil) must not return nil")
	require.Empty(t, got)

	require.Equal(t, []string{"totp"}, nonNilFactors([]string{"totp"}))
}

// TestFactorRequiredResponseJSONNeverEmitsNull proves the actual byte
// this endpoint would emit for a nil Factors slice is `[]`, not `null`
// — the concrete, wire-level version of the guarantee
// TestNonNilFactorsNeverReturnsNil pins at the function level. A naive
// browser-side `for (const f of body.factor_required)` throws on
// `null` but iterates zero times over `[]`.
func TestFactorRequiredResponseJSONNeverEmitsNull(t *testing.T) {
	b, err := json.Marshal(factorRequiredResponse{Factors: nonNilFactors(nil)})
	require.NoError(t, err)
	require.Equal(t, `{"factor_required":[]}`, string(b))
}

// TestFactorRejectsUnsupportedFactor proves the "factor" field is
// actually checked, not merely bound and forwarded — a value this
// handler cannot check (anything but "totp") is a 400, never sent on to
// loginclient.VerifyTOTP to be misreported as a wrong code.
func TestFactorRejectsUnsupportedFactor(t *testing.T) {
	h := newFactorTestHandlers(t, zitadelFactorGoodCode(t))
	r := newFactorRouter(h)
	require.Equal(t, http.StatusOK, doPassword(t, r, loginUITestAuthRequestID, "test@helivanta.dev", "HmsDev123!").Code)

	w := doFactor(t, r, loginUITestAuthRequestID, "webauthn", "000000")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestFactorRefusesOverBudget proves Factor's OWN budget
// (factorRateBucket, h.factorLimit) actually refuses, and is DISTINCT
// from h.limit — Password's own over-budget test
// (TestPasswordRefusesOverBudget) already proves the shared bucket
// works, so this test's job is proving Factor draws from a DIFFERENT
// one with its OWN Rule, per #867's plan ("a six-digit guessing endpoint
// must not share a budget with anything else").
func TestFactorRefusesOverBudget(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	migs := New(nil).Migrations()
	var loginAttemptMig *tenantdb.Migration
	for i := range migs {
		if migs[i].ID == "0004_iam" {
			loginAttemptMig = &migs[i]
		}
	}
	require.NotNil(t, loginAttemptMig)
	require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{*loginAttemptMig}))

	limiter := ratelimit.NewMemory(100)
	// limit (h.limit, Password's own budget) is generous so the
	// PASSWORD calls below never trip it; factorLimit is what this test
	// actually exercises.
	limit := ratelimit.Rule{Rate: 60, Burst: 10, Per: time.Minute}
	factorLimit := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(zitadelFactorBadCode(t), loginUITestHostedLoginBaseURL, db, limiter, limit, factorLimit)
	r := newFactorRouter(h)

	post := func(authRequestID string) *httptest.ResponseRecorder {
		require.Equal(t, http.StatusOK, doPassword(t, r, authRequestID, "test@helivanta.dev", "HmsDev123!").Code)
		w := httptest.NewRecorder()
		body := `{"auth_request_id":"` + authRequestID + `","factor":"totp","code":"000000"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/factor", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.21:12345"
		r.ServeHTTP(w, req)
		return w
	}

	w1 := post("V2_factor_budget_1")
	require.Equal(t, http.StatusUnauthorized, w1.Code, w1.Body.String())

	w2 := post("V2_factor_budget_2")
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
	require.NotEmpty(t, w2.Header().Get("Retry-After"))
}

// TestFactorRateLimitNotBypassableBySpoofedXForwardedFor is Finding C1's
// proof (#867 Task 4 fix round 3): iam.allowedByLimiter keys every
// bucket on gin.Context.ClientIP(), and — this is the point of routing
// this test through httpserver.New rather than a bare gin.New() the way
// every other test in this file does — ClientIP() honours a
// caller-supplied X-Forwarded-For ONLY when the request's IMMEDIATE TCP
// peer is a TRUSTED proxy. TestFactorRefusesOverBudget above already
// proves the budget itself refuses; this test proves a caller cannot
// buy a fresh budget per request just by sending a different
// X-Forwarded-For value each time.
//
// httpserver.New(nil, nil) — no trusted proxy CIDRs — is what EVERY
// environment gets unless TRUSTED_PROXY_CIDRS is explicitly set
// (config.getenvCIDRList's fail-closed default): with nothing trusted,
// ClientIP() ignores X-Forwarded-For entirely and always returns the
// raw TCP RemoteAddr — httptest.NewRequest's default
// ("192.0.2.1:1234"), left unchanged here so both calls present the
// SAME peer, exactly what a real attacker's own TCP connection would
// do regardless of which header value they claim.
func TestFactorRateLimitNotBypassableBySpoofedXForwardedFor(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	migs := New(nil).Migrations()
	var loginAttemptMig *tenantdb.Migration
	for i := range migs {
		if migs[i].ID == "0004_iam" {
			loginAttemptMig = &migs[i]
		}
	}
	require.NotNil(t, loginAttemptMig)
	require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{*loginAttemptMig}))

	limiter := ratelimit.NewMemory(100)
	limit := ratelimit.Rule{Rate: 60, Burst: 10, Per: time.Minute}
	factorLimit := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(zitadelFactorBadCode(t), loginUITestHostedLoginBaseURL, db, limiter, limit, factorLimit)

	// The production wiring's own constructor — NOT gin.New() — so this
	// test exercises the SAME SetTrustedProxies decision cmd/api/main.go
	// makes, not a replica of it. No trusted proxies configured: the
	// fail-closed default.
	srv := httpserver.New(nil, nil)
	srv.Engine.POST("/v1/auth/login/password", h.Password)
	srv.Engine.POST("/v1/auth/login/factor", h.Factor)

	post := func(authRequestID, spoofedXFF string) *httptest.ResponseRecorder {
		require.Equal(t, http.StatusOK, doPassword(t, srv.Engine, authRequestID, "test@helivanta.dev", "HmsDev123!").Code)
		w := httptest.NewRecorder()
		body := `{"auth_request_id":"` + authRequestID + `","factor":"totp","code":"000000"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/factor", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// A DIFFERENT spoofed value on every call — the attack this test
		// disproves. RemoteAddr is deliberately left at httptest's
		// default rather than set explicitly, so every call in this
		// test shares the SAME real peer.
		req.Header.Set("X-Forwarded-For", spoofedXFF)
		srv.Engine.ServeHTTP(w, req)
		return w
	}

	w1 := post("V2_spoof_budget_1", "203.0.113.201")
	require.Equal(t, http.StatusUnauthorized, w1.Code, w1.Body.String())

	w2 := post("V2_spoof_budget_2", "198.51.100.77")
	require.Equal(t, http.StatusTooManyRequests, w2.Code,
		"a spoofed X-Forwarded-For value must not reset the factor rate-limit bucket when no proxy is trusted (Finding C1)")
	require.NotEmpty(t, w2.Header().Get("Retry-After"))
}
