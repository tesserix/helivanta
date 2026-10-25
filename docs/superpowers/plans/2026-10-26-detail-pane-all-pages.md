# Detail pane on the remaining pharmacy pages: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every outlet-scoped pharmacy page with records opens them beside the list, in a
URL that carries the pharmacy, outlet and record, using one shared mechanism.

**Architecture:**

- Supplier bills' inline pane behaviour moves into `useListDetail`, a hook in
  `apps/shell/lib/`, and a `DetailHeader` in `@helivanta/ui`. Each page then supplies its
  rows and its body.
- `ListDetail` always renders its grid, so the list never remounts.
- The workspace takes its scope from the URL and writes it back.
- The plan has four slices. Each is one PR that is merged on green, and the three later
  slices each add one small backend read or filter.

**Tech Stack:** Next.js App Router, React 19, Tailwind v4 container queries, TanStack Query
via `@helivanta/api`, Vitest with `renderWithProviders`, Playwright, Go/gin/gorm.

**Spec:** `docs/superpowers/specs/2026-10-24-detail-pane-all-pages-design.md` (E0–E8). It
builds on `docs/superpowers/specs/2026-10-23-detail-pane-design.md` (D1–D8).

## Global Constraints

- **Threshold.** Split at `@[840px]/main`. The pane is 400px, or 600px at `@[1400px]/main`.
  Below 840px the detail is a full page (D1).
- **URL.** `?open=<uuid>` opens a record and `?new=1` creates one. Opening from closed pushes
  a history entry, and switching rows replaces it (D3). Every outlet page carries
  `pharmacy=<id>&outlet=<id>` (E8).
- **Leave guard.** The dialog reads "Discard changes to {name}?" with **Discard** and **Keep
  editing**. A save in flight holds the move with no Discard (D5).
- **Width.** Every page with a pane declares `width: "full"` in `packages/ui/src/zones.ts`
  (E0).
- **Copy.** Labels are "Previous {noun}" and "Next {noun}". The not-found message is "This
  record was not found, or you do not have access."
- **Frontend rules.**
  - Use `useApiQuery`/`useApiMutation` only.
  - Use design tokens only.
  - Use `SelectControl`, never a native `<select>`.
  - Use `renderWithProviders` for every changed panel test.
- **Backend rules.**
  - Use `respond.*`.
  - Answer 404 for another outlet or another tenant, and 403 for a missing permission.
  - Every route declares a permission.
  - Run `go run ./cmd/contractdocs` and then `-check` when routes change.
- **Gates.** Run these one at a time, and never rebuild the shell while it is serving:
  - `pnpm turbo lint type-check test build format:check`, with
    `NEXT_PUBLIC_ZITADEL_CLIENT_ID=ci-build-client-id` and
    `NEXT_PUBLIC_ZITADEL_ISSUER_URL=http://auth.tesserix.localhost:20080`, using the Node at
    `$HOME/.nvm/versions/node/v24.21.0/bin`;
  - `make lint-shell` and `make lint-go`;
  - `cd backend && GOTOOLCHAIN=go1.27.3 ./scripts/coverage-gate.sh` and `go test -race ./...`.
- **Git.** No AI attribution in commits or PRs. Each PR body says `Part of #1303`, and the
  last PR says `Closes #1303`.

## Review Focus

1. **A link to an outlet that is not loaded or not permitted.**
   - Expected: "Choose an outlet" or "no access" is shown.
   - It must never show another outlet's data under the requested `open` id.
   - Test: Task 3.
2. **A rail link while the URL scope differs from the saved scope.**
   - The rail carries no scope, so arriving uses the saved choice, and the address must then
     be rewritten.
   - Test: Task 3.
3. **A modified click on a pager or tab link while dirty.**
   - Expected: it opens a new tab and holds nothing.
   - Covered by the `holdLinks` path in Task 2 as well as `GuardedLink`.
4. **Refresh snapshot with a position open (Stock on hand).**
   - Expected: the same position stays open at the new cutoff, and Previous / Next is
     retaken.
   - Test: Task 10.
5. **Two concerns about one batch on Last 30 days.**
   - Expected: Next skips to the next distinct batch.
   - Test: Task 14.

---

## Slice 1: shared mechanism, scope in the URL, Supplier bills, Product catalogue, E7

Branch: `feat/1303-detail-pane-all-pages`. The spec is already on it.

### Task 1: Named Load more, and the pickers' Load more moves into the heading

**Files:**

- Modify:
  - `packages/ui/src/load-more.tsx`
  - `packages/ui/src/load-more.test.tsx` (create it if it is absent)
  - `apps/shell/components/pharmacy/pharmacy-workspace.tsx`, lines 121–147 and 183–192
  - every `<LoadMore` in `apps/shell/components/pharmacy/*.tsx`
- Test:
  - `packages/ui/src/load-more.test.tsx`
  - `apps/shell/components/pharmacy/pharmacy-workspace.test.tsx`

**Interfaces:**

- Produces: `LoadMore({ hasMore, isLoading, onClick, label? }: {…; label?: string })`.
  - Its text is `label ?? "Load more"`, and "Loading…" while loading.
  - The class wrapper is unchanged.

- [ ] **Step 1: Write the failing tests.**
  - `load-more.test.tsx` "names what it loads":
    - render `label="Load more outlets"` with `hasMore`;
    - expect `getByRole("button", {name: "Load more outlets"})`.
  - `pharmacy-workspace.test.tsx` "the outlet picker's Load more sits in the heading":
    - mock the outlets list with `has_more: true`;
    - expect the button "Load more outlets" to be inside the element containing the label
      "Outlet" (`closest("header")`, or the `PageHeading` actions container);
    - expect no button named exactly "Load more".
- [ ] **Step 2: Run both tests.**
  - Run: `pnpm --filter @helivanta/ui test -- load-more` and
    `pnpm --filter shell test -- pharmacy-workspace`.
  - Expected: FAIL. The prop is unknown, and the button is at the bottom of the page.
- [ ] **Step 3: Implement.**
  - Add the `label` prop.
  - In `TenantPharmacy`, render each picker's `LoadMore` directly under its `Field` inside
    `actions`. Use the labels "Load more pharmacies" and "Load more outlets", and delete the
    two at the bottom.
  - Name every other pharmacy `LoadMore` by what it loads:
    - stock-register: "Load more locations", "Load more products", "Load more stock
      positions";
    - stock-panel: "Load more locations", "Load more products", "Load more lots";
    - batch-safety: "Load more batches", "Load more products", "Load more locations",
      "Load more concerns";
    - catalogue: "Load more revisions";
    - payable-forms: the noun of each list;
    - registry: "Load more hospital links";
    - tax: "Load more registrations" or "Load more classifications".
  - Drop the now-redundant `<p>` captions in batch-safety lines 317–350.
- [ ] **Step 4: Run the tests and `pnpm --filter shell test -- pharmacy`.**
  - Expected: PASS.
  - Fix any existing test that queried `{name: "Load more"}` to use the new name. Ledger
    each fix as a ruling.
- [ ] **Step 5: Commit**: `fix(pharmacy): name every Load more, and keep the pickers' in the heading`.

### Task 2: The leave guard lets modified clicks through, and exposes `useConfirmLeave`

**Files:**

- Modify:
  - `packages/ui/src/navigation-guard.tsx`
  - `packages/ui/src/index.ts`
- Test: `packages/ui/src/navigation-guard.test.tsx`

**Interfaces:**

- Produces:
  - `isModifiedClick(event: MouseEvent): boolean`. It is true for `metaKey || ctrlKey ||
shiftKey || altKey || button !== 0`, and it is exported.
  - `useConfirmLeave(): (go: () => void) => void`. It returns the provider's `confirm`
    without registering a guard, and it throws without a provider, like `useLeaveGuard`.

- [ ] **Step 1: Write the failing tests.**
  - "a Cmd-click on a guarded link while dirty is not held":
    - `fireEvent.click(link, { metaKey: true })`;
    - expect no dialog;
    - expect `event.defaultPrevented` to be false.
  - Repeat with `button: 1`.
  - "useConfirmLeave asks while another component is dirty".
- [ ] **Step 2: Run it.**
  - Run: `pnpm --filter @helivanta/ui test -- navigation-guard`.
  - Expected: FAIL. The dialog opens, and the hook is not exported.
- [ ] **Step 3: Implement.** In `useGuardedLinkClick`, return early when
      `isModifiedClick(event)`. Export both names.
- [ ] **Step 4: Run it.** Expected: PASS, 9 old tests plus 3 new.
- [ ] **Step 5: Commit**: `fix(ui): modified clicks on guarded links open a new tab untouched`.

### Task 3: The scope lives in the URL (E8)

**Files:**

- Modify:
  - `apps/shell/lib/use-url-page.ts`
  - `apps/shell/components/pharmacy/pharmacy-workspace.tsx`
  - `e2e/tests/pharmacy-detail-pane.spec.ts`
  - `e2e/tests/pharmacy-overview.spec.ts`
- Test:
  - `apps/shell/lib/use-url-page.test.ts` (create it if it is absent)
  - `apps/shell/components/pharmacy/pharmacy-workspace.test.tsx`

**Interfaces:**

- Produces:
  - `SCOPE_KEYS = ["pharmacy", "outlet"] as const`, exported from `use-url-page.ts`.
  - Every href `useUrlPage` builds (`first`, `previousHref`, `nextHref`, `hrefFor`) keeps the
    current `pharmacy` and `outlet` values.
  - `useStepBackWhenEmpty` is unchanged; it inherits this.
  - The workspace's resolved scope is always reflected in the address.

- [ ] **Step 1: Write the failing tests.**
  - `use-url-page.test.ts` "links keep the scope":
    - the params are `pharmacy=P&outlet=O&tab=all`;
    - expect `hrefFor({tab: "overdue"})` to contain `pharmacy=P&outlet=O`.
  - `pharmacy-workspace.test.tsx`, each with the live `next/navigation` mock from
    `accounts-panel.test.tsx`:
    - (a) "the URL's outlet wins over the saved one": `sessionStorage` holds outlet A and the
      URL holds B; expect B's outlet name in the picker.
    - (b) "the resolved scope is written back with a replace": with no URL scope, expect
      `nav.calls` to have one `replace` whose href carries `pharmacy` and `outlet`, and no
      `push`.
    - (c) "an outlet in the URL that is not loaded is not replaced": expect "Choose an
      outlet", and no outlet panel rendered.
    - (d) "choosing another outlet clears open and new": start at `open=<uuid>`, pick another
      outlet, and expect the replace href to have no `open`.
    - (e) "choosing another outlet while dirty asks first": register a dirty guard with
      `useLeaveGuard` in a test child, pick another outlet, expect the dialog, click Keep
      editing, and expect the outlet to be unchanged.
    - (f) "the URL's scope is saved as the working outlet": expect `sessionStorage` to hold
      B.
- [ ] **Step 2: Run them.** Expected: FAIL on every new case.
- [ ] **Step 3: Implement.**
  - In `use-url-page.ts`, have `query(list)` append the `SCOPE_KEYS` values from the current
    params, after the list's own values.
  - In `TenantPharmacy`:
    - read `pharmacy` and `outlet` from params, keeping them only if they pass
      `z.string().guid()`;
    - `selection` takes the URL values first, then `sessionStorage`;
    - in an effect, once `pharmacy` and `outlet` resolve, write the resolved ids: if the
      params differ, call `router.replace` with them set, and save to `sessionStorage`;
    - `select()` deletes `page`, `cursor`, `open` and `new`, sets the new scope, and runs
      inside `useConfirmLeave()`;
    - a URL `outlet` id absent from `outlets.items` keeps today's "Choose an outlet" branch.
      Add a comment at that decision point that this fails closed (see spec E8).
  - In e2e, replace the end-anchored address regexes with a helper in
    `e2e/tests/support/pharmacy.ts`:
    `expectList(page, path, params: Record<string,string>, { open?: boolean })`. It parses
    `page.url()` and asserts the path, each param, and that `open` is absent unless
    requested.
- [ ] **Step 4: Run them.**
  - Run: `pnpm --filter shell test -- pharmacy use-url-page`.
  - Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): the address carries the pharmacy and outlet, so a shared link opens where it was copied`.

### Task 4: `ListDetail` keeps the list mounted, and a phone detail opens at its top

**Files:**

- Modify:
  - `packages/ui/src/list-detail.tsx`
  - `packages/ui/src/hms-shell.tsx`, adding `data-hms-scroll` on the scroll column
- Test: `packages/ui/src/list-detail.test.tsx`

**Interfaces:**

- Produces: the same `ListDetail` props.
  - The wrapper grid and the list `div` always render.
  - The grid columns and `hidden @[840px]/main:block` apply only while `detail !== null`.
  - The `section` renders only while open.

- [ ] **Step 1: Write the failing tests.**
  - "the list is not remounted when a record opens": render the list as a child holding a
    `useRef` counter, or capture its DOM node; rerender with a detail; expect the same node
    (`toBe`).
  - "on a narrow main, opening scrolls the content area to the top":
    - wrap the component in `<div data-hms-scroll>` and stub its `scrollTo`;
    - make the list `div` report `display: none` (mock `getComputedStyle`);
    - open, and expect `scrollTo({top: 0})`.
  - "on a wide main it does not scroll".
- [ ] **Step 2: Run it.**
  - Run: `pnpm --filter @helivanta/ui test -- list-detail`.
  - Expected: FAIL. The node differs, and there is no scroll.
- [ ] **Step 3: Implement.**
  - Use one tree, with the grid classes switched by `open`.
  - In a layout effect on the transition from closed to open, if the list `div`'s computed
    `display` is `none`, call
    `section.closest("[data-hms-scroll]")?.scrollTo({ top: 0 })`.
  - Add `data-hms-scroll` to the shell's
    `relative flex min-w-0 flex-1 flex-col overflow-y-auto` div.
- [ ] **Step 4: Run it, and run `pnpm --filter @helivanta/ui test`.** Expected: PASS.
- [ ] **Step 5: Commit**: `fix(ui): the list stays mounted beside its record; a phone record opens at its top`.

### Task 5: `useListDetail` and `DetailHeader`

**Files:**

- Create:
  - `apps/shell/lib/use-list-detail.ts`
  - `apps/shell/lib/use-list-detail.test.tsx`
  - `packages/ui/src/detail-header.tsx`
  - `packages/ui/src/detail-header.test.tsx`
- Modify:
  - `packages/ui/src/index.ts`
  - `apps/shell/components/pharmacy/payable-forms.tsx`, moving `FormState` and
    `useReportState` out

**Interfaces:**

- Consumes:
  - `useDetail()` (`apps/shell/lib/use-detail.ts`);
  - `useLeaveGuard` and `isModifiedClick` (Task 2).
- Produces:

```ts
export type FormState = (dirty: boolean, pending: boolean) => void;
export function useReportState(
  report: FormState | undefined,
  dirty: boolean,
  pending: boolean,
): void;

export function useListDetail<Row extends { id: string }>(options: {
  rows: Row[]; // the list's rows now, in list order
  name: (row: Row) => string; // "TEST-001", the brand name, ...
  fallbackName: string; // "this bill" when the open row is not in the rows
  newName: string; // "the new bill"
  headingId: string; // the list heading to focus when the row has gone
}): {
  detail: ReturnType<typeof useDetail>;
  isOpen: boolean;
  openName: string | null; // the open record's name, from the snapshot or the rows
  busy: boolean; // some registered form is saving
  formState: (key: string) => FormState; // stable per key; dirty = any key dirty
  openRow: (id: string) => void; // guard, snapshot the rows, then open
  create: () => void;
  saved: (id: string) => void; // after a create: open id, and focus returns to it
  step: (by: -1 | 1) => void;
  canStep: (by: -1 | 1) => boolean; // false without a snapshot, at the ends, or busy
  close: () => void; // guard, focus [data-open-row=id] or #headingId
  holdLinks: (event: React.MouseEvent) => void; // onClickCapture; modified clicks pass
  paneRef: React.RefObject<HTMLDivElement | null>; // Escape, and focus the first input on create
  rowProps: (id: string) => { "aria-current"?: "true" };
  openProps: (id: string) => { "data-open-row": string };
};
```

```tsx
export function DetailHeader(props: {
  title: ReactNode;
  subtitle?: ReactNode;
  headingRef?: Ref<HTMLHeadingElement>; // the record focuses it when viewing (D6)
  noun: string; // "bill" → "Previous bill" / "Next bill"
  previous: { disabled: boolean; onClick: () => void };
  next: { disabled: boolean; onClick: () => void };
  closeLabel: string;
  onClose: () => void;
  stepping?: boolean; // default true; false hides Previous / Next (Receiving)
}): JSX.Element;
```

- The behaviour is exactly that of `accounts-panel.tsx` lines 75–163. That covers:
  - the snapshot, taken on `openRow` and cleared on `close`;
  - the `mounted` ref, which ignores a late `saved` once the component is unmounted;
  - the Escape rule, which skips `[role="listbox"],[aria-expanded="true"]`;
  - focus on create.
- The new behaviour is that `saved(id)` sets the return target to `id` (E7).

- [ ] **Step 1: Write the failing tests.**
  - `use-list-detail.test.tsx`: drive a small harness component with three rows. Port the
    accounts-panel "bill detail pane" cases to it:
    - Previous / Next order and the ends;
    - no snapshot after a reload, so both are disabled;
    - a row that leaves the list keeps Next on the following one;
    - dirty plus Next asks, and Keep editing keeps the edits;
    - busy disables stepping;
    - Escape closes, except inside an open listbox;
    - focus returns to `[data-open-row]`, or to the heading when the row has gone;
    - a late `saved` after unmount does nothing.
  - Add: "after create and save, close focuses the new record's row".
  - Add: "a Cmd-click on a held link passes through".
  - `detail-header.test.tsx`: it renders "Previous bill" and "Next bill" buttons, honours
    `disabled`, and its close button carries `closeLabel`.
- [ ] **Step 2: Run them.** Expected: FAIL, because the modules do not exist.
- [ ] **Step 3: Implement** by moving the code from `accounts-panel.tsx` into the hook. Do
      not change Supplier bills yet.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(shell): one list-and-record mechanism for every pharmacy page`.

### Task 6: Supplier bills moves onto the shared mechanism

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/accounts-panel.tsx`
  - `apps/shell/components/pharmacy/payable-forms.tsx`, where `PayableHistory`'s header
    becomes `DetailHeader` with `noun="bill"` and `closeLabel="Close document"`
- Test: the existing `accounts-panel.test.tsx` and `e2e/tests/pharmacy-detail-pane.spec.ts`,
  unchanged apart from Task 3's address helper.

- [ ] **Step 1: Run the accounts-panel tests first, as the baseline.** Expected: PASS. Record
      the count in the ledger.
- [ ] **Step 2: Replace lines 75–163 with `useListDetail`.**
  - `rows` are the page rows, and `name` is `row.reference`.
  - `data-open-bill` becomes `openProps(id)`, and `aria-current` becomes `rowProps(id)`.
  - `PayableForm`'s `onSaved` becomes `saved`.
- [ ] **Step 3: Run the accounts-panel tests.** Expected: the same count, all PASS.
- [ ] **Step 4: Commit**: `refactor(pharmacy): supplier bills on the shared list-and-record mechanism`.

### Task 7: Product catalogue opens beside its list

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/catalogue-panel.tsx`
  - `apps/shell/components/pharmacy/tax-classification.tsx`, giving `ProductTaxSection` an
    `onStateChange?: FormState`
  - `packages/ui/src/zones.ts`, giving the catalogue `width: "full"`
- Test:
  - `apps/shell/components/pharmacy/catalogue-panel.test.tsx` (create it if it is absent;
    otherwise extend it)
  - `packages/ui/src/zones.test.ts`

**Interfaces:**

- Consumes: `useListDetail` and `DetailHeader` (Task 5).
- The record is fetched by id:
  - `useApiQuery<PharmacyProduct>(pharmacyKey(scope, "catalogue", "product", id),
\`${pharmacyRoot(scope)}/catalogue/products/${id}\`)`;
  - a 404 shows the not-found copy;
  - saving invalidates this key and the list.

- [ ] **Step 1: Write the failing tests.**
  - "`?open=<id>` reopens the product": mock the GET and expect a region named
    `Product {brand}`.
  - "a 404 shows the not-found message".
  - "Previous / Next product follow list order".
  - "the tax section's unsaved edit asks before Next": make the tax form dirty, press Next,
    and expect the dialog.
  - "viewing focuses the heading; Add product focuses the first field and is titled New
    product".
  - "changing tab clears open".
  - "compact rows show brand, SKU and the tax badge only".
  - In `zones.test.ts`, the catalogue page has `width: "full"`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Remove `reveal`, `selected` and the below-list `Panel`.
  - Wrap the list in `ListDetail` with `backLabel="Back to products"`. The heading, tabs and
    search stay above the split, as on Supplier bills.
  - In the pane, render `DetailHeader` (`noun="product"`, `closeLabel="Close product"`),
    then:
    - the form or the summary, with `formState("product")`;
    - the tax section, with `formState("tax")`;
    - the history.
  - The new-product form uses `saved`.
- [ ] **Step 4: Run them, and run `pnpm --filter shell test -- pharmacy`.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): products open beside the catalogue`.

### Task 8: e2e, docs, gates and the PR for slice 1

**Files:**

- Create: `e2e/tests/pharmacy-catalogue-pane.spec.ts`
- Modify:
  - `e2e/tests/navigation.spec.ts`
  - `docs/standards/frontend.md` §6
  - `e2e/tests/support/pharmacy.ts`, adding `addProduct` if it is not already there

- [ ] **Step 1: Write the e2e tests.**
  - The catalogue at 1366 (rail expanded) and at 1920:
    - a product opens beside the list, and the row moves by 2px or less;
    - Next product works;
    - a reload keeps it.
  - At 390:
    - the product takes the screen, and `[data-hms-scroll]` `scrollTop` is 0 after
      opening from a scrolled list;
    - Back returns.
  - A shared link: open a product, copy `page.url()`, set `sessionStorage` to another outlet
    in a new page of the same context, go to the URL, and expect the same product.
  - In `navigation.spec.ts`, "no unnamed Load more on any pharmacy page": for each pharmacy
    route, expect `getByRole("button", {name: "Load more", exact: true})` to have count 0.
- [ ] **Step 2: Run the e2e tests.**
  - Run `HELIVANTA_PG_PORT=5434 make seed`, restart the API, then run
    `pnpm --filter e2e exec playwright test pharmacy-catalogue-pane pharmacy-detail-pane navigation`.
  - Expected: PASS.
- [ ] **Step 3: Update `frontend.md` §6.** It should say that a list page with records uses
      `useListDetail`, `DetailHeader` and `ListDetail`, and that outlet pages carry `pharmacy`
      and `outlet` in the URL.
- [ ] **Step 4: Run every gate in Global Constraints.** Expected: all green.
- [ ] **Step 5: Commit.** Then do the final review (fresh reviewer), push, and open the PR
      with `Part of #1303`. Merge on green.

## Slice 2: Stock on hand and Receiving

Branch: `feat/1303-pane-stock`, from main after slice 1 merges.

### Task 9: The positions list filters by `batch_id`

**Files:**

- Modify: `backend/internal/modules/pharmacy/stock_register.go`, in `listStockPositions`
- Test: the existing stock register Go test file (`stock_register_test.go`, or wherever
  `listStockPositions` is tested)

- [ ] **Step 1: Write the failing tests.**
  - "batch_id with location_id returns that one position".
  - "an unknown batch_id is refused like an unknown location_id": it gets the same status as
    the existing location case. Check that status first and assert it.
  - "a batch from another outlet is refused".
- [ ] **Step 2: Run them.**
  - Run: `cd backend && go test ./internal/modules/pharmacy/ -run StockPositions`.
  - Expected: FAIL, because the filter is ignored.
- [ ] **Step 3: Implement.**
  - Add `batch, ok := stockFilter(c, "batch_id", false)`.
  - Add `stockReference(tx, "pharmacy_stock_batches", …, batch)`.
  - Add `Where("batch_id=?", batch)` when it is set.
  - Apply the same filter to the CSV export, so the two never disagree.
- [ ] **Step 4: Run them, then `make lint-go`.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): stock positions filter by lot`.

### Task 10: Stock on hand opens a position beside the register

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/stock-register.tsx`
  - `apps/shell/components/pharmacy/pharmacy-workspace.tsx`, where `initialExpiry` and
    `initialBalance` are now read inside
  - `packages/ui/src/zones.ts`
- Test: `apps/shell/components/pharmacy/stock-register.test.tsx`

**Interfaces:**

- The URL holds `location`, `product`, `expiry` and `balance`, with `all` left out.
  `open=<batch id>&at=<location id>` names the position.
- `useListDetail` rows are `{ id: \`${batch_id}:${location_id}\` }`.
  - `openRow` writes both parameters. Extend `useDetail` with
    `openHref(id, extra?: Record<string,string>)`, and clear `at` with `open`.
  - The rows are every loaded position, in order.
- A reload fetches
  `positions?as_of=<now>&batch_id=…&location_id=…` with no other filter.
- A filter change is a `router.replace` that drops `open` and `at`, through the guard.

- [ ] **Step 1: Write the failing tests.**
  - "`?open&at` reopens the position, fetched with batch_id and location_id only": assert
    the request URL.
  - "no matching row shows the not-found message".
  - "changing a filter clears open and at".
  - "Refresh snapshot keeps the position open and retakes Previous / Next": Next is disabled
    before the refresh only if it was at the end, and is enabled after it when a new row
    follows.
  - "Previous / Next position step through loaded rows across Load more".
  - "viewing focuses the heading".
  - "the period field does not register a guard": type a date, press Next, and expect no
    dialog.
  - The zones test: the stock page has `width: "full"`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - The filters come from the URL.
  - Wrap the register in `ListDetail`, with `backLabel="Back to stock"`.
  - Compact rows show product and lot, location, and on hand.
  - `PositionDetail` takes `DetailHeader` (`noun="position"`), and loses its own
    `scrollIntoView`.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): stock positions open beside the register`.

### Task 11: Receiving shows the chosen lot in the pane

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/stock-panel.tsx`
  - `packages/ui/src/zones.ts`
- Test: `apps/shell/components/pharmacy/stock-panel.test.tsx` (create it if it is absent)

**Interfaces:**

- The URL holds `location=…&product=…&open=<batch id>`.
- There is no Previous / Next: render `DetailHeader` with `stepping={false}` (Task 5).
- The guard keys are `"movement"`, `"location"` and `"lot"`. The chooser changes run through
  `useConfirmLeave`.

- [ ] **Step 1: Write the failing tests.**
  - "a reload keeps the chosen location, product and lot".
  - "the lot opens in the pane, titled `{lot} at {location code}`".
  - "an unsaved movement asks before the chooser changes".
  - "an unsaved new location asks before leaving by the rail": use a `GuardedLink` in the
    harness.
  - The zones test: receiving has `width: "full"`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - The chooser and the register forms are the list column.
  - `StockBucket` is the pane, with `backLabel="Back to receiving"`.
  - `MovementForm`, `LocationForm` and `BatchForm` report through `formState`.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): receiving shows the chosen lot beside the chooser`.

### Task 12: e2e, gates and the PR for slice 2

- [ ] **Step 1: Write `e2e/tests/pharmacy-stock-pane.spec.ts`.**
  - At 1366 (expanded) and 1920, a position opens beside the register, and the row moves by
    2px or less.
  - Next works, a reload keeps it, and phone Back returns.
  - Receiving: choose a lot, reload, and expect it still open.
- [ ] **Step 2: Run the e2e tests, then all the gates.** Expected: green.
- [ ] **Step 3:** Do the final review, open the PR with `Part of #1303`, and merge on green.

## Slice 3: Expiry & safety

Branch: `feat/1303-pane-safety`.

### Task 13: `GET /stock/batches/:id`

**Files:**

- Modify: `backend/internal/modules/pharmacy/inventory.go`, adding
  `s.GET("/batches/:id", PermStockRead, h.getBatch)` beside the PUT
- Test: the inventory Go test file
- Regenerate: `go run ./cmd/contractdocs`

- [ ] **Step 1: Write the failing tests.**
  - "it returns the batch".
  - "a batch of another outlet is 404".
  - "another tenant is 404".
  - "it is 403 without pharmacy.stock.read".
  - "a malformed id is 400". Match the product read; check `getProduct` for its malformed-id
    answer and use the same.
- [ ] **Step 2: Run them.** Expected: FAIL with 404 route-not-found.
- [ ] **Step 3: Implement it on the model of `getProduct`.** Use the same transaction,
      `batchColumns`, and `respond.OK` / `respond.Error`.
- [ ] **Step 4: Run them.** Then run `go run ./cmd/contractdocs` and `-check`, and
      `make lint-go`. Expected: PASS, with the docs diff committed.
- [ ] **Step 5: Commit**: `feat(pharmacy): read one batch`.

### Task 14: Batches and concerns open the batch in the pane

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/batch-safety.tsx`
  - `packages/ui/src/zones.ts`
- Test: `apps/shell/components/pharmacy/batch-safety.test.tsx`

**Interfaces:**

- By batch:
  - the rows are the loaded batches (100 a page, with Load more);
  - the row text is the product (SKU and brand, or `Product {id}`), the lot and the expiry;
  - the Open button reads `Open lot {lot}`.
- Last 30 days:
  - each concern row gets an "Open batch" button, labelled
    `Open batch for {kind} recorded {time}`. The concern carries `batch_id` but not the lot,
    and the pane's batch read supplies the lot for its title;
  - the rows for `useListDetail` are the distinct `batch_id`s in list order.
- The pane fetches `GET /stock/batches/:id`, then the incidents. A 404 shows the not-found
  copy.
- `ConcernForm` reports through `formState("concern")`.

- [ ] **Step 1: Write the failing tests.**
  - "By batch: `?open=<batch>` reopens its history and form".
  - "a 404 batch shows the not-found message".
  - "Last 30 days: two concerns on one lot are one Next stop".
  - "an unsaved concern asks before Next".
  - "changing tab clears open".
  - "read-only access shows history without the form".
  - The zones test: safety has `width: "full"`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Replace `SafetyBatchPicker`'s dropdown with the list.
  - `SelectedBatchSafety` becomes the pane body under `DetailHeader` (`noun="lot"`,
    `closeLabel="Close lot"`), with `backLabel="Back to safety"`.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): safety opens a batch beside its list`.

### Task 15: e2e, gates and the PR for slice 3

- [ ] **Step 1: Write `e2e/tests/pharmacy-safety-pane.spec.ts`.**
  - Seed a lot, record a concern on it, and open it from Last 30 days.
  - Next skips the second concern on the same lot.
  - A reload keeps it, and phone Back returns.
- [ ] **Step 2: Run the e2e tests, then all the gates.** Then do the final review, open the
      PR, and merge on green.

## Slice 4: Outlets & access

Branch: `feat/1303-pane-outlets`.

### Task 16: `GET /pharmacy/pharmacies/:pharmacy/outlets/:outlet`

**Files:**

- Modify: `backend/internal/modules/pharmacy/registry.go`, beside `listOutlets`, with the
  same permission (`PermCatalogueRead`) and the same visibility rule as the list
- Test: the registry Go test file

- [ ] **Step 1: Write the failing tests.**
  - It returns the outlet.
  - An outlet of another pharmacy is 404.
  - Another tenant is 404.
  - It is 403 without the permission.
  - Visibility matches the list: an outlet the list hides is 404 here. Read `listOutlets`'
    filter and reuse it, never re-derive it.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement, then run contractdocs and `-check`.**
- [ ] **Step 4: Run them, then `make lint-go`.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): read one outlet`.

### Task 17: Outlets & access lists outlets and opens one in the pane

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/pharmacy-workspace.tsx`, for the setup view
  - `apps/shell/components/pharmacy/pharmacy-registry-setup.tsx`
  - `apps/shell/components/pharmacy/tax-registration-panel.tsx`, adding `onStateChange`
  - `packages/ui/src/zones.ts`
- Test:
  - `pharmacy-workspace.test.tsx`
  - `tax-registration-panel.test.tsx`

**Interfaces:**

- On `view === "setup"`:
  - the heading shows only the Pharmacy picker;
  - the list column is the outlets list (Open `{code}`), then Register pharmacy and Hospital
    affiliations.
- `?open=<outlet id>` is the opened outlet. It is a different parameter from `outlet`, the
  working outlet, which opening never changes (spec E6).
- The pane fetches `GET …/outlets/:open`, then shows:
  - the read-only details;
  - an `OutletAccess` block, which is today's access check plus `AccessSetup` at that outlet's
    scope;
  - `TaxRegistrationPanel` at that outlet's scope.
- `?new=1` shows "Register outlet" (`OutletForm`) in the pane.
- The guard keys are `"access"`, `"tax"` and `"outlet"`.

- [ ] **Step 1: Write the failing tests.**
  - "setup lists outlets; opening one shows its details, access and tax registration".
  - "opening an outlet does not change the working outlet": `sessionStorage` and the URL
    `outlet` are unchanged.
  - "`?new=1` shows Register outlet; saving opens the new outlet".
  - "an unsaved tax registration asks before Next outlet".
  - "a 404 outlet shows the not-found message".
  - "the setup heading has no Outlet picker".
  - The zones test: setup has `width: "full"`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.** Use `backLabel="Back to outlets"`, `noun="outlet"` and
      `closeLabel="Close outlet"`.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit**: `feat(pharmacy): outlets open beside the outlet list in setup`.

### Task 18: e2e, gates and the closing PR

- [ ] **Step 1: Write `e2e/tests/pharmacy-outlets-pane.spec.ts`.**
  - It covers the split at 1366 and 1920, a reload, and phone Back.
  - Granting my access in the pane shows "Pharmacy access granted".
- [ ] **Step 2: Run the e2e tests, then all the gates.**
- [ ] **Step 3:** Do the final review, then open the PR with `Closes #1303`, and merge on
      green.
