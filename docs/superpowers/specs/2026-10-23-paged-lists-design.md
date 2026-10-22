# Paged lists: Previous / Next with a total, and the Dispense queue on tabs

Resolves #1295. Slice 1 of the PharmaConnect rework: slice 2 is the overview page (#576),
slice 3 moves Expiry & safety, Supplier bills and the Catalogue onto this. Builds on the
pagination contract (#816), which stands unchanged for every list this slice does not touch.

## Intent

A pharmacist works the counter from lists. "Load more" makes every list one growing column
with no sense of size, no way back, and no position that survives a reload or a shared
link. The Dispense queue is worse: it lists the newest waiting patient first, so the
longest wait is at the end of the list, and it stacks Waiting and Dispensed with a "Load
more" each.

Success: a pharmacist sees "1–25 of 41 waiting", oldest first, steps Next and Previous,
and a link to page 2 of Dispensed opens page 2 of Dispensed.

## Decisions

- **D1 A list may declare a total, from the same filter as its rows.**
  - A new `platform.CountedListRoute` takes the module's `*tenantdb.DB`, a direction and a
    handler.
  - The handler validates the request; on a bad request it responds and returns. It then
    returns one scope, `func(*gorm.DB) *gorm.DB`, that applies the filter.
  - The platform opens the transaction and applies that one scope twice: once for the
    count, and once with the keyset for the rows. A total that counts something the rows
    do not is therefore not expressible.
  - When the handler has aborted, no transaction is opened.
  - The envelope gains `page.total`, an exact integer with no cap, only on counted lists.
    `ListRoute` and its envelope are unchanged.
- **D2 Rows and total come from one snapshot.** Both are read in one read-only
  `REPEATABLE READ` transaction, opened by a new `tenantdb.WithTenantSnapshot` that sets the
  same tenant and actor settings as `WithTenant`. Under the default isolation a row committed between the
  two statements would make them disagree; `ApplyKeyset`'s comment records exactly that
  race. The total is the count at that snapshot, not a live figure, and the queue's poll
  refreshes it.
- **D3 A list may page oldest first.** The keyset gains a direction:
  - newest-first is `(created_at, id) <`;
  - oldest-first is `>`, with the matching `ORDER BY`.

  - The cursor records the direction it was issued for. A cursor reused against the other
    direction is a 400, never a silent repositioning.
  - Newest-first cursors keep today's encoding, and an encoded cursor without a direction
    reads as newest-first, so every live link keeps working. Only oldest-first cursors
    carry the new field.
  - The direction is fixed per route by the code that registers it, not chosen by the
    client.
- **D4 Previous needs no server support.** The browser keeps the cursors of the pages it
  has visited and steps back through them.
- **D5 The page lives in the URL.**
  - `?tab=`, the filters, `?page=` (the 1-based page number, used only to show the range)
    and `?cursor=` (the current page's start) are all in the address.
  - Back and forward move between pages, and a shared link opens the same page.
  - After a reload or a shared link to a later page, the earlier cursors are not known, so
    Previous is replaced by "First page". It does not guess.
  - A `page` that disagrees with the rows (hand-edited) changes only the range label. The
    rows always come from the cursor.
- **D6 25 rows a page, then Previous / Next with "26–50 of 312".**
  - One shared `Pager` component in `@helivanta/ui` shows the range and the two controls.
  - A shared `useApiPageQuery` hook in `@helivanta/api` fetches one page at a time; it is a
    sibling of `useApiPagedQuery`, which stays for the lists still on "Load more".
  - A page that empties while open falls back to the previous page, or to the first. This
    happens when the last row on it is dispensed.
- **D7 Tabs.**
  - A `PageTabs` component in `@helivanta/ui` puts fixed states of one list across the top.
  - Each tab is a link (`?tab=`), so it works with the keyboard, the back button and shared
    links. Changing tab resets the page to 1 and keeps the filters.
  - Tabs carry no counts: the open tab's total is in the pager. A count on an inactive tab
    would come from a separate read and could disagree with the pager. _(My call.)_
- **D8 The Dispense queue.**
  - Two tabs, each its own counted route with a fixed direction:
    - **Waiting:** `GET /pharmacy/dispenses/waiting`, oldest first by `created_at`.
    - **Dispensed:** `GET /pharmacy/dispenses/dispensed`, newest first by `dispensed_at`, the
      time the medicine was handed over. _(My call.)_
    - The keyset column is the row type's `PageKey`, and `dispensed_at` is never null once
      the status is `dispensed`.
  - `GET /pharmacy/dispenses?status=` stays as it is: uncounted, newest first, the v1
    contract its callers rely on.
  - Dispensed is "all dispensed", not "dispensed today". Dispenses are tenant-wide, with no
    outlet time zone to say when "today" is.
  - The stacked layout and its two "Load more" buttons go. The phone layout (#1293) stays.
  - A new migration adds two partial indexes so each tab's page and count stay index-only:
    - `(tenant_id, created_at, id) WHERE status = 'pending'`;
    - `(tenant_id, dispensed_at, id) WHERE status = 'dispensed'`.
  - The Dispensed count grows with history. It stays exact: an index-only count over a
    hospital's dispenses is fast at the volumes this product targets. _(My call; a cap such
    as "10,000+" can follow if one hospital proves otherwise.)_

- **D9 Waiting searches by patient name.** _(Added while building; my call.)_
  - Oldest first puts a newly arrived patient at the end of the queue. In a long queue the
    patient standing at the counter would be pages deep, so Waiting takes `?q=`.
  - The search is a case-insensitive substring match on the patient name. LIKE wildcards in
    `q` are taken literally, and `q` is at most 100 characters.
  - The search lives in the URL, is kept while paging, and a new search starts at page 1.
  - Dispensed history is unbounded, so a search there is refused with a 400, not ignored.
- **D10 No rows carried over while a page loads.** The range is computed from the page
  number, so the previous page's rows must never sit under the next page's range. A page
  shows its loading state until its own rows arrive.

## Not covered

- Numbered page links: jumping to a page is not offered (agreed: filters narrow instead).
- Moving any list other than the Dispense queue. The other 90 `useApiPagedQuery` lists keep
  "Load more" until their slice.
- Filters beyond the status tabs and the Waiting name search.
- "Dispensed today", which needs an outlet-scoped dispense.

## Verification

- Go:
  - A counted list's `total` equals the number of rows reachable by paging to the end, for
    both directions.
  - An insert committed between the count and the rows inside a test transaction does not
    make them disagree. This test must fail at `READ COMMITTED`.
  - An oldest-first cursor sent to a newest-first route is a 400.
  - A cross-tenant cursor is still refused.
- Vitest:
  - `Pager` shows the range and disables Next on the last page.
  - It offers "First page" in place of Previous when the earlier cursors are unknown.
  - `PageTabs` marks the current tab and resets the page.
  - `DispenseList` requests `/dispenses/waiting?limit=25` and shows "1–25 of N".
- e2e:
  - On a seeded queue with more than 25 waiting, Next then Previous returns to the same
    first row.
  - A reload on page 2 keeps page 2 and offers "First page".
  - Switching to Dispensed and back keeps the URL in step.
  - Dispensing the only row on the last page falls back a page.
- Contract docs:
  - `docs/api/http-contract.md` documents `page.total` and oldest-first ordering.
  - `docs/api/domains/pharmacy.md` lists the two new routes.
  - `cmd/contractdocs` records `counted` next to `paginated`.
  - Regenerate with `go run ./cmd/contractdocs`.
