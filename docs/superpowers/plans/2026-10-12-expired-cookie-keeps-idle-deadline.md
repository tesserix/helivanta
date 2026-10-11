# Plan — An expired session cookie still decides the idle deadline (#859)

Spec: `docs/superpowers/specs/2026-10-12-expired-cookie-keeps-idle-deadline-design.md`

## Task 1 — reproduce first

- [x] `iam` login test: an expired cookie with the original `auth_time` must
      carry its deadline. Run against the current code and record the failure.

## Task 2 — `pkg/session`

- [x] `RenewalEvidence` type and `Verifier.RenewalEvidence`, sharing the key,
      algorithm and issuer checks with `Verify`.
- [x] Tests: live token, expired token (with `Verify` still refusing it),
      wrong key, wrong `kid`, wrong issuer, `alg: none`, missing `exp`, missing
      `idle_deadline`.

## Task 3 — login

- [x] `idleDeadlineFor` uses `RenewalEvidence`; doc comment's "not covered"
      paragraph replaced.
- [x] Tests: later `auth_time` still gets a fresh window; a different subject's
      expired cookie still gets a fresh window.

## Task 4 — arch test

- [x] `RenewalEvidence(` allowed only in `pkg/session` and
      `internal/modules/iam/login.go`; shown failing with a reference in
      `pkg/authn`.

## Task 5 — docs

- [x] Idle-timeout spec D3 "Not covered" paragraph points here.

## Mutations (each must fail a test, then be reverted)

- `idleDeadlineFor` back on `Verify`.
- `RenewalEvidence` skipping the signature check.
- `RenewalEvidence` skipping the issuer check.

## Gates

- `make lint-go`; `GOTOOLCHAIN=go1.26.6 ./scripts/coverage-gate.sh`.
