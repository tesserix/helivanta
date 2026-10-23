# Detail pane and full-width lists

Resolves #1303. The UX review was done with a specialist (summarised below). The user decided
on **split where it fits, full page otherwise, and no slide-over sheet**.

## Intent

Opening a bill, a product or a safety concern puts its detail _below_ the list, so the page
scrolls away from the row that was opened. On a wide screen the content is capped at
`max-w-6xl` (about 1150px, `packages/ui/src/hms-shell.tsx`), so a third of the page is empty.

Pharmacy work is done row after row:

- reconciling a supplier's statement bill by bill;
- recording payments;
- classifying unclassified products.

The list is the pharmacist's place, so it must stay visible and clickable while a record is
open.

Success:

- On a laptop or larger, opening a bill shows it beside the list, and the list does not move.
- Next goes to the following bill without touching the list.
- A reload or a shared link reopens the same bill on the same tab and page.
- On a phone the bill takes the screen, and Back returns to the same row.

## Decisions

- **D1 Two layouts, chosen by the content area's width, not the screen's.**
  - **Split** applies when the content area is at least 840px wide, which covers a 1366–1440px laptop with the rail expanded or collapsed, and anything larger (agreed, option A).
    - The pane is 400px wide, or 600px once the content area reaches 1400px. At 840px that leaves about 440px for the compact list.
    - The pane is sticky, scrolls on its own, and is as tall as the viewport below the header.
    - While the pane is open the list shows compact rows:
      - bills: document, payee, due date, outstanding;
      - catalogue: brand, SKU, tax badge.
  - **Full page** applies below 840px. The detail replaces the list, with a "Back to bills"
    link to the same tab, page and row.
  - The breakpoint is a container query on the shell's `main` (Tailwind v4 `@container`). The
    rail can expand without the viewport changing, and the layout must follow the space
    actually there.
  - **No slide-over sheet (agreed).** A modal sheet breaks row-to-row work. A non-modal one
    covers the columns being checked, and hides the focused row (WCAG 2.4.11).
  - **Opening below the list is removed.** It is the problem this fixes.
- **D2 The detail has Previous / Next.**
  - In both layouts, the detail header steps to the previous or next row of the current
    page, in list order. It is disabled at the page's ends. It never jumps pages. _(My call:
    crossing pages would need the next page's rows, which is a second request whose order
    can change underneath.)_
- **D3 The open record is in the URL.**
  - `?open=<id>` opens a record, and `?new=1` opens the create form. Both sit beside `tab`,
    the filters, `page` and `cursor`.
  - Opening from a closed list adds a history entry, so phone Back closes the detail.
    Switching rows replaces the entry, so twenty bills do not need twenty Backs.
  - Changing tab, filter or page clears `open` and `new`.
  - An `open` id the server answers 404 shows "This record was not found, or you do not have
    access" inside the pane or page, and nothing else. It never crashes, and it never
    fetches anything else for it.
- **D4 A record that leaves the list stays open.**
  - Recording a payment can take a bill out of Overdue, and classifying a product takes it
    out of Not classified. The pane keeps showing that record.
  - Previous / Next follow the page's rows as they were when the record was opened. That
    snapshot is held in memory for the visit, so a payment that drops the bill out of Overdue
    still leaves Next on the bill below it. _(My call.)_
  - When there is no snapshot, Previous / Next are disabled. That happens after a reload, from
    a shared link, or when the record is not on the page's rows. Nothing is re-fetched to
    place the record. _(My call: guessing its neighbours would be wrong as often as right.)_
  - The list's step-back-when-empty (`useStepBackWhenEmpty`) keeps `open` and `new` in the
    address it moves to. Moving back does not close the pane, and Next keeps following the
    snapshot. _(My call. This is the one page change that does not clear `open`, because the
    person did not ask for it.)_
- **D5 Unsaved edits are never lost silently.**
  - A detail is dirty when any form in it is dirty. The catalogue detail has two: the product
    form and the tax section.
  - Opening another row, Previous / Next, closing, or changing tab, filter or page while dirty
    asks with `ConfirmDialog`: "Discard changes to {name}?", with **Discard** and **Keep
    editing**.
  - A reload or leaving the app while dirty triggers the browser's `beforeunload` prompt.
  - **Leaving the page in the app while dirty asks too.** _(My call: these are client-side
    moves, so `beforeunload` never fires, and the edits would go silently.)_
    - In-app moves cover the rail, the page menu and the phone menu. The shell gains a
      navigation guard that a dirty detail registers, and `HmsShell`'s links consult it.
    - Phone or browser Back, which cannot be cancelled, puts the detail's entry back and then
      asks.
  - While a save is in flight, switching is disabled.
  - There is no autosave and no local draft. These are financial and regulated records.
- **D6 Focus and keyboard.**
  - The split pane is a non-modal `region` labelled with the record. It follows the list in
    the DOM, so Tab goes list, then pane.
  - The open row carries `aria-current="true"` and a selected style made from tokens.
  - **Viewing** moves focus to the pane's heading. Today `reveal()` focuses the first input,
    which drops keyboard and screen-reader users into an edit field.
  - **Creating** focuses the first field.
  - Escape closes the pane, unless a select or combobox inside it is open. Focus returns to the
    row's Open button, or to the list heading if the row has gone.
  - No J/K shortcuts in this slice.
- **D7 Create uses the same pane or page.**
  - It is titled "New supplier bill" or "New product".
  - On save it becomes the view of the new record, as `onSaved` already does.
  - "Save and record another" is not added: the user has not confirmed that bills arrive in
    batches. _(My call: add it once confirmed.)_
- **D8 Full width is a page's declared choice.**
  - A page entry in `packages/ui/src/zones.ts` gains `width?: "full"`. The default is the
    current `max-w-6xl`. The shell already resolves the current page from `zones.ts` for its
    navigation, and applies the cap from there.
    - Zone navigation lives only in `zones.ts` (frontend rule).
    - One declaration per page, enforced by the zone types and their tests, beats a wrapper
      each page has to remember.
  - "Full" fills the content area up to 1760px, left-aligned as now (#1296).
  - Inside a full page:
    - prose is at most about 70ch;
    - forms are at most about 640px and two columns;
    - numeric columns are right-aligned and `tabular-nums`;
    - a wide table scrolls inside its panel, never the page.
  - Settings and form-only pages keep the cap.

## Slices

1. **Shell and Supplier bills (this spec's first plan).**
   - The shell gets the `width` option and the `main` container.
   - Supplier bills becomes full width, with split / full page, the URL, the dirty guard,
     Previous / Next and focus. Its Inventory cost audit tab becomes full width only; it has
     no detail.
2. **Product catalogue.** Split, two forms in the dirty guard, and the tax section. "Save and
   next" for the Not-classified run follows if the user wants it.
3. **Expiry & safety.** First check how concerns are reviewed. The specialist's assumption is
   batch by batch.

The Dispense queue and the Overview stay as they are: the counter flow is a different task.

## Not covered

- A slide-over sheet or a resizable split. A resizable split adds saved state nobody asked
  for.
- Previous / Next across pages.
- Keyboard row shortcuts (J/K).
- Pages outside PharmaConnect.

## Verification

- **Vitest:**
  - The open record comes from `?open=`, and a 404 shows the not-found message.
  - Previous / Next follow list order, and are disabled at the page's ends.
  - A record that leaves the list stays open, and Next goes to the following row.
  - Dirty plus switching asks and keeps the edits on Keep editing.
  - Viewing focuses the heading, and creating focuses the first field.
  - Escape returns focus to the row.
  - Changing tab clears `open`.
  - A page without `width` keeps `max-w-6xl`, and a `width: "full"` page drops it.
- **e2e at 1366 (rail collapsed and expanded), 1440, 1920 and 390 wide:**
  - The split appears only when the content area is at least 840px, otherwise the full page.
  - The list does not scroll when a row opens.
  - Phone Back closes the detail onto the same row.
  - A reload keeps the open bill.
- **Accessibility:** the pane is a labelled region, the open row has `aria-current`, and
  `csp.spec.ts` and the existing axe checks pass.
- **Measured while building:** the content area is 854px at 1366 with the rail expanded, the narrowest laptop case, so the threshold is 840px rather than the 860px first planned. It is 990px at 1366 collapsed and 1008px or more at 1440. The e2e test records each width.
