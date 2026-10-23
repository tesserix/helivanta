# Detail pane and full-width lists, slice 1: Supplier bills

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Supplier bills opens a bill beside the list where the content area is at least 860px,
and as a full page below that. The open bill is in the URL, unsaved edits are never lost
silently, and the page uses the full width.

**Architecture:**

- **Shell:** a page declares `width: "full"` in `zones.ts`. The shell drops the `max-w-6xl` cap
  for that page and makes `main` a size container. A navigation guard in the shell lets a dirty
  detail stop in-app links and phone Back.
- **Shared parts:**
  - A `ListDetail` layout in `@helivanta/ui` switches between split and full page with a
    container query.
  - A `useDetail` hook in the shell keeps `open` and `new` in the URL beside the paging state.
- **Supplier bills** is rebuilt on these parts.

**Tech Stack:** Next.js 16 App Router, React 19, Tailwind v4 (`@container`), react-hook-form
via `useZodForm`, Vitest with `renderWithProviders`, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-detail-pane-design.md` (slice 1).

## Global Constraints

- **Layout:**
  - Split applies when the `main` content box is at least 860px. Below that, full page.
  - The pane is 400px wide, or 600px when the content box is at least 1400px.
  - The breakpoint is a container query, never a viewport breakpoint.
  - A full page fills the content area up to 1760px, left-aligned.
  - Prose is at most `70ch`. Forms are at most `640px` and two columns.
- **URL:**
  - The keys are `open=<uuid>` and `new=1`, beside `tab`, `page` and `cursor`.
  - Opening from closed pushes a history entry. Switching rows replaces it.
  - Changing tab, filter or page clears `open` and `new`, except the list's own step-back,
    which keeps them.
- **Unsaved edits:**
  - Dirty plus row switch, Previous / Next, close, tab, filter, page, rail or menu link, or
    Back shows `ConfirmDialog` "Discard changes to {name}?" with **Discard** and
    **Keep editing**. Reload triggers `beforeunload`.
  - Switching is disabled while a save is pending.
  - No autosave.
- **Focus:**
  - Viewing focuses the pane heading. Creating focuses the first field.
  - Escape closes, unless a select or combobox is open, and focus returns to the row's Open
    button, or to the list heading.
  - The pane is a labelled `region`. The open row has `aria-current="true"`.
- **Previous / Next:**
  - They follow the page's rows as snapshotted when the record was opened, held in memory.
  - They are disabled with no snapshot, and at the page's ends.
- **Frontend standards** (docs/standards/frontend.md):
  - `next/link` for links; `ConfirmDialog` only for destructive confirmation;
  - design tokens only; no native `<select>`;
  - every changed component has a Vitest test with `renderWithProviders`.
- **Git:** no AI attribution in commits or the PR.
- **Gates**, run one at a time:
  - `pnpm turbo lint type-check test build format:check`, then `make lint-shell`;
  - full e2e on a production build;
  - never rebuild the shell while it is serving.

## Review Focus

1. **A row leaves the list under the open pane.** A payment drops a bill out of Overdue. The
   pane stays open, and Next goes to the bill that was below it. Pinned in Task 5.
2. **Back on a phone with unsaved edits.** `popstate` cannot be cancelled. The guard re-pushes
   the detail's entry, then asks. Discard goes back. Pinned in Task 2.
3. **The content box changes while a bill is open**, for example when the rail expands. The
   layout flips between split and full page without losing the open bill or the form's state.
   Pinned in Task 4: the detail's subtree is the same element in both layouts, with only
   classes changing.
4. **`?open=` names another outlet's or tenant's bill.** The server returns 404. The pane shows
   the not-found message, and fetches nothing else for it. Pinned in Task 5.
5. **Width declared on a page with a page menu**, for example Supplier bills at 1366 with the
   rail expanded. The cap is dropped only on that page. Other pharmacy pages keep
   `max-w-6xl`. Pinned in Task 1.

---

### Task 1: Page width in the shell

**Files:**

- Modify: `packages/ui/src/zones.ts` (`ZonePage.width?: "full"`; set it on the Supplier bills
  page), `packages/ui/src/hms-shell.tsx:816-819`
- Test: `packages/ui/src/zones.test.ts`, `packages/ui/src/hms-shell.test.tsx`

**Interfaces:**

- Produces:
  - `ZonePage.width?: "full"`.
  - The shell's `main` carries `@container/main`, and the content wrapper carries
    `data-width="full"` or `data-width="capped"`.
  - Capped means `max-w-6xl`. Full means `max-w-[1760px]`.

- [ ] **Step 1: Write the failing tests:**
  - **zones:** only `/pharmacy/accounts` declares `width: "full"`.
  - **hms-shell:**
    - At `/pharmacy/accounts`, the content wrapper has `data-width="full"`, with no
      `max-w-6xl`.
    - At `/pharmacy/catalogue`, it is `capped`, with `max-w-6xl`.
    - `main` has the `@container/main` class.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test -- zones hms-shell`. Expected: FAIL.
- [ ] **Step 3:** Implement. The shell reads `currentPage?.width`.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(ui): a page can use the full width (#1303)`

### Task 2: Navigation guard

**Files:**

- Create: `packages/ui/src/navigation-guard.tsx`, `packages/ui/src/navigation-guard.test.tsx`
- Modify:
  - `packages/ui/src/hms-shell.tsx`: wrap the children in `NavigationGuardProvider`, and guard
    the rail, page-menu and phone-menu `Link`s.
  - `packages/ui/src/index.ts`

**Interfaces:**

- Produces:

  ```ts
  // While `dirty`, in-app links, Back and reload ask before leaving. `name`
  // fills "Discard changes to {name}?".
  export function useLeaveGuard(
    dirty: boolean,
    name: string,
  ): {
    // Run `go` now if clean; otherwise ask, and run it only on Discard.
    confirm: (go: () => void) => void;
  };
  export function NavigationGuardProvider({
    children,
  }: {
    children: ReactNode;
  }): JSX.Element;
  // For shell links: prevents the default navigation and asks while a guard is dirty.
  export function useGuardedLinkClick(): (
    event: React.MouseEvent<HTMLAnchorElement>,
    href: string,
  ) => void;
  ```
  - One `ConfirmDialog` is rendered by the provider.
  - Back is handled with `popstate`: when dirty, `history.pushState` restores the entry, the
    dialog opens, and Discard calls `history.back()`.
  - Reload is handled with `beforeunload` while dirty.

- [ ] **Step 1: Write the failing tests:**
  - When clean, `confirm(go)` calls `go` at once.
  - When dirty, it shows "Discard changes to TEST bill?". **Keep editing** does not call `go`.
    **Discard** does.
  - A guarded `Link` click while dirty prevents navigation and asks.
  - A `popstate` while dirty re-pushes the entry and asks.
  - A `beforeunload` listener is added only while dirty.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test -- navigation-guard`. Expected: FAIL.
- [ ] **Step 3:** Implement, and guard the shell's three link sets.
- [ ] **Step 4: Run** `pnpm --filter @helivanta/ui test`. Expected: PASS, with the hms-shell
      tests unchanged.
- [ ] **Step 5: Commit** `feat(ui): unsaved edits guard in-app navigation and Back (#1303)`

### Task 3: The open record in the URL

**Files:**

- Modify: `apps/shell/lib/use-url-page.ts`
- Create: `apps/shell/lib/use-detail.ts`, `apps/shell/lib/use-detail.test.tsx`
- Test: `apps/shell/lib/use-url-page.test.tsx`

**Interfaces:**

- Consumes: `useUrlPage`, `useStepBackWhenEmpty`.
- Produces:

  ```ts
  export function useDetail(): {
    openId: string | null; // a UUID from ?open=, else null
    creating: boolean; // ?new=1
    openHref: (id: string) => string; // keeps tab/filters/page/cursor
    open: (id: string) => void; // push when closed, replace when one is open
    create: () => void; // push ?new=1
    close: () => void; // replace, dropping open/new
  };
  ```
  - `useUrlPage(...).hrefFor` drops `open` and `new`.
  - `useStepBackWhenEmpty` keeps them in its target.

- [ ] **Step 1: Write the failing tests:**
  - `?open=` that is not a UUID gives `openId` null.
  - `open` from closed calls `router.push`, and from open calls `router.replace`.
  - `close` keeps `tab` and `page`.
  - `hrefFor` drops `open`.
  - Step-back keeps `open`.
- [ ] **Step 2: Run** `pnpm --filter shell test -- use-detail use-url-page`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4: Run** the same command. Expected: PASS. Run `pnpm --filter shell test` too:
      `DispenseList` must be unchanged.
- [ ] **Step 5: Commit** `feat(shell): the open record lives in the URL (#1303)`

### Task 4: ListDetail layout

**Files:**

- Create: `packages/ui/src/list-detail.tsx`, `packages/ui/src/list-detail.test.tsx`
- Modify: `packages/ui/src/index.ts`

**Interfaces:**

- Produces:

  ```ts
  // The list and an open record: side by side when main is at least 860px,
  // the record alone below that. `detail` null means nothing is open.
  export function ListDetail(props: {
    list: ReactNode;
    detail: ReactNode | null;
    label: string; // the region's accessible name, e.g. "Bill TEST-001"
    backHref: string; // "Back to …" on the full page
    backLabel: string;
  }): JSX.Element;
  ```
  - One grid at all widths, so that the detail is the same element in both layouts:
    - `@[860px]/main:grid-cols-[minmax(0,1fr)_400px]`;
    - `@[1400px]/main:grid-cols-[minmax(0,1fr)_600px]`.
  - Below 860px with a detail open, the list is `hidden` and the back link shows.
  - The pane is `sticky top-0` with `max-h-[calc(100dvh-var(--hms-header))]` (or the shell's
    existing header measure) and `overflow-y-auto`.
  - It is a `section` with `aria-label={label}` (role `region`).

- [ ] **Step 1: Write the failing tests:**
  - With no detail, there is no region and no back link.
  - With a detail, the region named `label` exists, and so does the back link with
    `backHref`.
  - The detail's DOM node stays the same object across re-renders that change only the width
    classes (focus 3).
  - The container classes are present.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test -- list-detail`. Expected: FAIL.
- [ ] **Step 3:** Implement with design tokens only.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(ui): list and detail side by side, or the detail alone (#1303)`

### Task 5: Supplier bills on the detail pane

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/accounts-panel.tsx`
  - `apps/shell/components/pharmacy/payable-forms.tsx`: `PayableForm`, `PayableHistory` and
    `SettlementForm` report `isDirty` and `isSubmitting` through an `onDirtyChange` prop.
- Test: `apps/shell/components/pharmacy/accounts-panel.test.tsx`

**Interfaces:**

- Consumes: `useDetail`, `useLeaveGuard`, `ListDetail`, `useUrlPage`.
- Produces: no new exports.
- **Removed:** the `selected` and `creating` state and `reveal()`.
- **The open bill is `openId`.**
  - When the URL holds `new=1`, the pane shows `PayableForm`, titled "New supplier bill".
  - On save, call `open(newId)`.
- **Snapshot:** on the click that opens a row, store the current page's row ids, in a ref keyed
  by `tab` plus the page. Previous / Next walk that array.
- **Compact rows** while open: document, payee, due date, outstanding. The row's Open button is
  focusable and is the focus return target.
- **Focus:**
  - When the open bill changes, focus the pane heading, "Bill {document_reference}".
  - When creating, focus the first field.
  - Escape on the pane closes, unless the event's target is inside an open select or
    combobox.
- **Leaving:** every row switch, close, Previous / Next and tab change goes through
  `guard.confirm`.

- [ ] **Step 1: Write the failing tests**, using `nav.search`:
  - `?tab=all&open=<id>` renders the pane with the region "Bill TEST-EXPENSE-001" and the
    heading focused.
  - A 404 detail shows "This record was not found, or you do not have access", with no other
    request for that id.
  - Clicking View on a row pushes `?tab=all&open=<id>`. Switching to another row replaces it.
  - Previous / Next follow the snapshot. They are disabled at the ends, and disabled when the
    pane opened from the URL with no snapshot.
  - **Focus 1:** after the list refetches without the open bill, the pane still shows it, and
    Next goes to the row that was below it.
  - **Dirty:**
    - With a settlement amount typed, clicking another row shows
      "Discard changes to TEST-EXPENSE-001?".
    - **Keep editing** keeps the typed amount, and the URL is unchanged.
  - **Create:**
    - `?new=1` shows "New supplier bill", and the first field (payee name) has focus.
    - Save calls `open` with the new id.
  - **Escape:** closes, and focus returns to that row's View button.
  - A tab link drops `open`.
- [ ] **Step 2: Run** `pnpm --filter shell test -- accounts-panel`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4: Run** `pnpm --filter shell test`. Expected: PASS. The existing accounts tests
      keep their claims; "focuses the expense form" moves to `?new=1`.
- [ ] **Step 5: Commit** `feat(pharmacy): supplier bills open beside the list (#1303)`

### Task 6: e2e, the threshold and gates

**Files:**

- Create: `e2e/tests/pharmacy-detail-pane.spec.ts`
- Modify:
  - the spec's "Confirmed while building" line, with the measured content widths;
  - `docs/standards/frontend.md` §7: one paragraph on `width: "full"`, `ListDetail` and
    `useLeaveGuard`.

- [ ] **Step 1: Write the e2e** on a fresh outlet with 3 bills, as the overview spec seeds:
  - Viewports 1366×768 (rail collapsed, then expanded), 1440×900 and 1920×1080:
    - opening a bill shows the region beside the list;
    - the list's first row keeps its on-screen position;
    - Next opens the second bill.
  - Record the measured `main` content width at each viewport in the test output, and assert
    at least 860 for every laptop case.
  - At 390×844: opening a bill hides the list, and phone Back returns to the list with the row
    visible.
  - A reload keeps the open bill.
  - With a payment amount typed, clicking the rail's Overview link asks. **Keep editing**
    stays on the page.
- [ ] **Step 2: Rebuild** the shell (stop it first), restart the API, and run the full suite
      with `npx playwright test --project=specs --reporter=line --workers=2`. Expected: all pass.
- [ ] **Step 3:** Update the spec line and frontend.md, then run every gate in the Global
      Constraints, one at a time. Expected: all green.
- [ ] **Step 4: Commit** `test(pharmacy): detail pane end to end (#1303)`
