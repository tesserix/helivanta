# Plan — retry a failed renewal inside the session's lifetime (#941)

Spec: `docs/superpowers/specs/2026-10-08-renewal-retry-cadence-design.md`

- [x] Backend: `expiresAtFor` (truncated to the second) in `renew.go`;
      `expires_at` on login and renew responses.
- [x] Backend tests: login/renew agreement on `expires_at`; never after the
      real cookie's `exp` (mutation: no truncation fails it).
- [x] `packages/api`: `storeExpiresAt` / `loadExpiresAt`; `clearRenewAt` also
      clears the expiry; exported.
- [x] Shell: `LoginResult.expires_at`; callback stores it; `renewSession`
      returns `expiresAt`; `retryDelayMs`; the component stores the expiry on
      success and retries with `retryDelayMs(loadExpiresAt())`.
- [x] Comments describing the fixed retry updated (component, `renew.ts`).
- [x] Tests per spec §Tests; mutations per spec.
- [x] Gates: `make lint-go`, `go test -race ./...`,
      `./scripts/coverage-gate.sh`, `pnpm turbo lint type-check test build
      format:check`.
