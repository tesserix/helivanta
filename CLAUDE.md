# HMS — agent rules

Binding rules for all frontend work. Full document: docs/standards/frontend.md

- Data fetching: `useApiQuery`/`useApiMutation` from `@hms/api` only. Never raw fetch/useState/setInterval in components (sole exception: shell login session POST — see standards doc).
- Forms: `useZodForm` + `Field` from `@hms/ui`, `noValidate` on every form, inline zod errors. Native browser validation is banned.
- Feedback: sonner toasts (success verb matches the button verb). `ConfirmDialog` only for destructive confirmations. `alert`/`confirm`/`prompt` are lint errors.
- Navigation: cross-zone and sidebar links are plain `<a>`. Zone nav lives only in `packages/ui/src/zones.ts`.
- Styling: design tokens only — no hardcoded colors. Read the pitfall comments in `packages/ui/styles.css` before touching sidebar/border styles.
- New zone apps: run `pnpm new-zone <name>`; never hand-copy an app.
- Every new/changed panel component needs a Vitest test using `renderWithProviders` from `@hms/api/testing`.
- Before done: `pnpm turbo lint type-check test build` green; keep `e2e/tests/smoke.spec.ts` selectors working.

## Backend rules

Full document: docs/standards/backend.md

- New modules: `make new-module NAME=<name>`; register in cmd/api/main.go AND internal/archtest/arch_test.go allModules().
- Modules never import other modules (lint + arch-test enforced). Cross-module data flows via events only.
- Tenant data only via `WithTenant`; every tenant table gets the forced-RLS boilerplate; migration IDs `NNNN_<module>`, append-only.
- Handlers: `authn.TenantPrincipal(c)` for identity; `respond.*` helpers for every response; 404 (never 403) for cross-tenant, 409 via status-guarded UPDATE, 202 for async creates.
- Events: subjects `hms.<dir>.<module>.<event>.vN`; consumers `<module>-<purpose>`; publish through the outbox inside the business tx; handlers must be idempotent.
- slog only (logrus banned); request-scoped logger via `requestid.Logger(c)`; wrap errors with `%w`.
- Before done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green.
