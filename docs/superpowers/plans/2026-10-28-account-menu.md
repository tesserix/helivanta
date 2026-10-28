# Account menu plan

Spec: `docs/superpowers/specs/2026-10-28-account-menu-design.md` (#1428).

1. **`loginclient.UserProfile`.** Write the tests first, then implement.
   - Parse `user.human.profile.displayName` and `user.human.email.email` from
     `GET /v2/users/{id}`.
   - A body with no `human` object, a 404, a 5xx or a transport failure is an
     error wrapping `ErrUnavailable`.
   - Escape the id with `url.PathEscape`.
2. **`GET /v1/iam/me/profile`.** Write the tests first, then implement.
   - Add an `iam.UserProfileReader` and set it with `Module.SetUserProfileReader`.
   - `bootstrap.Modules` takes an `iam.IdentityProvider` (state checker plus
     profile reader), so the one login client feeds both.
   - The bootstrap test asserts the same instance landed in both setters.
3. **Contracts.** Run `go run ./cmd/contractdocs`, then `-check`. Mention the
   route in `docs/api/domains/platform.md`.
4. **`AccountMenu`** in `packages/ui`. Write the Vitest tests first.
   - Parse the response with a zod schema; a failed parse shows "unavailable".
   - Replace the header Sign out link with the menu.
   - Update the `hms-shell.test.tsx` sign-out tests to open the menu first.
5. **e2e.** Add `support/sign-out.ts`, use it in `journey`/`signout`, and assert
   the menu names the seeded user.
6. **Gates.**
   - `make lint-go`
   - `go test -race ./...` for the affected packages
   - `./scripts/coverage-gate.sh`
   - `pnpm turbo lint type-check test build format:check` on Node 24
   - Verify in the browser against the local stack where it is available.
