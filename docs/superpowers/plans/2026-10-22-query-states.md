# Query States Implementation Plan

**Goal:** One component renders a paged list's loading, failed, stale, empty and filled states, and is the only way to render an empty state.

**Spec:** `docs/superpowers/specs/2026-10-22-query-states-design.md` (#1276)

## Task 1: `QueryState` and `RequestFailure` in `@helivanta/ui` (D1, D2, D4)

- `packages/ui/src/request-failure.tsx`: moved unchanged from `apps/shell/components/request-failure.tsx`.
- `packages/ui/src/query-state.tsx`: `QueryState<T>({ query, label, empty: { icon, title, hint? }, children: (items: T[]) => ReactNode })`.
- Tests in `query-state.test.tsx`, one per D1 branch:
  - pending: a status named "Loading visits…", with no empty text and no items;
  - failed and empty: an alert "Could not load visits.", no empty text, and Try again calls `retry`;
  - failed with items: the items, plus the "may be out of date" alert;
  - empty: the empty title;
  - filled: the items.
- `index.ts`: export `QueryState` and `RequestFailure`; stop exporting `EmptyState` (D3).

## Task 2: Move every caller (D3, D4)

- The 8 `EmptyState` sites move to `QueryState`: order-list, ping-panel, visit-panel, dispense-list, medications-panel, and the benefits dependents, enrollments and plans panels.
- The 23 `RequestFailure` imports point at `@helivanta/ui`, and the shell file is deleted.
- Each of the five lists that were false-empty gets a panel test: a failing list request shows "Could not load …" and not the empty title.

## Verify

`pnpm turbo lint type-check test build format:check`, `make lint-shell`, then the full Playwright suite on a production build.
