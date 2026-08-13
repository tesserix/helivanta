# HMS — agent rules

## Quality bar (read first, applies to everything)

Full document: docs/standards/engineering-principles.md — read it before
proposing any design or writing any code.

- **No minimal, MVP, quick or temporary solutions.** "For now", "we can harden
  it later" and "good enough to unblock" are not acceptable justifications.
  When work is too big, decompose it into *correct* slices; never build an
  incorrect small one.
- **Scope down, never quality down.** Cut the set of cases handled, never
  correctness, isolation, error handling, tests or observability. State what a
  slice does not cover, in the spec and the PR body.
- **Fail closed**, and argue the direction in a comment at the decision point.
- **Enforce structurally**: compile error > boot failure > CI failure >
  documented convention. A rule needing a developer to remember it is not a
  control.
- **Verify the claim, not a proxy.** Assert on the bytes/rows/responses
  actually produced; prove every new assertion can fail; test a claim before
  recording it as a blocker.
- **Design before code**: issue → spec in `docs/superpowers/specs/` → plan in
  `docs/superpowers/plans/` → implementation. Correct superseded docs in the
  same change.
- **Vertical slices**: backend + frontend + migration + tests in one PR.
- If a request looks like it asks for a quick fix, say what the correct
  solution is and propose a decomposition. Do not silently comply.

## Before starting any work (all agents)

Every piece of work is tracked by a GitHub issue. Before writing code:

1. **Search first — this repo has 750+ issues, so the work is usually already filed.**
   `gh issue list --state all --search "<keywords> in:title"`. Try the feature
   name, the component, and the technology (e.g. `OpenFGA`, `authorization`,
   `consent`). Read near-misses before concluding nothing exists.
2. **If an issue exists, assign it** — `gh issue edit <n> --add-assignee @me`
   — and reference it in the branch name and PR body. Never open a duplicate.
3. **Only if genuinely nothing matches, create one** — `gh issue create` —
   using the repo's existing label taxonomy: `type:*`, `product:*`, `team:*`,
   `epic:*`, and `area:*` where it applies. Match the labelling of a
   comparable issue rather than inventing labels.
4. **Link the work back:** PR bodies close their issue (`Closes #<n>`), and
   design specs record the issue they resolve in their header.

The `gh` CLI must be authenticated as the personal account `mahesh-sangawar`
for this repo.

## Frontend rules

Binding rules for all frontend work. Full document: docs/standards/frontend.md

- Data fetching: `useApiQuery`/`useApiMutation` from `@hms/api` only. Never raw fetch/useState/setInterval in components (sole exception: shell login session POST — see standards doc).
- Forms: `useZodForm` + `Field` from `@hms/ui`, `noValidate` on every form, inline zod errors. Native browser validation is banned.
- Feedback: sonner toasts (success verb matches the button verb). `ConfirmDialog` only for destructive confirmations. `alert`/`confirm`/`prompt` are lint errors.
- Navigation: cross-zone and sidebar links are plain `<a>`. Zone nav lives only in `packages/ui/src/zones.ts`.
- Permission gating uses `Can`/`usePermissions` from `@hms/api` and is convenience only — the API enforces. Every zone/page in `packages/ui/src/zones.ts` declares a permission.
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
- Every route declares a permission via `*platform.Router` (`authz.Public` to opt out); modules declare `Permissions() []authz.Grant` and never list `RoleTenantAdmin`.
- Authorization fails closed: FGA errors are 503, never fail open. 403 = member lacking permission; 404 stays the cross-tenant answer.
- Events: subjects `hms.<dir>.<module>.<event>.vN`; consumers `<module>-<purpose>`; publish through the outbox inside the business tx; handlers must be idempotent.
- slog only (logrus banned); request-scoped logger via `requestid.Logger(c)`; wrap errors with `%w`.
- Before done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green.
