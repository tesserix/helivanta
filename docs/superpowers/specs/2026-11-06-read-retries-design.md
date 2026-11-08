# No hand-drawn retries for a read (#1595)

Issue: #1595. Follows #1591 (a refused request is never shown as an outage).

## Why

#1591 made `RequestFailure` take the failed request's error, so a 403 or 404
shows as a refusal with no retry and a 401 as an ended session. Screens that
draw their own retry or refresh button for a read bypass it. A refused read
then shows as an outage, and its button re-runs a request that can only be
refused again. Several emergency screens also print the server's raw error
message.

#1591 chose a required prop so that no screen could forget the error. That
holds only if every screen goes through the shared component. Nothing stops a
screen from drawing its own button today.

## What exists (checked against source on 2026-11-06)

- **148 click handlers call a query's `refetch` or `retry`** (a member call
  such as `q.refetch()` or `rows.retry`), found with the selector this spec
  adds. Three are in `packages/ui` (`RequestFailure`, `QueryState`). The rest are
  in `apps/shell`:
  - **Workforce: 103 handlers in 41 files** under `app/(workspace)/workforce/`.
  - **Everything else: 42 handlers in 21 files.** These are in emergency (8
    files, the largest share), supply (4), organization (2), pharmacy (2),
    the dashboard (2, including `components/dashboard/workforce-insights.tsx`),
    payments, settings/branding and country policy.
- **They come in two kinds:**
  - **A retry after a failure.** The button sits in an `isError` branch,
    for example "Retry bases", "Refresh fleet", "Recheck cutoff admission" or
    "Try again".
  - **A refresh of data that loaded**, for example "Refresh policies",
    "Refresh departments", "Check again" or the dashboard's "as at" link. These
    are legitimate: the data can change underneath the person.
- **Not reads, and not matched:** mutation retries (`retry.mutate(image)`, a
  command's `retry()`), Load more (`loadMore`) and the route error boundaries
  (`reset`).
- **The lint pattern to follow:** design rules live in
  `packages/config/eslint.config.mjs`. They are tested against real snippets in
  `eslint.config.test.mjs`, and a file not yet migrated is listed in an app-side
  legacy file (`apps/shell/surface-legacy.mjs` for #1275).

## Decisions

1. **A lint rule rejects a hand-drawn read retry.** In `apps/shell`, a
   handler in `onClick`, `onRetry`, `onRefresh` or `onReload` that calls
   `.refetch` or `.retry` on anything is an error. The last three are props
   that pass the call on to a button drawn elsewhere (country policy's
   `onRefresh`, supply structure's `onReload`, pharmacy staff source's
   `onRetry`). Other `on*` props, such as returns' `onRefused={sale.refetch}`,
   refresh without a button and stay allowed. (My call. If wrong, a new
   forwarding prop name escapes the rule.) A prop that hands a retry
   to a shared component is named `retry` or `refresh`, as on
   `RequestFailure`, and is not matched: `StaffDirectory`'s `onRetry`
   becomes `retry`, and the country policy and supply structure children
   take `refresh` and render `RefreshButton`. It is an error
   with this message: "A failed read renders RequestFailure or QueryState; a
   refresh of loaded data is RefreshButton (#1595)." Tests are exempt.
   - **Its own rule:** it is a rule in a small local plugin in
     `packages/config` (`helivanta/no-hand-rolled-retry`), not another
     `no-restricted-syntax` entry. `no-restricted-syntax` options don't merge
     across config objects, so a second legacy list would multiply the
     combinations the surface rule already needs. A separate rule name takes a
     plain `ignores` list.
   - **Enforced in CI:** it fails `pnpm turbo lint` rather than being a
     documented convention.
2. **`RefreshButton` in `packages/ui` is the one way to refresh loaded
   data.** It takes `refresh: () => unknown`, a required `error: unknown`,
   an optional `busy`, and a label. It renders nothing when
   `isRefusal(error)`: re-asking cannot change an access answer, and the
   refusal itself is shown by the screen's `RequestFailure`. This also fails
   closed if it is misused in a failure branch. Several queries pass
   `firstError(...)`, as in #1591.
   - **A refused refresh hides the loaded data**, as #1591 decided for
     `QueryState`. React Query keeps `data` after a failed refetch, so every
     screen with a `RefreshButton` checks `isRefusal(error)` before it
     renders the data, and shows `RequestFailure` instead. Records the
     server now refuses must not stay on screen with no button and no
     message.
3. **Every failure retry becomes `RequestFailure`** (or `QueryState` for a
   paged list), with its query's error. Wording that is already a refusal
   ("Campus access is unavailable") goes in as `refused`; otherwise the
   screen's message stays as `message`.
4. **Access that changes while the screen is open gets "Check access
   again".** `RequestFailure` gains an optional `recheck` label. On a 403 or
   404 it shows the refusal sentence and a button with that label, which
   calls `retry`. It never says "Try again", so it doesn't read as an
   outage. A 401 still gets no button, since only signing in fixes it.
   Emergency access follows the current assignment: dispatch assigns a crew,
   or a hospital accepts a pre-alert, and a 403 becomes a 200. So the
   emergency crew, receiving and dispatch screens pass
   `recheck="Check access again"`. No other screen passes it, so #1591
   holds everywhere else. Its sentence keeps "refresh before acting" only
   where the button is shown.
5. **No raw server message.** The emergency screens that print
   `error.message` show a written sentence instead. A server's text is not
   written for this person, and on a refusal it could disclose more than
   "you don't have access". (My call. If wrong, some emergency screens lose
   detail the server gave.)
6. **Workforce is converted in later slices behind a legacy list.**
   `apps/shell/retry-legacy.mjs` lists the 41 workforce files. They are
   exempt from the rule until converted, and every other file, including any
   new workforce file, gets the rule from this slice on. Each later slice
   converts files and removes them from the list. The list is checked: a test
   fails if a listed file no longer breaks the rule, so a converted file can't
   stay exempt. It is deleted when it is empty.
7. **Tests:**
   - **The rule:** rejects `onClick={() => void q.refetch()}`,
     `onClick={rows.retry}`, and a multi-statement handler that calls
     `a.retry()`. It allows `retry.mutate(x)`, `onClick={reset}` and
     `onClick={rows.loadMore}`. Mutation-checked: removing the rule fails
     the tests.
   - **`RequestFailure` with `recheck`:** a 403 or 404 shows the refusal
     and a button with that label that calls `retry`; a 401 shows none; a
     500 still shows "Try again".
   - **`RefreshButton`:** refreshes on click and disables while busy. It is
     hidden for 401, 403 and 404 and shown for a 500 or `null`.
   - **The screens:** each converted area (emergency, supply, organization,
     pharmacy, dashboard, payments, settings, country policy) has a Vitest
     showing a 403 as a refusal with no retry or refresh button, and a
     refused refresh hiding the data it had shown. The emergency crew test
     shows a 403 with "Check access again", and clicking it after the
     server allows access renders the assignment. Existing tests
     that click a renamed button are updated.
   - **The legacy list:** the staleness test from decision 6.
8. **Docs:** `docs/standards/frontend.md` §5 "Failed and refused reads" names
   the rule, `RefreshButton` and `recheck`. The #1591 spec's "Not covered" points here.

## Slices

1. **This slice:** the rule, `RefreshButton`, the legacy list and every
   non-workforce site: 42 `onClick` handlers in 21 files, plus the
   forwarding props above and in supply's access and access-limits editors.
2. **Workforce, in two PRs.** Each converts its files and removes them from
   the list, and lowers the list's ceiling to match.
   - **2a (27 files, 55 handlers):** the directory and its editors,
     enrollment, documents, employment periods, evidence and reviews,
     identity, login links, leave, person reviews, rosters, setup, shifts,
     and access assignments and membership.
   - **2b (10 files, 25 handlers):** posting cutoffs (page and workspace),
     drafts, posting reviews' page, submission, approval history, the
     posting workspace, and pharmacy binding and relationships.
   - **2c (4 files, 12 handlers):** `access/page.tsx`, `postings/page.tsx`,
     `posting-proposal-decision.tsx` and `posting-review-workspace.tsx`.
     It waited for #1602 and #1603, which changed these files. 2c deletes
     the list. It also converts two failures that drew no button but told
     the person to refresh (the access page's identity and assignments
     reads), since a refusal there read as an outage.
   - **The posting and pharmacy binding screens already hand a refusal to
     their own "Staff access changed" branch** (`useStaffAccessCutoff`),
     which keeps precedence. Their `RequestFailure` and `RefreshButton`
     therefore only ever see an outage. A 2b screen's tests prove the outage
     path ("Try again" asks again); the account pages above them, whose
     identity read has no such branch, also prove a 401 ends the session
     with no retry.
   - **`RequestFailure` gains `busy`, and its button is `type="button"`.**
     Several 2b retries are disabled while a command is in flight, and some
     sit inside a form, where an untyped button would submit it.

## Not covered

- **A retry hidden behind a named handler** (`onClick={reload}` where
  `reload` calls `refetch`) passes the rule. Following calls across functions
  is beyond a syntax rule. The rule catches the common shape, and review
  catches the rest.
- **Mutation errors, "Load more" under a refused list and a sign-in link on
  the 401 sentence** are as recorded in #1591.
