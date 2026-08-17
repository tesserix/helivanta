# Native MFA + Auth Components (#867) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Helivanta collects the TOTP code on its own themed page, using `@tesserix/web`'s auth components, with Zitadel still checking the factor.

**Architecture:** `POST /v1/auth/login/password` gains a third outcome, `factor_required`. A `login_attempt` row holds the Zitadel session between steps so the browser never sees a Zitadel token. `POST /v1/auth/login/factor` adds the TOTP check to that session and finalizes. Sufficiency stays in `loginclient`, behind its existing arch test.

**Tech Stack:** Go 1.26, Gin, Postgres, Next.js 16, `@tesserix/web` 2.2.1, Zitadel v4.15.3.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-17-native-mfa-auth-components-design.md` (D1–D8).
- **Spike:** `docs/superpowers/spikes/2026-08-17-zitadel-login-client-mfa.md`. Protocol facts come from here, not from documentation.
- **THE SESSION TOKEN ROTATES.** `PATCH /v2/sessions/{id}` returns a NEW `sessionToken`. Finalize must use the newest one. Reusing the creation token fails finalize *after* a correct code, so the clinician is told their TOTP was wrong. This is spec D3 and the highest-risk defect in the plan.
- **The method is `PATCH`.** `POST` to a session id returns 405.
- **The page never receives a Zitadel session token** (spec D2, inherited from login-client D2).
- **`Email`, `Password`, `Sign in` MUST NOT CHANGE** (spec D7). `e2e/tests/support/login.ts` drives login by accessible name; a rename fails all 12 specs. `AuthCredentialForm`'s default login-name label is policy-derived and will NOT say "Email" — pass `loginNameLabel`, `passwordLabel`, `submitLabel` explicitly.
- **`login_attempt` is NOT tenant-scoped and gets NO forced-RLS boilerplate** (spec D2). Login precedes tenant selection. Do not add a `tenant_id`.
- **`OutcomeHandoff` must stay the zero value.** It is the fail-closed default; a new outcome goes after `OutcomeComplete`, never before.
- **Never log** the TOTP code, the password, the Zitadel session token, or the login name on a failed attempt.
- **Refusals never reveal remaining attempts.** A countdown tells an attacker their budget.
- Commit messages: single line, conventional commits, no signature, no `Co-Authored-By`.
- Before done: `make lint-go`, `make test-go`, `make coverage-go` green; `pnpm turbo lint type-check test build` green; `make e2e` green.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/modules/iam/loginclient/client.go` (modify) | `VerifyTOTP` — the PATCH call; `SessionFactors` — read verified factors |
| `backend/internal/modules/iam/loginclient/sufficiency.go` (modify) | `OutcomeFactorRequired`; assert on verified factors |
| `backend/internal/modules/iam/module.go` (modify) | migration `0004_iam` |
| `backend/internal/modules/iam/loginattempt.go` (create) | the `login_attempt` store: put, get, bump, delete |
| `backend/internal/modules/iam/loginui.go` (modify) | `factor_required` outcome; `Factor` handler; policy in the request response |
| `backend/internal/bootstrap/unauthenticated.go` (modify) | register + rate-limit the new route |
| `apps/shell/lib/login-client.ts` (modify) | three-way union; `checkFactor` |
| `apps/shell/app/login/page.tsx` (modify) | `AuthCredentialForm` + the OTP step |
| `e2e/tests/mfa.spec.ts` (create) | a real TOTP login |

---

### Task 1: `login_attempt` store and migration

**Files:**
- Modify: `backend/internal/modules/iam/module.go` (add migration after `0003_iam`)
- Create: `backend/internal/modules/iam/loginattempt.go`
- Create: `backend/internal/modules/iam/loginattempt_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `loginAttemptStore` with `Put(ctx, attempt) error`, `Get(ctx, authRequestID) (loginAttempt, error)`, `BumpAndGet(ctx, authRequestID) (loginAttempt, error)`, `UpdateToken(ctx, authRequestID, token string) error`, `Delete(ctx, authRequestID) error`, and `errAttemptNotFound`, `errAttemptsExhausted`.

- [ ] **Step 1: Add the migration**

In `module.go`'s `Migrations()`, after the `0003_iam` entry:

```go
{
    // login_attempt holds the Zitadel session between the password step and
    // the factor step, because spec D2 forbids handing a Zitadel session
    // token to the browser.
    //
    // DELIBERATELY NOT TENANT-SCOPED, and deliberately WITHOUT the forced-RLS
    // boilerplate every tenant table in this repo carries: login happens
    // BEFORE a tenant is selected, so there is no tenant_id to scope on and
    // inventing one would mean fabricating a value the user has not chosen
    // yet. A reviewer applying the RLS convention here by reflex produces a
    // policy that matches nothing.
    ID: "0004_iam",
    SQL: `
CREATE TABLE IF NOT EXISTS login_attempt (
  auth_request_id       text PRIMARY KEY,
  zitadel_session_id    text        NOT NULL,
  zitadel_session_token text        NOT NULL,
  subject               text        NOT NULL,
  factor_attempts       int         NOT NULL DEFAULT 0,
  expires_at            timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS login_attempt_expires_at_idx ON login_attempt (expires_at);
`,
},
```

- [ ] **Step 2: Write the failing test**

Create `backend/internal/modules/iam/loginattempt_test.go`. Follow the existing test-infra pattern in this package for obtaining a DB handle (read a sibling `*_test.go` in `internal/modules/iam` and copy how it gets its store — do not invent a new harness).

**`newTestLoginAttemptStore` below is a PLACEHOLDER NAME.** Use whatever this
package's existing tests already use to obtain a DB-backed store; if there is no
such helper, add one following the neighbouring pattern. Do not create a second
test harness, and do not assume this name exists — an invented identifier is how
an earlier task in this repo shipped a brief that would not compile.

```go
// The attempt counter is a SECURITY control, not bookkeeping: a TOTP code is
// six digits, and Zitadel applies a delay rather than a lockout (spec D6).
// These tests therefore assert on the exhaustion boundary, not just on
// increment.
func TestLoginAttempt_BumpExhaustsAtFive(t *testing.T) {
    store := newTestLoginAttemptStore(t)
    ctx := context.Background()

    require.NoError(t, store.Put(ctx, loginAttempt{
        AuthRequestID: "V2_test", SessionID: "s1", SessionToken: "t1",
        Subject: "u1", ExpiresAt: time.Now().Add(5 * time.Minute),
    }))

    // Four wrong codes are survivable.
    for i := 1; i <= 4; i++ {
        got, err := store.BumpAndGet(ctx, "V2_test")
        require.NoError(t, err, "attempt %d must not exhaust", i)
        require.Equal(t, i, got.FactorAttempts)
    }

    // The fifth exhausts.
    _, err := store.BumpAndGet(ctx, "V2_test")
    require.ErrorIs(t, err, errAttemptsExhausted)

    // And the row is gone, so the session cannot be reused.
    _, err = store.Get(ctx, "V2_test")
    require.ErrorIs(t, err, errAttemptNotFound)
}

func TestLoginAttempt_ExpiredIsNotFound(t *testing.T) {
    store := newTestLoginAttemptStore(t)
    ctx := context.Background()
    require.NoError(t, store.Put(ctx, loginAttempt{
        AuthRequestID: "V2_old", SessionID: "s", SessionToken: "t",
        Subject: "u", ExpiresAt: time.Now().Add(-time.Second),
    }))
    _, err := store.Get(ctx, "V2_old")
    require.ErrorIs(t, err, errAttemptNotFound)
}

func TestLoginAttempt_UpdateTokenReplaces(t *testing.T) {
    store := newTestLoginAttemptStore(t)
    ctx := context.Background()
    require.NoError(t, store.Put(ctx, loginAttempt{
        AuthRequestID: "V2_rot", SessionID: "s", SessionToken: "old",
        Subject: "u", ExpiresAt: time.Now().Add(time.Minute),
    }))
    require.NoError(t, store.UpdateToken(ctx, "V2_rot", "new"))
    got, err := store.Get(ctx, "V2_rot")
    require.NoError(t, err)
    require.Equal(t, "new", got.SessionToken, "the rotated token must replace the old one (spec D3)")
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd backend && go test ./internal/modules/iam/ -run TestLoginAttempt -v`
Expected: FAIL — `undefined: loginAttempt` and friends.

- [ ] **Step 4: Implement the store**

Create `backend/internal/modules/iam/loginattempt.go`. Requirements, not code — match this package's existing repository style:

- `maxFactorAttempts = 5`, a named constant with spec D6's reasoning in a comment (five, not three, because mistyping a rolling code is ordinary; five, not fifty, because the 10⁶ space erodes).
- `Get` treats an expired row as **not found** and deletes it, so expiry needs no cleanup job for correctness.
- `BumpAndGet` increments and reads in **one statement** (`UPDATE … SET factor_attempts = factor_attempts + 1 … RETURNING …`). Two statements race, and the race is in the direction that grants extra guesses.
- When the incremented value reaches `maxFactorAttempts`, delete the row and return `errAttemptsExhausted`.
- `errAttemptNotFound` and `errAttemptsExhausted` are distinct: the handler answers differently (spec "Errors").

- [ ] **Step 5: Run to verify it passes**

Run: `cd backend && go test ./internal/modules/iam/ -run TestLoginAttempt -v`
Expected: all three PASS.

- [ ] **Step 6: Prove the exhaustion test can fail**

Temporarily change `maxFactorAttempts` to `50`, rerun:

Run: `cd backend && go test ./internal/modules/iam/ -run TestLoginAttempt_BumpExhaustsAtFive -v`
Expected: **FAIL** — the fifth bump returns no error.

This proves the test binds to the boundary rather than merely exercising the code path. Revert and confirm it passes again.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/iam/
git commit -m "feat: add login_attempt store bounding factor guesses per login (#867)"
```

---

### Task 2: `VerifyTOTP` and session-factor reads in loginclient

**Files:**
- Modify: `backend/internal/modules/iam/loginclient/client.go`
- Modify: `backend/internal/modules/iam/loginclient/client_test.go`

**Interfaces:**
- Consumes: `Session{ID, Token}` (existing).
- Produces:
  - `func (c *Client) VerifyTOTP(ctx context.Context, s Session, code string) (Session, error)` — returns the session with the **rotated** token. Returns `ErrBadCredentials` for a rejected code.
  - `func (c *Client) SessionFactors(ctx context.Context, sessionID string) (Factors, error)` where `type Factors struct { Password, TOTP bool }`.

**The protocol, observed (spike §1–§3):**
```
PATCH /v2/sessions/{id}   {"sessionToken":"<current>","checks":{"totp":{"code":"123456"}}}
  → 200 {"sessionToken":"<NEW>", "details":{...}}
POST  /v2/sessions/{id}   → 405 Method Not Allowed
GET   /v2/sessions/{id}   → 200 {"session":{"factors":{"password":{"verifiedAt":...},"totp":{"verifiedAt":...}}}}
```

- [ ] **Step 1: Write the failing tests**

Add to `client_test.go`, using this file's existing fake-Zitadel harness (read how neighbouring tests stand up their test server and reuse it):

```go
// VerifyTOTP MUST return the token Zitadel sent back, not the one it was
// given. Spec D3: finalize takes the newest token, and reusing the creation
// token fails finalize AFTER a correct code — which the clinician reads as
// "my TOTP was wrong".
func TestVerifyTOTP_ReturnsTheRotatedToken(t *testing.T) {
    var gotMethod, gotBody string
    srv := fakeZitadel(t, func(w http.ResponseWriter, r *http.Request) {
        gotMethod = r.Method
        b, _ := io.ReadAll(r.Body)
        gotBody = string(b)
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"sessionToken":"ROTATED"}`))
    })
    c := newTestClient(t, srv.URL)

    out, err := c.VerifyTOTP(context.Background(), Session{ID: "s1", Token: "ORIGINAL"}, "123456")
    require.NoError(t, err)
    require.Equal(t, "ROTATED", out.Token, "the rotated token must be returned")
    require.Equal(t, "s1", out.ID)
    require.Equal(t, http.MethodPatch, gotMethod, "observed: POST to a session id is 405")
    require.Contains(t, gotBody, `"totp"`)
    require.Contains(t, gotBody, "ORIGINAL", "the CURRENT token authenticates the check")
}

func TestVerifyTOTP_WrongCodeIsBadCredentials(t *testing.T) {
    srv := fakeZitadel(t, func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusBadRequest)
        _, _ = w.Write([]byte(`{"code":3,"message":"Invalid code"}`))
    })
    c := newTestClient(t, srv.URL)
    _, err := c.VerifyTOTP(context.Background(), Session{ID: "s", Token: "t"}, "000000")
    require.ErrorIs(t, err, ErrBadCredentials)
}

func TestSessionFactors_ReportsVerifiedTOTP(t *testing.T) {
    srv := fakeZitadel(t, func(w http.ResponseWriter, r *http.Request) {
        _, _ = w.Write([]byte(`{"session":{"factors":{
            "password":{"verifiedAt":"2026-08-17T07:49:14Z"},
            "totp":{"verifiedAt":"2026-08-17T07:49:14Z"}}}}`))
    })
    c := newTestClient(t, srv.URL)
    f, err := c.SessionFactors(context.Background(), "s1")
    require.NoError(t, err)
    require.True(t, f.Password)
    require.True(t, f.TOTP)
}

// An absent factor must read as false, not as an error — a password-only
// session is a legitimate state, and spec D4 relies on distinguishing it.
func TestSessionFactors_AbsentTOTPIsFalse(t *testing.T) {
    srv := fakeZitadel(t, func(w http.ResponseWriter, r *http.Request) {
        _, _ = w.Write([]byte(`{"session":{"factors":{"password":{"verifiedAt":"2026-08-17T07:49:14Z"}}}}`))
    })
    c := newTestClient(t, srv.URL)
    f, err := c.SessionFactors(context.Background(), "s1")
    require.NoError(t, err)
    require.True(t, f.Password)
    require.False(t, f.TOTP)
}
```

If the helper names above (`fakeZitadel`, `newTestClient`) differ in this file, use the real ones — do not add duplicates.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/modules/iam/loginclient/ -run 'TestVerifyTOTP|TestSessionFactors' -v`
Expected: FAIL — `c.VerifyTOTP undefined`.

- [ ] **Step 3: Implement both methods**

In `client.go`. `VerifyTOTP` uses `http.MethodPatch` on `/v2/sessions/{id}` with the id `url.PathEscape`d (the existing `finalize` does this because the id originates as a browser-supplied query parameter — same reasoning applies). It must:

- send the **current** token as `sessionToken` in the body,
- return `Session{ID: s.ID, Token: <token from response>}`,
- map a 400 to `ErrBadCredentials`,
- never place the code or either token in an error string.

Add a doc comment recording that the token rotates, citing spike §2, so the next reader does not have to rediscover it.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/modules/iam/loginclient/ -run 'TestVerifyTOTP|TestSessionFactors' -v`
Expected: all four PASS.

- [ ] **Step 5: Prove the rotation test can fail**

Temporarily change `VerifyTOTP` to `return s, nil` on success (i.e. return the *input* session, keeping the old token — the exact defect spec D3 warns about). Rerun:

Run: `cd backend && go test ./internal/modules/iam/loginclient/ -run TestVerifyTOTP_ReturnsTheRotatedToken -v`
Expected: **FAIL** — `expected "ROTATED", got "ORIGINAL"`.

Revert and confirm it passes. Record the actual failure output.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/iam/loginclient/
git commit -m "feat: verify TOTP against a zitadel session, threading the rotated token (#867)"
```

---

### Task 3: `OutcomeFactorRequired` and sufficiency on verified factors

**Files:**
- Modify: `backend/internal/modules/iam/loginclient/sufficiency.go`
- Modify: `backend/internal/modules/iam/loginclient/sufficiency_test.go`

**Interfaces:**
- Consumes: `VerifyTOTP`, `SessionFactors` (Task 2); `LoginPolicy{ForceMFA}` (existing, already the folded `forceMfa || forceMfaLocalOnly`).
- Produces: `OutcomeFactorRequired` added **after** `OutcomeComplete`; `Result` gains `Factors []string` (e.g. `["totp"]`), non-empty only when `Outcome == OutcomeFactorRequired`.

**Do not move `OutcomeHandoff`.** It is `iota` zero deliberately: anything that forgets to set an outcome hands off, which fails closed.

- [ ] **Step 1: Write the failing tests**

```go
// A user with TOTP enrolled must now be PROMPTED, not handed off — that is
// the whole point of this spec. Previously this returned OutcomeHandoff.
func TestCompleteIfSufficient_TOTPEnrolledAsksForTheFactor(t *testing.T) {
    c := clientWithEnrolledMethods(t, []string{"AUTHENTICATION_METHOD_TYPE_TOTP", "AUTHENTICATION_METHOD_TYPE_PASSWORD"})
    res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
    require.NoError(t, err)
    require.Equal(t, OutcomeFactorRequired, res.Outcome)
    require.Equal(t, []string{"totp"}, res.Factors)
    require.Empty(t, res.CallbackURL, "nothing to redirect to until the factor is verified")
}

// A factor Helivanta cannot collect still hands off (spec D1).
func TestCompleteIfSufficient_OtpEmailStillHandsOff(t *testing.T) {
    c := clientWithEnrolledMethods(t, []string{"AUTHENTICATION_METHOD_TYPE_OTP_EMAIL", "AUTHENTICATION_METHOD_TYPE_PASSWORD"})
    res, err := c.CompleteIfSufficient(context.Background(), "V2_1", Session{ID: "s", Token: "t"})
    require.NoError(t, err)
    require.Equal(t, OutcomeHandoff, res.Outcome)
}

// OutcomeHandoff must remain the zero value: a forgotten assignment must
// fail closed.
func TestOutcomeHandoffIsZero(t *testing.T) {
    var o Outcome
    require.Equal(t, OutcomeHandoff, o)
}
```

Use this file's existing helper for a client with a stubbed policy/methods response; if none exists with that shape, extend the existing one rather than adding a parallel harness.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/modules/iam/loginclient/ -run 'TestCompleteIfSufficient_TOTP|TestOutcomeHandoffIsZero' -v`
Expected: FAIL — `undefined: OutcomeFactorRequired`.

- [ ] **Step 3: Implement**

Add the outcome and `Factors`. In `CompleteIfSufficient`, when the enrolled methods contain `AUTHENTICATION_METHOD_TYPE_TOTP` **and no method Helivanta cannot collect**, return `OutcomeFactorRequired` with `Factors: []string{"totp"}` instead of handing off. Any other non-password method still hands off.

Add `CompleteAfterFactor(ctx, authRequestID string, s Session) (Result, error)`: reads `SessionFactors`, and finalizes **only if** `TOTP` is true. Otherwise returns `OutcomeHandoff`. This keeps finalize reachable solely through a sufficiency decision, which the arch test pins.

- [ ] **Step 4: Run to verify they pass, and that nothing else broke**

```bash
cd backend && go test ./internal/modules/iam/... -count=1
```
Expected: green, including the pre-existing forceMfa and enrolled-factor tests.

- [ ] **Step 5: Prove the arch test still pins the finalize call site**

Add a second call to the unexported `finalize` from somewhere else in the package (e.g. a temporary method), then:

Run: `cd backend && go test ./internal/archtest/ -v 2>&1 | grep -i finalize`
Expected: **FAIL**, naming the extra call site.

If it passes, the arch test no longer constrains anything and that must be reported. Revert the temporary call.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/iam/loginclient/
git commit -m "feat: ask for a TOTP factor instead of handing off when it is the only one (#867)"
```

---

### Task 4: The API — `factor_required`, `POST /v1/auth/login/factor`, policy in the request response

**Files:**
- Modify: `backend/internal/modules/iam/loginui.go`
- Modify: `backend/internal/modules/iam/loginui_test.go`
- Modify: `backend/internal/bootstrap/unauthenticated.go` (registration + rate limit)

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `GET /v1/auth/login/request/:id` response gains `policies: {allow_password, require_mfa, second_factors, ignore_unknown_usernames}`.
  - `POST /v1/auth/login/password` may answer `{"factor_required": ["totp"]}`.
  - `POST /v1/auth/login/factor` — `{auth_request_id, factor, code}` → `{callback_url}` | shared refusal | attempt-expired.

- [ ] **Step 1: Write the failing handler tests**

Cover, one test each:

1. Password check returning `OutcomeFactorRequired` responds `200` with `factor_required: ["totp"]` and **no** `callback_url` and **no** `handoff_url`.
2. A `login_attempt` row is written on that outcome, holding the session id and token.
3. `POST /factor` with a good code answers `callback_url`, and the row is deleted afterwards.
4. `POST /factor` with a wrong code answers the **same** body and status as the password endpoint's shared refusal — assert byte-equality of the response body against the wrong-password case, because "same wording" is the D5 guarantee and prose drifts.
5. Five wrong codes: the fifth answers the attempt-expired shape, distinct from the refusal, and the row is gone.
6. `POST /factor` for an unknown `auth_request_id` answers attempt-expired, never a refusal (a refusal would confirm the id existed).
7. `GET /request/:id` includes `policies` with `require_mfa` reflecting `LoginPolicy.ForceMFA`.
8. **`requireMfaLocalOnly` is NOT in the response** — spec D5 folds it into `require_mfa` in Go, and exposing a field the enforcer does not consult would let the component render from a value nothing enforces.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/modules/iam/ -run TestLoginUI -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handlers**

`Password`: on `OutcomeFactorRequired`, write the `login_attempt` row (session id, current token, subject, `expires_at`) and answer `factor_required`. On `OutcomeComplete` and `OutcomeHandoff`, behaviour is unchanged.

`Factor`: look up the row; on miss or expiry answer attempt-expired. Call `VerifyTOTP`. On success, **`UpdateToken` with the rotated token**, then `CompleteAfterFactor`, then delete the row and answer `callback_url`. On a rejected code, `BumpAndGet`; `errAttemptsExhausted` → attempt-expired, otherwise the shared refusal.

Equalise the failure duration the way `respondEqualisedFailure` already does for the password path, and reuse that helper rather than writing a second one.

- [ ] **Step 4: Register the route with its own rate-limit budget**

Add to `bootstrap.MountUnauthenticated` and enumerate it in `bootstrap.UnauthenticatedRoutes` with its reason. Give it its **own** limiter prefix and a `RATE_LIMIT_FACTOR_PER_MIN` config knob — a six-digit guessing endpoint must not share a budget with anything else. Follow how `RateLimitLoginPerMin` is wired in `bootstrap/ratelimit.go`, including a doc comment with the arithmetic.

- [ ] **Step 5: Run the full backend suite**

```bash
make lint-go && make test-go && make coverage-go
```
Expected: green. `archtest.TestEveryEngineRouteIsDeclaredOrAllowlisted` must pass — if it fails, the new route is not enumerated.

- [ ] **Step 6: Prove the shared-refusal test can fail**

Change the wrong-code response message to anything different from the wrong-password one, rerun the byte-equality test:
Expected: **FAIL**. Revert.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/iam/ backend/internal/bootstrap/
git commit -m "feat: add the factor endpoint and the factor_required login outcome (#867)"
```

---

### Task 5: The login page — `AuthCredentialForm` plus the OTP step

**Files:**
- Modify: `apps/shell/lib/login-client.ts`
- Modify: `apps/shell/app/login/page.tsx`
- Modify: `apps/shell/app/login/login.test.tsx`

**Interfaces:**
- Consumes: Task 4's API.
- Produces: a two-step login UI.

**The three labels are passed explicitly:**
```tsx
<AuthCredentialForm
  loginNameLabel="Email"
  passwordLabel="Password"
  submitLabel="Sign in"
  …
/>
```
`AuthCredentialForm`'s default login-name label comes from `describeLoginName(methodPolicy)` and will not say `Email`. Omitting these fails all 12 e2e specs at the login step.

- [ ] **Step 1: Widen the outcome union**

In `login-client.ts`:

```ts
export type PasswordCheckResult =
  | { outcome: "complete"; callbackUrl: string }
  | { outcome: "handoff"; handoffUrl: string }
  | { outcome: "factorRequired"; factors: string[] };
```

Add `checkFactor({authRequestId, factor, code})` returning
`{ outcome: "complete"; callbackUrl: string } | { outcome: "refused" } | { outcome: "expired" }`.

Keep the existing doc-comment style: state why the union exists (reading the wrong field is a compile error, not a runtime `undefined`).

- [ ] **Step 2: Write the failing Vitest cases**

Using `renderWithProviders`:

1. `factorRequired` renders the OTP step and **not** an error — assert no alert/error role is present. This is the same trap the handoff outcome documents: rendering a non-failure as a failure sends a clinician to reset a correct password.
2. Submitting a code calls `checkFactor` and navigates on `complete`.
3. `refused` shows the shared refusal wording and keeps the OTP step visible.
4. `expired` returns the user to the credential step with the start-again wording.
5. The credential step exposes accessible names `Email`, `Password`, `Sign in` — query **by role and name**, not by test id, so the test binds to the same contract e2e does.

- [ ] **Step 3: Run to verify they fail**

Run: `cd apps/shell && pnpm vitest run app/login`
Expected: FAIL.

- [ ] **Step 4: Implement the page**

Replace the hand-rolled fields with `AuthCredentialForm`, keeping `AuthLayoutCentered`/`AuthCardCentered` chrome. Add the OTP step using `AuthOtpStep` (or `AuthMfaSelector` if more than one factor is ever returned — today it is always `["totp"]`, so render the single step directly and do not build a selector for one option).

Preserve every existing behaviour: the expired-auth-request state, the shared refusal, the handoff navigation via `window.location.assign`, and the signed-out/idle-ended greetings.

- [ ] **Step 5: Run the frontend suite**

```bash
pnpm turbo lint type-check test build
```
Expected: green.

- [ ] **Step 6: Prove the accessible-name test can fail**

Temporarily remove `loginNameLabel="Email"` so the policy-derived default applies, then rerun the accessible-name test:
Expected: **FAIL** — no textbox named `Email`.

This is the defect that would otherwise be found by a red e2e run reading as a broken app. Revert.

- [ ] **Step 7: Commit**

```bash
git add apps/shell/
git commit -m "feat: collect the TOTP code on Helivanta's own login page (#867)"
```

---

### Task 6: End-to-end proof against the real Zitadel

**Files:**
- Create: `e2e/tests/mfa.spec.ts`
- Modify: `scripts/seed-dev.mjs` (seed a TOTP-enrolled account)

**Interfaces:**
- Consumes: Tasks 1–5.

**Protocol facts needed here (spike §5):** enrol with `POST /v2/users/{id}/totp` (returns `secret`), verify with `POST /v2/users/{id}/totp/verify` — **not** `/totp/_verify`, which 404s. The seed PAT can create and delete users; **the login-client PAT cannot delete (403)**, which is how the spike left a user behind.

- [ ] **Step 1: Seed a TOTP account**

In `seed-dev.mjs`, add `mfa@helivanta.dev` with the same `PASSWORD` constant, enrol TOTP, verify it, and record the base32 secret where the e2e suite can read it (follow how the script already surfaces seeded values; do not print the secret to stdout unconditionally).

- [ ] **Step 2: Write the e2e spec**

`e2e/tests/mfa.spec.ts`: sign in as the TOTP account by accessible name (`Email`, `Password`, `Sign in`), assert the OTP step appears, compute the current TOTP code from the seeded secret, submit it, and then **assert against the API** — not a rendered heading — that a Helivanta session exists. The idle-timeout spec records why: the dashboard renders for a session the API refuses.

Also assert a wrong code keeps the user on the OTP step with the shared refusal.

- [ ] **Step 3: Run the full suite**

```bash
make down && RESET_YES=1 make reset && make up && make e2e
```
Expected: all existing specs plus the new one green.

If the stack will not come up, check for leftover `next dev` processes holding ports, and note that `zitadel-login` reads its PAT once at boot — if it is unhealthy, `docker restart helivanta-dev-zitadel-login-1`. Both are documented hazards, not defects in this work.

- [ ] **Step 4: Prove the finalize path actually exercises token rotation**

This is spec D3's regression proof at the integration level. Temporarily change the `Factor` handler to skip `UpdateToken` (so finalize uses the creation token), rerun **only** the MFA spec:

Run: `cd e2e && pnpm playwright test tests/mfa.spec.ts`
Expected: **FAIL** — the login does not complete despite a correct code.

If it PASSES, the rotated token is not actually required and spec D3's premise is wrong for this path — report that rather than proceeding, because it changes the design. Revert.

- [ ] **Step 5: Commit**

```bash
git add e2e/ scripts/
git commit -m "test: prove a real TOTP login completes end to end (#867)"
```

---

## Definition of done

- `make lint-go`, `make test-go`, `make coverage-go` green; `pnpm turbo lint type-check test build` green.
- `make e2e` green — all existing specs plus `mfa.spec.ts`.
- **Six** proofs **observed failing** before being trusted: the attempt-exhaustion boundary (T1), the rotated token (T2), the arch test's finalize pin (T3), the shared-refusal byte equality (T4), the accessible-name contract (T5), and the integration-level token rotation (T6).
- `login_attempt` has **no** `tenant_id` and **no** RLS policy.
- `OutcomeHandoff` is still `iota` zero.
- No `requireMfaLocalOnly` in any API response.

## Out of scope (do not build these)

Passkeys/WebAuthn (#422); email or SMS codes (delivery unverified); factor enrolment UI; `passwordChangeRequired` (#856); account lockout (#855); any import of `zitadel.ts` in the frontend (spec D5 maps policy in Go).
