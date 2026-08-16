# Idle Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A session with no human interaction for 15 minutes stops being usable, and the terminal returns to sign-in.

**Architecture:** The HMS session token gains an `idle_deadline` claim, enforced server-side by `authn.Middleware`. It is set at genuine login, carried forward unchanged by every re-mint (renewal, tenant switch), and moved **only** by an explicit activity call the browser makes on real interaction. Interaction tracking and the warning modal live in `@hms/ui` so every zone app inherits them.

**Tech Stack:** Go 1.26 + Gin, `golang-jwt/v5`, Ed25519 session tokens, Next.js 16 + React 19, `@hms/ui`, `@hms/api`, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-08-16-idle-timeout-design.md`
**Issue:** #848

## Global Constraints

- **The idle window is 15 minutes**, uniform. One named constant, sourced from config (`IDLE_TIMEOUT`, default `15m`), never duplicated as a literal.
- **The warning appears at `idle_deadline - 2m`.**
- **Activity calls are debounced to at most once per 60 seconds.**
- **`idle_deadline` is NEVER reset to `now` by a re-mint.** Only `POST /v1/auth/session/activity` moves it. This is spec D3 and the whole point of the feature.
- **`Mint` takes the deadline as an explicit parameter with no default** — a caller must state what it is doing. This is the structural enforcement of D3.
- Interaction = `pointerdown`, `keydown`, real scroll. **Mouse movement alone does not count.**
- A failed activity call must **never** sign a user out. The server-side deadline is the backstop.
- Teardown on expiry is **this browser only**: clear the HMS session and end the Zitadel SSO session. **Never** subject-wide revocation (that is sign-out's semantics, #781).
- New/changed panel components need a Vitest test using `renderWithProviders` from `@hms/api/testing`.
- `respond.*` helpers for every response; slog via `requestid.Logger(c)`; logrus banned; errors wrapped `%w`.
- Every new route declares a permission via `*platform.Router`, or is added to `bootstrap.UnauthenticatedRoutes` with a reason.
- Before done: `cd backend && go test -race ./...` green, `./scripts/coverage-gate.sh` exit 0, `make lint-go` clean, `pnpm turbo lint type-check test build` green.
- Single-line commit messages, no signatures, no `Co-Authored-By` trailer.

---

### Task 1: The session token carries `idle_deadline`

**Files:**
- Modify: `backend/pkg/session/session.go` (add to `Claims` and `tokenClaims`)
- Modify: `backend/pkg/session/signer.go` (`Mint` signature)
- Modify: `backend/pkg/session/verifier.go` (map the claim, refuse when absent)
- Test: `backend/pkg/session/session_test.go`

**Interfaces:**
- Produces:
  - `Claims.IdleDeadline time.Time`
  - `func (s *Signer) Mint(subject, tenantID string, authTime, idleDeadline time.Time) (string, error)`
  - Verify refuses a token with no `idle_deadline`

**A deliberate decision to implement, and to state in the doc comment:** a token WITHOUT `idle_deadline` is **invalid**, not "unlimited". Fail closed. The cost is that sessions minted before this deploys are refused, so everyone signs in again once — acceptable, and far better than a class of token that is exempt from the control.

- [ ] **Step 1: Write the failing tests**

```go
func TestMintCarriesIdleDeadlineThroughVerify(t *testing.T) {
	s, v := newSignerVerifier(t)
	authTime := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	deadline := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)

	raw, err := s.Mint("sub-1", "11111111-1111-1111-1111-111111111111", authTime, deadline)
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	got, err := v.Verify(raw)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !got.IdleDeadline.Equal(deadline) {
		t.Errorf("IdleDeadline = %v, want %v", got.IdleDeadline, deadline)
	}
}

func TestMintRefusesAZeroIdleDeadline(t *testing.T) {
	s, _ := newSignerVerifier(t)
	_, err := s.Mint("sub-1", "11111111-1111-1111-1111-111111111111", time.Now(), time.Time{})
	if err == nil {
		t.Fatal("Mint() error = nil, want a refusal: a mint with no idle deadline would be exempt from the timeout")
	}
}

// Fail closed. A token predating this claim must not be treated as
// "no idle limit" — that would be a class of session the control cannot reach.
func TestVerifyRefusesATokenWithNoIdleDeadlineClaim(t *testing.T) {
	_, v := newSignerVerifier(t)
	raw := mintLegacyTokenWithoutIdleDeadline(t) // helper: signs tokenClaims without the field
	if _, err := v.Verify(raw); err == nil {
		t.Fatal("Verify() error = nil, want a refusal for a token carrying no idle_deadline")
	}
}
```

Write `newSignerVerifier` and `mintLegacyTokenWithoutIdleDeadline` following the existing helper style in that file.

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./pkg/session/... -run Idle -v
```
Expected: compile failure — `Mint` takes 3 arguments.

- [ ] **Step 3: Implement**

Add to `Claims`:

```go
	// IdleDeadline is when this session stops being usable without
	// further human interaction (spec D2, #848). It is set at genuine
	// login and CARRIED FORWARD unchanged by every re-mint — renewal
	// and tenant switch included — exactly as AuthTime is. A re-mint
	// that reset it to time.Now() would let an untouched tab renew
	// itself forever and the timeout would never fire, while every
	// test that only checks "renewal works" still passed. That is spec
	// D3, and it is the reason Mint takes this as an explicit
	// parameter with no default: a caller must say what it is doing.
	IdleDeadline time.Time
```

Add `IdleDeadline int64 \`json:"idle_deadline"\`` to `tokenClaims`. Extend `Mint` with the parameter and refuse a zero value alongside the existing checks. In `Verify`, refuse when `claims.IdleDeadline == 0`, and map it to `time.Unix(...).UTC()`.

- [ ] **Step 4: Run and watch them pass**

```bash
cd backend && go test -race ./pkg/session/... -v
```

- [ ] **Step 5: Prove the fail-closed test discriminates**

Make `Verify` default an absent claim to `time.Now().Add(time.Hour)` instead of refusing. Re-run: `TestVerifyRefusesATokenWithNoIdleDeadlineClaim` must FAIL. Restore.

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/session/
git commit -m "feat: HMS session tokens carry an idle deadline (#848)"
```

---

### Task 2: The middleware refuses an idle-expired session

**Files:**
- Modify: `backend/pkg/authn/authn.go` (the check, and `Principal`)
- Test: `backend/pkg/authn/authn_test.go`

**Interfaces:**
- Consumes: `session.Claims.IdleDeadline`
- Produces: `Principal.IdleDeadline time.Time`

- [ ] **Step 1: Write the failing tests**

```go
func TestMiddlewareRefusesASessionPastItsIdleDeadline(t *testing.T) {
	rec := doRequest(t, principalWithIdleDeadline(time.Now().Add(-1*time.Second)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a session past its idle deadline", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "session_idle") {
		t.Errorf("body = %s, want a distinguishable idle reason", rec.Body.String())
	}
}

func TestMiddlewareAdmitsASessionInsideItsIdleDeadline(t *testing.T) {
	rec := doRequest(t, principalWithIdleDeadline(time.Now().Add(5*time.Minute)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 well inside the idle window", rec.Code)
	}
}
```

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./pkg/authn/... -run Idle -v
```

- [ ] **Step 3: Implement**

Add `IdleDeadline` to `Principal`, populate it from the claims, and add the check immediately after the existing revocation check, following its shape and comment density:

```go
		// Idle timeout (#848, spec D2). Server-side and authoritative:
		// a killed tab, a suspended laptop or a stolen cookie replayed
		// by a direct API caller all fail closed here, because the
		// deadline travels inside the signed token rather than living
		// in a timer the browser owns.
		//
		// A DISTINCT error code, not the generic one: the frontend must
		// be able to tell "your session went idle" from "your
		// credentials are bad", because the two need different wording
		// on a shared terminal — see spec D6.
		if !p.IdleDeadline.IsZero() && !time.Now().Before(p.IdleDeadline) {
			requestid.Logger(c).InfoContext(c.Request.Context(), "refused an idle session",
				"subject", p.Subject, "idle_deadline", p.IdleDeadline, "path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "session_idle", "message": "your session ended after a period of inactivity"})
			return
		}
```

- [ ] **Step 4: Run and watch them pass**

```bash
cd backend && go test -race ./pkg/authn/... -v
```

- [ ] **Step 5: Prove the test discriminates**

Invert the comparison to `time.Now().Before(...)`. Re-run: `TestMiddlewareRefusesASessionPastItsIdleDeadline` must FAIL. Restore.

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/authn/
git commit -m "feat: refuse a session past its idle deadline (#848)"
```

---

### Task 3: Every re-mint carries the deadline forward — spec D3

**This is the task the feature lives or dies by.**

There are two PRODUCTION `Mint` call sites, and both must carry rather than reset:
`internal/modules/iam/login.go` and `internal/modules/iam/me.go`.

**Correction to an earlier draft of this plan, which said "exactly two":** four
TEST files also call `Mint` and will not compile until they are updated —
`pkg/authn/session_verifier_test.go` (three calls), `internal/bootstrap/chain_test.go`,
and `internal/modules/iam/me_test.go`. The whole backend build is red from the end
of Task 1 until this task lands. Update those call sites to pass a deadline;
choose a value that keeps each test's intent intact (a comfortably-future
deadline where the test is not about idling), and do not weaken any existing
assertion to make it compile.

**Files:**
- Modify: `backend/internal/modules/iam/login.go:210` (login AND renewal — the same endpoint)
- Modify: `backend/internal/modules/iam/me.go:210` (tenant switch)
- Modify: `backend/internal/config/config.go` (`IdleTimeout`)
- Test: `backend/internal/modules/iam/login_test.go`, `me_test.go`

**The crux, which the implementer must understand before writing code:** renewal (spec D4a) is `POST /v1/auth/login` run again with a fresh Zitadel token — the *same handler* as a first login. So the handler cannot tell them apart by route. The discriminator is **the caller's existing HMS session cookie**:

- A valid, non-idle-expired session cookie on the request ⇒ this is a re-mint (renewal). **Carry its `idle_deadline` forward unchanged.**
- No cookie, or one that fails verification ⇒ a genuine new login. **Set `idle_deadline = now + IdleTimeout`.**

Carrying forward can only ever keep or shorten the window, never extend it, so this cannot be abused. And an already-idle-expired cookie fails verification, so it falls to the "genuine login" branch — which is correct: the human is right there, signing in.

- [ ] **Step 1: Write the failing tests**

```go
// THE test for spec D3. An untouched tab renews every 5 minutes; if
// renewal moved the deadline, the timeout would never fire and every
// other test would still pass.
func TestRenewalDoesNotMoveTheIdleDeadline(t *testing.T) {
	env := newLoginEnv(t)
	first := env.login(t)                       // genuine login, sets deadline
	original := env.deadlineOf(t, first)

	for i := 0; i < 3; i++ {
		next := env.renewWith(t, first)         // POST /v1/auth/login carrying the session cookie
		got := env.deadlineOf(t, next)
		if !got.Equal(original) {
			t.Fatalf("renewal %d moved the idle deadline: got %v, want %v unchanged — "+
				"an untouched tab would renew itself forever and the timeout would never fire",
				i+1, got, original)
		}
		first = next
	}
}

func TestGenuineLoginSetsAFreshIdleDeadline(t *testing.T) {
	env := newLoginEnv(t)
	tok := env.login(t) // no session cookie on the request
	got := env.deadlineOf(t, tok)
	want := time.Now().Add(env.cfg.IdleTimeout)
	if got.Before(want.Add(-30*time.Second)) || got.After(want.Add(30*time.Second)) {
		t.Errorf("idle deadline = %v, want ~%v for a genuine login", got, want)
	}
}

func TestTenantSwitchCarriesTheIdleDeadlineForward(t *testing.T) {
	env := newMeEnv(t)
	before := env.sessionWithDeadline(t, time.Now().Add(4*time.Minute))
	after := env.switchTenant(t, before)
	if !env.deadlineOf(t, after).Equal(env.deadlineOf(t, before)) {
		t.Error("tenant switch reset the idle deadline; switching hospitals is not human activity on a timer")
	}
}
```

Write the env helpers following the existing helper style in those test files.

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./internal/modules/iam/... -run "IdleDeadline" -v
```

- [ ] **Step 3: Add config**

`IdleTimeout time.Duration` from `IDLE_TIMEOUT`, default `15 * time.Minute`, using the same `getenvDuration` pattern as `SessionTTL`. Document that it is a clinical-workflow value (spec D1), not a technical one, and that it is independent of `SESSION_TTL` despite currently sharing a value.

- [ ] **Step 4: Implement both call sites**

In `login.go`, before minting, attempt to verify the inbound session cookie and carry its deadline; otherwise compute a fresh one. Comment it with the reasoning above — that renewal and login are the same endpoint and the cookie is the discriminator.

In `me.go`'s tenant switch, pass `p.IdleDeadline` straight through.

- [ ] **Step 5: Run and watch them pass**

```bash
cd backend && go test -race ./internal/modules/iam/... -v
```

- [ ] **Step 6: Prove the D3 test can fail — MANDATORY**

Change `login.go` to always mint `time.Now().Add(cfg.IdleTimeout)`. Re-run:

```bash
cd backend && go test ./internal/modules/iam/... -run TestRenewalDoesNotMoveTheIdleDeadline -v
```

Expected: **FAIL** with "renewal 1 moved the idle deadline". Restore and confirm green.

Do not skip this. This project has repeatedly found tests that passed while proving nothing, and D3 is invisible when broken — the application looks perfect while the timeout never fires.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/
git commit -m "feat: carry the idle deadline through renewal and tenant switch (#848)"
```

---

### Task 4: `POST /v1/auth/session/activity`

**Files:**
- Create: `backend/internal/modules/iam/activity.go`
- Create: `backend/internal/modules/iam/activity_test.go`
- Modify: `backend/internal/modules/iam/module.go` (register the route)
- Modify: `backend/cmd/api/main.go` (wire the handler)

**Interfaces:**
- Produces: `POST /v1/auth/session/activity` → `200 {"idle_deadline":"<RFC3339>"}`

This route sits INSIDE the authenticated chain (it requires a live session), so it is declared through `*platform.Router` like any other route rather than added to `UnauthenticatedRoutes`. It needs no permission beyond membership — use the codebase's existing convention for a self-service route (see how `me.go`'s routes are declared).

- [ ] **Step 1: Write the failing tests**

```go
func TestActivityExtendsTheIdleDeadline(t *testing.T) {
	env := newActivityEnv(t)
	before := time.Now().Add(3 * time.Minute)
	rec := env.post(t, env.sessionWithDeadline(t, before))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := env.deadlineFromBody(t, rec)
	if !got.After(before) {
		t.Errorf("idle_deadline = %v, want later than %v", got, before)
	}
}

func TestActivityCarriesSubjectTenantAndAuthTimeUnchanged(t *testing.T) {
	env := newActivityEnv(t)
	before := env.sessionWithDeadline(t, time.Now().Add(3*time.Minute))
	after := env.reissued(t, env.post(t, before))
	if after.Subject != before.Subject || after.TenantID != before.TenantID || !after.AuthTime.Equal(before.AuthTime) {
		t.Errorf("activity changed identity: got %+v, want sub/tenant/auth_time from %+v", after, before)
	}
}

// auth_time must never be laundered into a fresh one — the #781
// revocation watermark compares against exactly that value.
func TestActivityDoesNotResetAuthTime(t *testing.T) { /* as above, asserted alone */ }

func TestActivityRefusesAnAlreadyIdleSession(t *testing.T) {
	env := newActivityEnv(t)
	rec := env.post(t, env.sessionWithDeadline(t, time.Now().Add(-1*time.Second)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — an expired session must not be revivable by claiming activity", rec.Code)
	}
}
```

- [ ] **Step 2: Run and watch them fail**

```bash
cd backend && go test ./internal/modules/iam/... -run Activity -v
```

- [ ] **Step 3: Implement**

Read the principal, mint with `idleDeadline = now + cfg.IdleTimeout` and everything else carried through, set the session cookie exactly as `login.go` does (same flags, same helper), and return the new deadline in the body. The doc comment must say why the deadline is returned in the body: the cookie is `httpOnly`, so the browser has no other way to know when to warn.

- [ ] **Step 4: Run and watch them pass**

- [ ] **Step 5: Prove the refusal test discriminates**

Remove the idle check from the middleware chain for this route. `TestActivityRefusesAnAlreadyIdleSession` must FAIL. Restore.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/ backend/cmd/
git commit -m "feat: an activity endpoint that extends the idle deadline (#848)"
```

---

### Task 5: Interaction tracking in `@hms/ui`

**Files:**
- Create: `packages/ui/src/idle-timer.ts` (framework-free timer + broadcast logic)
- Create: `packages/ui/src/idle-timer.test.ts`
- Modify: `packages/ui/src/hms-shell.tsx` (mount it)

**Interfaces:**
- Produces:
  - `ACTIVITY_DEBOUNCE_MS = 60_000`, `WARNING_LEAD_MS = 120_000`
  - `createIdleTracker({ onActivity, onWarn, onExpire })` returning `{ start, stop, noteDeadline(d: Date) }`

Keep the timer logic in a plain module with no React, so it is testable without a DOM harness; the component is a thin mount.

- [ ] **Step 1: Write the failing tests**

```ts
it("calls onActivity at most once per debounce window under a burst", () => {
  vi.useFakeTimers();
  const onActivity = vi.fn();
  const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
  t.start();
  for (let i = 0; i < 50; i++) window.dispatchEvent(new Event("keydown"));
  expect(onActivity).toHaveBeenCalledTimes(1);
  vi.advanceTimersByTime(ACTIVITY_DEBOUNCE_MS + 1);
  window.dispatchEvent(new Event("keydown"));
  expect(onActivity).toHaveBeenCalledTimes(2);
});

it("does not treat mouse movement alone as activity", () => {
  const onActivity = vi.fn();
  const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
  t.start();
  window.dispatchEvent(new Event("mousemove"));
  expect(onActivity).not.toHaveBeenCalled();
});

it("warns at the lead time before the deadline and expires at it", () => {
  vi.useFakeTimers();
  const onWarn = vi.fn(), onExpire = vi.fn();
  const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire });
  t.start();
  t.noteDeadline(new Date(Date.now() + 5 * 60_000));
  vi.advanceTimersByTime(3 * 60_000 - 1);
  expect(onWarn).not.toHaveBeenCalled();
  vi.advanceTimersByTime(2);
  expect(onWarn).toHaveBeenCalledTimes(1);
  vi.advanceTimersByTime(2 * 60_000);
  expect(onExpire).toHaveBeenCalledTimes(1);
});

it("a later deadline from another tab cancels a pending warning", () => {
  vi.useFakeTimers();
  const onWarn = vi.fn();
  const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire: vi.fn() });
  t.start();
  t.noteDeadline(new Date(Date.now() + 3 * 60_000));
  t.noteDeadline(new Date(Date.now() + 15 * 60_000)); // another tab was active
  vi.advanceTimersByTime(2 * 60_000);
  expect(onWarn).not.toHaveBeenCalled();
});
```

- [ ] **Step 2: Run and watch them fail**

```bash
pnpm --filter @hms/ui test -- idle-timer
```

- [ ] **Step 3: Implement**

Listen for `pointerdown`, `keydown`, `scroll` (passive). Debounce `onActivity`. Schedule `onWarn`/`onExpire` from the last noted deadline. Broadcast noted deadlines over a `BroadcastChannel("hms.session")` so every tab and zone app shares one timer, falling back to per-tab timers when unavailable. Comment why mouse movement is excluded (a sleeve on a desk is not a clinician).

- [ ] **Step 4: Run and watch them pass**

- [ ] **Step 5: Prove one test discriminates**

Remove the debounce. The burst test must FAIL with 50 calls. Restore.

- [ ] **Step 6: Commit**

```bash
git add packages/ui/
git commit -m "feat: shared idle interaction tracking for every HMS app (#848)"
```

---

### Task 6: The warning modal

**Files:**
- Create: `packages/ui/src/idle-warning.tsx`
- Create: `packages/ui/src/idle-warning.test.tsx`
- Modify: `packages/ui/src/hms-shell.tsx`

- [ ] **Step 1: Write the failing tests**

```tsx
it("shows a live countdown and a Stay signed in button", async () => {
  renderWithProviders(<IdleWarning secondsRemaining={120} onStay={vi.fn()} />);
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /stay signed in/i })).toBeInTheDocument();
  expect(screen.getByText(/2:00|120/)).toBeInTheDocument();
});

it("calls onStay when the button is pressed", async () => {
  const onStay = vi.fn();
  const user = userEvent.setup();
  renderWithProviders(<IdleWarning secondsRemaining={120} onStay={onStay} />);
  await user.click(screen.getByRole("button", { name: /stay signed in/i }));
  expect(onStay).toHaveBeenCalledTimes(1);
});
```

- [ ] **Step 2: Run and watch them fail**

- [ ] **Step 3: Implement**

Use the design system's dialog primitive (`ConfirmDialog` is for destructive confirmations — check `docs/standards/frontend.md` for which primitive applies here). Design tokens only. The wording must say the session will end because of inactivity, and must not imply the user did something wrong.

- [ ] **Step 4: Run and watch them pass**

- [ ] **Step 5: Commit**

```bash
git add packages/ui/
git commit -m "feat: warn two minutes before an idle session ends (#848)"
```

---

### Task 7: Teardown on expiry, and honest wording on `/login`

**Files:**
- Modify: `packages/ui/src/hms-shell.tsx` (wire `onExpire`)
- Modify: `packages/ui/src/zitadel-session.ts` (an idle mark alongside `SIGNED_OUT_MARK`)
- Modify: `apps/shell/app/login/page.tsx` (the third message)
- Modify: `apps/shell/app/login/login.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
it("says the session ended through inactivity, not that the user signed out", async () => {
  window.sessionStorage.setItem(IDLE_ENDED_MARK, "1");
  renderWithProviders(<LoginPage />);
  expect(await screen.findByText(/ended after a period of inactivity/i)).toBeInTheDocument();
  expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run and watch it fail**

- [ ] **Step 3: Implement**

`onExpire` clears the HMS session (`POST /logout`) and then calls `endZitadelSession()`, setting `IDLE_ENDED_MARK` first — mirroring how `SIGNED_OUT_MARK` is set before `signoutRedirect()` navigates away. `/login` now distinguishes three states: idle-ended, signed out, and neither. Do not collapse them; #850 exists because claiming someone signed out when they did not is untrue, and this is the same class of lie.

- [ ] **Step 4: Run and watch it pass**

- [ ] **Step 5: Full frontend gate**

```bash
pnpm turbo lint type-check test build
```

- [ ] **Step 6: Commit**

```bash
git add packages/ui/ apps/shell/
git commit -m "feat: end an idle session in this browser and say so on /login (#848)"
```

---

### Task 7b: Pin the cookie that D3's discriminator depends on

**Added after Task 5's review.** Small, but it protects the spec's single most
important rule.

**Files:**
- Modify: `apps/shell/lib/auth-exchange.ts`
- Test: `apps/shell/lib/auth-exchange.test.ts`

**The problem.** The backend distinguishes a renewal from a genuine login by
whether the request carries the HMS session cookie (spec D3). Task 5 verified
that it does today — `exchangeIdToken` calls `fetch("/api/v1/auth/login", …)`
with a relative same-origin URL, and fetch's default credentials mode is
`same-origin`, so the cookie is sent.

But nothing states it and nothing tests it. A refactor to an absolute URL, a
different origin, or `credentials: "omit"` would make **every renewal take the
fresh-window branch**, so an untouched tab would extend itself forever — the
exact failure D3 exists to prevent — **with every backend test still green.**

This repo's ladder is compile error > boot failure > CI failure > documented
convention. Right now this sits on the bottom rung, held up by a language
default, and its failure mode is silent. Two lines move it to CI-failure level.

- [ ] **Step 1: Write the failing test**

```ts
it("sends the session cookie, because the backend's renewal-vs-login discriminator depends on it", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ ok: true }), { status: 200 }),
  );
  await exchangeIdToken("id-token", "11111111-1111-1111-1111-111111111111");

  const [url, init] = fetchSpy.mock.calls[0];
  expect(String(url).startsWith("/")).toBe(true);           // same-origin, relative
  expect((init as RequestInit).credentials).toBe("same-origin");
});
```

- [ ] **Step 2: Run and watch it fail**

```bash
pnpm --filter shell test -- auth-exchange
```
Expected: FAIL — `credentials` is `undefined`.

- [ ] **Step 3: Make it explicit**

Add `credentials: "same-origin"` to the `fetch` init, with a comment naming
spec D3: the backend reads the session cookie on this request to decide whether
this is a renewal (carry the idle deadline forward) or a genuine login (fresh
window), so dropping the cookie silently disables the idle timeout while every
backend test stays green.

- [ ] **Step 4: Run and watch it pass**

- [ ] **Step 5: Prove the test discriminates**

Change it to `credentials: "omit"`. The test must FAIL. Restore.

- [ ] **Step 6: Commit**

```bash
git add apps/shell/lib/
git commit -m "fix: pin the session cookie the idle-deadline discriminator depends on (#848)"
```

---

### Task 8: End-to-end proof, and the documentation loop

**Files:**
- Create: `e2e/tests/idle-timeout.spec.ts`
- Modify: `scripts/seed-dev.mjs` (the new spec's account pair — the suite derives accounts from spec filenames)
- Modify: `docs/standards/backend.md`, `docs/standards/frontend.md`

- [ ] **Step 1: Write the spec**

Run the API with a short `IDLE_TIMEOUT` (e.g. `20s`) for this spec only, so the test does not sit for fifteen minutes. Assert, in order:

1. sign in, confirm an authenticated API call succeeds;
2. do nothing past the window;
3. the next API call is refused — **assert against the API**, not the dashboard heading, because the dashboard renders for a session the API refuses (this repo has been bitten by exactly that proxy assertion);
4. the browser lands on `/login` with the inactivity wording;
5. signing in again requires credentials.

Remember `make seed` derives accounts from spec filenames, so a new spec file needs `make seed` re-run.

- [ ] **Step 2: Run it**

```bash
export NODE_AUTH_TOKEN=$(gh auth token)
make up && make seed
pnpm --filter e2e exec playwright test idle-timeout --reporter=list
```

- [ ] **Step 3: Run the whole suite**

```bash
pnpm --filter e2e exec playwright test --reporter=list
```
Expected: 12 passed (the existing 11 plus this one).

- [ ] **Step 4: Document**

Add to `docs/standards/backend.md`: `idle_deadline` is carried forward by every re-mint and moved only by the activity endpoint, and `Mint` takes it explicitly so a caller must state its intent. Add to `docs/standards/frontend.md`: interaction tracking lives in `@hms/ui` so every zone app inherits it, and a failed activity call must never sign a user out.

- [ ] **Step 5: Commit and open the PR**

```bash
git add e2e/ scripts/ docs/
git commit -m "test: prove an untouched session ends and returns to sign-in (#848)"
git push -u origin feat/848-idle-timeout
```

PR body: what shipped, that renewal deliberately does not count as activity, that teardown is this-browser-only rather than subject-wide, what is not covered (OS screen lock, in-place re-authentication), and `Closes #848`.

---

## Self-Review

**Spec coverage:** D1 → Task 3 (config constant). D2 → Tasks 1, 2. D3 → Task 3, with a mandatory proven-failing test. D4 → Tasks 4, 5. D5 → Tasks 5, 6. D6 → Task 7. D7 → Tasks 5, 6 (both land in `@hms/ui`). Errors-and-failure section → Task 5 (failed call does not sign out) and Task 4 (401 handling). Testing section → Tasks 1–8.

**Placeholders:** none. Where a codebase convention must be followed rather than invented (the dialog primitive, the self-service route declaration, test helper style), the plan names the file to read rather than guessing at an API.

**Type consistency:** `Mint(subject, tenantID string, authTime, idleDeadline time.Time)` is used identically in Tasks 1, 3 and 4. `Claims.IdleDeadline` / `Principal.IdleDeadline` are consistent across Tasks 1, 2, 4. `createIdleTracker`'s `{ start, stop, noteDeadline }` and `ACTIVITY_DEBOUNCE_MS` / `WARNING_LEAD_MS` are consistent across Tasks 5, 6, 7.
