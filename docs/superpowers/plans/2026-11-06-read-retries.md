# No hand-drawn retries for a read: implementation plan (#1595, slice 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** every read retry or refresh outside workforce goes through `RequestFailure`,
`QueryState` or a new `RefreshButton`, and a lint rule fails CI on a new hand-drawn one.

**Architecture:** a custom ESLint rule in `packages/config` (a local plugin, not
`no-restricted-syntax`), wired into `apps/shell` with a shrinking legacy list for
workforce. Two shared UI pieces in `packages/ui`: `RefreshButton`, and a `recheck`
option on `RequestFailure`. Then the 21 non-workforce files are converted, by area.

**Tech Stack:** ESLint 10 flat config, typescript-eslint parser, React 19, React Query,
Vitest + `renderWithProviders`.

**Spec:** `docs/superpowers/specs/2026-11-06-read-retries-design.md`

## Global Constraints

- Lint message, verbatim: "A failed read renders RequestFailure or QueryState; a refresh of loaded data is RefreshButton (#1595)."
- Rule name: `helivanta/no-hand-rolled-retry`. Matched props: `onClick`, `onRetry`, `onRefresh`, `onReload`. Matched calls: a member `.refetch` or `.retry` (called or passed). Not matched: bare `retry()`, `x.mutate(...)`, `loadMore`, `reset`.
- Test files (`**/*.test.{ts,tsx,mjs}`) are exempt from the rule.
- `RefreshButton` renders nothing when `isRefusal(error)` (401, 403, 404).
- `RequestFailure` `recheck`: on 403/404 shows the refusal sentence and a button labelled `recheck` that calls `retry`; on 401 no button; never "Try again" on a refusal.
- Only emergency crew, receiving, receiving-dispatch and dispatch pass `recheck="Check access again"`.
- No `error.message` from the server is rendered on a read failure.
- A screen with a `RefreshButton` checks `isRefusal(error)` before rendering loaded data, and renders `RequestFailure` instead.
- Node v24.21.0; the gate is `pnpm turbo lint type-check test build format:check` plus `make lint-shell`.

## Review Focus

1. **A refused refresh on a screen that still holds `data`:** the stale data must disappear, not sit under a hidden button. Pinned by the "hides data" test in each area task.
2. **Recheck after access is granted:** the emergency crew screen must render the assignment once the server allows it. Pinned in Task 3.
3. **Rule false positives on mutation retries:** `retry.mutate(image)` and the dispatch command's `retry()` must stay legal. Pinned in Task 1's allow cases.
4. **A listed legacy file converted but left on the list:** it would stay exempt silently. Pinned by Task 5's staleness test.
5. **A 401 with `recheck`:** a button would invite a doomed retry. Pinned in Task 2.

---

### Task 1: The lint rule

**Files:**

- Create: `packages/config/eslint-rules/no-hand-rolled-retry.mjs`
- Modify: `packages/config/eslint.config.mjs` (export `hmsReadRetries`)
- Test: `packages/config/eslint.config.test.mjs`

**Interfaces:**

- Produces: `hmsReadRetries(files: string[], ignores?: string[]): FlatConfig`, a config object with `plugins: { helivanta: { rules: { "no-hand-rolled-retry": rule } } }`, the rule at `"error"`, and `ignores: ["**/*.test.{ts,tsx,mjs}", ...ignores]`.
- Produces: `READ_RETRY_RULE = "helivanta/no-hand-rolled-retry"` (exported, for Task 5's staleness test).

- [ ] **Step 1: Write the failing tests** in a new `describe("read retries (#1595)")`. Lint with `[...hmsEslint(dir), hmsReadRetries(["**/*.tsx"])]` (`filePath: "fixture.tsx"`) and collect the messages from `READ_RETRY_RULE`:
  - rejects (exactly one message each, matching `/RefreshButton \(#1595\)/`):
    `<Button onClick={() => void q.refetch()}>x</Button>`, `<Button onClick={rows.retry}>x</Button>`,
    `<Button onClick={() => { a.retry(); void b.refetch(); }}>x</Button>` (one report per attribute),
    `<Editor onReload={() => rows.retry()} />`, `<Detail onRefresh={scope.refetch} />`, `<List onRetry={() => q.refetch()} />`.
  - allows: `<Button onClick={() => retry.mutate(image)}>x</Button>`, `<Button onClick={() => void retry()}>x</Button>`,
    `<Button onClick={reset}>x</Button>`, `<LoadMore onClick={rows.loadMore} />`, `<Sale onRefused={sale.refetch} />`,
    `<RequestFailure message="m" retry={q.refetch} error={q.error} />`.
  - a file matched by `**/*.test.tsx` with `onClick={q.refetch}` gets no message.
- [ ] **Step 2: Run** `cd packages/config && pnpm vitest run eslint.config.test.mjs`. Expected: FAIL, `hmsReadRetries` is not exported.
- [ ] **Step 3: Implement the rule.** Visit `JSXAttribute` whose name matches `/^on(Click|Retry|Refresh|Reload)$/`; walk its value for a `MemberExpression` with non-computed `property.name` `refetch` or `retry`; report once on the attribute. Then implement `hmsReadRetries` as above.
- [ ] **Step 4: Run** the same command. Expected: PASS. Mutation-check: change the property regex to `/^refetch$/`, and the `rows.retry` case fails. Restore it.
- [ ] **Step 5: Commit** `feat(config): lint rule against hand-drawn read retries (#1595)`.

### Task 2: `RefreshButton` and `RequestFailure` `recheck`

**Files:**

- Create: `packages/ui/src/refresh-button.tsx`, `packages/ui/src/refresh-button.test.tsx`
- Modify: `packages/ui/src/request-failure.tsx`, `packages/ui/src/request-failure.test.tsx`, `packages/ui/src/index.ts`
- Modify: `packages/ui/src/staff-directory.tsx` (`onRetry` → `retry`) and its two callers, `apps/shell/components/pharmacy/staff-source.tsx` and `apps/shell/app/(workspace)/workforce/page.tsx`

**Interfaces:**

- Produces: `RefreshButton({ refresh, error, busy, variant, children }: { refresh: () => unknown; error: unknown; busy?: boolean; variant?: "outline" | "ghost" | "link"; children: ReactNode })`, default variant `outline`, `type="button"`, `disabled={busy}`.
- Produces: `RequestFailure` gains `recheck?: string`.
- Produces: `StaffDirectory` prop `retry` (was `onRetry`). Shared components name the hand-off `retry`/`refresh`, which the rule doesn't match. An `on*` name is reserved for a button the screen draws itself.

- [ ] **Step 1: Write the failing tests.**
  - `refresh-button.test.tsx`: clicking "Refresh" calls `refresh` once; `busy` disables it; `it.each([401, 403, 404])` renders no button for `new ApiError(..., status)`; a 500 `ApiError` and `null` render the button.
  - `request-failure.test.tsx`: `it.each([403, 404])("offers the recheck for a %i")`: the alert has the refusal sentence and a "Check access again" button that calls `retry`, with no "Try again". The 401 with `recheck` renders the session sentence and no button. A 500 with `recheck` still shows "Try again".
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test`. Expected: FAIL, `refresh-button` is missing and no recheck button is found.
- [ ] **Step 3: Implement.** `RefreshButton` returns `null` when `isRefusal(error)`, with a one-line comment giving the why: asking again can't change an access answer, and the screen shows the refusal. `RequestFailure`: in the refusal branch, when `recheck` is set and the status isn't 401, render a `Button variant="outline" className="mt-3"` labelled `recheck`. Export `RefreshButton`. Rename `StaffDirectory`'s `onRetry` to `retry` and update both callers.
- [ ] **Step 4: Run** the ui tests and `pnpm --filter @helivanta/shell type-check`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(ui): RefreshButton, and recheck for access that changes while open (#1595)`.

### Task 3: Emergency

**Files** (all `apps/shell/components/emergency/`): `emergency-crew.tsx`, `emergency-dispatch.tsx`, `emergency-receiving.tsx`, `emergency-receiving-dispatch.tsx`, `emergency-case-detail.tsx`, `emergency-fleet.tsx`, `emergency-review.tsx`, `emergency-workspace.tsx`, and their `*.test.tsx`. Create `emergency-case-detail.test.tsx` (none exists).

**Interfaces:** consumes `RefreshButton`, `RequestFailure` `recheck`, `firstError`, `isRefusal`.

The classification below, from the spec's sweep, is binding:

- **Failure retries → `RequestFailure`.**
  - crew "Refresh access": `error=firstError(row.error, incident.error)`, `recheck`, `refused`="Assignment or incident access is unavailable. Contact dispatch and check access before acting."
  - dispatch "Refresh dispatches", "Refresh review eligibility" and "Refresh fleet": `recheck` on all three.
  - receiving "Refresh campuses": `recheck`, `refused`="Campus access is unavailable."
  - receiving-dispatch "Receiving status is unavailable": `recheck`.
  - case-detail (both).
  - workspace "Retry bases" and "Retry reports".

  Replace every `error.message` with a written "Could not load …" message.

- **Refreshes of loaded data → `RefreshButton`.** These are crew "Refresh assignments" and "Refresh current assignment", receiving "Refresh pre-alerts" and "Refresh current pre-alert", fleet, and review. Each is gated by the same refusal check before its data renders.
- **Untouched:** dispatch's "Retry unchanged reservation" (a command retry).

- [ ] **Step 1: Write the failing tests.**
  - **crew:** an assignment read answering 403 shows "Assignment or incident access is unavailable…" and "Check access again", with no "Refresh access". After the mock answers 200 and the button is clicked, the assignment renders.
  - **dispatch, receiving, case-detail and workspace:** each has a 403 test showing a refusal, no "Try again" and no raw server text. The mock's error message is `"secret detail"`, and `queryByText(/secret detail/)` is null.
  - **The test for hidden data:** in crew, an assignment loads, then a refetch answers 403 and the assignment details are gone.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell vitest run components/emergency`. Expected: the new tests FAIL. Existing tests that click "Refresh …" buttons whose labels are kept still pass.
- [ ] **Step 3: Convert** the files per the classification. Update existing tests that asserted `error.message` text or a renamed label.
- [ ] **Step 4: Run** the same command. Expected: PASS. (Task 5's lint run proves emergency has no rule hits left.)
- [ ] **Step 5: Commit** `fix(emergency): reads fail through RequestFailure, refreshes through RefreshButton (#1595)`.

### Task 4: Supply, organization, pharmacy, country policy, dashboard, payments, settings

**Files** (under `apps/shell/`, each with its existing test):

- `components/supply/`: `organisation-operations.tsx`, `organisation-support.tsx`, `party-registration.tsx`, `platform-identity.tsx`, `organisation-structure.tsx`
- `components/organization/`: `structure.tsx`, `department-handoff.tsx`
- `components/pharmacy/`: `batch-safety.tsx`, `counter-sale.tsx`
- `components/country-policy/`: `workspace.tsx`, `detail.tsx`
- `components/dashboard/operational-dashboard.tsx`
- `components/payments/razorpay-account.tsx`
- `app/(workspace)/settings/branding/page.tsx`

**Interfaces:** consumes Task 2. Renames forwarding props to the shared names: country-policy `onRefresh` → `refresh` (`detail.tsx` renders `RefreshButton refresh={refresh} error={…}`), organisation-structure `onReload` → `refresh` on both children, and the `TerritoryEditor`'s reload-after-conflict uses `RefreshButton` with `error={null}` (the list's own refusal hides the editor).

The classification is binding:

- **Failure retries → `RequestFailure`:** branding "Could not load branding.", Razorpay "Couldn't load the payment account.", dashboard insights (its `aria-label="Retry {title}"` goes; tests find "Try again" within the insight's section) and counter-sale services.
- **Refreshes → `RefreshButton`:**
  - supply: operations, support, party-registration licences, platform-identity "Check again" and "Check workspace identity", and structure's two children
  - organization: structure ×2, department-handoff history
  - batch-safety history
  - country policy: workspace and detail
  - the dashboard "as at" link, with `variant="link"`

- [ ] **Step 1: Write the failing tests.**
  - One per area, in its existing test file: a refresh answering 403 removes the loaded rows and shows the refusal, with no refresh button. That covers supply operations, organization structure, pharmacy batch-safety history, country-policy workspace, dashboard and payments (Razorpay, a 403 on first load). Settings is branding `page.test.tsx`, also a 403 on first load.
  - counter-sale gets a 403 on its services read: no "Try again".
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell vitest run components/supply components/organization components/pharmacy components/country-policy components/dashboard components/payments "app/(workspace)/settings"`. Expected: the new tests FAIL.
- [ ] **Step 3: Convert** per the classification. Update existing tests for renamed props and labels.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `fix(shell): read retries and refreshes outside workforce go through the shared components (#1595)`.

### Task 5: Wire the rule, the workforce legacy list, docs

**Files:**

- Create: `apps/shell/retry-legacy.mjs` (`export const RETRY_LEGACY = [...]`, the workforce files the rule still hits, with a header comment in `surface-legacy.mjs`'s style), `apps/shell/retry-legacy.test.mjs`
- Modify: `apps/shell/eslint.config.mjs`, `docs/standards/frontend.md` §5, `docs/superpowers/specs/2026-11-05-refusal-states-design.md` ("Not covered" → "#1595")

**Interfaces:** consumes `hmsReadRetries`, `READ_RETRY_RULE`.

- [ ] **Step 1: Write the failing test** `retry-legacy.test.mjs`:
  - `it.each(RETRY_LEGACY)("%s still draws its own read retry")`: every entry is under `app/(workspace)/workforce/`. Linting the file with `[...hmsEslint(dir), hmsReadRetries(["**/*.tsx"])]` gives at least one `READ_RETRY_RULE` message.
  - "is never added to": length ≤ the count at creation, with no duplicates.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell vitest run retry-legacy`. Expected: FAIL, because `retry-legacy.mjs` doesn't exist.
- [ ] **Step 3: Implement.** Add `hmsReadRetries(["app/**/*.tsx", "components/**/*.tsx"], RETRY_LEGACY)` to the shell config. Fill `RETRY_LEGACY` with exactly the files `pnpm --filter @helivanta/shell lint` reports (41 workforce files; `components/dashboard/workforce-insights.tsx` is a dashboard file and is converted here instead). Docs: §5 gains a paragraph naming the rule, `RefreshButton`, `recheck` and the legacy list.
- [ ] **Step 4: Run** the test, then `pnpm --filter @helivanta/shell lint`. Expected: PASS and lint clean. Mutation-check: remove one entry from `RETRY_LEGACY`, and lint fails on that file. Add `components/payments/razorpay-account.tsx` to the list, and the staleness test fails. Restore both.
- [ ] **Step 5: Run the gate:** `pnpm turbo lint type-check test build format:check` and `make lint-shell`. Expected: 30/30 and clean.
- [ ] **Step 6: Commit** `feat(shell): enforce the read-retry rule; workforce on a shrinking legacy list (#1595)`.
