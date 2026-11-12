# No hand-drawn retries for a read: workforce 2c (#1595)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** convert the last 4 workforce files to `RequestFailure` and `RefreshButton` and delete
`apps/shell/retry-legacy.mjs`, so `helivanta/no-hand-rolled-retry` holds for every file.

**Architecture:** the same conversion as 2a and 2b. The last task deletes the list and its test
and takes the `ignores` argument off the shell's `hmsReadRetries` call.

**Tech Stack:** React 19, React Query, `@helivanta/ui`, Vitest + `renderWithProviders`.

**Spec:** `docs/superpowers/specs/2026-11-06-read-retries-design.md` (slice 2c)

## Global Constraints

- A failed read renders `RequestFailure` with its query's `error`; a refresh of loaded data is `RefreshButton` with the read's `error`. Refresh labels stay as they are.
- `useStaffAccessCutoff` keeps precedence; its `STAFF_ACCESS_CHANGED` branch is unchanged.
- A retry that was disabled while a command is in flight passes the same condition as `busy`.
- No `recheck` in workforce.
- Stage files by path; never `git add -A`.

## Review Focus

1. **The access page's identity and assignments reads** have no access-change branch: a 401 ends the session and a 403 is a refusal, never "Refresh to try again". Pinned by Tasks 1 and 2.
2. **"Refresh assignments" re-reads eleven queries** but takes only the assignments read's error. A catalog's refusal is shown as "unavailable in this workspace" and must not hide the button.
3. **A retained command** (posting admission recheck, employment source) keeps its retry disabled exactly as before.
4. **The department-moved 409** keeps its own sentence and still offers "Try again".
5. **A file left with a hand-drawn retry once the list is gone:** caught by `pnpm --filter @helivanta/shell lint`.

---

Each task: failing tests, run them (expected FAIL), convert, run tests plus
`pnpm --filter @helivanta/shell lint` (expected PASS), commit
`fix(workforce): <group> reads fail and refresh through the shared components (#1595)`.

### Task 1: Account pages

Files: `postings/page.tsx` (1), and the identity read of `access/page.tsx` (no button today; its
"Refresh this page to try again" sentence is an outage message shown for a refusal).

- `posting-account-pages.test.tsx` gains both pages: a 401 shows "Your session has ended…" and no
  button; a 503 shows the page's message and "Try again", which asks again.

### Task 2: Access page

Files: `access/page.tsx` (4).

- "Refresh assignments" is a `RefreshButton`: `error={records.error}`, `busy={records.isFetching}`,
  `refresh` re-reads all eleven queries.
- The assignments failure ("Could not load assignments. Refresh to try again.") is `RequestFailure`,
  so a refused refresh hides the records and the button and shows the refusal.
- "Retry department", "Retry history" and "Retry decisions" are `RequestFailure`.
- Tests in `access/page.test.tsx`: a 403 assignments read shows the refusal with no "Refresh
  assignments"; a 503 history read offers "Try again", which asks again.

### Task 3: Posting reviews and decision

Files: `posting-review-workspace.tsx` (6), `posting-proposal-decision.tsx` (1).

- Review hospitals, proposals and protected proposal failures are `RequestFailure`; "Recheck posting
  admission" is a `RequestFailure` with the retained-command message and `busy={hospitals.isFetching}`;
  "Refresh review access" and "Refresh proposals" are `RefreshButton` with `busy={busy}`.
- The employment source failure is `RequestFailure` with `busy={retained !== null}`.
- Tests: `posting-review-workspace.test.tsx` clicks "Try again" in place of "Recheck posting admission"
  and looks for it in place of "Retry proposals"; a new case shows a 503 hospitals read offering
  "Try again", which asks again.

### Task 4: Delete the list

- Delete `apps/shell/retry-legacy.mjs` and `retry-legacy.test.mjs`; `hmsReadRetries` takes no
  `ignores` in `apps/shell/eslint.config.mjs`.
- `docs/standards/frontend.md` §5 and the spec drop the legacy list.
- `pnpm turbo lint type-check test build format:check` (expect 30/30) and `make lint-shell`.
