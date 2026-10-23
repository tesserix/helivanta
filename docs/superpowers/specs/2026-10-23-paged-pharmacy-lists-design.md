# Paged pharmacy lists: Expiry & safety, Supplier bills and the Catalogue

Resolves #576 (slice 3 of the PharmaConnect rework). Slice 1 (#1295) built Previous / Next
with a total, tabs and URL state for the Dispense queue
([paged-lists design](2026-10-23-paged-lists-design.md)). Slice 2 (#1301) built the overview,
whose counts open these lists already filtered
([overview design](2026-10-23-pharmacy-overview-design.md)). This slice moves the three
outlet pages onto the slice 1 pattern.

## Intent

The pharmacist asked for pagination and filters instead of endless scrolling. Slice 1's
answer, already agreed for the Dispense queue, was:

- 25 rows a page;
- Previous / Next with "26–50 of 312";
- fixed states as tabs;
- filters to narrow the list;
- the page, tab and filters in the URL.

Expiry & safety, Supplier bills and the Product catalogue still grow one "Load more"
column with no size and no position that survives a reload. Since slice 2, their filter
arrives from the overview but is lost as soon as it is changed.

Success:

- The overview's "Overdue 3" opens Supplier bills on the Overdue tab, which says "1–3 of 3".
- Switching to All and paging to page 2 gives a URL that opens page 2 of All.
- The same holds on the catalogue and on safety concerns.

## Decisions

- **D1 Outlet lists can be counted.** The three pages read outlet-scoped data. Each read runs
  the pharmacy access preamble inside its transaction:
  - the session settings that row-level security reads;
  - the caller's outlet grant, which is 404 without one and 503 when it cannot be checked.

  `platform.CountedListRoute` today runs only a scope inside its own snapshot. It gains a
  guard: the handler may return a function that runs first, inside the same read-only
  `REPEATABLE READ` snapshot, before the count and the rows.
  - A guard error is answered by the handler's own error responder, with the existing
    404 / 503 / 400 answers. It is never a 500, and no rows are sent.
  - The guard may compute values the scope needs, such as the outlet's local date for the
    due filter. Count and rows then use one value read in one snapshot.
  - The snapshot is read-only, so the grant is checked without a row lock. The existing
    preamble takes a `FOR SHARE` lock, which a read-only transaction refuses. A read gains
    nothing from the lock: the snapshot already fixes what it sees.
  - The grant query is one function shared by reads and writes. The two differ only in
    whether it takes the lock, so the read path cannot drift from the write path.
    _(My call.)_
  - The outlet's date and time zone are read once, in the guard. The scope then takes both,
    so the count, the rows and the overview agree across midnight. _(My call.)_
  - Two more optional parts, both run in the same snapshot. _(My call.)_
    - A projection applied only to the rows. These lists use `to_char(...)` select lists
      and read views, and a `Select` inside the shared scope would break the count.
    - A step after the fetch. The catalogue attaches each product's tax state there, which
      drives the badge.
  - Dispense routes, which have no guard, keep their exact behaviour.
- **D2 Each counted list is a new route; the uncounted v1 route stays.** As slice 1 did with
  `/dispenses`, existing callers keep their contract. Slice 1 registered one counted route
  per tab. These pages are filtered rather than split into fixed states, so here a
  counted route takes the same filters as its uncounted original:

  | Page | Counted route (newest first by `created_at`) | Filters |
  |---|---|---|
  | Expiry & safety | `GET …/stock/safety-incidents/recent` | none; always the last 30 days on the outlet's calendar |
  | Expiry & safety | `GET …/stock/batches/:id/safety-incidents` | the batch is the path, 404 when not the outlet's |
  | Supplier bills | `GET …/accounts/payables/counted` | `due=overdue\|week` |
  | Supplier bills | `GET …/accounts/inventory-audit/counted` | none |
  | Catalogue | `GET …/catalogue/products/counted` | `q`, `status`, `tax=unclassified` |

  - Each counted route reuses the scope helper its uncounted route uses. Examples are
    `payableDueScope`, `recentSafetyScope` and `unclassifiedScope`. The overview's counts,
    the totals and the rows therefore cannot disagree.
  - Validation is unchanged: an unknown or repeated value is a 400. The bill scope names
    the `pharmacy_payable_balances` view, which the rows already read.
  - The names are the builder's to settle against `docs/api/http-contract.md`. The rule
    is: a new path, never a changed response on an existing one.
- **D3 Tabs and filters per page.** _(My call.)_ Each tab is a link (`?tab=`). Changing tab
  resets the page and keeps the filters (slice 1 D7).
  - **Supplier bills:** tabs Overdue, Due within 7 days, All bills and Inventory cost audit.
    - The default is All bills.
    - Inventory cost audit is a tab, not a second list below the bills: one page has one
      pager and one `?page=`.
  - **Expiry & safety:**
    - Tab "Last 30 days" holds the outlet's recent concerns.
    - Tab "By batch" holds the batch picker and that batch's concerns, with the batch in
      the URL (`?batch=`) and paged.
    - The default is "Last 30 days". Before, the recent list showed only when you arrived
      from the overview; now it is always available.
  - **Catalogue:**
    - Tabs All, Active, Discontinued and Withdrawn, mapping to `status`. The default is All.
    - The search `?q=` and the "Not classified" filter `?tax=unclassified` stay above the
      list and survive paging and tab changes.
    - A new search or filter starts at page 1.
    - "Not classified" is hidden, and ignored in the URL, where the outlet has no tax scheme.
- **D4 The URL is the state.** Tab, filters, `?page=` and `?cursor=` are read from and
  written to the address. This replaces slice 2's "read once on arrival" props
  (`initialDue`, `showRecent`, `initialUnclassified`), which are removed.
  - Back and forward step through pages and filters.
  - Reload keeps the page and offers "First page" (slice 1 D5).
  - The overview's links are updated in the same change, from `?due=` to `?tab=overdue`,
    `?tab=week` and so on. An old `?due=` link opens All bills. _(My call; the old links
    shipped the same day.)_
  - The safety tile's link becomes `?tab=recent`. The old `?recorded=30d` opens the default
    tab, which is the same list.
- **D5 Shared parts only.** The pages use `useApiPageQuery`, `Pager`, `PageTabs` and
  `PAGE_SIZE` exactly as the Dispense queue does. No new paging component. A page that
  empties while open falls back a page (slice 1 D6), and no rows are carried over while a
  page loads (slice 1 D10).

## Not covered

- The Stock on hand register, the stock movements panel and the medication list stay on
  "Load more" for now: they are not part of this slice.
- Pickers stay on "Load more" because they hold options, not result lists. These are the
  batch, product and location options on Expiry & safety and Stock.
- Detail histories stay on "Load more": a product's revisions, a bill's settlements and a
  position's movements.
- Sorting other than newest first, for example overdue bills oldest-due first. The keyset
  has one column per route, and `due_on` is a date, not a timestamp.
- Totals on tabs. Only the open tab's total is shown, in the pager, as in slice 1.
- Any change to the catalogue search. It stays the existing prefix match on brand, generic
  name or SKU, so the count and the rows match the uncounted route.

## Verification

- **Go:**
  - Each counted route's `total` equals the rows reached by paging to the end, with and
    without each filter.
  - With no outlet grant, a route answers 404; with a foreign outlet, 404; when access
    cannot be checked, 503. In every case no count is read.
  - A guard error never reaches `InternalErr`. A test proves the guard runs inside the
    snapshot: an insert committed after the guard and before the count is not seen.
  - Each overview count equals the counted route's `total` for the same filter.
  - A bad filter is a 400.
  - The dispense counted routes' existing tests still pass unchanged.
- **Vitest, per page:**
  - The tab in the URL is the selected tab.
  - Changing tab or filter resets `page` and keeps the other filters.
  - The pager shows "1–25 of N".
  - "Not classified" is hidden without a tax scheme.
  - The overview tile links point at `?tab=`.
- **e2e:**
  - From the overview, "Overdue" lands on the Overdue tab, and its total equals the tile's
    count.
  - On a catalogue with more than 25 products, Next then Previous returns to the same
    first row, and a reload on page 2 keeps page 2.
  - A search survives paging.
  - Expiry & safety's "By batch" keeps the batch across a reload.
- **Contract docs:** `docs/api/domains/pharmacy.md` lists the new routes. Regenerate with
  `go run ./cmd/contractdocs`, then check with `-check`.
