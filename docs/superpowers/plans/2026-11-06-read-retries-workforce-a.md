# No hand-drawn retries for a read: workforce 2a (#1595)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** convert the 28 workforce files of slice 2a to `RequestFailure`, `QueryState` and
`RefreshButton`, and take them off `apps/shell/retry-legacy.mjs`.

**Architecture:** no new shared code. Each task converts one group of files the same way as
slice 1 (#1597), proves a refusal with a test that fails first, removes the files from
`RETRY_LEGACY` and lowers its ceiling. Lint then holds the converted files to the rule.

**Tech Stack:** React 19, React Query, `@helivanta/ui`, Vitest + `renderWithProviders`.

**Spec:** `docs/superpowers/specs/2026-11-06-read-retries-design.md` (slice 2a)

## Global Constraints

- A failed read renders `RequestFailure` (or `QueryState`) with its query's `error`; a refresh of loaded data is `RefreshButton` with the read's `error`.
- A screen with a `RefreshButton` checks `isRefusal(error)` before rendering the data it holds, including forms built from it.
- No `recheck` in workforce: access there doesn't change while the screen is open.
- Wording that already reads as a refusal goes in as `refused`; otherwise the screen's message stays as `message`.
- A child that refreshes for its parent takes a `refresh` prop and passes the parent's query `error`, not `null` (slice 1's deferred minor).
- Mutation retries stay as they are.
- Each task removes its files from `RETRY_LEGACY` and lowers the "is never added to" ceiling to the new length.
- Stage files by path; never `git add -A` (`apps/shell/AGENTS.md` is untracked and stays so).

## Review Focus

1. **A refused background re-read on a screen holding data:** rows and forms built from them must go, not only the button. Pinned by each task's "load, then refuse" test.
2. **Multi-query panels** (setup, shifts, access assignment forms, person reviews): a refusal from any query wins over another's outage. Pinned by using `firstError` and one test per task where two queries fail differently.
3. **Workforce's own "Reopen the Staff workspace" branch** (`workforce/page.tsx`) must keep working; it branches before `RequestFailure`. Pinned by the existing page test staying green.
4. **A file converted but left listed:** caught by `retry-legacy.test.mjs`.
5. **A file removed from the list with a retry left in it:** caught by `pnpm --filter @helivanta/shell lint`.

---

Each task follows the same five steps:

1. **Write the failing tests** named in the task.
2. **Run them** with `pnpm --filter @helivanta/shell exec vitest run <paths>`. Expected: the new tests FAIL.
3. **Convert the task's files** and remove them from `RETRY_LEGACY`, lowering its ceiling.
4. **Run the tests again,** plus `pnpm --filter @helivanta/shell lint` and the `retry-legacy` test. Expected: everything passes and lint is clean.
5. **Commit** `fix(workforce): <group> reads fail and refresh through the shared components (#1595)`.

### Task 1: Setup, shifts, rosters, leave

Files (under `apps/shell/app/(workspace)/workforce/`):

- `setup/page.tsx` (4)
- `shifts/page.tsx` (4)
- `roster-setup/roster-editor.tsx` (2)
- `roster-setup/roster-workspace.tsx` (1)
- `leave-setup/leave-workspace.tsx` (1)
- `leave-review/page.tsx` (1)

Tests:

- `setup/page.test.tsx` and `shifts/page.test.tsx`: records load, then a re-read answers 403. The rows are gone, the refusal shows, and there is no "Try again" or refresh button. A hospitals 403 plus a records 500 shows the refusal (`firstError`).
- `roster-setup/page.test.tsx`: a refused shifts read in the roster editor shows a refusal with no retry.
- `leave-setup/page.test.tsx` and `leave-review/page.test.tsx`: a refused refresh hides the loaded rows.

### Task 2: Directory, enrollment, documents, login links

Files:

- `page.tsx` (3)
- `directory-editor.tsx` (1)
- `enrollment-editor.tsx` (4)
- `document-classification-editor.tsx` (4)
- `document-panel.tsx` (3)
- `login-link-panel.tsx` (3)

Tests:

- `page.test.tsx`: refusals for the enrollment, documents and login-link panels, with no retry. The existing "Reopen the Staff workspace" test stays green.
- `directory-editor.test.tsx` and `document-classification-editor.test.tsx`: load, then a 403 hides the loaded record and offers no retry.

### Task 3: Employment, evidence, identity

Files:

- `employment-periods-panel.tsx` (3)
- `employment-evidence-panel.tsx` (1)
- `employment-reviews/page.tsx` (2)
- `employment-reviews/preparation.tsx` (1)
- `employment-reviews/review-detail.tsx` (1)
- `employment-reviews/review-history.tsx` (1)
- `identity-cases-panel.tsx` (2)
- `identity-source-panel.tsx` (2)

Tests:

- Each file's test (`<file>.test.tsx`; the three review files share `employment-reviews/page.test.tsx`) gets a 403 shown as a refusal with no retry.
- `identity-source-panel.test.tsx`: loaded identity details are hidden after a refused "Refresh identity details".
- `employment-periods-panel.test.tsx`: a sponsors 403 alongside a periods 500 shows the refusal.

### Task 4: Person reviews, access assignments and membership

Files:

- `person-reviews/page.tsx` (2)
- `person-reviews/manager-preparation.tsx` (3)
- `person-reviews/review-detail.tsx` (1)
- `person-reviews/review-history.tsx` (1)
- `access/assignment-forms.tsx` (2)
- `access/membership-form.tsx` (1)
- `access/membership-management.tsx` (1)

Tests:

- `person-reviews/page.test.tsx`: a loaded review, then a refused re-read hides it with no retry.
- `access/membership-management.test.tsx` and `access/membership-form.test.tsx`: a refusal with no retry.
- `access/page.test.tsx`: assignment forms with a refused hospitals or departments read show no retry and no stale options.

### Task 5: Docs and gate

- The spec's "What exists" keeps its numbers. "Slices" already records 2a/2b.
- `RETRY_LEGACY` now holds exactly the 13 files of 2b, and its ceiling is 13.
- Run `pnpm turbo lint type-check test build format:check` (expect 30/30) and `make lint-shell` (clean).
- Commit `docs(workforce): 2a converted; 13 files left for 2b (#1595)` if anything changed.
