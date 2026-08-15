# Login exchange rate limit — implementation plan

Spec: `docs/superpowers/specs/2026-08-16-login-rate-limit-design.md`
Issue: #841

## Tasks

1. **Config** — `backend/internal/config/config.go`: add
   `RateLimitLoginPerMin int`, `getenvInt("RATE_LIMIT_LOGIN_PER_MIN", 20)`.
2. **Rule construction** — `backend/internal/bootstrap/ratelimit.go`: add
   `LoginRateLimitRule(cfg config.Config) ratelimit.Rule` (Burst fixed at 10,
   per spec D2's arithmetic, not derived from `Rate/6`).
3. **Handler** — `backend/internal/modules/iam/login.go`: thread a
   `ratelimit.Limiter` and `ratelimit.Rule` into `LoginHandlers`; check the
   budget, keyed `"login:"+principal.Subject`, immediately after
   `h.verifier.Verify` succeeds and before `h.roles.ListRoles`. Nil limiter
   admits (D3). Refusal responds via `respond.TooManyRequests` and returns
   without reaching `ListRoles`.
4. **Wiring** — `backend/cmd/api/main.go`: pass the existing `limiter`
   (`ratelimit.NewMemory(10_000)`) and `bootstrap.LoginRateLimitRule(cfg)`
   into `iam.NewLoginHandlers`.
5. **Tests** (`backend/internal/modules/iam/login_test.go` or existing
   module test file):
   - `TestLoginThrottledMakesNoRolesListCall` — second call over budget
     never calls `ListRoles` (counting fake `RoleLister`).
   - `TestLoginRefusedOverBudget` — 429 with `Retry-After` once the burst is
     exhausted.
   - `TestLoginAdmitsWhenLimiterUnavailable` — nil limiter never blocks a
     login that would otherwise succeed.
   - `TestLoginRateLimitRuleMatchesDesign` (config/bootstrap level) — pins
     Burst=10, and the default Rate=20 from `config.Load()`.
6. **Mutations** (§5, each observed failing then reverted):
   - move the check after `ListRoles` → breaks
     `TestLoginThrottledMakesNoRolesListCall`.
   - remove the check → breaks `TestLoginRefusedOverBudget`.
   - flip nil-limiter to deny → breaks `TestLoginAdmitsWhenLimiterUnavailable`.
   - raise `RATE_LIMIT_LOGIN_PER_MIN`/burst so the e2e loop can't drain it →
     breaks `ratelimit.spec.ts`.
7. **e2e** — un-skip and rewrite `e2e/tests/ratelimit.spec.ts` to flood
   `POST /v1/auth/login` with the same id_token, reusing the existing
   flood/backoff/recovery shape.
8. **Gates** — `go build`, `go vet`, `go test -race`, coverage gate,
   `make lint-go`, Playwright x2.
