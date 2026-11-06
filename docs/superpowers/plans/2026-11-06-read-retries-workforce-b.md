# No hand-drawn retries for a read: workforce 2b (#1595)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** convert the 10 workforce files of slice 2b to `RequestFailure` and `RefreshButton`,
and take them off `apps/shell/retry-legacy.mjs`, leaving the 4 files of 2c.

**Architecture:** one shared change (`RequestFailure` gains `busy` and stops submitting forms),
then the same conversion as 2a. Each task proves its change with a test that fails first,
removes its files from `RETRY_LEGACY` and lowers the ceiling.

**Tech Stack:** React 19, React Query, `@helivanta/ui`, Vitest + `renderWithProviders`.

**Spec:** `docs/superpowers/specs/2026-11-06-read-retries-design.md` (slice 2b)

## Global Constraints

- A failed read renders `RequestFailure` with its query's `error`; a refresh of loaded data is `RefreshButton` with the read's `error`. Refresh labels stay as they are.
- `useStaffAccessCutoff` keeps precedence: it is called before any `RequestFailure` and its `STAFF_ACCESS_CHANGED` branch is unchanged.
- A retry that was disabled while a command is in flight passes the same condition as `busy`.
- No `recheck` in workforce.
- Tests that touch `posting-review-workspace.test.tsx` (also changed by open PR #1602) change only a label; new tests go in their own files.
- Stage files by path; never `git add -A`.

## Review Focus

1. **A retry inside a form** (drafts editor, submission supervisors) must not submit it. Pinned by Task 1's form test.
2. **A retry disabled while a command is in flight** must stay disabled. Pinned by Task 1's `busy` test; each converted site passes its old `disabled` condition.
3. **The account pages' identity read** has no access-change branch: a 401 must end the session with no retry. Pinned by Task 2.
4. **A converted file left listed:** caught by `retry-legacy.test.mjs`.
5. **A file removed from the list with a retry left in it:** caught by `pnpm --filter @helivanta/shell lint`.

---

Each task follows the same five steps as 2a: failing tests, run them (expected FAIL), convert and
de-list, run tests plus `pnpm --filter @helivanta/shell lint` and the `retry-legacy` test
(expected PASS), commit `fix(workforce): <group> reads fail and refresh through the shared components (#1595)`.

### Task 1: `RequestFailure` takes `busy` and never submits a form

- `packages/ui/src/request-failure.tsx`: `busy?: boolean` disables the button; both buttons are `type="button"`.
- Tests in `request-failure.test.tsx`: inside a `<form onSubmit>`, clicking "Try again" calls `retry` and not `onSubmit`; with `busy`, "Try again" (and a `recheck` button) is disabled.

### Task 2: Account pages and the posting workspace

Files: `pharmacy-relationships/page.tsx`, `posting-cutoff-page.tsx`, `posting-reviews/page.tsx` (1 each), `posting-workspace.tsx` (2).

Tests:

- New `posting-account-pages.test.tsx`, `it.each` over the three pages: a 401 permissions read shows "Your session has ended…" and no button; a 503 shows the page's message and "Try again", which asks again.
- `posting-workspace.test.tsx`: a 503 hospitals read offers "Try again", which asks again.

### Task 3: Cutoffs and approval history

Files: `posting-cutoff-workspace.tsx` (7), `posting-approval-history.tsx` (2).

- Hospitals, approved postings and cutoff history failures are `RequestFailure`; "Recheck cutoff admission" is a `RequestFailure` with the retained-command message; "Refresh cutoff access", "Refresh approved postings" and "Recheck posting cutoff" are `RefreshButton` with their old `disabled` as `busy`.
- Tests: `posting-cutoff-workspace.test.tsx`, a 503 hospitals read offers "Try again"; new `posting-approval-history.test.tsx`, a 503 history read offers "Try again", which asks again.

### Task 4: Drafts and submission

Files: `posting-drafts-panel.tsx` (3), `posting-submission-workspace.tsx` (5).

- Tests: `posting-drafts-panel.test.tsx`, a 503 drafts read offers "Try again"; the department-outage test clicks "Try again" instead of "Retry department source" (and keeps its typed reason, proving no submit). `posting-review-workspace.test.tsx:2179` looks for "Try again" in place of "Retry supervisor references".

### Task 5: Pharmacy binding

Files: `pharmacy-binding-workspace.tsx` (2), `pharmacy-binding-command.tsx` (1).

- `VerifiedRead` renders `RequestFailure`; "Recheck relationship" and "Recheck department source" are `RefreshButton`.
- Test: `pharmacy-binding-workspace.test.tsx`, a 503 hospitals read offers "Try again", which asks again.

### Task 6: Docs and gate

- `RETRY_LEGACY` holds exactly the 4 files of 2c; ceiling 4.
- `pnpm turbo lint type-check test build format:check` (expect 30/30) and `make lint-shell`.
