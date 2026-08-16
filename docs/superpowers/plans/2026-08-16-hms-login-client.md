# HMS as Zitadel's Login Client — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** HMS renders its own email/password login form, driving Zitadel's Session API as a login client, so no Zitadel-branded page is ever shown.

**Architecture:** The `hms-web` OIDC app points its per-app login base URL at HMS's `/login`. The Go API (module `iam`, which already owns `POST /v1/auth/login`) holds the `IAM_LOGIN_CLIENT` credential and drives `GET /v2/oidc/auth_requests/{id}` → `POST /v2/sessions` → `POST /v2/oidc/auth_requests/{id}`. The returned `callbackUrl` is HMS's existing callback, so PKCE, the login exchange, session minting, renewal and sign-out are untouched.

**Tech Stack:** Go 1.26 + Gin, Next.js 16 + React 19, `@tesserix/web` auth components, `useZodForm`/`Field` from `@hms/ui`, Vitest, Playwright, Zitadel v4.15.3.

**Spec:** `docs/superpowers/specs/2026-08-16-hms-login-client-design.md`
**Spike (observed behaviour — trust this over any documentation):** `docs/superpowers/spikes/2026-08-16-zitadel-login-client.md`
**Issue:** #854

## Global Constraints

- **New code goes in the existing `iam` module**, as new files. Do **not** run `make new-module` — modules may not import each other, `iam` already owns `POST /v1/auth/login`, and `make new-module` has emitted non-compiling code three times (#830).
- **Every new engine-level route must be added to `bootstrap.UnauthenticatedRoutes`** with its reason, or `TestEveryEngineRouteIsDeclaredOrAllowlisted` fails. This is the control that makes an undeclared route impossible; do not weaken it.
- **The `IAM_LOGIN_CLIENT` token never leaves the Go API.** No Next.js route, no browser code, no log line may contain it.
- **Never log** a submitted password, `failedAttempts`, or the login name of a *failed* attempt.
- **Accessible names are a contract** (spec D6): the field labels must be exactly `Email` and `Password`, and the submit button's accessible name exactly `Sign in`.
- **slog only** (logrus banned); request-scoped logger via `requestid.Logger(c)`; wrap errors with `%w`.
- **`respond.*` helpers for every response** — never `c.JSON` directly.
- Frontend: `useZodForm` + `Field` from `@hms/ui`, `noValidate` on the form, inline zod errors. Native browser validation is banned. No raw `fetch`/`useState` polling in components.
- Before any task is done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green, and for frontend tasks `pnpm turbo lint type-check test build` green.
- The dev Zitadel is at `http://localhost:20080`; the login-client PAT is `dev/zitadel/secrets/login-client.pat`; the seeded user is `test@hms.dev` / `HmsDev123!`.

---

### Task 1: Prove the per-app login base URL — the blocking unknown

**This task can invalidate the whole approach. Do it first and stop if it fails.**

Spec D1 scopes the change to the `hms-web` app so other Tesserix products on the shared Zitadel instance are unaffected. The per-app setting is documented but was **not** exercised in the spike. If only the instance-wide setting works, stop and report — the decision goes back to the platform team.

**Files:**
- Modify: `scripts/lib/zitadel.mjs` (the app-provisioning helper)
- Modify: `scripts/zitadel-bootstrap.mjs` (calls it)

- [ ] **Step 1: Find the app-update call and the current app config**

```bash
grep -n "oidc\|apps\|createApp\|updateApp" scripts/lib/zitadel.mjs | head -30
```

Then read the live app config, to see exactly which fields an update must preserve:

```bash
Z=http://localhost:20080; SEED=$(cat dev/zitadel/secrets/hms-seed.pat)
PROJECT=$(curl -s -X POST "$Z/management/v1/projects/_search" -H "Authorization: Bearer $SEED" -H 'Content-Type: application/json' -d '{}' | python3 -m json.tool)
echo "$PROJECT"
```

- [ ] **Step 2: Set the per-app login base URL and observe the redirect change**

The `hms-web` app id and project id come from Step 1. The v1 management API updates an OIDC app's config with `PUT /management/v1/projects/{projectId}/apps/{appId}/oidc_config`; the login-version fields are `loginVersion.loginV2.baseUri`.

```bash
Z=http://localhost:20080; SEED=$(cat dev/zitadel/secrets/hms-seed.pat)
curl -s -w '\nHTTP %{http_code}\n' -X PUT \
  "$Z/management/v1/projects/$PROJECT_ID/apps/$APP_ID/oidc_config" \
  -H "Authorization: Bearer $SEED" -H 'Content-Type: application/json' \
  -d '{"loginVersion":{"loginV2":{"baseUri":"http://localhost:4301/login"}}}'
```

**If that endpoint or field shape is rejected**, find the right one before improvising: check `GET /management/v1/projects/{p}/apps/{a}` for the field name the instance actually returns, and consult the v4 API reference. Do not fall back to the instance-wide setting — that is the thing this task exists to avoid.

- [ ] **Step 3: Verify the redirect target actually moved**

```bash
Z=http://localhost:20080; CID=$(grep ZITADEL_CLIENT_ID dev/zitadel/secrets/zitadel.env | cut -d= -f2)
curl -s -o /dev/null -D - "$Z/oauth/v2/authorize?client_id=$CID&redirect_uri=http%3A%2F%2Flocalhost%3A4301%2Fapi%2Fauth%2Fcallback&response_type=code&scope=openid%20profile%20email&state=t&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256" | grep -i "^location"
```

Expected: `Location:` now points at `http://localhost:4301/login?authRequest=V2_…` instead of `/ui/v2/login/login?authRequest=…`.

**This is the assertion that matters.** A 200 on the PUT proves nothing — the spike's §2 records Zitadel returning success for a request it did not honour. Only the changed redirect proves it.

- [ ] **Step 4: Make it reproducible from a fresh clone**

Add the same call to `scripts/lib/zitadel.mjs` so `make up` configures it, immediately after the `hms-web` app is created/updated. Give it a comment naming #854 and stating that the per-app scope is deliberate because the instance is shared.

- [ ] **Step 5: Prove it from scratch**

```bash
make down && docker compose -f docker-compose.dev.yml down -v && rm -rf dev/zitadel/secrets/*
export NODE_AUTH_TOKEN=$(gh auth token)
make up
```

Then re-run Step 3's curl. Expected: the redirect points at `/login?authRequest=…` with no manual step.

- [ ] **Step 6: Commit**

```bash
git add scripts/
git commit -m "feat: point the hms-web app's login UI at HMS, per-app not instance-wide (#854)"
```

---

### Task 2: Zitadel login-client Go client

A thin, well-tested wrapper over the four calls the spike proved. No policy logic, no HTTP handlers — just the protocol.

**Files:**
- Create: `backend/internal/modules/iam/loginclient/client.go`
- Create: `backend/internal/modules/iam/loginclient/client_test.go`

**Interfaces:**
- Produces:
  - `type Client struct{}` with `func New(baseURL, token string, hc *http.Client) *Client`
  - `func (c *Client) AuthRequest(ctx context.Context, id string) (AuthRequest, error)`
  - `func (c *Client) CreatePasswordSession(ctx context.Context, loginName, password string) (Session, error)`
  - `func (c *Client) Finalize(ctx context.Context, authRequestID string, s Session) (string, error)` — returns `callbackUrl`
  - `func (c *Client) LoginPolicy(ctx context.Context) (LoginPolicy, error)`
  - `type AuthRequest struct { ID, ClientID, RedirectURI string; Scope []string }`
  - `type Session struct { ID, Token string }`
  - `type LoginPolicy struct { ForceMFA bool }`
  - Sentinel errors: `ErrBadCredentials`, `ErrUserNotFound`, `ErrAuthRequestInvalid`, `ErrUnavailable`

- [ ] **Step 1: Write the failing tests**

Create `client_test.go`. These encode the spike's **observed** responses verbatim — do not invent shapes.

```go
package loginclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-pat", srv.Client())
}

func TestCreatePasswordSessionReturnsSessionOnSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-pat" {
			t.Errorf("Authorization = %q, want Bearer test-pat", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sessionId":"386477864129658887","sessionToken":"tok-abc"}`))
	})
	s, err := c.CreatePasswordSession(context.Background(), "test@hms.dev", "HmsDev123!")
	if err != nil {
		t.Fatalf("CreatePasswordSession() error = %v", err)
	}
	if s.ID != "386477864129658887" || s.Token != "tok-abc" {
		t.Errorf("session = %+v, want id/token from body", s)
	}
}

// Observed: HTTP 400, COMMAND-3M0fs, with a failedAttempts counter.
func TestCreatePasswordSessionMapsWrongPasswordToErrBadCredentials(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":3,"message":"Password is invalid (COMMAND-3M0fs)","details":[{"@type":"type.googleapis.com/zitadel.v1.CredentialsCheckError","id":"COMMAND-3M0fs","message":"Password is invalid","failedAttempts":1}]}`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "test@hms.dev", "wrong")
	if !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("error = %v, want ErrBadCredentials", err)
	}
	// failedAttempts must not survive into the error text — it reaches a log line.
	if got := err.Error(); contains(got, "failedAttempts") || contains(got, "1") {
		t.Errorf("error text %q leaks the attempt counter", got)
	}
}

// Observed: HTTP 404, QUERY-Dfbg2.
func TestCreatePasswordSessionMapsUnknownUserToErrUserNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":5,"message":"User could not be found (QUERY-Dfbg2)"}`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "nobody@hms.dev", "x")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestFinalizeReturnsCallbackURL(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=abc&state=s"}`))
	})
	got, err := c.Finalize(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if got != "http://localhost:4301/api/auth/callback?code=abc&state=s" {
		t.Errorf("callbackUrl = %q", got)
	}
}

func TestAuthRequestParsesClientAndRedirect(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"authRequest":{"id":"V2_386477922262777863","clientId":"386401161701228551","scope":["openid"],"redirectUri":"http://localhost:4301/api/auth/callback"}}`))
	})
	ar, err := c.AuthRequest(context.Background(), "V2_386477922262777863")
	if err != nil {
		t.Fatalf("AuthRequest() error = %v", err)
	}
	if ar.ClientID != "386401161701228551" || ar.ID != "V2_386477922262777863" {
		t.Errorf("authRequest = %+v", ar)
	}
}

func TestLoginPolicyReportsForceMFA(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"policy":{"allowUsernamePassword":true,"forceMfa":true}}`))
	})
	p, err := c.LoginPolicy(context.Background())
	if err != nil {
		t.Fatalf("LoginPolicy() error = %v", err)
	}
	if !p.ForceMFA {
		t.Error("ForceMFA = false, want true")
	}
}

// A policy read that fails must NOT report "no MFA required" — spec D4 fails closed.
func TestLoginPolicyErrorsRatherThanReportingNoMFA(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.LoginPolicy(context.Background()); err == nil {
		t.Fatal("LoginPolicy() error = nil, want an error so the caller can fail closed")
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
})() }
```

- [ ] **Step 2: Run the tests and watch them fail**

```bash
cd backend && go test ./internal/modules/iam/loginclient/... -v
```

Expected: build failure — `undefined: New`, `undefined: Client`.

- [ ] **Step 3: Implement `client.go`**

Write the client with:
- `New(baseURL, token string, hc *http.Client) *Client` storing all three; `hc` defaulted to a client with a **10s timeout** if nil.
- A private `do(ctx, method, path string, body any, out any) error` that sets `Authorization: Bearer <token>` and `Content-Type: application/json`, and maps non-2xx by status: 400 → `ErrBadCredentials`, 404 → `ErrUserNotFound` (or `ErrAuthRequestInvalid` on the auth-request paths), 5xx/transport → `ErrUnavailable`, wrapping with `%w`.
- The four methods against these exact paths (from the spike):
  - `GET /v2/oidc/auth_requests/{id}`
  - `POST /v2/sessions` with body `{"checks":{"user":{"loginName":…},"password":{"password":…}}}`
  - `POST /v2/oidc/auth_requests/{id}` with body `{"session":{"sessionId":…,"sessionToken":…}}`
  - `GET /management/v1/policies/login`
- Error text must never include the response body for the credential paths, because it carries `failedAttempts`. Include the status code and the Zitadel error `id` only.

Package doc comment must state: this package speaks the protocol and makes **no** authorization decision; sufficiency lives in `sufficiency.go` (Task 3).

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd backend && go test ./internal/modules/iam/loginclient/... -v
```

Expected: PASS, all cases.

- [ ] **Step 5: Prove the tests can fail**

Temporarily change the 404 mapping to return `ErrBadCredentials`. Re-run: `TestCreatePasswordSessionMapsUnknownUserToErrUserNotFound` must fail. Revert.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/iam/loginclient/
git commit -m "feat: Zitadel login-client protocol wrapper (#854)"
```

---

### Task 3: Authentication sufficiency (spec D4) — the security core

The spike proved Zitadel issues an authorization code for a **password-only** session even when the org policy sets `forceMfa`. So HMS must decide sufficiency itself, and the decision must be **structurally unavoidable**.

**Files:**
- Create: `backend/internal/modules/iam/loginclient/sufficiency.go`
- Create: `backend/internal/modules/iam/loginclient/sufficiency_test.go`
- Modify: `backend/internal/modules/iam/loginclient/client.go` (unexport `Finalize`)
- Modify: `backend/internal/archtest/arch_test.go`

**Interfaces:**
- Consumes: Task 2's `Client`, `Session`, `LoginPolicy`, sentinel errors.
- Produces:
  - `type Outcome int` with `OutcomeComplete`, `OutcomeHandoff`
  - `type Result struct { Outcome Outcome; CallbackURL string }`
  - `func (c *Client) CompleteIfSufficient(ctx context.Context, authRequestID string, s Session) (Result, error)`

- [ ] **Step 1: Write the failing tests**

```go
package loginclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// countingZitadel serves a login policy and records whether finalize was called.
func countingZitadel(t *testing.T, policyJSON string, finalized *atomic.Bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/management/v1/policies/login":
			w.Write([]byte(policyJSON))
		case r.Method == http.MethodPost && len(r.URL.Path) > len("/v2/oidc/auth_requests/"):
			finalized.Store(true)
			w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "pat", srv.Client())
}

// THE test this whole spec exists for. Zitadel will happily finalize a
// password-only session under forceMfa (spike §2) — HMS must not ask it to.
func TestCompleteIfSufficientDoesNotFinalizeWhenForceMFA(t *testing.T) {
	var finalized atomic.Bool
	c := countingZitadel(t, `{"policy":{"forceMfa":true}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called under forceMfa: this is an MFA bypass")
	}
	if got.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, want empty on handoff", got.CallbackURL)
	}
}

func TestCompleteIfSufficientFinalizesWhenNoMFARequired(t *testing.T) {
	var finalized atomic.Bool
	c := countingZitadel(t, `{"policy":{"forceMfa":false}}`, &finalized)

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v", err)
	}
	if got.Outcome != OutcomeComplete {
		t.Fatalf("Outcome = %v, want OutcomeComplete", got.Outcome)
	}
	if !finalized.Load() {
		t.Error("finalize was not called for a sufficient session")
	}
	if got.CallbackURL == "" {
		t.Error("CallbackURL is empty on a completed login")
	}
}

// Fail closed: an unreadable policy must hand off, never complete.
func TestCompleteIfSufficientHandsOffWhenPolicyUnreadable(t *testing.T) {
	var finalized atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/management/v1/policies/login" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		finalized.Store(true)
		w.Write([]byte(`{"callbackUrl":"x"}`))
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())

	got, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("CompleteIfSufficient() error = %v, want a handoff not an error", err)
	}
	if got.Outcome != OutcomeHandoff {
		t.Errorf("Outcome = %v, want OutcomeHandoff when the policy cannot be read", got.Outcome)
	}
	if finalized.Load() {
		t.Fatal("finalize was called while the policy was unknown: fails open")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./internal/modules/iam/loginclient/... -run Sufficient -v
```

Expected: `undefined: CompleteIfSufficient`.

- [ ] **Step 3: Implement `sufficiency.go`, and close the bypass**

Implement `CompleteIfSufficient`: read `LoginPolicy`; if the read errors **or** `ForceMFA` is true, return `Result{Outcome: OutcomeHandoff}` with a nil error; otherwise call the finalize call and return `OutcomeComplete` with the callback URL.

Then **rename `Client.Finalize` to unexported `Client.finalize`** in `client.go` and update Task 2's `TestFinalizeReturnsCallbackURL` to call `finalize`. This is the structural half of D4: outside this package there is no way to finalize without passing the sufficiency check. Add a comment at `finalize` saying exactly that, and why (naming the spike's §2 finding).

- [ ] **Step 4: Run and watch them pass**

```bash
cd backend && go test ./internal/modules/iam/loginclient/... -v
```

Expected: PASS.

- [ ] **Step 5: Prove the MFA test can fail — mandatory**

Comment out the `ForceMFA` branch so `CompleteIfSufficient` always finalizes. Re-run:

```bash
cd backend && go test ./internal/modules/iam/loginclient/... -run ForceMFA -v
```

Expected: `TestCompleteIfSufficientDoesNotFinalizeWhenForceMFA` **FAILS** with "finalize was called under forceMfa". Restore the branch and confirm it passes again.

A D4 test that cannot fail is worse than no test, because D4 is invisible when broken. Do not skip this step.

- [ ] **Step 6: Add the arch test pinning the single finalize call site**

In `backend/internal/archtest/arch_test.go`, add a test that walks the repo's Go sources and asserts the string `/v2/oidc/auth_requests/` used with a POST appears in exactly one file, `internal/modules/iam/loginclient/client.go`. Follow the file-walking style of the existing tests in that file.

Failure message must say: *"the OIDC finalize call must stay behind CompleteIfSufficient (spec D4) — Zitadel does not enforce forceMfa for a login client"*.

- [ ] **Step 7: Run the arch test and prove it fails too**

```bash
cd backend && go test ./internal/archtest/... -run Finalize -v
```

Then add a second POST to that path in another file, re-run, confirm FAIL, and remove it.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/modules/iam/loginclient/ backend/internal/archtest/
git commit -m "feat: HMS enforces MFA sufficiency itself; Zitadel does not for a login client (#854)"
```

---

### Task 4: HTTP handlers, identical failures, equalised timing (spec D5)

**Files:**
- Create: `backend/internal/modules/iam/loginui.go`
- Create: `backend/internal/modules/iam/loginui_test.go`
- Modify: `backend/internal/bootstrap/unauthenticated.go`

**Interfaces:**
- Consumes: Task 3's `CompleteIfSufficient`, `Result`, `Outcome*`; Task 2's sentinel errors.
- Produces:
  - `func NewLoginUIHandlers(c *loginclient.Client, hostedLoginBaseURL string, limiter ratelimit.Limiter, limit ratelimit.Rule) *LoginUIHandlers`
  - Methods `AuthRequest`, `Password`, `Handoff` (all `gin.HandlerFunc`-shaped)

- [ ] **Step 1: Write the failing tests**

```go
package iam

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Spec D5: a wrong password and an unknown user must be indistinguishable
// by status, by body, AND by timing. Mapping the status alone leaves the
// ~55x timing oracle the spike measured (0.72s vs 0.013s) fully intact.
func TestPasswordFailuresAreIdenticalForWrongPasswordAndUnknownUser(t *testing.T) {
	wrong := postPassword(t, zitadelWrongPassword(t), "test@hms.dev", "nope")
	unknown := postPassword(t, zitadelUnknownUser(t), "nobody@hms.dev", "nope")

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

func TestPasswordFailureTimingIsEqualised(t *testing.T) {
	// The fake unknown-user server answers instantly; the fake wrong-password
	// server sleeps to imitate the real hash cost.
	start := time.Now()
	postPassword(t, zitadelUnknownUser(t), "nobody@hms.dev", "nope")
	unknownElapsed := time.Since(start)

	if unknownElapsed < MinFailedLoginDuration {
		t.Errorf("unknown-user path returned in %v, faster than the %v floor: timing oracle intact",
			unknownElapsed, MinFailedLoginDuration)
	}
}

func TestPasswordSuccessReturnsCallbackURL(t *testing.T) {
	rec := postPassword(t, zitadelHappyPath(t), "test@hms.dev", "HmsDev123!")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/api/auth/callback") {
		t.Errorf("body = %s, want a callback_url", rec.Body.String())
	}
}

func TestPasswordUnderForceMFAReturnsHandoffNotSession(t *testing.T) {
	rec := postPassword(t, zitadelForceMFA(t), "test@hms.dev", "HmsDev123!")
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
	rec := postPassword(t, zitadelDown(t), "test@hms.dev", "HmsDev123!")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — never a credentials error when the IdP is down", rec.Code)
	}
}
```

Write the `zitadel*(t)` helpers as `httptest` servers returning the spike's exact payloads, and `postPassword(t, client, email, password) *httptest.ResponseRecorder` building a Gin context against the handler. Follow `login_test.go`'s existing helper style in the same package.

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./internal/modules/iam/... -run Password -v
```

Expected: undefined symbols.

- [ ] **Step 3: Implement `loginui.go`**

- `MinFailedLoginDuration` — an exported constant, **900ms**, with a comment recording the measurement it derives from (spike §3: wrong password 0.72–0.78s, unknown user 0.013s) and that it must be revisited if Zitadel's hash cost changes.
- A helper that runs a failing path and sleeps until `MinFailedLoginDuration` has elapsed since the request started, before responding. Apply it to `ErrBadCredentials` **and** `ErrUserNotFound`, which both return the **same** `respond.*` refusal with one message such as `"email or password is incorrect"`.
- `ErrUnavailable` → 503 with a retryable message, and **no** timing floor (an outage should not be slowed further).
- `ErrAuthRequestInvalid` → a distinct 400 telling the user the sign-in attempt expired and to start again.
- Success → `{"callback_url": …}`. Handoff → `{"handoff_url": …}` built from `hostedLoginBaseURL` + the auth request id.
- Rate limiting: check the limiter **before** calling Zitadel, keyed on client IP (there is no verified subject at this point). Reuse `bootstrap.LoginRateLimitRule` style; do not invent a second limiter.
- Log every outcome via `requestid.Logger(c)` with the auth request id and outcome. Never the password, never `failedAttempts`, never the login name on failure.

- [ ] **Step 4: Run and watch them pass**

```bash
cd backend && go test ./internal/modules/iam/... -run Password -v
```

- [ ] **Step 5: Prove the timing test can fail**

Set `MinFailedLoginDuration` to `0`. Re-run `TestPasswordFailureTimingIsEqualised` — it must FAIL. Restore 900ms.

- [ ] **Step 6: Declare the routes**

Add to `bootstrap.UnauthenticatedRoutes`, each with its reason:

```go
"GET /v1/auth/login/request/:id": "renders the login form before any HMS session exists",
"POST /v1/auth/login/password":   "checks the credential that creates the session, so it cannot require one",
"POST /v1/auth/login/handoff/:id": "hands an auth request to the hosted login when HMS cannot complete it",
```

and register them in `MountUnauthenticated`, extending its signature.

- [ ] **Step 7: Run the arch test**

```bash
cd backend && go test ./internal/archtest/... -v
```

Expected: PASS — `TestEveryEngineRouteIsDeclaredOrAllowlisted` accounts for the three new routes.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/modules/iam/ backend/internal/bootstrap/
git commit -m "feat: login-client endpoints with identical, timing-equalised failures (#854)"
```

---

### Task 5: Wire it up and prove it against the real Zitadel

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/cmd/api/main.go`
- Create: `backend/internal/modules/iam/loginui_integration_test.go`
- Modify: `docker-compose.dev.yml`, `Makefile` (pass the PAT to the API)

- [ ] **Step 1: Add config**

Add to `config.Config`, following the existing field-comment style:
- `ZitadelLoginClientToken string` — from `ZITADEL_LOGIN_CLIENT_TOKEN`. **No default.** Document that this is an instance-level `IAM_LOGIN_CLIENT` credential, that it can finalise auth requests for *any* app on the shared instance, and that #45 must cover it in production.
- `ZitadelHostedLoginURL string` — from `ZITADEL_HOSTED_LOGIN_URL`, default `http://localhost:20080/ui/v2/login`.

- [ ] **Step 2: Fail fast when it is missing**

If `ZitadelLoginClientToken` is empty, the API must refuse to boot with a clear message — login is unusable without it and a half-working login is worse than a stopped process. Mirror how `SESSION_SIGNING_KEY` is handled.

- [ ] **Step 3: Mount in `main.go`**

Construct the `loginclient.Client` and `LoginUIHandlers`, pass them to `bootstrap.MountUnauthenticated`. Reuse the **same** `ratelimit.Limiter` instance already built for `V1Chain` — do not create a second.

- [ ] **Step 4: Pass the PAT in dev**

In `docker-compose.dev.yml` / the `Makefile`, read `dev/zitadel/secrets/login-client.pat` and export it as `ZITADEL_LOGIN_CLIENT_TOKEN` for the API. Note in a comment that the file is written by Zitadel first-instance provisioning and may not exist at compose time — the same ordering trap `zitadel-pat-ready` documents.

- [ ] **Step 5: Write the integration test**

Guard it the way the repo's other integration tests are guarded (skip when the stack is absent). It must, against the **real** dev Zitadel:

1. Create a real auth request via `/oauth/v2/authorize` and capture the id.
2. `POST /v1/auth/login/password` with `test@hms.dev` / `HmsDev123!`.
3. Assert a `callback_url` comes back containing `code=` and `state=`.
4. Repeat with a wrong password and assert the refusal is byte-identical to the unknown-user refusal.

- [ ] **Step 6: Run everything**

```bash
cd backend && go test -race ./... && ./scripts/coverage-gate.sh && make -C .. lint-go
```

Expected: all green, coverage ≥70%.

- [ ] **Step 7: Commit**

```bash
git add backend/ docker-compose.dev.yml Makefile
git commit -m "feat: wire the login client into the API and prove it against real Zitadel (#854)"
```

---

### Task 6: The login form

**Files:**
- Modify: `apps/shell/app/login/page.tsx`
- Modify: `apps/shell/app/login/login.test.tsx`
- Create: `apps/shell/lib/login-client.ts`

- [ ] **Step 1: Write the failing tests**

Extend `login.test.tsx` using `renderWithProviders` from `@hms/api/testing`:

```tsx
it("renders fields whose accessible names match the e2e contract", async () => {
  renderWithProviders(<LoginPage />);
  expect(await screen.findByLabelText("Email")).toBeInTheDocument();
  expect(screen.getByLabelText("Password")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
});

it("shows an inline error and does not submit when the email is empty", async () => {
  const user = userEvent.setup();
  renderWithProviders(<LoginPage />);
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByText(/enter your email/i)).toBeInTheDocument();
});

it("shows one message for a refused credential and never says which field was wrong", async () => {
  // mock POST /v1/auth/login/password -> 401 with the shared refusal
  renderWithProviders(<LoginPage />);
  // ...submit valid-shaped credentials...
  expect(await screen.findByRole("alert")).toHaveTextContent(/email or password is incorrect/i);
});

it("navigates to the handoff url when the API says a handoff is required", async () => {
  // mock the API to return { handoff_url: "..." } and assert navigation
});
```

- [ ] **Step 2: Run and watch them fail**

```bash
pnpm --filter shell test -- login
```

- [ ] **Step 3: Implement the form**

- Read `authRequest` from the query string. **If it is absent, keep today's behaviour**: the landing page with a Sign in button that calls `signinRedirect({ prompt: "login" })` — that is how a user arriving at `/login` directly gets an auth request at all, and #847's reasons for a button (not a mount-time redirect) still hold.
- With an `authRequest` present, render the credential form: `useZodForm` + `Field` from `@hms/ui`, `noValidate`, inline zod errors, labels exactly `Email` and `Password`, submit button exactly `Sign in`.
- Keep `AuthLayoutCentered` / `AuthCardCentered` / `AuthCardFooter` from `@tesserix/web`.
- On submit call `POST /v1/auth/login/password` through `@hms/api`. On `callback_url`, navigate there. On `handoff_url`, navigate there. On refusal, one `role="alert"` message that never distinguishes which field was wrong.
- **Rewrite the page's long header comment.** It currently states HMS renders no password field and that doing so would be wrong. That is now false. Replace it with: what this page is, that D5a was reversed by #854, that the accessible names are a contract, and — most importantly — that a password check alone is **not** sufficient authentication (spec D4), which is why the API may answer with a handoff and the page must honour it rather than treating it as an error.
- Keep the `SIGNED_OUT_MARK` handling exactly as it is.

- [ ] **Step 4: Run and watch them pass**

```bash
pnpm --filter shell test -- login
```

- [ ] **Step 5: Full frontend gate**

```bash
pnpm turbo lint type-check test build
```

- [ ] **Step 6: Commit**

```bash
git add apps/shell/
git commit -m "feat: HMS renders its own login form (#854)"
```

---

### Task 7: Repoint the e2e suite and prove the whole thing

**Files:**
- Modify: `e2e/tests/support/login.ts`
- Modify: `docs/standards/frontend.md` (if it states login is a redirect)

- [ ] **Step 1: Rewrite `fillAndSubmit` / `signInOnce` for a one-step form**

Zitadel's hosted UI was two steps (Loginname → next → Password → continue). Ours is one. Replace with:

```ts
async function signInOnce(page: Page, user: Credentials): Promise<boolean> {
  await page.getByLabel("Email").fill(user.email);
  await page.getByLabel("Password").fill(user.password);
  await page.getByRole("button", { name: "Sign in" }).click();

  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible({
    timeout: 30_000,
  });
  return page.evaluate(async (url) => (await fetch(url)).ok, PERMISSIONS_PROBE);
}
```

Update the file's header comment: it currently says every field lives on `auth.tesserix.app` and that HMS's own form "never existed here". Both are now wrong. State that the form is HMS's own and that the accessible names are D6's contract.

Keep the retry loop, `MAX_SIGN_IN_ATTEMPTS`, the `auth_time` reasoning and the per-spec account derivation **exactly** as they are — none of that changes.

- [ ] **Step 2: Run the full suite**

```bash
export NODE_AUTH_TOKEN=$(gh auth token)
make up
pnpm --filter e2e exec playwright test --reporter=list
```

Expected: **11 passed**. Any failure at the login step means D6's contract is broken — fix the form's accessible names, not the selector.

- [ ] **Step 3: Prove the fresh-clone path**

```bash
make down && docker compose -f docker-compose.dev.yml down -v && rm -rf dev/zitadel/secrets/*
export NODE_AUTH_TOKEN=$(gh auth token)
make up && make verify-local
```

Expected: `All checks passed`, and signing in at http://localhost:4301 shows **HMS's** form with no Zitadel-branded page at any point.

- [ ] **Step 4: Commit**

```bash
git add e2e/ docs/
git commit -m "test: drive HMS's own login form in the e2e suite (#854)"
```

---

### Task 8: Close the documentation loop

**Files:**
- Modify: `docs/superpowers/specs/2026-08-16-hms-login-client-design.md`
- Modify: `docs/superpowers/spikes/2026-08-16-zitadel-login-client.md`
- Modify: `docs/standards/backend.md`

- [ ] **Step 1: Record what the implementation actually observed**

The spec lists two unknowns. Resolve both, in writing:
- **`passwordChangeRequired`** — provoke it (create a user via the `_import` endpoint with the flag set), record the real session-create response in the spike doc, and make the D3 table row factual rather than assumed. If it turns out HMS cannot detect it, say so plainly and file a follow-up issue.
- **Per-user enrolled factors** — find the endpoint that lists a user's configured second factors, or record that it was not found and what was tried. If found, extend Task 3's sufficiency check to hand off when the user has factors even if the org does not force MFA, with a test proven to fail.

- [ ] **Step 2: Note the credential in the backend standards**

Add a line to `docs/standards/backend.md` stating that `ZITADEL_LOGIN_CLIENT_TOKEN` is instance-level, must never appear in a frontend service or a log, and that the OIDC finalize call lives behind `CompleteIfSufficient` by arch test.

- [ ] **Step 3: Commit and open the PR**

```bash
git add docs/
git commit -m "docs: record observed login-client behaviour and the credential's handling (#854)"
git push -u origin feat/854-hms-login-client
```

PR body must state: what shipped, that D5a is reversed and why, the D4 finding in full (Zitadel does not enforce forceMfa for a login client), what the slice does **not** cover (MFA #41, passkeys #570, lockout #855), and `Closes #854`.

---

## Self-Review

**Spec coverage:** D1 → Task 1. D2 → Tasks 4, 5. D3 → Tasks 4 (handoff endpoint), 6 (page honours it), 8 (password-change row made factual). D4 → Task 3, with a proven-failing test and an arch test. D5 → Task 4, both status/body and timing. D6 → Tasks 6, 7. Errors-and-failure section → Task 4 Step 3. Testing section → Tasks 2–7. Risks → Task 1 (base URL), Task 8 (both unknowns).

**Placeholders:** none. Where an exact API shape could not be verified (Task 1's `oidc_config` field), the plan says how to discover it and forbids the wrong fallback rather than hand-waving.

**Type consistency:** `Session{ID, Token}`, `Result{Outcome, CallbackURL}`, `Outcome{OutcomeComplete, OutcomeHandoff}`, `LoginPolicy{ForceMFA}` and the sentinel errors are used identically in Tasks 2–5. `Finalize` is deliberately renamed to `finalize` in Task 3 Step 3, and Task 2's test is updated in the same step.
