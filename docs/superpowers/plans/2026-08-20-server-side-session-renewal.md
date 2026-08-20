# Server-side session renewal (#916) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A signed-in clinician stays signed in. Renewal stops depending on a cross-site cookie the browser never sends, and the upstream-deactivation bound D4a bought starts actually working for the first time.

**Architecture:** Renewal becomes `POST /v1/auth/renew` — a same-origin call carrying only the Helivanta session cookie. The API re-checks the subject's state at Zitadel with the login-client PAT it already holds, re-checks OpenFGA membership, and re-mints with `idle_deadline` carried forward. The hidden iframe, `signinSilent` and the silent-renew route are deleted.

**Tech Stack:** Go 1.26, Next.js 16, Zitadel v4.15.3, Playwright.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-20-server-side-session-renewal-design.md`. D1–D6 refer to it.
- **Fail closed** (D3): if Zitadel cannot be reached to answer whether the subject is active, renewal is REFUSED, not granted. Argue the direction in a comment at the decision point.
- **D3 of the idle-timeout design is untouchable**: renewal must NOT move `idle_deadline`. `TestRenewalDoesNotMoveTheIdleDeadline` and its four siblings (`login_test.go:596-708`) stay green with no edits to their assertions. If a change requires editing those tests, the change is wrong.
- **Never store an IdP refresh token, never request `offline_access`.** D4a's two surviving properties.
- **No secret value passes through an agent session.** Read PATs inside a pipeline or shell variable; never print them.
- **Prove every assertion can fail** — mutate, watch it fail, revert, and confirm the revert landed.
- **Commit messages:** single line, conventional commits, no signature, no `Co-Authored-By`.
- **Before done:** `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green, `go test -race ./...` green, and the frontend five-task gate `pnpm turbo lint type-check test build format:check` green.

## Verified live before planning (2026-08-20, dev Zitadel v4.15.3)

Do not re-litigate these; they are observed, not assumed:

- The **login-client PAT can read `GET /v2/users/{id}`** → HTTP 200 with a `state` field.
- `state` is `USER_STATE_ACTIVE`, and after `POST /v2/users/{id}/deactivate` it reads `USER_STATE_INACTIVE`. **The check can fail**, which is what makes it a control.
- Production Zitadel session cookie is `SameSite=Lax` — the cause of #916.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/modules/iam/loginclient/client.go` | `UserState(ctx, userID)` — reads the subject's Zitadel state via the PAT |
| `backend/internal/modules/iam/renew.go` (create) | `POST /v1/auth/renew` handler |
| `backend/internal/modules/iam/renew_test.go` (create) | Handler tests, incl. fail-closed and idle-deadline carry-forward |
| `backend/internal/modules/iam/module.go` | Route registration + permission declaration |
| `apps/shell/lib/renew.ts` | Same-origin renew call; interval driven by the server's hint |
| `apps/shell/lib/oidc.ts` | Remove `silent_redirect_uri` |
| `apps/shell/app/api/auth/silent-renew/` | Delete the route and its test |
| `apps/shell/components/session-renewal.tsx` | Honour the server-supplied next-renew hint |
| `e2e/`, `docker-compose.dev.yml`, `playwright.config.ts` | Cross-site harness + renewal survival test (D6) |
| `docs/superpowers/specs/2026-08-15-zitadel-auth-design.md` | Record that D4a is amended by this spec |

---

### Task 1: Read the subject's Zitadel state

**Files:** `loginclient/client.go`, `loginclient/client_test.go`

**Interfaces:** Produces `UserState` / `IsActive`, consumed by Task 2.

**Steps:**
- [ ] Add a method reading `GET /v2/users/{id}` with the login-client PAT and returning whether the subject is active. Model it on `sessionSubject`/`enrolledMethodTypes` — same `do` call, same `url.PathEscape`, same doc-comment density citing the live verification above.
- [ ] **Fail closed, structurally:** an unreadable or unrecognised state must be an ERROR, never "active". Do not return a bare bool that a transport failure can make `false`-then-inverted into a pass. Follow `LoginPolicy`'s precedent: never a zero value with a nil error.
- [ ] Map a 404 (user deleted upstream) to "not active", not to an error — a deleted user must not renew.
- [ ] Tests against the fake Zitadel: active → active; inactive → not active; 404 → not active; 5xx/transport failure → error; a body with no `state` field → error (not a silent pass).

**Verification:** mutate the state comparison to accept any value → the inactive test fails.

---

### Task 2: `POST /v1/auth/renew`

**Files:** `iam/renew.go` (create), `iam/renew_test.go` (create), `iam/module.go`

**Interfaces:** Consumes Task 1. Produces the endpoint and response shape Task 3 consumes.

**Context the implementer needs.** Renewal today is `POST /v1/auth/login` with a fresh `id_token` (`login.go:203-331`). The new endpoint takes **no token at all** — the session cookie is the credential. It must reuse, not reimplement:
- `idleDeadlineFor` (`login.go:413-435`) — the carry-forward that keeps the idle timeout real (D4).
- The OpenFGA membership re-check that `Login` performs, so a since-revoked member is refused (`TestLogin_RenewalForSinceRevokedMemberIsRefused`, `login_test.go:297-330`).

**Steps:**
- [ ] Handler: verify the existing session cookie → subject + tenant; refuse (401) if absent, invalid, expired, or past `idle_deadline`.
- [ ] Re-check the subject's Zitadel state (Task 1). Not active → refuse. Unreadable → **refuse** (D3 fail-closed), with a comment arguing the direction at the branch.
- [ ] Re-check OpenFGA membership for the cookie's tenant; refuse if it is gone.
- [ ] Re-mint via the same signer, carrying `idle_deadline` forward through `idleDeadlineFor`.
- [ ] Response carries **when to renew next** (D5), derived from the server's own `SESSION_TTL` — not a client constant.
- [ ] Register the route and declare its permission via `*platform.Router` (`authz.Public` only if genuinely correct — argue it, since the session cookie is the credential).
- [ ] Tests: happy path re-mints; **`idle_deadline` is carried forward, not refreshed** (assert the exact value); a lapsed idle deadline is refused and NOT resurrected; an inactive Zitadel subject is refused; an unreadable Zitadel answer is refused (fail closed); a revoked member is refused; no cookie → 401.

**Verification:** mutate the carry-forward to mint a fresh deadline → the carry-forward test fails. Mutate the fail-closed branch to allow on error → the unreadable test fails.

---

### Task 3: The browser stops talking to the IdP

**Files:** `apps/shell/lib/renew.ts`, `apps/shell/lib/oidc.ts`, `apps/shell/components/session-renewal.tsx`, delete `apps/shell/app/api/auth/silent-renew/`, plus the affected tests

**Steps:**
- [ ] `renewSession` becomes a same-origin `POST /api/v1/auth/renew` with `credentials: "same-origin"`. No `signinSilent`, no `id_token`.
- [ ] **Delete** `silent_redirect_uri` (`oidc.ts:38`) and the silent-renew route + test. A mechanism that cannot work must not remain looking like a fallback (D1).
- [ ] `SessionRenewal` schedules from the server's next-renew hint (D5) rather than the hardcoded `RENEWAL_INTERVAL_MS`. Keep a sane bounded fallback if the hint is absent, and say in a comment why the bound exists.
- [ ] **CORRECTED mid-execution (2026-08-20).** This step originally said "preserve today's failure behaviour: a failed renewal still sends the user to `/login`". That is **wrong** against the endpoint Task 2 actually built, and the original wording is kept here only so the change is legible. Today's blanket redirect was safe when renewal could only fail for auth reasons; the new endpoint **fails closed on a Zitadel outage**, so a blanket redirect would evict every clinician simultaneously mid-consultation on a backend blip — #916's own harm from a different cause. A 429 from `RenewRateLimitRule` would do the same. The client must therefore distinguish *"this session is over"* (401/404 → log out) from *"the server could not answer right now"* (429/503/network → retry, bounded by the session's own `exp`, since once the cookie lapses the next request 401s anyway). Argue the policy in a comment at the decision point.
- [ ] Update `renew.test.ts`, `session-renewal.test.tsx`, `auth-exchange.test.ts` to the new shape. `auth-exchange.ts` keeps carrying the cookie for the INITIAL login exchange — do not remove that (D4).
- [ ] Frontend gate is **five** tasks: `pnpm turbo lint type-check test build format:check`.

---

### Task 4: A harness that can see this class of bug (D6)

**Files:** `docker-compose.dev.yml`, `e2e/playwright.config.ts`, `e2e/tests/`, dev bootstrap/env as needed

**Context.** `playwright.config.ts:74,94` serves the app at `localhost:4301` with the IdP at `localhost:20080`. Ports are not part of a "site", so dev is **same-site** and the production cross-site condition is never exercised. This is why no test caught #916.

**Steps:**
- [ ] Serve the app and the IdP on **different registrable domains** in dev/CI — e.g. `helivanta.localhost` and `auth.tesserix.localhost`. Chrome treats `*.localhost` as loopback (secure context) while they are distinct sites. Verify the browser really does treat them as cross-site rather than assuming it.
- [ ] E2E: sign in, remain idle across at least one renewal interval, assert **still authenticated** (not bounced to `/login`).
- [ ] **Prove this test would have caught #916**: run it against the OLD iframe renewal (stash the Task 3 change) and record that it fails. A harness fix never seen to catch the bug it was built for is not a fix. Put the observed failure in the task report and the PR body.
- [ ] Keep `e2e/tests/smoke.spec.ts` selectors working.

---

### Task 5: Close out

- [ ] Record in `docs/superpowers/specs/2026-08-15-zitadel-auth-design.md` that **D4a is amended** by this spec — mechanism changed, guarantee kept — with a pointer. Do not silently leave D4a reading as current.
- [ ] All gates green, output quoted in the PR body.
- [ ] PR body: `Closes #916`, the live-verified Zitadel facts, the Task 4 observed-red evidence, and what this does not cover (#915's initial code exchange; issuer origin unchanged).
