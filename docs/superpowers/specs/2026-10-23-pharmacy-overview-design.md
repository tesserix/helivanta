# PharmaConnect overview: what needs attention

Resolves #576 (slice 2 of the PharmaConnect rework; slice 1 was #1295). Decisions made with the
user on 2026-10-23: an overview page is PharmaConnect's landing page; it shows exact counts in
priority order, each linking to the list already filtered; nothing the system cannot measure is
shown.

## Intent

A pharmacist opening PharmaConnect at the start of a shift should see, in one place and in
order of urgency, what needs them: patients waiting, stock about to be written off, safety
concerns, money due, and gaps in setup. Today they have to visit five pages to find out.

Success: the overview loads in one view, every number on it is exact, and each number opens
the list it counts, already filtered to those rows.

## Anti-goals

- No placeholder or zero tile for something not measured. A tile the person may not see is
  absent, never "0".
- No figure computed in the browser from a page of rows.

## Decisions

- **D1 The overview is the landing page.**
  - `/pharmacy` becomes the overview. The Dispense queue moves to `/pharmacy/dispense`.
  - `/pharmacy` with any of the queue's parameters (`tab`, `page`, `cursor`, `q`) redirects
    there with the query string intact, so shared queue links keep working. Bare `/pharmacy`
    is the overview. _(My call.)_
  - The page list gains **Overview** first, above the Dispensing section, with no section
    heading of its own.
  - The page's permission is `pharmacy.catalogue.read`, held by every outlet role:
    pharmacist, owner, purchasing and inventory.
  - A doctor (holding only `pharmacy.dispense.read`) does not see the overview. The zone opens
    at the first permitted page (`visibleZones`), which for them is the Dispense queue. A
    doctor who opens `/pharmacy` directly is sent there client-side.
  - Owner, purchasing and inventory do not hold `pharmacy.dispense.read`, so they see no
    tile 1. This is intended.
- **D2 Tiles, in this order.** Each is shown only when the person holds its permission and,
  for outlet tiles, an outlet is selected and they have access to it.

  | #   | Tile             | Count                                                                                                                      | Permission                                                                                                                        | Opens                                                                       |
  | --- | ---------------- | -------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
  | 1   | Patients waiting | waiting dispenses, and how long the oldest has waited                                                                      | `pharmacy.dispense.read`                                                                                                          | Dispense queue, Waiting                                                     |
  | 2   | Stock expiring   | stock lines (a batch at a location, the stock register's rows) with stock on hand: expired; within 30 days; within 90 days | `pharmacy.stock.read`                                                                                                             | Stock on hand, `balance=on_hand` and `expiry=expired`, `due_30` or `due_90` |
  | 3   | Safety concerns  | concerns recorded (`created_at`) in the last 30 outlet-local days, today included                                          | `pharmacy.stock.read`                                                                                                             | Expiry & safety, `recorded=30d`                                             |
  | 4   | Supplier bills   | bills with an amount outstanding: overdue; due within 7 days                                                               | `pharmacy.accounts.read`                                                                                                          | Supplier bills, `due=overdue` or `due=week`                                 |
  | 5   | Setup gaps       | active and discontinued products with no tax classification in force; whether the outlet has a registration in force       | the route's own `pharmacy.catalogue.read` (products, always present); `pharmacy.tax.manage` (registration, and the link to Setup) | Catalogue `tax=unclassified`; Setup                                         |
  - Tile 1 is hospital-wide: dispenses are not outlet-scoped. Its label says so.
  - Tiles 2–5 are for the selected outlet.
  - Tile 2:
    - "Within 30 / 90 days" are cumulative, matching the stock register's own filters, so a
      tile's number equals the rows its link shows.
    - It counts stock lines, not distinct batches, because the register lists one row per
      batch per location. It is worded "stock lines". _(My call.)_
    - It counts at the moment of reading with no location filter, as the register does by
      default. The link carries no `as_of`, so a link opened later shows the stock then.
  - Tile 4 counts bills, not money. Bills are per currency, so a sum needs per-currency
    grouping; that can follow. _(My call.)_

- **D3 Counts come from the server.**
  - Tile 1 reads the existing counted route `GET /pharmacy/dispenses/waiting?limit=1`. The
    total is the count, and the one row, oldest first, gives the oldest wait.
  - Tiles 2–5 come from one new route, `GET …/pharmacies/:pharmacy/outlets/:outlet/overview`,
    with permission `pharmacy.catalogue.read`.
    - It runs inside the existing outlet-access transaction, so no grant means 404 (#1285).
    - It returns a section only for each permission the principal holds, read from the
      request's resolved permission set (`authz.PermissionsFrom`). An absent section means
      "not permitted"; a present zero means zero. Failure to resolve permissions is the
      route's 503, as everywhere.
    - It returns `as_of`: the server's read time, in the outlet's time zone, shown on the
      outlet tiles. Tile 1 is the live queue and shows no time.
    - Its dates are the outlet's local date, as the register uses.
    - It is not paginated: it is a fixed handful of counts. It is allowlisted like the
      in-force tax route.
- **D4 Every link lands filtered.** In this slice, each target page reads the filter its tile
  sets from the URL:
  - Stock on hand reads `expiry` and `balance`.
  - Supplier bills reads `due`. The server gains `?due=overdue|week` on the payables list:
    - Both values keep only bills with `outstanding_minor > 0`; an over-credited bill counts
      as settled.
    - overdue: `due_on` before the outlet's today, strictly.
    - week: `due_on` between today and today + 7, both inclusive.
    - Without `due`, the list is unchanged.
    - An unknown or repeated `due` is a 400.
  - Expiry & safety reads `recorded=30d`. The safety incident list today requires a
    `batch_id`; it gains an outlet-wide mode when `batch_id` is absent and `recorded=30d` is
    given, newest first, and the page shows that list above the per-batch view. This is the
    "safety log" the tile opens. _(My call: without it the tile could link to nothing it
    counts.)_
  - The catalogue reads `tax=unclassified`.
  - The filter shown on the page reflects the URL. Slice 3 moves these lists fully onto
    Previous / Next and URL state; this slice only makes the one filter arrive.
  - _Superseded by slice 3 ([paged pharmacy lists](2026-10-23-paged-pharmacy-lists-design.md)):_
    the tiles now link to tabs. Bills are `?tab=overdue` and `?tab=week`, safety is `?tab=recent`,
    and the catalogue keeps `?tax=unclassified`. Each list reads its counted route, and the old
    links still open sensibly.
- **D5 Freshness.**
  - The overview refreshes on the queue's poll interval.
  - It shows the time it was read ("as of 09:42"), in the outlet's time zone.
  - A section that fails to load says so in its tile, with retry. The others still show.
    A failure is never a zero.
- **D6 Empty and quiet.**
  - A tile with nothing to act on says so plainly ("No patients waiting"), with its link.
  - The overview uses the same pharmacy and outlet picker and selection as the other outlet
    pages, including defaulting to the first outlet. The links rely on that shared
    selection, as every pharmacy page does.
  - When there is no outlet to show (none registered, or no grant: #1285), the outlet tiles
    are replaced by that page's existing message.

## Not covered

- Tiles for:
  - the verification queue;
  - fulfilment blockers;
  - reorder levels;
  - the Schedule H1/X register;
  - recalls with dispensed units.

  None of these is recorded yet. Each arrives with the slice that records it, and #576 is
  noted accordingly.

- Comparisons against the same weekday.
- Charts.
- A shared tile framework (#573).
- Open or closed safety holds: concerns are an append-only journal with no resolution state
  (user decision, 2026-10-23).

## Verification

- Go:
  - Overview sections appear only for held permissions.
  - Each count equals the matching list, filtered, paged to the end. This is checked for:
    - every expiry bucket, including the 30-day boundary on the outlet's date;
    - safety in the last 30 days;
    - bills overdue and due this week, including a fully paid bill, which is excluded;
    - unclassified products, including a withdrawn one, which is excluded;
    - a registration in force, and one not yet in force.
  - A missing outlet grant is 404.
  - Another tenant's outlet is invisible.
  - `?due=` is validated: an unknown value is 400.
- Vitest:
  - Tiles render in order.
  - An absent section renders no tile.
  - A failed section shows a failure, not zero.
  - The oldest wait comes from the first waiting row.
  - Links carry the filters.
  - The target pages apply a filter from the URL.
- e2e:
  - With seeded data, the overview's expiring count equals the rows on the linked filtered
    stock page.
  - The waiting count matches the queue's total.
  - `/pharmacy?tab=waiting` redirects to `/pharmacy/dispense?tab=waiting`.
- Contract docs:
  - The new route.
  - `?due=` on the payables list.
  - The outlet-wide `recorded=30d` safety list.
  - The pharmacy domain doc.
