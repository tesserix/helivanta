# Plan: one sign-in screen (#1751)

**Spec:** [2026-11-19-single-screen-sign-in-design.md](../specs/2026-11-19-single-screen-sign-in-design.md)
**Depends on:** tesserix/onboarding-service#46 (PR #47), merged before slice 1 merges.

Two vertical slices, each backend + frontend + migration + tests in one PR.
Slice 1 replaces the first screen; slice 2 adds the new-device check. Slice 1
alone is no weaker than today: a workspace link already reaches a password card
with no emailed code, and slice 1 adds the same card for an unscoped request.

## Slice 1 — single card, server-side resolution, chooser (D1–D10)

### Backend

1. `go.mod` and `packages/api/package.json`: bump the onboarding pin to the
   commit that merged PR #47.
2. `internal/customeronboarding`: `TenantLoginTargets(ctx, email)` calls
   `POST /internal/v1/customer-signups/login-targets` with the same credential,
   size limit, `DisallowUnknownFields` and `Validate()` discipline as
   `TenantLoginSubject`. Maps 400 to `ErrUserNotFound`, any non-200 to
   `ErrUnavailable`; `truncated` is returned to the caller, never guessed at.
3. `loginclient`:
   - `DeleteSession(ctx, id, token)` for `DELETE /v2/sessions/{id}` (D3).
   - `CreatePasswordSessionForRequest`: the unscoped branch, when the login
     name contains `@`, resolves targets, refuses `truncated` with a new
     `ErrTooManyAccounts`, skips policy-disallowed candidates, checks the rest
     concurrently (bound five) with `createPasswordSession({userId})` plus the
     existing `sessionSubject` org check, and returns every accepted session.
     No `@` or no targets keeps today's login-name check. A new return type
     `[]TenantSession{TenantID, Name, Account, Session}` replaces `Session` for
     this method only; the scoped branch returns one element.
4. `loginui.go` `Password`:
   - Per-email rate bucket `sha256(lower(email))`, below ten per lockout
     window (D6); hash never logged.
   - Zero sessions: `respondEqualisedFailure`. One: today's path unchanged.
     Several: `200 account_choice_required`, sessions parked in
     `login_account_choice`.
   - `ErrTooManyAccounts`: `400 too_many_accounts`. D10's `idp_required`
     moves to its own slice (spec "Slices").
5. `POST /v1/auth/login/account` `{auth_request_id, tenant_id}`: loads the
   chosen row, deletes sibling rows and their sessions (logged on failure,
   rows still expire), then continues through `CompleteIfSufficient` exactly
   as `Password` does after one match. Public, same rate bucket as password.
6. Migration `0053_iam_login_account_choice`: a separate `login_account_choice`
   table keyed `(auth_request_id, tenant_id)` (spec D3 explains why not a
   column on `login_attempt`), swept with it. Append-only.
7. `respondLoginClientError` maps `ErrTooManyAccounts`.
8. Contracts: `platform.md`, `go run ./cmd/contractdocs`,
   `release-capabilities.json` classification for the new route, regenerate.

### Frontend

9. `RedirectLanding`: when no workspace, no `SIGNED_OUT_MARK`/`IDLE_ENDED_MARK`
   and storage readable, start an unscoped `signinRedirect` with
   `state: {purpose: "email-login"}`; otherwise show the button (D1). Discovery
   moves behind a "Find my accounts by email" link (D7).
10. `customerSignupSigninStateContract`: `email-login` purpose, no tenant (D4).
11. `LoginFlow`: credential label "Email or username" on an unscoped request;
    an `accountChoice` step for `account_choice_required` that posts to
    `/auth/login/account` via `useApiMutation` and follows `callback_url`;
    `too_many_accounts` and `idp_required` wording.
12. `login-client.ts`: response types for the two new 202 bodies and the route.

### Tests (spec "Verification", slice 1 rows)

- Go: one candidate right/wrong; two candidates same password → chooser →
  account completes and deletes the sibling; password matches one of two;
  truncated → 400 with no session created; federated skip and `idp_required`;
  no `@` fallthrough; lookup down → 503 with no session; per-email bucket
  across two IPs; five-concurrent timing within floor + measured slack
  (record the number in `loginui.go`).
- Migration test for the new key and stage.
- Vitest: unscoped auto-start once under Strict Mode; mark shows button;
  chooser posts and navigates; discovery link reaches the emailed-code flow.
- Playwright `e2e/tests/support/login.ts`: sign in from bare `/login`.

## Slice 2 — emailed code on a new device (D11)

1. Migration `NNNN_iam`: `login_device(account_subject, device_hash, last_seen)`,
   primary key `(account_subject, device_hash)`, sweep rows older than 90 days
   in the existing sweeper. Stage check gains `email_code`.
2. After a verified password and the org's own policy steps, when the account
   has no enrolled second factor and the `helivanta_device` cookie does not
   match a row: start the identity owner's existing mailbox challenge
   (`account-discovery/start`, browser hash derived from the attempt), store
   the attempt at `stageEmailCode`, answer `202 email_code_required` with the
   masked address. No new onboarding endpoint and no Helivanta mailer.
3. `POST /v1/auth/login/email-code` `{auth_request_id, code}`: verifies via
   `account-discovery/verify`, five attempts like `/factor`, then completes.
4. On every fully completed sign-in: mint or renew the cookie (`HttpOnly`,
   `Secure`, `SameSite=Strict`, 90 days, 256-bit random, stored hashed).
5. Frontend `emailCode` step in `LoginFlow`, reusing `MfaCodeForm`.
6. Tests: no factor + no cookie → code; right code completes and sets cookie;
   five wrong codes expire; enrolled factor never asked; valid cookie skips.

## Order and gates

Slice 1 PR after onboarding #47 merges. Each PR: `make lint-go`,
`go test -race ./...`, `./scripts/coverage-gate.sh`,
`go run ./cmd/contractdocs -check`, `pnpm turbo lint type-check test build
format:check`, local shell build, then CI. Production release by the
maintainer's `Release - Production` run.
