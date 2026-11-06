# A refused request is never shown as an outage (#1591)

Issue: #1591. Follows #1276 (shared loading, empty and error states), #1285
(pharmacy outlet access) and #1589 (pharmacy report screens).

## Why

`RequestFailure` shows a failed request as its message with **Try again**.
For a 403 or 404 that's wrong twice over:

- **Retrying can't help.** An authorization answer doesn't change on retry.
- **"Could not load" reads as an outage.** It suggests the record exists and
  merely failed to arrive.

A 401 is different again: the session has ended, so the person needs to sign
in again.

#1589 made the refusal state optional on `RequestFailure`, and the pharmacy
report screens use it. About 160 other call sites, and the shared
`QueryState`, pass no error, so on most screens a refusal still reads as an
outage. Nothing makes a new screen handle it.

## What exists (checked against source on 2026-11-05)

- **`RequestFailure`** (`packages/ui/src/request-failure.tsx`) takes
  `{message, retry, error?, refused?}`. It shows a refusal only when both
  `error` and `refused` are given.
- **`QueryState`** (`packages/ui/src/query-state.tsx`, 26 uses) renders
  `RequestFailure` when its first load fails. When a refresh fails, it keeps
  the rows under an inline "Could not load all …" banner with **Try again**.
  It takes `query: Pick<UseApiPagedQueryResult, "items" | "isPending" |
"isError" | "retry">`, which has no `error`.
- **About 170 direct call sites in 73 files.** Their `retry` is
  `q.retry` (91), `q.refetch` (42), `() => void q.refetch()` (18), or a
  function over several queries (10).
- **Three local `Failure` components** (two in workforce, one in patients)
  copy `RequestFailure` without an error: 92 more call sites across 45
  workforce files and the patient workspace. They are replaced by
  `RequestFailure`.
- **About 15 screens hand-roll their own "Try again" button** for a read
  (workforce setup, shifts, documents, enrollment, branding, the Razorpay
  account, the dashboard and a few pharmacy panels). See "Not covered".
- **Some screens give a 404 its own meaning** before `RequestFailure` is
  reached: "not set up yet" (GST, bed charging, daily collection), "no
  shift" (cash desk), "not found" (several detail panes), and the supply
  access screens' `denied()`. These keep their own handling.
- **No client-side 401 handling.** `apiFetch` throws `ApiError` with the
  status. Session renewal (`apps/shell/lib/renew.ts`) runs on its own.

## Decisions

1. **`error` becomes a required prop of `RequestFailure`.** A call site that
   passes no error doesn't compile. This is the strongest control the repo
   has (compile error before boot failure before CI). Its type is `unknown`,
   because a query's `error` is `Error | null`.
2. **What each error shows:**
   - **403 or 404** (`ApiError`): the caller's `refused` sentence, or by
     default "This wasn't found, or you don't have access to it." No
     **Try again**.
   - **401:** "Your session has ended. Sign in again to continue." No
     **Try again**: retrying with an ended session can't succeed.
   - **Anything else** (5xx, 409, network, abort, `null`): the `message`
     with **Try again**, as today.

   Failing closed: if a status can't be read, the error is treated as a
   failure, so it keeps the retry. That's the safe direction, because
   offering a retry discloses nothing.

3. **`QueryState` passes the error through.** Its `query` type gains
   `error`, which `UseApiPagedQueryResult` already has, so the uses that
   pass a paged query get the refusal without edits. `StaffDirectory`
   builds its query by hand, so it gains a required `error` prop too, and
   its two callers pass theirs. Its refresh-failure banner does the same: if a
   refresh is refused, the rows are hidden and the refusal is shown. Rows
   the server now refuses aren't left on screen as if current.
4. **Call sites pass their query's error.**
   - **A script adds `error={q.error}`** where `retry` names one query.
   - **Hand-written:** where `retry` covers several queries, `error` is
     `firstError(a.error, b.error)`: a refusal from any of them, otherwise
     the first error. A refusal wins because retrying can't fix it, even if
     another query merely failed.
   - Screens that already give a 404 its own meaning keep that branch;
     `RequestFailure` sees only what reaches it.
   - Pharmacy's report-specific sentences from #1589 stay.
5. **Tests:**
   - **`RequestFailure`:** each status, `null`, a non-`ApiError`, and the
     default and custom refusal sentences.
   - **`QueryState`:** a refused first load, and a refused refresh hiding
     the rows.
   - **The screens:** one Vitest per product area that renders these
     states (MediCore, pharmacy, supply, organization, patients, country
     policy, lab, workforce; emergency renders neither) shows a 403
     as a refusal with no retry. A type-check of the e2e and shell packages
     proves every call site passes `error`.
   - **Mutation-checked:** making `error` optional again fails the
     type-check, because a fixture call site in the test file passes none.
6. **The three Minors from the #1589 review:**
   - **401 wording:** done in decision 2. A 401 shows the session sentence
     even where a screen gives its own `refused` sentence.
   - **`pharmacy-reports.spec.ts`:** takes "today" from the outlet's time
     zone, which `newOutlet` returns.
   - **The daily-report Vitest:** checks the daily section's own alert.

## Not covered

- **Asking for access from the refusal,** e.g. a Setup link for an
  administrator (#1285 does this for pharmacy outlets).
- **Mutation errors** (`useApiMutation` failures shown inline or in toasts).
  This covers reads only.
- **Redirecting to sign-in on a 401.** The screen says so; the session flow
  is unchanged.
- **The hand-rolled read retries, and a lint rule banning them.** Screens
  that draw their own retry or refresh button for a read still show a
  refusal as an outage. The count was about 15 when this spec was written;
  #1595 found 148 handlers and adds the lint rule
  (`docs/superpowers/specs/2026-11-06-read-retries-design.md`).
- **"Load more" under a refused list.** A list's `LoadMore` sits outside
  `QueryState`, so it stays visible under the refusal.
