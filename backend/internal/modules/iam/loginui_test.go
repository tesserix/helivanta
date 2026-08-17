package iam

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/ratelimit"
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
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})

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
	h := NewLoginUIHandlers(zitadelHappyPath(t), loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})
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

	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, limiter, rule)
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
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, limiter, rule)

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
	h := NewLoginUIHandlers(nil, loginUITestHostedLoginBaseURL, limiter, rule)

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
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"user not found","details":[{"id":"QUERY-Dfbg2"}]}`))
	})
	limiter := ratelimit.NewMemory(100)
	rule := ratelimit.Rule{Rate: 6, Burst: 1, Per: time.Minute}
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, limiter, rule)

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
	h := NewLoginUIHandlers(newZitadelTestClient(t, mux), loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})

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
	client := newZitadelTestClient(t, mux)
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})

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
	h := NewLoginUIHandlers(client, loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})

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
	h := NewLoginUIHandlers(nil, loginUITestHostedLoginBaseURL, nil, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/handoff/:id", h.Handoff)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/auth/login/handoff/"+loginUITestAuthRequestID, nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "authRequest="+loginUITestAuthRequestID)
	require.Contains(t, w.Body.String(), loginUITestHostedLoginBaseURL)
}
