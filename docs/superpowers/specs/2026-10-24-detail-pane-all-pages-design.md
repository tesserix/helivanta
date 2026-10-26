# Detail pane on the remaining pharmacy pages

Continues #1303. It builds on [the detail-pane spec](2026-10-23-detail-pane-design.md) (D1–D8),
which shipped for Supplier bills in #1312. D1–D8 apply unchanged to every page here; this
spec covers only what each page adds or decides differently.

## Intent

Supplier bills now opens a record beside its list, and the user confirmed that is what they
wanted: "i didnt want empty right side". The user asked for the same on every pharmacy page,
"including outlets". Today the other pages either open the record below the list (Product
catalogue, Stock on hand) or choose it with a dropdown (Expiry & safety, Receiving, Outlets &
access). On a wide screen they leave the right side empty.

Success:

- On a laptop or larger, every page below opens its record beside the list. The list stays
  where it was, and Next goes to the following row.
- A reload or a shared link reopens the same record on every one of these pages, at the
  pharmacy and outlet it was opened in (E8).
- On a phone the record takes the screen and starts at its top, and Back returns to the list.

## Pages

| Page              | List (left)                                            | Pane (right)                                                         |
| ----------------- | ------------------------------------------------------ | -------------------------------------------------------------------- |
| Product catalogue | products, paged as now                                 | product form or summary, tax section, revision history               |
| Stock on hand     | stock positions (lot at a location), Load more         | that position's reconciliation and movements                         |
| Receiving         | the location/product/lot chooser, register forms       | the chosen lot at that location: balance, movements, record movement |
| Expiry & safety   | By batch: the outlet's batches; Last 30 days: concerns | the batch's safety history and the record-a-concern form             |
| Outlets & access  | the pharmacy's outlets; pharmacy-level forms below     | that outlet: details, outlet access, tax registration                |

**These keep the capped width, with no pane (my call):**

- **Dispense queue.** Each row already is the whole record (patient, waiting since) plus its one
  action, Dispense. A pane would add a click per patient at the counter.
- **Medication list.** A medication is a name and a strength, and there is nothing more to
  open.
- **Overview.** It is tiles that link into the lists above.

## Decisions

- **E0 Every page with a pane is `width: "full"` (my call).** That is all five pages above.
  Without it the split fits inside the 1150px cap and the right side stays empty, which is
  what the user asked to remove. D8's "settings and form-only pages keep the cap" still holds
  for pages with no pane. Outlets & access and Receiving are no longer form-only once they have
  a pane. Their forms keep D8's 640px limit.

- **E1 One shared mechanism, extracted from Supplier bills first.**
  - Supplier bills holds the pane's behaviour inline (`accounts-panel.tsx`):
    - the row snapshot, and Previous / Next;
    - the leave guard and the in-page link hold;
    - Escape, and focus on open and on close.
  - That behaviour moves into one shared hook in `apps/shell/lib/` and one detail header
    (title, Previous, Next, Close) in `@helivanta/ui`. A page supplies its rows, the open
    record's name, its noun for the labels ("Previous product") and the body.
  - Supplier bills moves onto it in the first slice. Its existing Vitest and e2e tests pass
    unchanged, and that is the proof the extraction kept the behaviour.
  - Five copies of focus and history rules would drift. One copy is changed once.
- **E2 Product catalogue.**
  - It becomes `width: "full"`, with a split or a full page as in D1.
  - Compact rows show brand, SKU and the tax badge (D1).
  - `?open=<product id>` reloads through the existing `GET /catalogue/products/:id`.
  - `?new=1` is "New product".
  - The product form and the tax section both register with the leave guard (D5).
  - The "Save and next" run for Not classified is still not added: the user has not asked for
    it.
- **E3 Stock on hand.**
  - The four filters move into the URL: `location`, `product`, `expiry` and `balance`.
    `expiry` and `balance` are already read from it on arrival. Changing a filter clears
    `open`, as D3 requires.
  - A position is a lot at a location, and its `id` is a hash of the two, which cannot be
    looked up. The URL therefore holds both: `?open=<batch id>&at=<location id>`, cleared
    together.
  - **Backend:** the positions list gains a `batch_id` filter, validated like `location_id`.
    A reload fetches `positions?batch_id=…&location_id=…` with no other filter, so a
    position that has left the filtered list still reopens (D4). No row means the D3
    not-found message. This is the only way to show the position's own figures without a second
    definition of a position.
  - **The snapshot time is not in the URL (my call).** A reload takes a fresh snapshot, and
    the pane reopens the same position at it. The pane already prints "Snapshot ends at …".
    A shared link means "this lot here, now". A link frozen at an old cutoff would show stale
    stock as if it were current.
  - Previous / Next follow the loaded rows (every page of Load more) as they were at open.
  - **Refresh snapshot keeps the pane open (my call).** It reopens the same position at the
    new cutoff and retakes the Previous / Next snapshot from the refreshed rows. Today it
    closes the position, but the person refreshing is usually checking that lot again.
  - The reconciliation period field is a query, not an edit, so it does not register with the
    leave guard.
- **E4 Receiving.**
  - Receiving has no list, so its left side is the chooser (location, product, exact lot) and
    the Register location and Register lot forms. The pane is the chosen lot at the chosen
    location: balance, movements and the record-movement form.
  - It uses the same split, the same sticky pane and the same full page below 840px. There is
    no Previous / Next, because there are no rows.
  - The choice is in the URL: `?location=…&product=…&open=<batch id>`, so a reload keeps it.
  - The record-movement, register-location and register-lot forms all register with the leave
    guard. Changing the chooser while a movement is unsaved asks first.
- **E5 Expiry & safety (batch-centred).**
  - **By batch.**
    - The batch dropdown becomes a list of the outlet's batches: product (SKU and brand when
      the person can read the catalogue, otherwise the product id, as now), lot and expiry.
    - It loads 100 at a time with Load more, as now. There is no counted endpoint for batches,
      and adding one is not needed for this.
    - Opening a batch shows its safety history and the record-a-concern form in the pane.
  - **Last 30 days.**
    - Each concern row opens its batch in the same pane. Previous / Next step through the
      distinct batches in list order, so two concerns about one lot are one stop. _(My call.)_
  - `?open=<batch id>` on either tab.
  - **Backend:** `GET /stock/batches/:id` is added, with permission `pharmacy.stock.read`. A
    batch outside the verified tenant and outlet is 404, like the product and payable reads.
  - The concern form registers with the leave guard. A concern is a permanent regulated
    record.
- **E6 Outlets & access.**
  - **The list is the pharmacy's outlets** (Load more, as the picker loads them now).
  - Opening an outlet shows in the pane:
    - its details (code, name, country, time zone), **read only**. `PUT …/outlets/:outlet`
      is create-or-match: changing an existing outlet is a 409. Editing an outlet needs an
      update command, an audit trail and its own spec, so it is not in this work;
    - its outlet access (the per-outlet access check, then grant-my-access);
    - its tax registration.
  - `?new=1` is "Register outlet" in the pane.
  - Register pharmacy and hospital affiliations belong to the pharmacy, not to an outlet. They
    stay in the list column, below the outlets.
  - **On this page the heading keeps only the Pharmacy picker (my call).** The list is the
    outlet picker, and two outlet choosers on one page would disagree.
  - Opening an outlet here does not change the saved working outlet the other pages use.
    Setting an outlet up is not choosing where to work.
  - **Backend:** `GET /pharmacy/pharmacies/:pharmacy/outlets/:outlet` is added. It has the
    same permission and visibility as the outlets list, and 404 outside the tenant.
  - The access form, the register-outlet form (`?new=1`) and the tax-registration form
    register with the leave guard.
- **E7 Fixes carried in this work.**
  - **The stray "Load more"** (the user's screenshot). The pharmacy and outlet pickers' Load
    more buttons render below each page's content with no label. On Outlets & access they sit
    between the outlet sections and the registration forms.
    - They move into the heading, under their pickers, as "Load more pharmacies" and "Load
      more outlets".
    - `LoadMore` gains an optional `label`. Every pharmacy Load more is named for what it
      loads, including the three each on Receiving, Stock on hand and Expiry & safety. Other
      zones are untouched.
  - **The phone detail opens at its top.** Below 840px, opening a record scrolls the shell's
    content area to the top. Closing it returns to the row it was opened from.
  - **Modified clicks pass through the leave guard.** Cmd, Ctrl, Shift and middle clicks on a
    guarded link open a new tab or window, and nothing in this tab is lost. Today they are
    held and asked about.
  - **Opening or closing the pane keeps the list mounted.** The list does not remount, so
    focus and the row's position survive.
  - **After create, save and close, focus goes to the new record's row** if it is on the
    page, otherwise to the list heading. Today it goes to whichever row was opened last.

- **E8 The URL carries the pharmacy and outlet (agreed: option b).**
  - Every outlet page's address holds `pharmacy=<id>&outlet=<id>` beside its tab, filters,
    page and `open`. A shared link opens the outlet it was copied from.
  - **On arrival** the scope is taken from the URL first, then from the tab's saved choice
    (`sessionStorage`, as now), then the first loaded pharmacy and outlet. Whichever is used is
    written back into the address with a replace, so a copied address always carries it.
  - **Following a link makes its outlet the working outlet for that tab,** and saves it like
    a choice made in the picker.
  - **An outlet in the URL that is not among the pharmacy's loaded outlets is not replaced by
    another one.** The page shows "Choose an outlet" with Load more, as for a saved choice
    today. _(Fail closed: an `open` id from one outlet must never be looked up under
    another.)_ Access is still checked by the server for every request.
  - **Choosing another pharmacy or outlet** clears `page`, `cursor`, `open` and `new`, and
    asks first while a record is unsaved (D5). It also clears `location`, `product` and `at`:
    those ids belong to one outlet, and another outlet would be asked for an id it does not
    have. The tab and the expiry and balance filters are kept.
  - Tabs, pagers and filter links keep `pharmacy` and `outlet` (`useUrlPage`).
  - Rail links stay scope-free: arriving from the rail uses the saved choice.
  - E1's "Supplier bills' tests pass unchanged" covers its behaviour. Its e2e address
    assertions gain the two parameters.

## Slices

Each slice is one PR, backend and frontend together, merged on green.

1. **Shared mechanism, scope in the URL (E8), Supplier bills on it, Product catalogue, and
   E7.** The fixes ride here
   because the shared mechanism is where most of them live.
2. **Stock on hand and Receiving.** These include the positions `batch_id` filter.
3. **Expiry & safety.** This includes `GET /stock/batches/:id`.
4. **Outlets & access.** This includes `GET …/outlets/:outlet`.

## Not covered

- A pane on the Dispense queue, the Medication list or the Overview (see above).
- Counted, paged batch or outlet lists. They keep Load more.
- Snapshot time in the Stock on hand URL.
- Editing an existing outlet.
- Previous / Next across pages, and J/K shortcuts (as in the first spec).
- "Save and record another" and "Save and next".

## Verification

- **Vitest, per page:**
  - `?open=` reopens the record.
  - A 404 shows the not-found message.
  - Previous / Next follow list order.
  - Dirty plus switching asks.
  - Viewing focuses the heading.
  - Changing a filter or tab clears `open`.
- **Shared:**
  - Supplier bills' existing tests pass unchanged after it moves onto the shared mechanism.
  - A modified click on a guarded link is not held.
  - The list does not remount when the pane opens.
- **Go:** each new read is tested:
  - it returns the record;
  - it is 404 for another outlet and for another tenant;
  - it is 403 without the permission.

  The positions `batch_id` filter is tested for:
  - one row back;
  - an unknown batch, refused like an unknown location.

  `go run ./cmd/contractdocs` is regenerated, and `-check` passes.

- **e2e:**
  - Each page opens a record beside its list at 1366 (rail expanded) and 1920, and the row
    does not move.
  - At 390 the record takes the screen, starts at its top, and Back returns.
  - A reload keeps the open record.
  - No "Load more" renders below a pharmacy page's content, and every pharmacy Load more has
    a name.
- **Accessibility:**
  - Every pane is a labelled region, and the open row has `aria-current`.
  - `csp.spec.ts` and the axe checks pass.
