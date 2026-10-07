# Plan — SESSION_TTL boot-time minimum (#921)

Spec: `docs/superpowers/specs/2026-10-07-session-ttl-minimum-design.md`

## Task 1 — config guard

- [x] `internal/config/sessionttl.go`: `DefaultSessionTTL` (15m),
      `MinSessionTTL` (90s), `ErrInvalidSessionTTL`, `RequireSessionTTL()`.
- [x] `config.Load()` uses `DefaultSessionTTL` instead of the literal.
- [x] `SessionTTL`'s doc comment points readers at the accessor.
- [x] `internal/config/sessionttl_test.go` per spec §Tests.
- [x] Mutation: change `<` to `<=` and to `< 0`; observe the boundary tests fail.

## Task 2 — boot wiring

- [x] `cmd/api/main.go`: `sessionTTL, err := cfg.RequireSessionTTL()` right
      after `RequireIdleTimeout`; every `cfg.SessionTTL` use replaced with
      `sessionTTL`.
- [x] `grep cfg.SessionTTL cmd/` returns nothing.

## Task 3 — structural coupling to the renewal schedule

- [x] Compile-time assertion in `iam/renew_test.go`.
- [x] `TestRenewAtAtMinimumSessionTTLIsProportional`.
- [x] Mutation: raise `renewAtFloor` to 31s; observe the compile failure.

## Task 4 — comments

- [x] Rewrite every "#921" gap comment listed in spec D4.

## Task 5 — follow-ups

- [x] File the retry-cadence issue (spec D5, #941) and the Zitadel URL
      dev-default issue (spec D6, #942), after searching for existing ones.

## Gates

- `make lint-go`, `go test -race ./...` (DB-backed packages need Docker),
  `./scripts/coverage-gate.sh`.
- `pnpm turbo lint type-check test build format:check` (comment-only
  frontend changes, still run).
