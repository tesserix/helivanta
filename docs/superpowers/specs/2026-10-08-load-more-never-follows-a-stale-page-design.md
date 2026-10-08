# Load more never appends to a stale first page

**Issue:** [#833](https://github.com/tesserix/helivanta/issues/833)
**Amends:** `useApiPagedQuery` (`packages/api/src/paged.ts`, #816).

## The problem, stated precisely

`e2e/tests/pagination.spec.ts` failed once in five runs because the newest of
its 55 visits appeared on no page at all. All 55 rows were in the database.
The issue suspected the client cache, and the hook's code confirms how.

1. Creating a visit invalidates the list query, so the pages already shown
   are refetched, starting with page one.
2. `loadMore` called `fetchNextPage()`. TanStack Query defaults that to
   `cancelRefetch: true`, so a click while the refetch was in flight
   **cancelled** it (`query-core` 5.101.4, `Query.fetch`).
3. The fresh page one was discarded, and the next page was fetched from the
   **stale** page one's cursor.

The newest row was in neither, and nothing would refetch page one until the
next invalidation or poll. A clinician paging through a ward list right
after a colleague registered a patient could reach the end and never see
that patient.

## Decisions

### D1: Load more joins an in-flight refetch instead of cancelling it

`fetchNextPage({ cancelRefetch: false })` returns the in-flight refetch's
promise instead of cancelling it. Page one is then fresh, but no next page
was fetched by that call.

### D2: After joining, ask again, bounded

`appendNextPage` compares the page count before and after. If no page was
added and the result still has a next page, the call joined a refetch that
has now finished, so it asks again. That request follows the **fresh** page
one's cursor.

The loop is bounded at three attempts. Exhausting it needs a refetch to land
between every attempt, which needs a poll faster than a page fetch. On that
path the click has no visible effect. That fails safe: stale rows are never
shown, at worst one click is lost.

### D3: The double-click guard is unchanged

`fetchingRef` still makes a second click during the first one's fetch a
no-op. It now covers the whole join-and-ask sequence.

## Not covered

- Forcing the race end to end. `pagination.spec.ts` cannot reliably land a
  click inside a refetch window. The race is pinned at the hook, where it
  can be made deterministic (Tests).

## Tests

- **The new hook test**, in `paged.test.tsx`:
  - Page one is `[b, c]` with cursor `c1`. A create invalidates the list,
    and the refetch, held open, will return `[a, b]` with cursor `c2`.
  - Load more is clicked during the refetch, then the refetch is released.
  - It asserts the rendered rows are exactly `[a, b, c, d]`, and that cursor
    `c1` is never requested.
  - Before the fix it failed with `[b, c, d]`: the newest row missing,
    exactly the #833 symptom.
- **Mutations, each failing:**
  - restoring `cancelRefetch: true`;
  - a single attempt with no ask-again.
- **The existing hook tests still pass:** append and stop, no duplicate on
  double-click, and an exactly-full final page.
