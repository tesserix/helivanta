# Plan — Helivanta never shows Zitadel's login page (#947)

Spec: `docs/superpowers/specs/2026-10-07-no-hosted-login-handoff-design.md`

## Task 1 — loginclient outcomes

- [x] `OutcomeHandoff` deleted; `OutcomeRefused` is the zero value.
- [x] `RefusalReason` (`unspecified`, `factor_unsupported`,
      `mfa_enrollment_required`, `factor_not_verified`); `Result.Reason` and
      `Result.EnrolledMethods`.
- [x] Unreadable factors, policy or session factors return an
      `ErrUnavailable`-wrapped error (`unreadable`), never a `Result`.
- [x] Tests rewritten per row; zero `Result` is a refusal.

## Task 2 — HTTP layer

- [x] `respondRefusal`: 403 with `sign_in_method_unsupported` /
      `mfa_enrollment_required` / `sign_in_incomplete`; WARN `login refused`
      with `refusal_reason` and `enrolled_methods`; unhandled outcome → ERROR,
      still refused.
- [x] Factor path deletes the `login_attempt` row on refusal.
- [x] `Handoff` handler, `handoffURL`, `hostedLoginBaseURL`, the route and its
      allowlist entry deleted; `TestHostedLoginHandoffRouteIsGone`.
- [x] `zitadelForceMFA` fixture fixed so it reaches the forceMfa branch (it
      previously exercised the unreadable path by omission).

## Task 3 — config and tooling

- [x] `ZitadelHostedLoginURL`, `HelivantaWebOrigin`, `hostedlogin.go` and its
      tests deleted; boot no longer calls `RequireDistinctHostedLoginOrigin`.
- [x] Makefile stops deriving/exporting `ZITADEL_HOSTED_LOGIN_URL` and
      `HELIVANTA_WEB_ORIGIN`.

## Task 4 — shell

- [x] `login-client.ts`: `handoff` → `blocked` (`SIGN_IN_BLOCKED_CODES`), for
      both `checkPassword` and `checkFactor`.
- [x] `page.tsx`: `blocked` ends on the start-again landing with the API's
      message; only `complete` navigates.
- [x] Vitest: each refusal code renders its message and does not navigate;
      a refused verified factor ends on the landing.

## Task 5 — docs

- [x] Superseded/amended notes on the login-client, native-MFA and org-scope
      specs; `docs/standards/backend.md` and `docs/runbooks/secrets.md`
      corrected.

## Mutations (each made the suite fail, then reverted)

- Enrolment refusal answered with the wrong code.
- Refusal log line removed.
- Unreadable policy turned into a refusal instead of an error.
- Shell: a `blocked` outcome that also navigates; a refused verified factor
  shown as a wrong code.

## Gates

- `make lint-go`, `go test -race ./...`, `./scripts/coverage-gate.sh`.
- `pnpm turbo lint type-check test build format:check`.

## Not covered

#948 (native TOTP enrolment), #422 (passkeys), #35 (email/SMS OTP), and the
logout end-session page.
